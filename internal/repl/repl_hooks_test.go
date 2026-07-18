package repl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/plugins"
)

// fireUserPromptSubmit is the exact seam chat() uses before sending a free-text
// message to the gateway. Testing it directly — with a bare REPL that has only
// the runner set (no gateway, no ~/.dojo state) — covers the block/continue
// decision and the DOJO_PROMPT env delivery end-to-end through repl → runner →
// exec, hermetically. chat() itself is not unit-tested here for the same reason
// Run() isn't (see repl_test.go): it owns a real gateway stream. The blocking
// decision it depends on lives entirely in this seam.

// TestFireUserPromptSubmit_BlockingHookVetoesSend proves a failing Blocking
// UserPromptSubmit hook vetoes the send (returns true → chat() returns to the
// prompt without contacting the gateway).
func TestFireUserPromptSubmit_BlockingHookVetoesSend(t *testing.T) {
	ps := []plugins.Plugin{{
		Name: "prompt-gate",
		Path: t.TempDir(),
		HookRules: []plugins.HookRule{{
			Event:    hooks.EventUserPromptSubmit,
			Blocking: true,
			Matcher:  "chat",
			Hooks:    []plugins.HookDef{{Type: "command", Command: "exit 1"}},
		}},
	}}
	r := &REPL{runner: hooks.New(ps, nil)}

	if !r.fireUserPromptSubmit(context.Background(), "some message") {
		t.Error("a failing Blocking UserPromptSubmit hook must veto the send (return true)")
	}
}

// TestFireUserPromptSubmit_NonBlockingFailureAllowsSend proves a failing
// non-Blocking hook logs and continues (returns false → the send proceeds).
func TestFireUserPromptSubmit_NonBlockingFailureAllowsSend(t *testing.T) {
	ps := []plugins.Plugin{{
		Name: "prompt-warn",
		Path: t.TempDir(),
		HookRules: []plugins.HookRule{{
			Event:   hooks.EventUserPromptSubmit,
			Matcher: "chat",
			Hooks:   []plugins.HookDef{{Type: "command", Command: "exit 1"}}, // non-blocking
		}},
	}}
	r := &REPL{runner: hooks.New(ps, nil)}

	if r.fireUserPromptSubmit(context.Background(), "hello") {
		t.Error("a non-blocking failure must NOT veto the send (return false)")
	}
}

// TestFireUserPromptSubmit_DeliversPromptToHookEnv proves the user's message
// reaches a command hook via $DOJO_PROMPT through the repl seam.
func TestFireUserPromptSubmit_DeliversPromptToHookEnv(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "prompt.txt")
	const msg = "the user's words"
	ps := []plugins.Plugin{{
		Name: "prompt-capture",
		Path: tmp,
		HookRules: []plugins.HookRule{{
			Event:   hooks.EventUserPromptSubmit,
			Matcher: "chat",
			Hooks:   []plugins.HookDef{{Type: "command", Command: `printf '%s' "$DOJO_PROMPT" > ` + marker}},
		}},
	}}
	r := &REPL{runner: hooks.New(ps, nil)}

	if r.fireUserPromptSubmit(context.Background(), msg) {
		t.Fatal("a clean hook should not veto the send")
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	if string(got) != msg {
		t.Errorf("hook saw DOJO_PROMPT=%q, want %q", got, msg)
	}
}

// TestFireUserPromptSubmit_NoHooks_AllowsSend proves the common case (no
// UserPromptSubmit hooks defined) is a silent pass-through.
func TestFireUserPromptSubmit_NoHooks_AllowsSend(t *testing.T) {
	r := &REPL{runner: hooks.New(nil, nil)}
	if r.fireUserPromptSubmit(context.Background(), "hi") {
		t.Error("with no hooks the send must proceed (return false)")
	}
}

// TestFireUserPromptSubmit_NonChatMatcherDoesNotFire proves the event matches
// rules against the literal "chat": a rule scoped to a different matcher must
// not fire on a chat prompt.
func TestFireUserPromptSubmit_NonChatMatcherDoesNotFire(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "should-not-exist.txt")
	ps := []plugins.Plugin{{
		Name: "scoped",
		Path: tmp,
		HookRules: []plugins.HookRule{{
			Event:   hooks.EventUserPromptSubmit,
			Matcher: "deploy*", // does not match "chat"
			Hooks:   []plugins.HookDef{{Type: "command", Command: "touch " + marker}},
		}},
	}}
	r := &REPL{runner: hooks.New(ps, nil)}

	if r.fireUserPromptSubmit(context.Background(), "hello") {
		t.Fatal("a deploy*-scoped rule should not veto")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("a rule matched to deploy* must not fire on the chat prompt event")
	}
}
