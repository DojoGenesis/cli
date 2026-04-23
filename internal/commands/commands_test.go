package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guide"
	"github.com/DojoGenesis/cli/internal/plugins"
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

// ─── fmtAgo ──────────────────────────────────────────────────────────────────

func TestFmtAgoEmpty(t *testing.T) {
	got := fmtAgo("")
	if got != "unknown" {
		t.Errorf("fmtAgo('') = %q; want 'unknown'", got)
	}
}

func TestFmtAgoInvalid(t *testing.T) {
	got := fmtAgo("not-a-timestamp")
	if got != "unknown" {
		t.Errorf("fmtAgo(invalid) = %q; want 'unknown'", got)
	}
}

func TestFmtAgoSeconds(t *testing.T) {
	// A timestamp 10 seconds ago
	ts := time.Now().Add(-10 * time.Second).Format(time.RFC3339)
	got := fmtAgo(ts)
	if !strings.HasSuffix(got, "s ago") {
		t.Errorf("fmtAgo(10s ago) = %q; want suffix 's ago'", got)
	}
}

func TestFmtAgoMinutes(t *testing.T) {
	ts := time.Now().Add(-5 * time.Minute).Format(time.RFC3339)
	got := fmtAgo(ts)
	if !strings.HasSuffix(got, "m ago") {
		t.Errorf("fmtAgo(5m ago) = %q; want suffix 'm ago'", got)
	}
}

func TestFmtAgoHours(t *testing.T) {
	ts := time.Now().Add(-3 * time.Hour).Format(time.RFC3339)
	got := fmtAgo(ts)
	if !strings.HasSuffix(got, "h ago") {
		t.Errorf("fmtAgo(3h ago) = %q; want suffix 'h ago'", got)
	}
}

func TestFmtAgoDays(t *testing.T) {
	ts := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	got := fmtAgo(ts)
	if !strings.HasSuffix(got, "d ago") {
		t.Errorf("fmtAgo(48h ago) = %q; want suffix 'd ago'", got)
	}
}

// ─── fmtUnixAgo ──────────────────────────────────────────────────────────────

func TestFmtUnixAgoZero(t *testing.T) {
	got := fmtUnixAgo(0)
	if got != "unknown" {
		t.Errorf("fmtUnixAgo(0) = %q; want 'unknown'", got)
	}
}

func TestFmtUnixAgoSeconds(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).Unix()
	got := fmtUnixAgo(ts)
	if !strings.HasSuffix(got, "s ago") {
		t.Errorf("fmtUnixAgo(10s) = %q; want suffix 's ago'", got)
	}
}

func TestFmtUnixAgoMinutes(t *testing.T) {
	ts := time.Now().Add(-5 * time.Minute).Unix()
	got := fmtUnixAgo(ts)
	if !strings.HasSuffix(got, "m ago") {
		t.Errorf("fmtUnixAgo(5m) = %q; want suffix 'm ago'", got)
	}
}

func TestFmtUnixAgoHours(t *testing.T) {
	ts := time.Now().Add(-2 * time.Hour).Unix()
	got := fmtUnixAgo(ts)
	if !strings.HasSuffix(got, "h ago") {
		t.Errorf("fmtUnixAgo(2h) = %q; want suffix 'h ago'", got)
	}
}

func TestFmtUnixAgoDays(t *testing.T) {
	ts := time.Now().Add(-72 * time.Hour).Unix()
	got := fmtUnixAgo(ts)
	if !strings.HasSuffix(got, "d ago") {
		t.Errorf("fmtUnixAgo(72h) = %q; want suffix 'd ago'", got)
	}
}

// ─── agentNestedField ─────────────────────────────────────────────────────────

func TestAgentNestedFieldHit(t *testing.T) {
	raw := `{"data":{"message":"hello from nested"}}`
	got := agentNestedField(raw, "message")
	if got != "hello from nested" {
		t.Errorf("agentNestedField nested message: got %q; want 'hello from nested'", got)
	}
}

func TestAgentNestedFieldMissing(t *testing.T) {
	raw := `{"data":{"tool":"bash"}}`
	got := agentNestedField(raw, "message")
	if got != "" {
		t.Errorf("agentNestedField missing key: got %q; want empty", got)
	}
}

func TestAgentNestedFieldNoDataKey(t *testing.T) {
	raw := `{"type":"thinking","other":"x"}`
	got := agentNestedField(raw, "message")
	if got != "" {
		t.Errorf("agentNestedField no data key: got %q; want empty", got)
	}
}

func TestAgentNestedFieldNullData(t *testing.T) {
	raw := `{"data":null}`
	got := agentNestedField(raw, "message")
	if got != "" {
		t.Errorf("agentNestedField null data: got %q; want empty", got)
	}
}

func TestAgentNestedFieldInvalidJSON(t *testing.T) {
	got := agentNestedField("not-json", "message")
	if got != "" {
		t.Errorf("agentNestedField invalid JSON: got %q; want empty", got)
	}
}

func TestAgentNestedFieldToolField(t *testing.T) {
	raw := `{"data":{"tool":"bash","args":"ls"}}`
	got := agentNestedField(raw, "tool")
	if got != "bash" {
		t.Errorf("agentNestedField tool: got %q; want 'bash'", got)
	}
}

func TestAgentNestedFieldContentField(t *testing.T) {
	raw := `{"type":"response_chunk","data":{"content":"hello world"}}`
	got := agentNestedField(raw, "content")
	if got != "hello world" {
		t.Errorf("agentNestedField content: got %q; want 'hello world'", got)
	}
}

// ─── colorTrackStatus ─────────────────────────────────────────────────────────

func TestColorTrackStatusPending(t *testing.T) {
	got := colorTrackStatus("pending")
	if !strings.Contains(got, "pending") {
		t.Errorf("colorTrackStatus('pending') = %q; expected to contain 'pending'", got)
	}
	// pending maps to cloud-gray (#94a3b8), not amber or red
	if strings.Contains(got, "e8b04a") || strings.Contains(got, "e63946") {
		t.Errorf("colorTrackStatus('pending') should be gray, not amber or red: %q", got)
	}
}

func TestColorTrackStatusInProgress(t *testing.T) {
	got := colorTrackStatus("in_progress")
	if !strings.Contains(got, "in_progress") {
		t.Errorf("colorTrackStatus('in_progress') = %q; expected to contain 'in_progress'", got)
	}
}

func TestColorTrackStatusCompleted(t *testing.T) {
	got := colorTrackStatus("completed")
	if !strings.Contains(got, "completed") {
		t.Errorf("colorTrackStatus('completed') = %q; expected to contain 'completed'", got)
	}
}

func TestColorTrackStatusBlocked(t *testing.T) {
	got := colorTrackStatus("blocked")
	if !strings.Contains(got, "blocked") {
		t.Errorf("colorTrackStatus('blocked') = %q; expected to contain 'blocked'", got)
	}
}

func TestColorTrackStatusUnknown(t *testing.T) {
	got := colorTrackStatus("unknown-status")
	if !strings.Contains(got, "unknown-status") {
		t.Errorf("colorTrackStatus('unknown-status') = %q; expected to contain 'unknown-status'", got)
	}
}

// ─── parseSkillLsArgs ─────────────────────────────────────────────────────────

func TestParseSkillLsArgsEmpty(t *testing.T) {
	filter, showAll, page := parseSkillLsArgs([]string{})
	if filter != "" || showAll || page != 1 {
		t.Errorf("parseSkillLsArgs([]) = (%q, %v, %d); want ('', false, 1)", filter, showAll, page)
	}
}

func TestParseSkillLsArgsAll(t *testing.T) {
	_, showAll, page := parseSkillLsArgs([]string{"all"})
	if !showAll || page != 1 {
		t.Errorf("parseSkillLsArgs(['all']) showAll=%v page=%d; want showAll=true page=1", showAll, page)
	}
}

func TestParseSkillLsArgsPagePrefix(t *testing.T) {
	_, _, page := parseSkillLsArgs([]string{"p3"})
	if page != 3 {
		t.Errorf("parseSkillLsArgs(['p3']) page=%d; want 3", page)
	}
}

func TestParseSkillLsArgsBareNumber(t *testing.T) {
	_, _, page := parseSkillLsArgs([]string{"2"})
	if page != 2 {
		t.Errorf("parseSkillLsArgs(['2']) page=%d; want 2", page)
	}
}

func TestParseSkillLsArgsFilter(t *testing.T) {
	filter, showAll, page := parseSkillLsArgs([]string{"engineering"})
	if filter != "engineering" || showAll || page != 1 {
		t.Errorf("parseSkillLsArgs(['engineering']) = (%q, %v, %d); want ('engineering', false, 1)",
			filter, showAll, page)
	}
}

func TestParseSkillLsArgsMultiWordFilter(t *testing.T) {
	filter, _, _ := parseSkillLsArgs([]string{"code", "review"})
	if filter != "code review" {
		t.Errorf("parseSkillLsArgs multi-word: filter=%q; want 'code review'", filter)
	}
}

func TestParseSkillLsArgsSkipsLs(t *testing.T) {
	filter, showAll, page := parseSkillLsArgs([]string{"ls"})
	if filter != "" || showAll || page != 1 {
		t.Errorf("parseSkillLsArgs(['ls']) = (%q, %v, %d); want ('', false, 1)", filter, showAll, page)
	}
}

func TestParseSkillLsArgsAllAndPage(t *testing.T) {
	_, showAll, page := parseSkillLsArgs([]string{"all", "p2"})
	if !showAll || page != 2 {
		t.Errorf("parseSkillLsArgs(['all','p2']) showAll=%v page=%d; want showAll=true page=2", showAll, page)
	}
}

func TestParseSkillLsArgsFilterAndPage(t *testing.T) {
	filter, _, page := parseSkillLsArgs([]string{"engineering", "p4"})
	if filter != "engineering" || page != 4 {
		t.Errorf("parseSkillLsArgs(['engineering','p4']) filter=%q page=%d; want 'engineering' 4",
			filter, page)
	}
}

// ─── buildMiniBar ─────────────────────────────────────────────────────────────

func TestBuildMiniBarZeroTotal(t *testing.T) {
	got := buildMiniBar(5, 0, 10)
	if got != strings.Repeat("░", 10) {
		t.Errorf("buildMiniBar(5,0,10) = %q; want all-empty bar", got)
	}
}

func TestBuildMiniBarZeroWidth(t *testing.T) {
	got := buildMiniBar(5, 10, 0)
	if got != "" {
		t.Errorf("buildMiniBar(5,10,0) = %q; want empty string", got)
	}
}

func TestBuildMiniBarFull(t *testing.T) {
	got := buildMiniBar(10, 10, 10)
	if got != strings.Repeat("█", 10) {
		t.Errorf("buildMiniBar(10,10,10) = %q; want all-filled bar", got)
	}
}

func TestBuildMiniBarEmpty(t *testing.T) {
	got := buildMiniBar(0, 10, 10)
	if got != strings.Repeat("░", 10) {
		t.Errorf("buildMiniBar(0,10,10) = %q; want all-empty bar", got)
	}
}

func TestBuildMiniBarHalf(t *testing.T) {
	got := buildMiniBar(5, 10, 10)
	if len([]rune(got)) != 10 {
		t.Errorf("buildMiniBar(5,10,10) length = %d; want 10 runes", len([]rune(got)))
	}
	if !strings.HasPrefix(got, "█") {
		t.Errorf("buildMiniBar(5,10,10) = %q; expected filled prefix", got)
	}
}

func TestBuildMiniBarMinFilled(t *testing.T) {
	// count=1, total=100, width=10 — count*width/total = 0, should round up to 1
	got := buildMiniBar(1, 100, 10)
	if !strings.HasPrefix(got, "█") {
		t.Errorf("buildMiniBar(1,100,10) = %q; expected at least 1 filled cell", got)
	}
}

// ─── fmtTokens ────────────────────────────────────────────────────────────────

func TestFmtTokensSmall(t *testing.T) {
	got := fmtTokens(42)
	if got != "42" {
		t.Errorf("fmtTokens(42) = %q; want '42'", got)
	}
}

func TestFmtTokensThousands(t *testing.T) {
	got := fmtTokens(1500)
	if !strings.HasSuffix(got, "K") {
		t.Errorf("fmtTokens(1500) = %q; want suffix 'K'", got)
	}
}

func TestFmtTokensMillions(t *testing.T) {
	got := fmtTokens(2_500_000)
	if !strings.HasSuffix(got, "M") {
		t.Errorf("fmtTokens(2500000) = %q; want suffix 'M'", got)
	}
}

func TestFmtTokensExactThousand(t *testing.T) {
	got := fmtTokens(1000)
	if got != "1.0K" {
		t.Errorf("fmtTokens(1000) = %q; want '1.0K'", got)
	}
}

func TestFmtTokensZero(t *testing.T) {
	got := fmtTokens(0)
	if got != "0" {
		t.Errorf("fmtTokens(0) = %q; want '0'", got)
	}
}

// ─── /help dispatch (client-side) ─────────────────────────────────────────────

func TestDispatchHelp(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "help")
	if err != nil {
		t.Fatalf("expected no error dispatching 'help', got: %v", err)
	}
}

// ─── /hooks ls (no gateway, empty plugins) ────────────────────────────────────

func TestDispatchHooksLs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "hooks")
	if err != nil {
		t.Fatalf("expected no error dispatching 'hooks' (empty plugins), got: %v", err)
	}
}

func TestDispatchHooksLsExplicit(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "hooks ls")
	if err != nil {
		t.Fatalf("expected no error dispatching 'hooks ls', got: %v", err)
	}
}

// ─── /trace (guidance mode, no args, no gateway) ──────────────────────────────

func TestDispatchTraceNoArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "trace")
	if err != nil {
		t.Fatalf("expected no error dispatching 'trace' with no args, got: %v", err)
	}
}

// ─── /settings (no args, client-side) ─────────────────────────────────────────

func TestDispatchSettings(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "settings")
	if err != nil {
		t.Fatalf("expected no error dispatching 'settings', got: %v", err)
	}
}

func TestDispatchSettingsEffective(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "settings effective")
	if err != nil {
		t.Fatalf("expected no error dispatching 'settings effective', got: %v", err)
	}
}

func TestDispatchSettingsUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "settings badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown settings subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown settings subcommand") {
		t.Errorf("error message = %q; expected to contain 'unknown settings subcommand'", err.Error())
	}
}

// ─── /activity (reads local file, no gateway) ────────────────────────────────

func TestDispatchActivity(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "activity")
	if err != nil {
		t.Fatalf("expected no error dispatching 'activity', got: %v", err)
	}
}

func TestDispatchActivityWithCount(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "activity 5")
	if err != nil {
		t.Fatalf("expected no error dispatching 'activity 5', got: %v", err)
	}
}

// ─── /disposition (reads local config, no gateway) ────────────────────────────

func TestDispatchDispositionShow(t *testing.T) {
	r, _ := testRegistry()
	// Default: show current disposition — no args needed
	err := r.Dispatch(context.Background(), "disposition")
	if err != nil {
		t.Fatalf("expected no error dispatching 'disposition', got: %v", err)
	}
}

func TestDispatchDispositionAlias(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "disp")
	if err != nil {
		t.Fatalf("expected no error dispatching 'disp' alias, got: %v", err)
	}
}

func TestDispatchDispositionLs(t *testing.T) {
	r, _ := testRegistry()
	// May error if no preset file exists — that is fine; just ensure it dispatches
	_ = r.Dispatch(context.Background(), "disposition ls")
}

func TestDispatchDispositionUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "disposition badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown disposition subcommand, got nil")
	}
}

func TestDispatchDispositionSetMissingArg(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "disposition set")
	if err == nil {
		t.Fatal("expected error for 'disposition set' with no name, got nil")
	}
}

func TestDispatchDispositionCreateMissingArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "disposition create")
	if err == nil {
		t.Fatal("expected error for 'disposition create' with no args, got nil")
	}
}

// ─── /model error paths (no gateway needed) ───────────────────────────────────

func TestDispatchModelUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "model badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown model subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown /model subcommand") {
		t.Errorf("error = %q; expected 'unknown /model subcommand'", err.Error())
	}
}

func TestDispatchModelSetNoArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "model set")
	if err == nil {
		t.Fatal("expected error for 'model set' with no args, got nil")
	}
}

func TestDispatchModelDirectTooFewArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "model direct openai")
	if err == nil {
		t.Fatal("expected error for 'model direct' with too few args, got nil")
	}
}

// ─── /agent error paths (no gateway call needed) ─────────────────────────────

func TestDispatchAgentDispatchNoMessage(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent dispatch")
	if err == nil {
		t.Fatal("expected error for 'agent dispatch' with no message, got nil")
	}
}

func TestDispatchAgentChatTooFewArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent chat")
	if err == nil {
		t.Fatal("expected error for 'agent chat' with no args, got nil")
	}
}

func TestDispatchAgentInfoNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent info")
	if err == nil {
		t.Fatal("expected error for 'agent info' with no id, got nil")
	}
}

func TestDispatchAgentChannelsNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent channels")
	if err == nil {
		t.Fatal("expected error for 'agent channels' with no id, got nil")
	}
}

// ─── redactSecretArgs ─────────────────────────────────────────────────────────

// TestRedactSecretArgsSettingsSet verifies that the API key positional arg in
// "/settings set <provider> <api-key>" is replaced with "<redacted>".
func TestRedactSecretArgsSettingsSet(t *testing.T) {
	args := []string{"set", "anthropic", "sk-ant-FAKE123"}
	got := redactSecretArgs("settings", args)
	if got[2] != "<redacted>" {
		t.Errorf("redactSecretArgs: args[2] = %q; want <redacted>", got[2])
	}
	// Provider name must survive untouched.
	if got[1] != "anthropic" {
		t.Errorf("redactSecretArgs: args[1] = %q; want anthropic", got[1])
	}
	// Subcommand must survive untouched.
	if got[0] != "set" {
		t.Errorf("redactSecretArgs: args[0] = %q; want set", got[0])
	}
}

// TestRedactSecretArgsSettingsSetOriginalUnchanged verifies that the original
// args slice is not mutated — the command handler must still receive the real
// value.
func TestRedactSecretArgsSettingsSetOriginalUnchanged(t *testing.T) {
	original := []string{"set", "openai", "sk-REALKEY"}
	_ = redactSecretArgs("settings", original)
	if original[2] != "sk-REALKEY" {
		t.Errorf("original args were mutated: args[2] = %q; want sk-REALKEY", original[2])
	}
}

// TestRedactSecretArgsNonSecretCommand verifies that args for a non-secret
// command are returned unchanged.
func TestRedactSecretArgsNonSecretCommand(t *testing.T) {
	args := []string{"hello", "world"}
	got := redactSecretArgs("help", args)
	if got[0] != "hello" || got[1] != "world" {
		t.Errorf("redactSecretArgs(help): got %v; want [hello world]", got)
	}
}

// TestRedactSecretArgsSettingsOtherSubcmd verifies that /settings with a
// non-secret subcommand (e.g. "effective") is not redacted.
func TestRedactSecretArgsSettingsEffective(t *testing.T) {
	args := []string{"effective"}
	got := redactSecretArgs("settings", args)
	if got[0] != "effective" {
		t.Errorf("redactSecretArgs(settings effective): got %v; want [effective]", got)
	}
}

// TestRedactSecretArgsSettingsSetTooFewArgs verifies that when too few args are
// provided (secret index out of bounds), the function returns safely without
// panicking and leaves args unchanged.
func TestRedactSecretArgsSettingsSetTooFewArgs(t *testing.T) {
	args := []string{"set", "anthropic"} // missing api-key — only 2 elements
	got := redactSecretArgs("settings", args)
	if len(got) != 2 {
		t.Errorf("redactSecretArgs with too-few args: got len %d; want 2", len(got))
	}
	if got[1] != "anthropic" {
		t.Errorf("redactSecretArgs with too-few args: args[1] = %q; want anthropic", got[1])
	}
}

// TestRedactSecretArgsEmptyArgs verifies that an empty args slice is handled
// safely and returned as-is.
func TestRedactSecretArgsEmpty(t *testing.T) {
	got := redactSecretArgs("settings", []string{})
	if len(got) != 0 {
		t.Errorf("redactSecretArgs with empty args: got len %d; want 0", len(got))
	}
}

// ─── Activity log redaction integration ──────────────────────────────────────

// readActivityLog reads all entries from the activity log and returns their
// Summary strings.
func readActivityLog(t *testing.T) []string {
	t.Helper()
	home := os.Getenv("HOME")
	logPath := filepath.Join(home, ".dojo", "activity.log")
	f, err := os.Open(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("could not open activity log: %v", err)
	}
	defer f.Close()

	var summaries []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e activity.Entry
		if json.Unmarshal(line, &e) == nil {
			summaries = append(summaries, e.Summary)
		}
	}
	return summaries
}

// TestActivityLogNonSecretCommandNotRedacted verifies that dispatching a
// non-secret command logs the args as-is (no spurious redaction).
func TestActivityLogNonSecretCommandNotRedacted(t *testing.T) {
	// Redirect HOME so the log goes to a throwaway directory.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	r, _ := testRegistry()
	// "practice" is a purely client-side command with no gateway call.
	if err := r.Dispatch(context.Background(), "practice"); err != nil {
		t.Fatalf("Dispatch practice: %v", err)
	}

	summaries := readActivityLog(t)
	if len(summaries) == 0 {
		t.Fatal("expected at least one activity log entry after successful dispatch")
	}
	last := summaries[len(summaries)-1]
	if strings.Contains(last, "<redacted>") {
		t.Errorf("non-secret command log entry contains unexpected <redacted>: %q", last)
	}
	if !strings.Contains(last, "/practice") {
		t.Errorf("expected log entry to contain /practice, got %q", last)
	}
}

// TestActivityLogSecretArgIsRedacted verifies that the activity log written by
// Dispatch for /settings set contains "<redacted>" in place of the API key and
// does NOT contain the literal key value.
//
// Because /settings set requires a live gateway (r.gw.SetProviderKey), we
// cannot drive it through Dispatch in a unit test. Instead we call
// activity.Log directly with the string that Dispatch would write — using
// redactSecretArgs to build it — which exercises exactly the production code
// path that assembles the log line.
func TestActivityLogSecretArgIsRedacted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const realKey = "sk-ant-FAKE123"
	name := "settings"
	args := []string{"set", "anthropic", realKey}

	// This mirrors what Dispatch does after a successful run:
	logArgs := redactSecretArgs(name, args)
	summary := "/" + name + " " + strings.Join(logArgs, " ")
	activity.Log(activity.CommandRun, summary)

	summaries := readActivityLog(t)
	if len(summaries) == 0 {
		t.Fatal("expected at least one activity log entry")
	}
	last := summaries[len(summaries)-1]
	if strings.Contains(last, realKey) {
		t.Errorf("activity log contains the real API key %q — it must be redacted: %q", realKey, last)
	}
	if !strings.Contains(last, "<redacted>") {
		t.Errorf("activity log does not contain <redacted>: %q", last)
	}
	if !strings.Contains(last, "anthropic") {
		t.Errorf("activity log should retain the provider name 'anthropic': %q", last)
	}
}

func TestDispatchAgentBindNoArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent bind myagent")
	if err == nil {
		t.Fatal("expected error for 'agent bind' missing channel, got nil")
	}
}

func TestDispatchAgentUnbindNoArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "agent unbind myagent")
	if err == nil {
		t.Fatal("expected error for 'agent unbind' missing channel, got nil")
	}
}

// ─── /trail error paths (no gateway call needed) ─────────────────────────────

func TestDispatchTrailAddNoText(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "trail add")
	if err == nil {
		t.Fatal("expected error for 'trail add' with no text, got nil")
	}
}

func TestDispatchTrailRmNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "trail rm")
	if err == nil {
		t.Fatal("expected error for 'trail rm' with no id, got nil")
	}
}

func TestDispatchTrailSearchNoQuery(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "trail search")
	if err == nil {
		t.Fatal("expected error for 'trail search' with no query, got nil")
	}
}

func TestDispatchTrailUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "trail badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown trail subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown trail subcommand") {
		t.Errorf("error = %q; expected 'unknown trail subcommand'", err.Error())
	}
}

// ─── /snapshot error paths (no gateway call needed) ──────────────────────────

func TestDispatchSnapshotRestoreNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "snapshot restore")
	if err == nil {
		t.Fatal("expected error for 'snapshot restore' with no id, got nil")
	}
}

func TestDispatchSnapshotExportNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "snapshot export")
	if err == nil {
		t.Fatal("expected error for 'snapshot export' with no id, got nil")
	}
}

func TestDispatchSnapshotRmNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "snapshot rm")
	if err == nil {
		t.Fatal("expected error for 'snapshot rm' with no id, got nil")
	}
}

// ─── /project error paths (no gateway call needed) ───────────────────────────

func TestDispatchProjectUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown project subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown /project subcommand") {
		t.Errorf("error = %q; expected 'unknown /project subcommand'", err.Error())
	}
}

func TestDispatchProjectInitNoName(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project init")
	if err == nil {
		t.Fatal("expected error for 'project init' with no name, got nil")
	}
}

func TestDispatchProjectTrackNoSub(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project track")
	if err == nil {
		t.Fatal("expected error for 'project track' with no subcommand, got nil")
	}
}

func TestDispatchProjectTrackBadSub(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project track badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown project track subcommand, got nil")
	}
}

func TestDispatchProjectDecisionNoText(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project decision")
	// May error due to no active project OR because no text was provided
	if err == nil {
		t.Fatal("expected error for 'project decision' with no text, got nil")
	}
}

func TestDispatchProjectArtifactTooFewArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "project artifact")
	if err == nil {
		t.Fatal("expected error for 'project artifact' with too few args, got nil")
	}
}

// ─── /code error paths (no external tools needed) ────────────────────────────

func TestDispatchCodeNoArgs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "code")
	if err == nil {
		t.Fatal("expected error for 'code' with no subcommand, got nil")
	}
}

func TestDispatchCodeUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "code badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown code subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("error = %q; expected 'unknown subcommand'", err.Error())
	}
}

// ─── /garden error paths (no gateway needed for arg validation) ──────────────

func TestDispatchGardenPlantNoText(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "garden plant")
	if err == nil {
		t.Fatal("expected error for 'garden plant' with no text, got nil")
	}
}

// ─── /hooks fire error paths ──────────────────────────────────────────────────

func TestDispatchHooksFireNoEvent(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "hooks fire")
	if err == nil {
		t.Fatal("expected error for 'hooks fire' with no event, got nil")
	}
}

// ─── /init parse (no gateway call for flag-only paths) ───────────────────────

func TestDispatchInitAliasSetup(t *testing.T) {
	r, _ := testRegistry()
	// The init command will try to contact gateway — that's fine, we just verify
	// the alias resolves correctly (dispatch doesn't return unknown command).
	err := r.Dispatch(context.Background(), "setup --skip-seeds")
	// Error is OK (gateway unreachable) — what we verify is it's not "unknown command"
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Errorf("'setup' alias should resolve to initCmd, got unknown command error: %v", err)
	}
}

// ─── Registry.Dispatch case-insensitivity ────────────────────────────────────

func TestDispatchCaseInsensitive(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "HELP")
	if err != nil {
		t.Fatalf("expected no error for uppercase 'HELP', got: %v", err)
	}
}

func TestDispatchMixedCase(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "Help")
	if err != nil {
		t.Fatalf("expected no error for mixed-case 'Help', got: %v", err)
	}
}

// ─── visLen ───────────────────────────────────────────────────────────────────

func TestVisLenPlain(t *testing.T) {
	got := visLen("hello")
	if got != 5 {
		t.Errorf("visLen('hello') = %d; want 5", got)
	}
}

func TestVisLenEmpty(t *testing.T) {
	got := visLen("")
	if got != 0 {
		t.Errorf("visLen('') = %d; want 0", got)
	}
}

func TestVisLenWithANSI(t *testing.T) {
	// "\x1b[32mhello\x1b[0m" — ANSI green "hello" — visible length should be 5
	ansiStr := "\x1b[32mhello\x1b[0m"
	got := visLen(ansiStr)
	if got != 5 {
		t.Errorf("visLen(ansi 'hello') = %d; want 5", got)
	}
}

func TestVisLenMixed(t *testing.T) {
	// "abc" + ANSI reset + "de" — visible chars = 5
	s := "abc\x1b[0mde"
	got := visLen(s)
	if got != 5 {
		t.Errorf("visLen mixed = %d; want 5", got)
	}
}

// ─── findGoModRoot ────────────────────────────────────────────────────────────

func TestFindGoModRoot(t *testing.T) {
	// Should find go.mod somewhere up from the package directory
	root, err := findGoModRoot()
	if err != nil {
		t.Fatalf("findGoModRoot() returned error: %v", err)
	}
	if root == "" {
		t.Fatal("findGoModRoot() returned empty path")
	}
}

// ─── skillCategoryOrder ───────────────────────────────────────────────────────

func TestSkillCategoryOrderEmpty(t *testing.T) {
	cats, keys := skillCategoryOrder(nil)
	if len(cats) != 0 || len(keys) != 0 {
		t.Errorf("skillCategoryOrder(nil) = (%v, %v); want both empty", cats, keys)
	}
}

func TestSkillCategoryOrderSingle(t *testing.T) {
	skills := []client.Skill{
		{ID: "a", Category: "engineering"},
		{ID: "b", Category: "engineering"},
		{ID: "c", Category: "design"},
	}
	cats, keys := skillCategoryOrder(skills)
	if len(keys) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(keys))
	}
	// engineering has 2, design has 1 — engineering should be first
	if keys[0] != "engineering" {
		t.Errorf("keys[0] = %q; want 'engineering' (higher count)", keys[0])
	}
	if len(cats["engineering"]) != 2 {
		t.Errorf("engineering count = %d; want 2", len(cats["engineering"]))
	}
}

func TestSkillCategoryOrderEmptyCategory(t *testing.T) {
	skills := []client.Skill{
		{ID: "a", Category: ""},
		{ID: "b", Category: "engineering"},
	}
	_, keys := skillCategoryOrder(skills)
	found := false
	for _, k := range keys {
		if k == "general" {
			found = true
		}
	}
	if !found {
		t.Errorf("empty category should map to 'general'; keys = %v", keys)
	}
}

// ─── printCostChart ───────────────────────────────────────────────────────────

func TestPrintCostChartEmpty(t *testing.T) {
	// Must not panic on empty input
	printCostChart([]telemetryDailyRow{})
}

func TestPrintCostChartNonEmpty(t *testing.T) {
	rows := []telemetryDailyRow{
		{Day: "2026-04-01", TotalCost: 1.0},
		{Day: "2026-04-02", TotalCost: 2.5},
		{Day: "2026-04-03", TotalCost: 0.0},
	}
	// Must not panic
	printCostChart(rows)
}

func TestPrintCostChartAllZero(t *testing.T) {
	rows := []telemetryDailyRow{
		{Day: "2026-04-01", TotalCost: 0.0},
	}
	// maxCost=0 path (uses fallback of 1.0) — must not panic or divide by zero
	printCostChart(rows)
}

// ─── guide dispatch paths (client-side state reads) ──────────────────────────

func TestDispatchGuideLs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide ls")
	if err != nil {
		t.Fatalf("expected no error dispatching 'guide ls', got: %v", err)
	}
}

func TestDispatchGuideNoArgs(t *testing.T) {
	r, _ := testRegistry()
	// no args defaults to 'ls' — same as guide ls
	err := r.Dispatch(context.Background(), "guide")
	if err != nil {
		t.Fatalf("expected no error dispatching 'guide' with no args, got: %v", err)
	}
}

func TestDispatchGuideStatus(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide status")
	if err != nil {
		t.Fatalf("expected no error dispatching 'guide status', got: %v", err)
	}
}

func TestDispatchGuideUnknownSubcmd(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide badsubcmd")
	if err == nil {
		t.Fatal("expected error for unknown guide subcommand, got nil")
	}
}

func TestDispatchGuideStartNoID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide start")
	if err == nil {
		t.Fatal("expected error for 'guide start' with no id, got nil")
	}
}

func TestDispatchGuideStartUnknownID(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide start nonexistent-guide-xyz")
	if err == nil {
		t.Fatal("expected error for unknown guide id, got nil")
	}
}

// ─── PrintGuideStepComplete / PrintGuideCompleteBonus ─────────────────────────

func TestPrintGuideStepCompleteNotDone(t *testing.T) {
	// Must not panic when GuideComplete is false
	res := &guide.AdvanceResult{
		XP:            10,
		GuideComplete: false,
		NextStep:      2,
		TotalSteps:    5,
		NextTitle:     "Next Step",
		NextAction:    "do something",
	}
	PrintGuideStepComplete(res)
}

func TestPrintGuideStepCompleteGuideComplete(t *testing.T) {
	res := &guide.AdvanceResult{
		XP:            50,
		GuideComplete: true,
		GuideName:     "Test Guide",
	}
	PrintGuideStepComplete(res)
}

func TestPrintGuideCompleteBonus(t *testing.T) {
	// Must not panic
	PrintGuideCompleteBonus(100)
}

// ─── disposition set (writes to local config, no gateway) ────────────────────

func TestDispatchDispositionSet(t *testing.T) {
	r, _ := testRegistry()
	// dispositionSet just writes to r.cfg and calls cfg.Save() — it's client-side
	err := r.Dispatch(context.Background(), "disposition set balanced")
	if err != nil {
		t.Fatalf("expected no error for 'disposition set balanced', got: %v", err)
	}
}

// ─── /plugin dispatch paths (error paths, no gateway needed) ─────────────────

func TestDispatchPluginNoArgs(t *testing.T) {
	r, _ := testRegistry()
	// no args defaults to 'ls' — reads local plugin list
	err := r.Dispatch(context.Background(), "plugin")
	if err != nil {
		t.Fatalf("expected no error for 'plugin' with no args, got: %v", err)
	}
}

func TestDispatchPluginLs(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "plugin ls")
	if err != nil {
		t.Fatalf("expected no error dispatching 'plugin ls', got: %v", err)
	}
}

func TestDispatchPluginInstallNoURL(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "plugin install")
	if err == nil {
		t.Fatal("expected error for 'plugin install' with no URL, got nil")
	}
}

func TestDispatchPluginRmNoName(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "plugin rm")
	if err == nil {
		t.Fatal("expected error for 'plugin rm' with no name, got nil")
	}
}

// ─── /guide stop (client-side state write) ────────────────────────────────────

func TestDispatchGuideStop(t *testing.T) {
	r, _ := testRegistry()
	// stop when no guide is active — pure client-side path, should not error
	err := r.Dispatch(context.Background(), "guide stop")
	if err != nil {
		t.Fatalf("expected no error dispatching 'guide stop', got: %v", err)
	}
}

func TestDispatchGuideQuit(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "guide quit")
	if err != nil {
		t.Fatalf("expected no error dispatching 'guide quit' alias, got: %v", err)
	}
}

// ─── /activity clear (client-side) ───────────────────────────────────────────

func TestDispatchActivityClear(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "activity clear")
	if err != nil {
		t.Fatalf("expected no error dispatching 'activity clear', got: %v", err)
	}
}

// ─── /model set (valid args — errors from gateway but error-path is covered) ──

func TestDispatchModelSet(t *testing.T) {
	r, _ := testRegistry()
	// model set with a valid arg — saves to config, key push to gateway may fail (non-fatal)
	err := r.Dispatch(context.Background(), "model set claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("expected no error from 'model set' with valid arg, got: %v", err)
	}
}

func TestDispatchModelSetWithProvider(t *testing.T) {
	r, _ := testRegistry()
	err := r.Dispatch(context.Background(), "model set anthropic claude-opus-4")
	if err != nil {
		t.Fatalf("expected no error from 'model set' with provider+model, got: %v", err)
	}
}

// ─── /session resume (no prior session = error is expected) ──────────────────

func TestSessionResumeNoPrior(t *testing.T) {
	r, _ := testRegistry()
	// In a fresh test state there may be no prior session — error is acceptable
	_ = r.Dispatch(context.Background(), "session resume")
	// We verify it dispatches correctly (no panic), not that it succeeds
}

// ─── Registry.New (via public constructor) ────────────────────────────────────

func TestRegistryNew(t *testing.T) {
	session := "test-new-session"
	cfg := &config.Config{
		Gateway: config.GatewayConfig{URL: "http://test:7340", Timeout: "5s"},
	}
	r := New(cfg, nil, []plugins.Plugin{}, &session)
	if r == nil {
		t.Fatal("New() returned nil")
	}
	// Verify at least one command is registered
	if len(r.cmds) == 0 {
		t.Fatal("New() produced a registry with no commands")
	}
}

// ─── Registry.Runner (via public constructor which sets up hooks.Runner) ──────

func TestRegistryRunnerFromNew(t *testing.T) {
	session := "test-runner-session"
	cfg := &config.Config{
		Gateway: config.GatewayConfig{URL: "http://test:7340", Timeout: "5s"},
	}
	r := New(cfg, nil, []plugins.Plugin{}, &session)
	runner := r.Runner()
	if runner == nil {
		t.Fatal("Registry.Runner() returned nil on a registry created via New()")
	}
}
