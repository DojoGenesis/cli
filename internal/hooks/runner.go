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
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DojoGenesis/cli/internal/plugins"
)

// Event names that dojo-cli fires.
const (
	EventPreCommand       = "PreCommand"
	EventPostCommand      = "PostCommand"
	EventPostSkill        = "PostSkill"
	EventPostAgent        = "PostAgent"
	EventSessionStart     = "SessionStart"
	EventSessionEnd       = "SessionEnd"
	EventUserPromptSubmit = "UserPromptSubmit"
)

// Runner executes hook rules for a given event.
type Runner struct {
	plugins []plugins.Plugin

	// warned de-dupes the "hook type X is not implemented" warning for the
	// unimplemented prompt/agent types: keys are "<plugin>\x00<type>", so the
	// warning fires at most once per (plugin, type) for the life of this
	// Runner rather than on every fire. The REPL builds exactly one Runner per
	// process (commands.Registry.Runner), so per-Runner is per-process in
	// practice. Zero value is ready to use; sync.Map so the async goroutine
	// path can warn concurrently.
	warned sync.Map
}

// New creates a Runner from a list of loaded plugins.
func New(ps []plugins.Plugin) *Runner {
	return &Runner{plugins: ps}
}

// FireResult is the classified outcome of FireChecked. The zero value means
// every matched hook ran cleanly (or none matched). Exactly one of the failure
// channels is ever set, and Blocked takes priority: FireChecked short-circuits
// on the first command-hook failure and reports it as Blocked when its rule is
// marked Blocking, or as a non-blocking Err otherwise.
type FireResult struct {
	// Blocked is true when a command hook in a Blocking:true rule failed or
	// exited non-zero — the caller MUST abort the action it was about to take.
	// Plugin, Event, and Reason name the offending hook for the abort message.
	Blocked bool
	Plugin  string // plugin that owns the blocking hook (set when Blocked)
	Event   string // event the blocking hook fired on (set when Blocked)
	Reason  string // failure detail — the hook's error/stderr (set when Blocked)

	// Err is a NON-blocking hook failure: the pre-Blocking behavior in which a
	// sync command hook failed. The caller logs it and continues — it does NOT
	// abort. Nil unless a non-blocking hook failed. Blocked and Err are never
	// both set (Blocked short-circuits first).
	Err error
}

// Message renders the standard one-line "blocked by <plugin>/<event>: <reason>"
// string the REPL prints when aborting. The reason is flattened to a single
// line (a hook can print multi-line stderr) and length-capped so a wall of
// output never floods the prompt.
func (fr FireResult) Message() string {
	reason := strings.Join(strings.Fields(fr.Reason), " ")
	const maxReason = 200
	if utf8.RuneCountInString(reason) > maxReason {
		reason = string([]rune(reason)[:maxReason]) + "…"
	}
	return fmt.Sprintf("[hooks] blocked by %s/%s: %s", fr.Plugin, fr.Event, reason)
}

// Fire runs all hook rules matching the given event name across all plugins and
// collapses the outcome into a single error, preserving the pre-Blocking
// contract for callers that only care whether "something failed" (e.g.
// /hooks fire in internal/commands, and the SessionStart/SessionEnd/PostCommand
// call sites that log-and-continue). A blocking failure and a non-blocking
// failure both surface here as a non-nil error; callers that must actually
// honor a block (abort the caller's action) use FireChecked instead.
func (r *Runner) Fire(ctx context.Context, event string, payload map[string]any) error {
	res := r.FireChecked(ctx, event, payload)
	if res.Blocked {
		return fmt.Errorf("hook error (%s/%s): %s", res.Plugin, res.Event, strings.TrimSpace(res.Reason))
	}
	return res.Err
}

// FireChecked runs all hook rules matching the given event name across all
// plugins and returns a classified FireResult distinguishing (a) all-clean,
// (b) a non-blocking failure to log-and-continue, and (c) a Blocking-rule
// command hook that failed and must abort the caller.
//
// Supported hook types: command, prompt, agent, http. Async hooks run in a
// goroutine (errors logged, never block). Sync hooks run to completion.
// ctx cancellation prevents new async hooks from starting and kills
// already-running processes (exec.CommandContext sends SIGKILL).
//
// Blocking is command-only and synchronous: a command hook in a Blocking:true
// rule runs to completion (ignoring its Async flag — you cannot block on a
// fire-and-forget hook) and a non-zero exit returns a Blocked result. An http
// hook is fire-and-forget by design (runHTTPHook always succeeds, logging any
// error) and prompt/agent are unimplemented no-ops, so none of those can ever
// block — even inside a Blocking rule. Like the pre-Blocking Fire(), the first
// command-hook failure short-circuits the remaining rules and hooks.
func (r *Runner) FireChecked(ctx context.Context, event string, payload map[string]any) FireResult {
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

				// Blocking gate: a command hook in a Blocking rule runs
				// synchronously and a failure vetoes the caller. Only command
				// hooks reach this branch (see doc comment).
				if rule.Blocking && h.Type == "command" {
					if err := r.runHook(ctx, h, pluginName, pluginPath, payload); err != nil {
						return FireResult{
							Blocked: true,
							Plugin:  pluginName,
							Event:   event,
							Reason:  err.Error(),
						}
					}
					continue
				}

				if h.Async {
					hCopy := h
					go func() {
						select {
						case <-ctx.Done():
							return
						default:
						}
						if err := r.runHook(ctx, hCopy, pluginName, pluginPath, payload); err != nil {
							log.Printf("[hooks] async hook error (%s/%s): %v", pluginName, event, err)
						}
					}()
					continue
				}

				// Sync, non-blocking: the pre-Blocking behavior — a failure is
				// reported to the caller, which logs it and continues (the
				// command still runs). Short-circuits the rest, as before.
				if err := r.runHook(ctx, h, pluginName, pluginPath, payload); err != nil {
					return FireResult{Err: fmt.Errorf("hook error (%s/%s): %w", pluginName, event, err)}
				}
			}
		}
	}
	return FireResult{}
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
func (r *Runner) runHook(ctx context.Context, h plugins.HookDef, pluginName, pluginRoot string, payload map[string]any) error {
	switch h.Type {
	case "command":
		return runCommand(ctx, h.Command, pluginRoot, promptFromPayload(payload))
	case "prompt", "agent":
		// prompt/agent dispatch is not implemented. These used to print a
		// "[hook:prompt] …" / "[hook:agent] …" label to stdout that looked like
		// the hook had done something — a silent no-op wearing a success mask.
		// Warn honestly instead, once per (plugin, type) so a hook that fires
		// every turn doesn't spam the stream.
		r.warnUnimplemented(pluginName, h.Type)
		return nil
	case "http":
		return runHTTPHook(ctx, h.URL, payload)
	default:
		log.Printf("[hooks] unknown hook type %q — skipping", h.Type)
		return nil
	}
}

// warnUnimplemented prints, at most once per (plugin, hook-type) for this
// Runner, a stderr warning that a hook type is not implemented and is being
// skipped. Concurrency-safe: the async path calls runHook from a goroutine.
func (r *Runner) warnUnimplemented(pluginName, hookType string) {
	key := pluginName + "\x00" + hookType
	if _, loaded := r.warned.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr, "[hooks] hook type %q is not implemented; skipping (plugin %s)\n", hookType, pluginName)
}

// promptFromPayload extracts the user prompt an event carries under the
// "prompt" key. Only UserPromptSubmit sets it (see the REPL chat path); every
// other event's payload lacks the key, so this returns "" and DOJO_PROMPT is
// left unset for those hooks.
func promptFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	s, _ := payload["prompt"].(string)
	return s
}

// maxPromptBytes caps the DOJO_PROMPT environment value handed to
// UserPromptSubmit command hooks, so an arbitrarily long chat message can't
// bloat the child process's environment block.
const maxPromptBytes = 4096

// truncateBytes returns s clamped to at most maxBytes bytes, trimmed back to a
// UTF-8 rune boundary so a multi-byte rune is never split mid-sequence.
func truncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	truncated := s[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
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
//
// When prompt is non-empty (UserPromptSubmit hooks), the user's chat text is
// exposed to the hook as DOJO_PROMPT, truncated to maxPromptBytes. It is passed
// through the process environment exactly like CLAUDE_PLUGIN_ROOT and is NEVER
// interpolated into the command string, so a prompt containing shell
// metacharacters ($(...), backticks, ;, |, &) is inert — the hook reads it via
// $DOJO_PROMPT, it is never evaluated as part of the command.
func runCommand(ctx context.Context, command, pluginRoot, prompt string) error {
	exe, flag := shellFor(runtime.GOOS)
	cmd := exec.CommandContext(ctx, exe, flag, command)
	env := append(cmd.Environ(),
		"CLAUDE_PLUGIN_ROOT="+pluginRoot,
	)
	if prompt != "" {
		env = append(env, "DOJO_PROMPT="+truncateBytes(prompt, maxPromptBytes))
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}
