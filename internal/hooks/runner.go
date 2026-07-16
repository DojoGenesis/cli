// Package hooks runs hook rules from loaded plugins.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/plugins"
)

// Event names that dojo-cli fires.
const (
	EventPreCommand  = "PreCommand"
	EventPostCommand = "PostCommand"
	EventPostSkill   = "PostSkill"
	EventPostAgent   = "PostAgent"
	EventSessionEnd  = "SessionEnd"
)

// Runner executes hook rules for a given event.
type Runner struct {
	plugins []plugins.Plugin
}

// New creates a Runner from a list of loaded plugins.
func New(ps []plugins.Plugin) *Runner {
	return &Runner{plugins: ps}
}

// Fire runs all hook rules matching the given event name across all plugins.
// Supported hook types: command, prompt, agent, http.
// Async hooks are run in a goroutine. Sync hooks block until completion.
// ctx cancellation prevents new async hooks from starting and kills
// already-running processes (exec.CommandContext sends SIGKILL).
func (r *Runner) Fire(ctx context.Context, event string, payload map[string]any) error {
	for _, p := range r.plugins {
		for _, rule := range p.HookRules {
			if !strings.EqualFold(rule.Event, event) {
				continue
			}

			// Evaluate matcher: glob against the command name in the payload.
			if !matcherMatches(rule.Matcher, payload) {
				continue
			}

			// Evaluate "if" condition.
			if !conditionTrue(rule.If) {
				continue
			}

			for _, h := range rule.Hooks {
				pluginName := p.Name
				pluginPath := p.Path
				isAsync := h.Async

				if isAsync {
					hCopy := h
					go func() {
						select {
						case <-ctx.Done():
							return
						default:
						}
						if err := runHook(ctx, hCopy, pluginPath, payload); err != nil {
							log.Printf("[hooks] async hook error (%s/%s): %v", pluginName, event, err)
						}
					}()
				} else {
					if err := runHook(ctx, h, pluginPath, payload); err != nil {
						return fmt.Errorf("hook error (%s/%s): %w", pluginName, event, err)
					}
				}
			}
		}
	}
	return nil
}

// matcherMatches returns true if the matcher glob matches the command in the payload,
// or if the matcher is empty / "*" (match everything).
// The leading "/" is stripped from the command before matching, so that a matcher
// like "garden*" matches both "/garden ls" and "garden ls".
func matcherMatches(matcher string, payload map[string]any) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	if payload == nil {
		return false
	}
	cmd, _ := payload["command"].(string)
	if cmd == "" {
		return false
	}
	// Strip leading slash so matchers like "garden*" work against "/garden ls".
	cmd = strings.TrimPrefix(cmd, "/")
	// path.Match operates on the full string; split on space to get just the name.
	if idx := strings.IndexByte(cmd, ' '); idx >= 0 {
		cmd = cmd[:idx]
	}
	matched, err := path.Match(matcher, cmd)
	if err != nil {
		// A malformed glob (e.g. an unbalanced "[") used to fail silently
		// here and the hook would just never fire, with no signal to the
		// plugin author about why. Surface it instead — still treated as
		// "no match" so behavior otherwise stays the same, it's just no
		// longer silent.
		log.Printf("[hooks] invalid matcher %q: %v", matcher, err)
		return false
	}
	return matched
}

// conditionTrue evaluates the "if" field.
// "" or "true" → always true
// "false" → always false
// anything else → treat as env var name; true if set and non-empty.
func conditionTrue(cond string) bool {
	switch cond {
	case "", "true":
		return true
	case "false":
		return false
	default:
		return os.Getenv(cond) != ""
	}
}

// runHook dispatches to the appropriate executor based on hook type.
func runHook(ctx context.Context, h plugins.HookDef, pluginRoot string, payload map[string]any) error {
	switch h.Type {
	case "command":
		return runCommand(ctx, h.Command, pluginRoot)
	case "prompt":
		fmt.Printf("[hook:prompt] %s\n", h.Prompt)
		return nil
	case "agent":
		// Use Command as agent ID/description if Prompt is empty (fallback).
		desc := h.Prompt
		if desc == "" {
			desc = h.Command
		}
		fmt.Printf("[hook:agent] %s\n", desc)
		return nil
	case "http":
		return runHTTPHook(ctx, h.URL, payload)
	default:
		log.Printf("[hooks] unknown hook type %q — skipping", h.Type)
		return nil
	}
}

// runHTTPHook POSTs the payload as JSON to the given URL.
// HTTP errors are logged but do not fail the command.
func runHTTPHook(ctx context.Context, url string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[hooks] http hook: failed to marshal payload: %v", err)
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[hooks] http hook: failed to build request: %v", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[hooks] http hook: request error: %v", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	log.Printf("[hooks] http hook: response status %s", resp.Status)
	return nil
}

// shellFor returns the shell executable and its "run this string" flag for
// the given GOOS. Stock Windows has no `sh` on PATH, so hook commands there
// need `cmd /C`; every other supported platform (darwin, linux, ...) uses
// the POSIX `sh -c`. Taking goos as a parameter — instead of reading
// runtime.GOOS inline — keeps the branch a pure, table-testable function.
func shellFor(goos string) (exe string, flag string) {
	if goos == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}

// runCommand executes a shell command string with CLAUDE_PLUGIN_ROOT set to
// the plugin's directory. The command is run through sh -c (cmd /C on
// Windows — see shellFor) so it can use shell expansion (e.g. variable
// substitution, quoting).
func runCommand(ctx context.Context, command, pluginRoot string) error {
	exe, flag := shellFor(runtime.GOOS)
	cmd := exec.CommandContext(ctx, exe, flag, command)
	cmd.Env = append(cmd.Environ(),
		"CLAUDE_PLUGIN_ROOT="+pluginRoot,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}
