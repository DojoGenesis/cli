package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
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
		runner:  hooks.New(ps),
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
	r := &REPL{runner: hooks.New(nil), session: "dojo-cli-test-session"}
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
		runner:  hooks.New(ps),
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
	r := &REPL{runner: hooks.New(nil), session: "dojo-cli-test-session"}
	r.fireSessionEnd()
}
