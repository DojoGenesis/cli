package hooks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DojoGenesis/cli/internal/plugins"
)

// ─── captureStderr ────────────────────────────────────────────────────────────

// captureStderr redirects os.Stderr for the duration of fn and returns whatever
// was written. Used to assert warnUnimplemented's fmt.Fprintf(os.Stderr, …).
// The log package holds its own os.Stderr reference captured at init, so log
// output is NOT redirected here — the capture stays clean of log noise.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = wr
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rd)
		done <- buf.String()
	}()
	fn()
	_ = wr.Close()
	os.Stderr = old
	out := <-done
	_ = rd.Close()
	return out
}

// ─── Blocking semantics ───────────────────────────────────────────────────────

// TestFireChecked_BlockingHookFailure_Blocks proves a command hook in a
// Blocking:true rule that exits non-zero returns a Blocked result naming the
// plugin+event, and that the Fire() wrapper still surfaces it as an error for
// the /hooks fire path in internal/commands.
func TestFireChecked_BlockingHookFailure_Blocks(t *testing.T) {
	ps := []plugins.Plugin{{
		Name: "gatekeeper",
		Path: t.TempDir(),
		HookRules: []plugins.HookRule{{
			Event:    EventPreCommand,
			Blocking: true,
			Hooks:    []plugins.HookDef{{Type: "command", Command: "echo denied >&2; exit 3"}},
		}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventPreCommand, map[string]any{"command": "/deploy"})
	if !res.Blocked {
		t.Fatalf("expected Blocked, got %+v", res)
	}
	if res.Plugin != "gatekeeper" {
		t.Errorf("Plugin = %q, want gatekeeper", res.Plugin)
	}
	if res.Event != EventPreCommand {
		t.Errorf("Event = %q, want %q", res.Event, EventPreCommand)
	}
	if res.Reason == "" {
		t.Error("Reason should carry the hook's failure detail")
	}
	if res.Err != nil {
		t.Errorf("Blocked result should not also set Err, got %v", res.Err)
	}
	if msg := res.Message(); !strings.Contains(msg, "blocked by gatekeeper/PreCommand") {
		t.Errorf("Message() = %q, want it to name plugin/event", msg)
	}

	// Backward compat: the Fire() wrapper collapses a block into an error so
	// /hooks fire (internal/commands/cmd_system.go) still reports it.
	if err := r.Fire(context.Background(), EventPreCommand, map[string]any{"command": "/deploy"}); err == nil {
		t.Error("Fire() should return an error when a blocking hook fails")
	}
}

// TestFireChecked_BlockingHookSucceeds_NotBlocked proves a Blocking rule whose
// command succeeds still runs and does not block.
func TestFireChecked_BlockingHookSucceeds_NotBlocked(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "ran.txt")
	ps := []plugins.Plugin{{
		Name:      "allow",
		Path:      tmp,
		HookRules: []plugins.HookRule{{Event: EventPreCommand, Blocking: true, Hooks: []plugins.HookDef{{Type: "command", Command: "touch " + marker}}}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventPreCommand, map[string]any{"command": "/ok"})
	if res.Blocked || res.Err != nil {
		t.Fatalf("a clean blocking hook should not fail: %+v", res)
	}
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Error("a blocking hook that succeeds should still have run")
	}
}

// TestFireChecked_NonBlockingCommandFailure_ContinuesNotBlocked proves a
// failing command hook whose rule is NOT Blocking surfaces as Err (log-and-
// continue), never as Blocked — the pre-Blocking behavior.
func TestFireChecked_NonBlockingCommandFailure_ContinuesNotBlocked(t *testing.T) {
	ps := []plugins.Plugin{{
		Name:      "noisy",
		Path:      t.TempDir(),
		HookRules: []plugins.HookRule{{Event: EventPreCommand, Blocking: false, Hooks: []plugins.HookDef{{Type: "command", Command: "exit 1"}}}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventPreCommand, map[string]any{"command": "/x"})
	if res.Blocked {
		t.Fatal("a non-blocking failure must NOT block")
	}
	if res.Err == nil {
		t.Fatal("a non-blocking failure should surface as Err (logged and continued)")
	}
}

// TestFireChecked_BlockingHTTPHook_CannotBlock proves only command hooks block:
// an http hook in a Blocking rule is still fire-and-forget even against a dead
// endpoint.
func TestFireChecked_BlockingHTTPHook_CannotBlock(t *testing.T) {
	ps := []plugins.Plugin{{
		Name:      "http-block",
		HookRules: []plugins.HookRule{{Event: EventPreCommand, Blocking: true, Hooks: []plugins.HookDef{{Type: "http", URL: "http://127.0.0.1:0"}}}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventPreCommand, map[string]any{"command": "/x"})
	if res.Blocked {
		t.Error("an http hook in a Blocking rule must not block (fire-and-forget by design)")
	}
}

// TestFireChecked_BlockingCommandHook_IgnoresAsyncAndBlocks proves a command
// hook in a Blocking rule runs synchronously even when marked Async:true — you
// cannot block on a fire-and-forget hook — so its failure still blocks.
func TestFireChecked_BlockingCommandHook_IgnoresAsyncAndBlocks(t *testing.T) {
	ps := []plugins.Plugin{{
		Name:      "async-but-blocking",
		Path:      t.TempDir(),
		HookRules: []plugins.HookRule{{Event: EventPreCommand, Blocking: true, Hooks: []plugins.HookDef{{Type: "command", Command: "exit 1", Async: true}}}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventPreCommand, map[string]any{"command": "/x"})
	if !res.Blocked {
		t.Error("a Blocking command hook must run synchronously and block on failure, even when Async:true")
	}
}

// ─── UserPromptSubmit: env delivery, non-interpolation, truncation ────────────

// TestFireChecked_UserPromptSubmit_DeliversDojoPromptEnv proves the prompt text
// reaches a command hook via $DOJO_PROMPT, and that matching on the literal
// "chat" works.
func TestFireChecked_UserPromptSubmit_DeliversDojoPromptEnv(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "prompt.txt")
	const prompt = "hello from the user"
	ps := []plugins.Plugin{{
		Name: "ups-plugin",
		Path: tmp,
		HookRules: []plugins.HookRule{{
			Event:   EventUserPromptSubmit,
			Matcher: "chat",
			Hooks:   []plugins.HookDef{{Type: "command", Command: `printf '%s' "$DOJO_PROMPT" > ` + marker}},
		}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventUserPromptSubmit, map[string]any{"command": "chat", "prompt": prompt})
	if res.Blocked || res.Err != nil {
		t.Fatalf("FireChecked returned failure: %+v", res)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	if string(got) != prompt {
		t.Errorf("DOJO_PROMPT delivered %q, want %q", got, prompt)
	}
}

// TestFireChecked_UserPromptSubmit_PromptNotShellInterpolated proves the prompt
// is passed through the environment, never spliced into the command string: a
// $(...) subshell in the prompt must NOT execute.
func TestFireChecked_UserPromptSubmit_PromptNotShellInterpolated(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "out.txt")
	pwned := filepath.Join(tmp, "pwned.txt")
	prompt := "safe $(touch " + pwned + ") text"
	ps := []plugins.Plugin{{
		Name: "safe-plugin",
		Path: tmp,
		HookRules: []plugins.HookRule{{
			Event:   EventUserPromptSubmit,
			Matcher: "chat",
			Hooks:   []plugins.HookDef{{Type: "command", Command: `printf '%s' "$DOJO_PROMPT" > ` + marker}},
		}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventUserPromptSubmit, map[string]any{"command": "chat", "prompt": prompt})
	if res.Blocked || res.Err != nil {
		t.Fatalf("unexpected failure: %+v", res)
	}
	if _, err := os.Stat(pwned); !os.IsNotExist(err) {
		t.Fatal("prompt was shell-interpolated: the $(...) subshell executed")
	}
	got, _ := os.ReadFile(marker)
	if string(got) != prompt {
		t.Errorf("DOJO_PROMPT = %q, want the literal prompt %q", got, prompt)
	}
}

// TestFireChecked_UserPromptSubmit_TruncatesDojoPrompt proves an oversized
// prompt is clamped to maxPromptBytes before it reaches the hook environment.
func TestFireChecked_UserPromptSubmit_TruncatesDojoPrompt(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "len.txt")
	big := strings.Repeat("a", maxPromptBytes+512)
	ps := []plugins.Plugin{{
		Name: "trunc-plugin",
		Path: tmp,
		HookRules: []plugins.HookRule{{
			Event:   EventUserPromptSubmit,
			Matcher: "chat",
			Hooks:   []plugins.HookDef{{Type: "command", Command: `printf '%s' "$DOJO_PROMPT" > ` + marker}},
		}},
	}}
	r := New(ps, nil)

	res := r.FireChecked(context.Background(), EventUserPromptSubmit, map[string]any{"command": "chat", "prompt": big})
	if res.Blocked || res.Err != nil {
		t.Fatalf("unexpected failure: %+v", res)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	if len(got) != maxPromptBytes {
		t.Errorf("DOJO_PROMPT length = %d, want %d (truncated)", len(got), maxPromptBytes)
	}
}

// ─── Honest no-op warnings (warn-once per plugin+type) ────────────────────────

// TestWarnUnimplemented_OncePerPluginType proves prompt/agent hooks emit a
// single stderr warning per (plugin, type) for the life of the Runner — never
// the old silent stdout label, and never spammed on repeated fires.
func TestWarnUnimplemented_OncePerPluginType(t *testing.T) {
	ps := []plugins.Plugin{{
		Name: "warn-plugin",
		HookRules: []plugins.HookRule{{
			Event: EventPreCommand,
			Hooks: []plugins.HookDef{
				{Type: "prompt", Prompt: "do X"},
				{Type: "prompt", Prompt: "do Y"}, // same (plugin, type) → still one warning
				{Type: "agent", Command: "agent-1"},
			},
		}},
	}}
	r := New(ps, nil)

	out := captureStderr(t, func() {
		_ = r.Fire(context.Background(), EventPreCommand, nil)
		_ = r.Fire(context.Background(), EventPreCommand, nil) // fire again — must NOT re-warn
	})

	if n := strings.Count(out, `hook type "prompt" is not implemented`); n != 1 {
		t.Errorf("prompt warning fired %d times, want exactly 1; stderr:\n%s", n, out)
	}
	if n := strings.Count(out, `hook type "agent" is not implemented`); n != 1 {
		t.Errorf("agent warning fired %d times, want exactly 1; stderr:\n%s", n, out)
	}
	if !strings.Contains(out, "(plugin warn-plugin)") {
		t.Errorf("warning should name the plugin; stderr:\n%s", out)
	}
}

// ─── truncateBytes ────────────────────────────────────────────────────────────

func TestTruncateBytes(t *testing.T) {
	if got := truncateBytes("hello", maxPromptBytes); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	if got := truncateBytes(strings.Repeat("a", maxPromptBytes+904), maxPromptBytes); len(got) != maxPromptBytes {
		t.Errorf("ascii truncation len = %d, want %d", len(got), maxPromptBytes)
	}
	// 3-byte runes: truncating 4096 bytes must land on a rune boundary. 1365*3 =
	// 4095 is the largest multiple of 3 <= 4096, so the split rune is dropped.
	euro := truncateBytes(strings.Repeat("€", 3000), maxPromptBytes)
	if len(euro) > maxPromptBytes {
		t.Errorf("multibyte truncation exceeded cap: %d", len(euro))
	}
	if !utf8.ValidString(euro) {
		t.Error("multibyte truncation split a rune (invalid UTF-8)")
	}
	if len(euro) != 4095 {
		t.Errorf("euro truncation len = %d, want 4095 (rune boundary)", len(euro))
	}
}
