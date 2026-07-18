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

	"github.com/DojoGenesis/cli/internal/client"
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

	// gw is the gateway client prompt/agent hooks dispatch through: ChatStream
	// for "prompt", CreateAgent + AgentChatStream for "agent" (see runHook,
	// runPromptHook, runAgentHook). May be nil — every production call site
	// (internal/commands.New) always passes a real client, but a bare Runner
	// built by hand (tests, or a hypothetical future no-gateway mode) is still
	// valid: runPromptHook/runAgentHook guard for nil and fall back to
	// warnUnimplemented instead of dereferencing it. Never mutated after New,
	// so it's safe to read from the async hook goroutine without a lock.
	gw *client.Client

	// warned de-dupes the "hook type X is not implemented" warning that fires
	// when a prompt/agent hook runs on a Runner with no gateway client wired
	// in (the nil-gw path above): keys are "<plugin>\x00<type>", so the
	// warning fires at most once per (plugin, type) for the life of this
	// Runner rather than on every fire. The REPL builds exactly one Runner per
	// process (commands.Registry.Runner), so per-Runner is per-process in
	// practice. Zero value is ready to use; sync.Map so the async goroutine
	// path can warn concurrently.
	warned sync.Map
}

// New creates a Runner from a list of loaded plugins and the gateway client
// that powers prompt/agent hook dispatch. gw may be nil — see the Runner.gw
// doc comment — in which case prompt/agent hooks fall back to the honest
// warnUnimplemented no-op instead of panicking on a nil client.
func New(ps []plugins.Plugin, gw *client.Client) *Runner {
	return &Runner{plugins: ps, gw: gw}
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
// error), and prompt/agent hooks are advisory-only BY DESIGN (runPromptHook /
// runAgentHook always return nil, whether the gateway call succeeds, fails,
// or the Runner has no gw wired in at all) — so none of those three types can
// ever block, even inside a Blocking rule. This is a locked decision, not a
// gap: an LLM/agent has no natural block signal, and defining one is a
// separate, larger design. Like the pre-Blocking Fire(), the first
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
	case "prompt":
		return r.runPromptHook(ctx, h, pluginName, payload)
	case "agent":
		return r.runAgentHook(ctx, h, pluginName, payload)
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

// runPromptHook implements the "prompt" hook type: it sends h.Prompt (after
// renderPrompt's minimal templating) to the gateway's /v1/chat endpoint via
// ChatStream and buffers the streamed response into a log line. Advisory-only
// per the locked design (see FireChecked's doc comment) — success, a gateway
// error, and an empty prompt all return nil here; only a "command" hook can
// ever veto the caller.
func (r *Runner) runPromptHook(ctx context.Context, h plugins.HookDef, pluginName string, payload map[string]any) error {
	if r.gw == nil {
		// No gateway wired into this Runner (e.g. a bare Runner built by hand
		// in a test) — fall back to the same honest no-op warning prompt/agent
		// hooks always got, rather than dereferencing a nil client.
		r.warnUnimplemented(pluginName, h.Type)
		return nil
	}

	promptText := renderPrompt(h.Prompt, payload)
	if promptText == "" {
		log.Printf("[hooks] prompt hook (plugin %s): empty prompt — skipping", pluginName)
		return nil
	}

	var out strings.Builder
	err := r.gw.ChatStream(ctx, client.ChatRequest{Message: promptText, Model: h.Model}, func(chunk client.SSEChunk) {
		out.WriteString(extractHookChunkText(chunk.Data))
	})
	if err != nil {
		// Advisory: log and move on, never surface as a caller-visible error.
		log.Printf("[hooks] prompt hook (plugin %s): gateway error: %v", pluginName, err)
		return nil
	}
	log.Printf("[hooks] prompt hook (plugin %s): %s", pluginName, truncateForLog(strings.TrimSpace(out.String())))
	return nil
}

// runAgentHook implements the "agent" hook type: it creates a scratch agent
// via CreateAgent, then sends h.Prompt to it via AgentChatStream, buffering
// the streamed response into a log line — same advisory contract as
// runPromptHook (never returns a non-nil error; a "command" hook is the only
// type that can veto the caller).
//
// h.Model is forward-compat only: CreateAgentRequest.Model and
// AgentChatRequest.Model exist on the request types but today's gateway (POST
// /v1/gateway/agents and POST /v1/gateway/agents/:id/chat) has no model field
// and silently ignores it — the same caveat internal/client/client.go already
// documents on ChatRequest.SystemPrompt. Passed through anyway so the day the
// gateway adds support, a hooks.json "model" key starts working with no
// dojo-cli change.
func (r *Runner) runAgentHook(ctx context.Context, h plugins.HookDef, pluginName string, payload map[string]any) error {
	if r.gw == nil {
		r.warnUnimplemented(pluginName, h.Type)
		return nil
	}

	promptText := renderPrompt(h.Prompt, payload)
	if promptText == "" {
		log.Printf("[hooks] agent hook (plugin %s): empty prompt — skipping", pluginName)
		return nil
	}

	agentResp, err := r.gw.CreateAgent(ctx, client.CreateAgentRequest{Model: h.Model})
	if err != nil {
		log.Printf("[hooks] agent hook (plugin %s): create-agent error: %v", pluginName, err)
		return nil
	}

	var out strings.Builder
	err = r.gw.AgentChatStream(ctx, agentResp.AgentID, client.AgentChatRequest{Message: promptText, Model: h.Model}, func(chunk client.SSEChunk) {
		out.WriteString(extractHookChunkText(chunk.Data))
	})
	if err != nil {
		log.Printf("[hooks] agent hook (plugin %s): gateway error: %v", pluginName, err)
		return nil
	}
	log.Printf("[hooks] agent hook (plugin %s): %s", pluginName, truncateForLog(strings.TrimSpace(out.String())))
	return nil
}

// renderPrompt returns h.Prompt with two minimal, documented placeholders
// substituted from payload: "{{.command}}" and "{{.prompt}}", matching the
// only two string fields payload ever actually carries (see
// matcherMatches/promptFromPayload — most events pass "command", only
// UserPromptSubmit also carries "prompt"). Deliberately not a text/template
// parse: plain substring substitution over a two-entry, hardcoded allowlist
// covers the whole real payload surface without opening up an arbitrary
// templating contract the data behind it can't back up. A placeholder with no
// matching payload field, or a nil payload, is left verbatim.
func renderPrompt(tmpl string, payload map[string]any) string {
	out := tmpl
	if payload != nil {
		if cmd, ok := payload["command"].(string); ok {
			out = strings.ReplaceAll(out, "{{.command}}", cmd)
		}
		if p, ok := payload["prompt"].(string); ok {
			out = strings.ReplaceAll(out, "{{.prompt}}", p)
		}
	}
	return out
}

// extractHookChunkText pulls the readable text out of one SSE chunk's data
// field for a prompt/agent hook's buffered response. Mirrors the extraction
// the interactive /agent and /run paths already use (agentExtractText in
// internal/commands/cmd_agent.go) — duplicated rather than imported, because
// internal/commands imports internal/hooks (commands.New calls hooks.New) and
// the reverse import would cycle. Gateway chunks are typically
// {"text|content|message|delta": "..."}; a chunk that isn't JSON, or has none
// of those keys, is passed through verbatim so no content is silently
// dropped.
func extractHookChunkText(data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		for _, key := range []string{"text", "content", "message", "delta"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}
	return data
}

// maxHookLogChars caps how much of a prompt/agent hook's buffered gateway
// response reaches the log. These hooks are advisory — logged for
// visibility, not archived — so an unbounded model response (a paragraph, an
// essay) would otherwise flood stderr every time a chatty hook fires.
const maxHookLogChars = 500

// truncateForLog clamps s to maxHookLogChars runes for a hook's log line,
// appending an ellipsis when it does. Rune-based (unlike truncateBytes, used
// for the DOJO_PROMPT env-size cap below) because this is for human/log
// readability, not a hard byte budget.
func truncateForLog(s string) string {
	if utf8.RuneCountInString(s) <= maxHookLogChars {
		return s
	}
	r := []rune(s)
	return string(r[:maxHookLogChars]) + "…"
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
