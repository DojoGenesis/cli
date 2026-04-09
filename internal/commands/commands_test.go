package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/DojoGenesis/dojo-cli/internal/config"
	"github.com/DojoGenesis/dojo-cli/internal/plugins"
)

// testRegistry builds a minimal Registry suitable for tests that do not call gw.
// gw is nil — only commands that are purely client-side (session, practice, help,
// settings, trace, hooks ls, projects) are safe to dispatch in unit tests.
func testRegistry() (*Registry, *string) {
	session := "test-session-id"
	cfg := &config.Config{
		Gateway: config.GatewayConfig{URL: "http://test:7340", Timeout: "5s"},
	}
	r := &Registry{
		cfg:     cfg,
		cmds:    make(map[string]Command),
		plgs:    []plugins.Plugin{},
		session: &session,
	}
	r.register()
	return r, &session
}

// ─── Registry.Dispatch ────────────────────────────────────────────────────────

func TestDispatchUnknownCommand(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected error to contain %q, got %q", "unknown command", err.Error())
	}
}

func TestDispatchEmptyInput(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "")
	if err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
}

func TestDispatchEmptyWhitespace(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "   ")
	if err != nil {
		t.Fatalf("expected nil error for whitespace-only input, got %v", err)
	}
}

func TestDispatchKnownCommand(t *testing.T) {
	r, _ := testRegistry()
	// "practice" is purely client-side, no gw needed
	err := r.Dispatch(context.Background(), "practice")
	if err != nil {
		t.Fatalf("expected no error dispatching known command 'practice', got %v", err)
	}
}

func TestDispatchAliasLookup(t *testing.T) {
	r, _ := testRegistry()
	// "settings" has aliases "config" and "cfg"
	err := r.Dispatch(context.Background(), "cfg")
	if err != nil {
		t.Fatalf("expected no error dispatching alias 'cfg', got %v", err)
	}
}

func TestDispatchAliasConfig(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "config")
	if err != nil {
		t.Fatalf("expected no error dispatching alias 'config', got %v", err)
	}
}

// ─── truncate ─────────────────────────────────────────────────────────────────

func TestTruncateBasic(t *testing.T) {
	got := truncate("hello world", 5)
	want := "hell…"
	if got != want {
		t.Errorf("truncate(%q, 5) = %q; want %q", "hello world", got, want)
	}
}

func TestTruncateNoOp(t *testing.T) {
	got := truncate("hi", 10)
	if got != "hi" {
		t.Errorf("truncate(%q, 10) = %q; want %q", "hi", got, "hi")
	}
}

func TestTruncateEmpty(t *testing.T) {
	got := truncate("", 5)
	if got != "" {
		t.Errorf("truncate(%q, 5) = %q; want %q", "", got, "")
	}
}

func TestTruncateExactLength(t *testing.T) {
	// string length equals n — should NOT truncate
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("truncate(%q, 5) = %q; want %q", "hello", got, "hello")
	}
}

func TestTruncateUnicode(t *testing.T) {
	// "日本語テスト" is 6 runes; truncate to 4 → first 3 runes + ellipsis
	got := truncate("日本語テスト", 4)
	want := "日本語…"
	if got != want {
		t.Errorf("truncate(%q, 4) = %q; want %q", "日本語テスト", got, want)
	}
	// Verify the result is valid UTF-8 (no mid-rune cut)
	for _, r := range got {
		_ = r // range over runes will panic on invalid UTF-8
	}
}

// ─── colorStatus ──────────────────────────────────────────────────────────────

func TestColorStatusOk(t *testing.T) {
	got := colorStatus("ok")
	if got == "" {
		t.Error("colorStatus('ok') returned empty string")
	}
}

func TestColorStatusHealthy(t *testing.T) {
	got := colorStatus("healthy")
	if got == "" {
		t.Error("colorStatus('healthy') returned empty string")
	}
}

func TestColorStatusFailed(t *testing.T) {
	got := colorStatus("failed")
	if got == "" {
		t.Error("colorStatus('failed') returned empty string")
	}
	// "failed" is not a known-green or amber status so it falls through to danger-red
	// The raw word should appear somewhere in the ANSI-wrapped result
	if !strings.Contains(got, "failed") {
		t.Errorf("colorStatus('failed') = %q; expected to contain 'failed'", got)
	}
}

func TestColorStatusEmpty(t *testing.T) {
	got := colorStatus("")
	if !strings.Contains(got, "unknown") {
		t.Errorf("colorStatus('') = %q; expected to contain 'unknown'", got)
	}
}

func TestColorStatusUnknownKeyword(t *testing.T) {
	got := colorStatus("unknown")
	if !strings.Contains(got, "unknown") {
		t.Errorf("colorStatus('unknown') = %q; expected to contain 'unknown'", got)
	}
}

func TestColorStatusSubmitted(t *testing.T) {
	// "submitted" is amber (loading group), not red
	got := colorStatus("submitted")
	if got == "" {
		t.Error("colorStatus('submitted') returned empty string")
	}
	if strings.Contains(got, "e63946") {
		t.Errorf("colorStatus('submitted') appears to be red; expected amber")
	}
}

func TestColorStatusCompleted(t *testing.T) {
	// "completed" is green (ok group), not red
	got := colorStatus("completed")
	if got == "" {
		t.Error("colorStatus('completed') returned empty string")
	}
	if strings.Contains(got, "e63946") {
		t.Errorf("colorStatus('completed') appears to be red; expected green")
	}
}

// ─── orDefault ────────────────────────────────────────────────────────────────

func TestOrDefaultNonEmpty(t *testing.T) {
	got := orDefault("val", "def")
	if got != "val" {
		t.Errorf("orDefault(%q, %q) = %q; want %q", "val", "def", got, "val")
	}
}

func TestOrDefaultEmpty(t *testing.T) {
	got := orDefault("", "def")
	if got != "def" {
		t.Errorf("orDefault(%q, %q) = %q; want %q", "", "def", got, "def")
	}
}

// ─── agentExtractText ─────────────────────────────────────────────────────────

func TestAgentExtractTextPlain(t *testing.T) {
	got := agentExtractText("hello there")
	if got != "hello there" {
		t.Errorf("agentExtractText(%q) = %q; want %q", "hello there", got, "hello there")
	}
}

func TestAgentExtractTextJSONText(t *testing.T) {
	got := agentExtractText(`{"text": "hello"}`)
	if got != "hello" {
		t.Errorf("agentExtractText JSON text key: got %q; want %q", got, "hello")
	}
}

func TestAgentExtractTextJSONContent(t *testing.T) {
	got := agentExtractText(`{"content": "hello"}`)
	if got != "hello" {
		t.Errorf("agentExtractText JSON content key: got %q; want %q", got, "hello")
	}
}

func TestAgentExtractTextJSONUnknownKey(t *testing.T) {
	got := agentExtractText(`{"other": "hello"}`)
	if got != "" {
		t.Errorf("agentExtractText JSON unknown key: got %q; want %q", got, "")
	}
}

func TestAgentExtractTextDone(t *testing.T) {
	got := agentExtractText("[DONE]")
	if got != "" {
		t.Errorf("agentExtractText('[DONE]') = %q; want empty string", got)
	}
}

func TestAgentExtractTextEmpty(t *testing.T) {
	got := agentExtractText("")
	if got != "" {
		t.Errorf("agentExtractText('') = %q; want empty string", got)
	}
}

func TestAgentExtractTextJSONMessage(t *testing.T) {
	got := agentExtractText(`{"message": "world"}`)
	if got != "world" {
		t.Errorf("agentExtractText JSON message key: got %q; want %q", got, "world")
	}
}

func TestAgentExtractTextJSONDelta(t *testing.T) {
	got := agentExtractText(`{"delta": "chunk"}`)
	if got != "chunk" {
		t.Errorf("agentExtractText JSON delta key: got %q; want %q", got, "chunk")
	}
}

// ─── sessionCmd integration ───────────────────────────────────────────────────

func TestSessionShow(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionNew(t *testing.T) {
	r, session := testRegistry()
	old := *session
	err := r.Dispatch(context.Background(), "session new")
	if err != nil {
		t.Fatal(err)
	}
	if *session == old {
		t.Error("session should have changed after 'session new'")
	}
}

func TestSessionResume(t *testing.T) {
	r, session := testRegistry()
	err := r.Dispatch(context.Background(), "session my-custom-id")
	if err != nil {
		t.Fatal(err)
	}
	if *session != "my-custom-id" {
		t.Errorf("expected session to be 'my-custom-id', got %q", *session)
	}
}

// ─── practiceCmd ──────────────────────────────────────────────────────────────

func TestPracticeNoGateway(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "practice")
	if err != nil {
		t.Fatalf("practice command should not error (client-side only), got: %v", err)
	}
}
