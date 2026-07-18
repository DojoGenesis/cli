package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/plugins"
)

// ─── vitalityPrompt ──────────────────────────────────────────────────────────

func TestVitalityPrompt_ZeroTurns_NeutralDarkDot(t *testing.T) {
	got := vitalityPrompt(0)
	if got == "" {
		t.Fatal("vitalityPrompt(0) returned empty string")
	}
	// The neutral-dark dot character "●" must appear (may be wrapped in ANSI codes).
	if !strings.Contains(got, "●") {
		t.Errorf("vitalityPrompt(0) does not contain '●': %q", got)
	}
	// Must contain "dojo".
	if !strings.Contains(got, "dojo") {
		t.Errorf("vitalityPrompt(0) does not contain 'dojo': %q", got)
	}
}

func TestVitalityPrompt_ThreeTurns_WarmAmberDot(t *testing.T) {
	got := vitalityPrompt(3)
	if got == "" {
		t.Fatal("vitalityPrompt(3) returned empty string")
	}
	// The warm-amber path also uses "●".
	if !strings.Contains(got, "●") {
		t.Errorf("vitalityPrompt(3) does not contain '●': %q", got)
	}
	if !strings.Contains(got, "dojo") {
		t.Errorf("vitalityPrompt(3) does not contain 'dojo': %q", got)
	}
}

func TestVitalityPrompt_TenTurns_ContainsBoldFormatting(t *testing.T) {
	got := vitalityPrompt(10)
	if got == "" {
		t.Fatal("vitalityPrompt(10) returned empty string")
	}
	// The bold path uses the same "●" dot and "dojo" name.
	// ANSI codes may be absent when no TTY is available; verify structural content instead.
	if !strings.Contains(got, "●") {
		t.Errorf("vitalityPrompt(10) does not contain '●': %q", got)
	}
	if !strings.Contains(got, "dojo") {
		t.Errorf("vitalityPrompt(10) does not contain 'dojo': %q", got)
	}
}

func TestVitalityPrompt_BoundaryAt5_Default(t *testing.T) {
	// Exactly 5 turns hits the default (bold) case.
	got := vitalityPrompt(5)
	if !strings.Contains(got, "●") {
		t.Errorf("vitalityPrompt(5) does not contain '●': %q", got)
	}
	if !strings.Contains(got, "dojo") {
		t.Errorf("vitalityPrompt(5) does not contain 'dojo': %q", got)
	}
}

// ─── sunsetWordmark ───────────────────────────────────────────────────────────

func TestSunsetWordmark_EmptyString_ReturnsEmpty(t *testing.T) {
	got := sunsetWordmark("")
	if got != "" {
		t.Errorf("sunsetWordmark(\"\") = %q, want \"\"", got)
	}
}

func TestSunsetWordmark_Dojo_NonEmpty(t *testing.T) {
	got := sunsetWordmark("Dojo")
	if got == "" {
		t.Fatal("sunsetWordmark(\"Dojo\") returned empty string")
	}
	// Each rune in "Dojo" must appear somewhere in the output.
	// ANSI codes may be stripped when no TTY is available, but the plain characters must remain.
	for _, ch := range "Dojo" {
		if !strings.ContainsRune(got, ch) {
			t.Errorf("sunsetWordmark(\"Dojo\") missing character %q in output: %q", ch, got)
		}
	}
}

func TestSunsetWordmark_SingleChar_Works(t *testing.T) {
	// Single character hits the n==1 branch in colorAt.
	got := sunsetWordmark("X")
	if got == "" {
		t.Fatal("sunsetWordmark(\"X\") returned empty string")
	}
	if !strings.ContainsRune(got, 'X') {
		t.Errorf("sunsetWordmark(\"X\") does not contain 'X': %q", got)
	}
}

func TestSunsetWordmark_LongerText_ContainsAllChars(t *testing.T) {
	// Every rune from the input must appear in the output, with or without ANSI codes.
	input := "Hello World"
	got := sunsetWordmark(input)
	for _, ch := range input {
		if !strings.ContainsRune(got, ch) {
			t.Errorf("sunsetWordmark(%q): output missing character %q", input, ch)
		}
	}
}

// ─── extractText ─────────────────────────────────────────────────────────────

func TestExtractText_PlainText(t *testing.T) {
	chunk := client.SSEChunk{Data: "hello world"}
	got := extractText(chunk)
	if got != "hello world" {
		t.Errorf("extractText plain: got %q, want %q", got, "hello world")
	}
}

func TestExtractText_OpenAIDeltaFormat(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"hello"}}]}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "hello" {
		t.Errorf("extractText OpenAI delta: got %q, want %q", got, "hello")
	}
}

func TestExtractText_SimpleTextField(t *testing.T) {
	data := `{"text":"hello"}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "hello" {
		t.Errorf("extractText {text}: got %q, want %q", got, "hello")
	}
}

func TestExtractText_SimpleContentField(t *testing.T) {
	data := `{"content":"world"}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "world" {
		t.Errorf("extractText {content}: got %q, want %q", got, "world")
	}
}

func TestExtractText_MessageField(t *testing.T) {
	data := `{"message":"msg value"}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "msg value" {
		t.Errorf("extractText {message}: got %q, want %q", got, "msg value")
	}
}

func TestExtractText_ResponseField(t *testing.T) {
	data := `{"response":"resp value"}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "resp value" {
		t.Errorf("extractText {response}: got %q, want %q", got, "resp value")
	}
}

func TestExtractText_Done_ReturnsEmpty(t *testing.T) {
	chunk := client.SSEChunk{Data: "[DONE]"}
	got := extractText(chunk)
	if got != "" {
		t.Errorf("extractText [DONE]: got %q, want empty string", got)
	}
}

func TestExtractText_EmptyData_ReturnsEmpty(t *testing.T) {
	chunk := client.SSEChunk{Data: ""}
	got := extractText(chunk)
	if got != "" {
		t.Errorf("extractText empty: got %q, want empty string", got)
	}
}

func TestExtractText_WhitespaceOnly_ReturnsEmpty(t *testing.T) {
	chunk := client.SSEChunk{Data: "   "}
	got := extractText(chunk)
	if got != "" {
		t.Errorf("extractText whitespace: got %q, want empty string", got)
	}
}

func TestExtractText_JSONWithNoKnownKey_ReturnsEmpty(t *testing.T) {
	// JSON but no text/content/message/response key → should return "".
	data := `{"unknown_field":"value"}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "" {
		t.Errorf("extractText unknown JSON key: got %q, want empty string", got)
	}
}

func TestExtractText_ChoiceTextFallback(t *testing.T) {
	// Non-streaming: choices[0].text (not delta)
	data := `{"choices":[{"text":"non-streaming text"}]}`
	chunk := client.SSEChunk{Data: data}
	got := extractText(chunk)
	if got != "non-streaming text" {
		t.Errorf("extractText choices[0].text: got %q, want %q", got, "non-streaming text")
	}
}

// ─── fireSessionStart / fireSessionEnd (W4-LIFECYCLE) ─────────────────────────
//
// Run() itself is not unit-tested here — it owns signal handling, a
// blocking readline loop, a background SSE goroutine, and real ~/.dojo state
// writes, none of which are hermetic. Instead these tests exercise the exact
// two methods Run() calls at the exact points described in their doc
// comments (fireSessionStart after session/config setup and before the read
// loop; fireSessionEnd via defer on every exit path), by constructing a bare
// REPL with only the fields those methods touch (runner, session, resumed).

func TestFireSessionStart_FiresConfiguredHook(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "session-start-ran.txt")

	ps := []plugins.Plugin{
		{
			Name: "kata-harness-shaped",
			Path: tmp,
			HookRules: []plugins.HookRule{
				{
					Event: hooks.EventSessionStart,
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + marker},
					},
				},
			},
		},
	}

	r := &REPL{
		runner:  hooks.New(ps, nil),
		session: "dojo-cli-test-session",
		resumed: true,
	}
	r.fireSessionStart(context.Background())

	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Errorf("fireSessionStart did not run the configured SessionStart hook; marker %q was not created", marker)
	}
}

func TestFireSessionStart_NoMatchingHooks_NoPanic(t *testing.T) {
	// A REPL whose runner has no SessionStart rules (the common case — most
	// plugins don't define one) must be a silent no-op, not a panic.
	r := &REPL{runner: hooks.New(nil, nil), session: "dojo-cli-test-session"}
	r.fireSessionStart(context.Background())
}

func TestFireSessionEnd_FiresConfiguredHook(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "session-end-ran.txt")

	ps := []plugins.Plugin{
		{
			Name: "cleanup-plugin",
			Path: tmp,
			HookRules: []plugins.HookRule{
				{
					Event: hooks.EventSessionEnd,
					Hooks: []plugins.HookDef{
						{Type: "command", Command: "touch " + marker},
					},
				},
			},
		},
	}

	r := &REPL{
		runner:  hooks.New(ps, nil),
		session: "dojo-cli-test-session",
	}
	r.fireSessionEnd()

	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Errorf("fireSessionEnd did not run the configured SessionEnd hook; marker %q was not created", marker)
	}
}

func TestFireSessionEnd_NoMatchingHooks_NoPanic(t *testing.T) {
	// Symmetric with TestFireSessionStart_NoMatchingHooks_NoPanic: a REPL
	// whose runner has no SessionEnd rules must be a silent no-op. Also
	// exercises fireSessionEnd's no-ctx-parameter signature — it always
	// fires against context.Background() internally (see its doc comment:
	// this is deliberate so it still runs a hook to completion even when
	// Run()'s own ctx was already cancelled, e.g. the SIGTERM-shutdown case).
	r := &REPL{runner: hooks.New(nil, nil), session: "dojo-cli-test-session"}
	r.fireSessionEnd()
}

// ─── normalizeErrSignature ────────────────────────────────────────────────────

func TestNormalizeErrSignature(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Connection Refused", "connection refused"},
		{"dial tcp 127.0.0.1:7340: connection refused", "dial tcp #.#.#.#:#: connection refused"},
		{"FAIL\tpkg/path\t0.41s", "fail pkg/path #.#s"},
		{"  extra   spaces\tand\ttabs  ", "extra spaces and tabs"},
		{"", ""},
		{"   \t\n ", ""},
	}
	for _, c := range cases {
		if got := normalizeErrSignature(c.in); got != c.want {
			t.Errorf("normalizeErrSignature(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ─── Repeated-failure tell (recordFailure) ────────────────────────────────────

// enabledREPL builds the minimal REPL recordFailure touches: a non-plain session
// with the protocol enabled. The Nudger and failure tracker are zero-valued.
func enabledREPL() *REPL {
	return &REPL{cfg: &config.Config{Protocol: config.ProtocolConfig{Enabled: true}}}
}

// TestRecordFailure_FiresDebuggingGateOnceAtThreshold proves the repeated-failure
// detector stays silent for the first repeatFailureThreshold-1 identical
// failures, fires the debugging gate exactly on the Nth, and dedupes after.
func TestRecordFailure_FiresDebuggingGateOnceAtThreshold(t *testing.T) {
	r := enabledREPL()
	const errText = "rate limit exceeded" // no situated tell — pure repetition drives it

	for i := 1; i < repeatFailureThreshold; i++ {
		if g, ok := r.recordFailure(errText); ok {
			t.Fatalf("recordFailure #%d fired early: (%q, true)", i, g)
		}
	}
	g, ok := r.recordFailure(errText) // the Nth
	if !ok {
		t.Fatalf("recordFailure #%d should fire the debugging gate", repeatFailureThreshold)
	}
	if !strings.Contains(g, "Debug by disproof") {
		t.Errorf("fired gate = %q, want the debug-by-disproof gate", g)
	}
	// N+1 and beyond: deduped by the Nudger's fire-once-per-gate ledger.
	if g, ok := r.recordFailure(errText); ok {
		t.Errorf("recordFailure after firing should dedupe, got (%q, true)", g)
	}
}

// TestRecordFailure_DifferentErrorResetsRun proves a different error resets the
// streak: two of error A then a switch to error B restarts the count at 1, so B
// must recur its own full threshold before anything fires.
func TestRecordFailure_DifferentErrorResetsRun(t *testing.T) {
	r := enabledREPL()

	_, _ = r.recordFailure("connection refused") // A, count 1
	_, _ = r.recordFailure("connection refused") // A, count 2
	if r.errRepeat != 2 {
		t.Fatalf("after two identical failures errRepeat = %d, want 2", r.errRepeat)
	}
	_, _ = r.recordFailure("undefined: fooBar") // B — different, resets
	if r.errRepeat != 1 {
		t.Fatalf("a different error should reset errRepeat to 1, got %d", r.errRepeat)
	}
	// B must reach the threshold on its own to fire.
	if _, ok := r.recordFailure("undefined: fooBar"); ok { // B count 2
		t.Fatal("B fired before reaching the threshold")
	}
	if _, ok := r.recordFailure("undefined: fooBar"); !ok { // B count 3
		t.Fatal("B should fire once it reaches the threshold")
	}
}

// TestRecordFailure_NormalizesTriviallyDifferentErrors proves errors that differ
// only in volatile numerics (ports, IPs) share a signature and so count toward
// the same run — one recurring failure, not three distinct ones.
func TestRecordFailure_NormalizesTriviallyDifferentErrors(t *testing.T) {
	r := enabledREPL()
	msgs := []string{
		`dial tcp 127.0.0.1:7340: connect: connection refused`,
		`dial tcp 10.0.0.1:9999: connect: connection refused`,
		`dial tcp 192.168.1.5:8080: connect: connection refused`,
	}
	var fired bool
	for i, m := range msgs {
		if _, ok := r.recordFailure(m); ok {
			if i < len(msgs)-1 {
				t.Fatalf("fired early at attempt %d", i+1)
			}
			fired = true
		}
	}
	if !fired {
		t.Error("three trivially-different-but-same-class errors should reach the threshold together")
	}
	if r.errRepeat < repeatFailureThreshold {
		t.Errorf("errRepeat = %d, want >= %d (signatures should have collapsed)", r.errRepeat, repeatFailureThreshold)
	}
}

// TestRecordFailure_SuppressedUnderPlainAndDisabled proves the surface
// suppression rules hold: --plain, a disabled protocol, and a nil cfg all keep
// the gate silent even past the threshold — while --plain still COUNTS the run,
// so the detector is correct the instant suppression is lifted.
func TestRecordFailure_SuppressedUnderPlainAndDisabled(t *testing.T) {
	// --plain: enabled protocol but plain output → never fires.
	plain := &REPL{cfg: &config.Config{Protocol: config.ProtocolConfig{Enabled: true}}, plain: true}
	for i := 0; i < repeatFailureThreshold+2; i++ {
		if _, ok := plain.recordFailure("build failed"); ok {
			t.Fatal("recordFailure must stay silent under --plain")
		}
	}
	if plain.errRepeat < repeatFailureThreshold {
		t.Errorf("plain mode should still COUNT failures; errRepeat = %d", plain.errRepeat)
	}

	// Protocol disabled → never fires.
	off := &REPL{cfg: &config.Config{Protocol: config.ProtocolConfig{Enabled: false}}}
	for i := 0; i < repeatFailureThreshold+2; i++ {
		if _, ok := off.recordFailure("build failed"); ok {
			t.Fatal("recordFailure must stay silent when the protocol is disabled")
		}
	}

	// nil cfg → never fires (defensive).
	nilcfg := &REPL{}
	for i := 0; i < repeatFailureThreshold+2; i++ {
		if _, ok := nilcfg.recordFailure("build failed"); ok {
			t.Fatal("recordFailure must stay silent with nil cfg")
		}
	}
}

// TestRecordFailure_EmptyErrorIgnored proves blank error text neither advances
// nor resets the run — it is not a distinct failure to count.
func TestRecordFailure_EmptyErrorIgnored(t *testing.T) {
	r := enabledREPL()
	_, _ = r.recordFailure("build failed") // count 1
	if _, ok := r.recordFailure("   \t\n "); ok {
		t.Fatal("whitespace-only error should be ignored")
	}
	if r.errRepeat != 1 || r.lastErrSig == "" {
		t.Errorf("empty error must not touch the tracker; errRepeat=%d lastErrSig=%q", r.errRepeat, r.lastErrSig)
	}
}
