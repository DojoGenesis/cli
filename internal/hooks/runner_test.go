package hooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DojoGenesis/cli/internal/plugins"
)

// ─── New() safety ─────────────────────────────────────────────────────────────

func TestNew_NilPlugins_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New(nil) panicked: %v", r)
		}
	}()
	r := New(nil)
	if r == nil {
		t.Fatal("New(nil) returned nil runner")
	}
}

func TestNew_PluginsWithNoHooks_Works(t *testing.T) {
	ps := []plugins.Plugin{
		{Name: "empty-plugin", Version: "1.0", HookRules: nil},
	}
	r := New(ps)
	if r == nil {
		t.Fatal("New() returned nil")
	}
	// Fire should return nil with no matching hooks.
	err := r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Errorf("Fire() with no hooks returned error: %v", err)
	}
}

// ─── Fire() with unknown event ────────────────────────────────────────────────

func TestFire_UnknownEvent_NoError(t *testing.T) {
	ps := []plugins.Plugin{
		{
			Name:    "test-plugin",
			Version: "1.0",
			HookRules: []plugins.HookRule{
				{
					Event: EventPostCommand,
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "echo test"},
					},
				},
			},
		},
	}
	r := New(ps)
	err := r.Fire(context.Background(), "NonExistentEvent", nil)
	if err != nil {
		t.Errorf("Fire() with unknown event returned unexpected error: %v", err)
	}
}

// ─── Fire() with "command" type hook ─────────────────────────────────────────

func TestFire_CommandHook_ExecutesScript(t *testing.T) {
	// Create a temp file that the hook will touch — proves sh -c execution works.
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "hook-ran.txt")

	ps := []plugins.Plugin{
		{
			Name:    "cmd-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					Hooks: []plugins.HookDef{
						{
							Type:    "command",
							Command: "touch " + markerFile,
							Async:   false,
						},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventPreCommand, map[string]any{"command": "/help"})
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	if _, statErr := os.Stat(markerFile); os.IsNotExist(statErr) {
		t.Errorf("hook command did not run: marker file %q was not created", markerFile)
	}
}

// ─── Fire() with SessionStart event (W4-LIFECYCLE) ────────────────────────────

// TestFire_SessionStartHook_ExecutesScript proves the new EventSessionStart
// constant flows through Runner.Fire() exactly like the pre-existing events
// (matching is purely string-based in Fire(); nothing event-specific is
// hardcoded there). The REPL fires this event once at startup — see
// internal/repl.REPL.fireSessionStart — so this is the mechanism a
// kata-harness-shaped SessionStart hook now relies on to actually run.
func TestFire_SessionStartHook_ExecutesScript(t *testing.T) {
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "session-start-ran.txt")

	ps := []plugins.Plugin{
		{
			Name:    "session-start-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventSessionStart,
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + markerFile},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventSessionStart, map[string]any{"session": "dojo-cli-test", "resumed": false})
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	if _, statErr := os.Stat(markerFile); os.IsNotExist(statErr) {
		t.Errorf("SessionStart hook did not run: marker file %q was not created", markerFile)
	}
}

// ─── Fire() with async hook ───────────────────────────────────────────────────

func TestFire_AsyncHook_ReturnsBeforeCompletion(t *testing.T) {
	// Use a sleep command as the hook body; Fire() should return before it finishes.
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "async-done.txt")

	// The hook sleeps briefly then touches the marker.
	// Fire() must return before the marker appears.
	ps := []plugins.Plugin{
		{
			Name:    "async-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventPostCommand,
					Hooks: []plugins.HookDef{
						{
							Type:    "command",
							Command: "sleep 0.3 && touch " + markerFile,
							Async:   true,
						},
					},
				},
			},
		},
	}

	r := New(ps)

	start := time.Now()
	err := r.Fire(context.Background(), EventPostCommand, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	// Fire() should return well before the 300 ms sleep completes.
	if elapsed >= 200*time.Millisecond {
		t.Errorf("Fire() took %v — async hook should have returned immediately", elapsed)
	}

	// Marker should NOT exist yet right after Fire() returns.
	if _, statErr := os.Stat(markerFile); !os.IsNotExist(statErr) {
		t.Logf("marker appeared faster than expected (flaky if machine is very fast)")
	}

	// Give the goroutine time to finish so we don't leave zombie processes.
	time.Sleep(500 * time.Millisecond)
}

// ─── Fire() cancelled context prevents new async hooks ───────────────────────

func TestFire_CancelledContext_AsyncHookNotStarted(t *testing.T) {
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "cancelled.txt")

	ps := []plugins.Plugin{
		{
			Name:    "cancel-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					Hooks: []plugins.HookDef{
						{
							Type:    "command",
							Command: "touch " + markerFile,
							Async:   true,
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before firing

	r := New(ps)
	err := r.Fire(ctx, EventPreCommand, nil)
	if err != nil {
		t.Fatalf("Fire() with cancelled context returned error: %v", err)
	}

	// Allow a brief window; the async hook should NOT have run.
	time.Sleep(50 * time.Millisecond)
	if _, statErr := os.Stat(markerFile); !os.IsNotExist(statErr) {
		t.Errorf("async hook ran despite cancelled context; marker file was created")
	}
}

// ─── Non-command hooks are no longer silently skipped (Phase 6) ──────────────

func TestFire_NonCommandHooks_Skipped(t *testing.T) {
	// Phase 6: prompt/agent print to stdout but return nil error.
	// HTTP hook to example.com may fail network-wise, but errors are logged, not returned.
	ps := []plugins.Plugin{
		{
			Name:    "skip-plugin",
			Version: "1.0",
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					Hooks: []plugins.HookDef{
						{Type: "prompt", Prompt: "do something"},
						{Type: "agent", Command: "some-agent"},
						{Type: "http", URL: "http://127.0.0.1:0"}, // invalid port → logged, not fatal
					},
				},
			},
		},
	}
	r := New(ps)
	err := r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Errorf("Fire() with non-command hooks returned error: %v", err)
	}
}

// ─── Event name matching is case-insensitive ──────────────────────────────────

func TestFire_CaseInsensitiveEventMatch(t *testing.T) {
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "case-match.txt")

	ps := []plugins.Plugin{
		{
			Name:    "case-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					// Rule uses mixed case
					Event: "precommand",
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + markerFile, Async: false},
					},
				},
			},
		},
	}

	r := New(ps)
	// Fire with the canonical constant (different case)
	err := r.Fire(context.Background(), EventPreCommand, nil) // "PreCommand"
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	if _, statErr := os.Stat(markerFile); os.IsNotExist(statErr) {
		t.Errorf("case-insensitive event match failed: marker file not created")
	}
}

// ─── HTTP hook ────────────────────────────────────────────────────────────────

func TestFireHTTPHook(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", req.Method)
		}
		ct := req.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		var err error
		received, err = io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := map[string]any{"command": "/garden ls", "user": "test"}
	ps := []plugins.Plugin{
		{
			Name:    "http-plugin",
			Version: "1.0",
			HookRules: []plugins.HookRule{
				{
					Event: EventPostCommand,
					Hooks: []plugins.HookDef{
						{Type: "http", URL: srv.URL},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventPostCommand, payload)
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	// Verify server received the POST with the payload.
	if len(received) == 0 {
		t.Fatal("http hook: server received no body")
	}
	var got map[string]any
	if err := json.Unmarshal(received, &got); err != nil {
		t.Fatalf("http hook: body is not valid JSON: %v", err)
	}
	if got["command"] != "/garden ls" {
		t.Errorf("http hook: expected command=/garden ls in body, got %v", got["command"])
	}
}

// ─── Prompt hook ──────────────────────────────────────────────────────────────

func TestFirePromptHook(t *testing.T) {
	ps := []plugins.Plugin{
		{
			Name:    "prompt-plugin",
			Version: "1.0",
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					Hooks: []plugins.HookDef{
						{Type: "prompt", Prompt: "summarize the session"},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Errorf("Fire() with prompt hook returned error: %v", err)
	}
	// Side effect is stdout output — no assertion needed beyond no error.
}

// ─── Agent hook ───────────────────────────────────────────────────────────────

func TestFireAgentHook(t *testing.T) {
	ps := []plugins.Plugin{
		{
			Name:    "agent-plugin",
			Version: "1.0",
			HookRules: []plugins.HookRule{
				{
					Event: EventPostSkill,
					Hooks: []plugins.HookDef{
						{Type: "agent", Command: "agent-id-42"},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventPostSkill, nil)
	if err != nil {
		t.Errorf("Fire() with agent hook returned error: %v", err)
	}
	// Side effect is stdout output — no assertion needed beyond no error.
}

// ─── Matcher glob ─────────────────────────────────────────────────────────────

func TestMatcherGlob(t *testing.T) {
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "garden-matched.txt")

	ps := []plugins.Plugin{
		{
			Name:    "matcher-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event:   EventPreCommand,
					Matcher: "garden*",
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + markerFile},
					},
				},
			},
		},
	}

	r := New(ps)

	// Should match: command starts with "garden"
	err := r.Fire(context.Background(), EventPreCommand, map[string]any{"command": "/garden ls"})
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}
	if _, statErr := os.Stat(markerFile); os.IsNotExist(statErr) {
		t.Errorf("matcher=garden* with command=/garden ls: hook should have fired but did not")
	}

	// Remove marker to reuse it.
	_ = os.Remove(markerFile)

	// Should NOT match: command is /health, not garden*
	err = r.Fire(context.Background(), EventPreCommand, map[string]any{"command": "/health"})
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}
	if _, statErr := os.Stat(markerFile); !os.IsNotExist(statErr) {
		t.Errorf("matcher=garden* with command=/health: hook should NOT have fired but did")
	}
}

// ─── "if" condition: false ────────────────────────────────────────────────────

func TestIfConditionFalse(t *testing.T) {
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "if-false.txt")

	ps := []plugins.Plugin{
		{
			Name:    "if-false-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					If:    "false",
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + markerFile},
					},
				},
			},
		},
	}

	r := New(ps)
	err := r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}
	if _, statErr := os.Stat(markerFile); !os.IsNotExist(statErr) {
		t.Errorf("if=false: hook should NOT have fired but did")
	}
}

// ─── shellFor (Windows vs POSIX shell selection) ──────────────────────────────

func TestShellFor(t *testing.T) {
	cases := []struct {
		goos     string
		wantExe  string
		wantFlag string
	}{
		{"windows", "cmd", "/C"},
		{"linux", "sh", "-c"},
		{"darwin", "sh", "-c"},
		{"freebsd", "sh", "-c"},
	}
	for _, tc := range cases {
		exe, flag := shellFor(tc.goos)
		if exe != tc.wantExe || flag != tc.wantFlag {
			t.Errorf("shellFor(%q) = (%q, %q), want (%q, %q)", tc.goos, exe, flag, tc.wantExe, tc.wantFlag)
		}
	}
}

// ─── matcherMatches: malformed glob is handled, not silently swallowed ───────

func TestMatcherMatches_BadPattern_ReturnsFalseNotPanic(t *testing.T) {
	// "[" is an unterminated character class — path.Match returns
	// ErrBadPattern. Before the fix this was swallowed via
	// `matched, _ := path.Match(...)`, so a malformed matcher just never
	// fired with zero signal to the plugin author. The observable contract
	// (no match, no panic) is unchanged by the fix — what changed is that
	// it's now logged instead of silently disappearing; this test pins the
	// safe-default behavior since matcherMatches has no injectable logger
	// to assert the log line itself against.
	if matcherMatches("[", map[string]any{"command": "/garden ls"}) {
		t.Error("matcherMatches with a malformed glob should return false, not match")
	}
}

func TestMatcherMatches_ValidPattern_StillWorks(t *testing.T) {
	// Sanity check alongside the bad-pattern test: a well-formed glob is
	// unaffected by checking the path.Match error.
	if !matcherMatches("garden*", map[string]any{"command": "/garden ls"}) {
		t.Error("matcherMatches(\"garden*\", .../garden ls) should match")
	}
	if matcherMatches("garden*", map[string]any{"command": "/health"}) {
		t.Error("matcherMatches(\"garden*\", .../health) should not match")
	}
}

// ─── "if" condition: env var ──────────────────────────────────────────────────

func TestIfConditionEnvVar(t *testing.T) {
	const envVar = "DOJO_HOOK_TEST_COND_VAR"
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "if-env.txt")

	ps := []plugins.Plugin{
		{
			Name:    "if-env-plugin",
			Version: "1.0",
			Path:    tmp,
			HookRules: []plugins.HookRule{
				{
					Event: EventPreCommand,
					If:    envVar,
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + markerFile},
					},
				},
			},
		},
	}

	r := New(ps)

	// Env var NOT set → hook should not fire.
	_ = os.Unsetenv(envVar)
	err := r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}
	if _, statErr := os.Stat(markerFile); !os.IsNotExist(statErr) {
		t.Errorf("if=envvar (unset): hook should NOT have fired but did")
	}

	// Env var SET → hook should fire.
	t.Setenv(envVar, "1")
	err = r.Fire(context.Background(), EventPreCommand, nil)
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}
	if _, statErr := os.Stat(markerFile); os.IsNotExist(statErr) {
		t.Errorf("if=envvar (set): hook should have fired but did not")
	}
}
