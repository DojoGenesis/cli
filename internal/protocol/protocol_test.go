package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
)

// hermetic isolates a test from the developer's real HOME and cwd so overlay
// resolution sees an empty world: HOME points at a fresh temp (so
// config.DojoDir()/DOJO.md never exists) and cwd is a fresh temp (so ./DOJO.md
// never exists unless the test writes one). Returns the cwd temp dir. t.Chdir
// and t.Setenv both auto-restore at test end, so this cannot be run in parallel.
func hermetic(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	t.Chdir(cwd)
	return cwd
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ─── DefaultDoc (embed) ───────────────────────────────────────────────────────

func TestDefaultDoc_ContainsCoreTells(t *testing.T) {
	doc := DefaultDoc()
	if strings.TrimSpace(doc) == "" {
		t.Fatal("DefaultDoc() is empty — go:embed of protocol.md failed")
	}
	for _, want := range []string{
		"Dojo Genius Protocol",   // heading
		"Done means verified",    // element 1
		"Debug by disproof",      // element 2
		"kata-harness",           // ratified harness pointer
		"DOJO_PROTOCOL_DISABLED", // override hint
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("DefaultDoc() missing %q", want)
		}
	}
}

// ─── LoadOverlay precedence ───────────────────────────────────────────────────

func TestLoadOverlay_Precedence_ProjectOverDojoOverDefault(t *testing.T) {
	proj := t.TempDir()
	dojo := t.TempDir()

	// Neither present → embedded default.
	doc, src := LoadOverlay(proj, dojo)
	if doc != DefaultDoc() {
		t.Errorf("no overlay: expected default doc, got %d bytes", len(doc))
	}
	if src != "(embedded default)" {
		t.Errorf("no overlay: expected source '(embedded default)', got %q", src)
	}

	// dojoDir overlay present → beats default.
	dojoDoc := "# Machine Overlay\nmachine rules"
	writeFile(t, filepath.Join(dojo, "DOJO.md"), dojoDoc)
	doc, src = LoadOverlay(proj, dojo)
	if doc != dojoDoc {
		t.Errorf("dojoDir overlay: got %q, want %q", doc, dojoDoc)
	}
	if src != filepath.Join(dojo, "DOJO.md") {
		t.Errorf("dojoDir overlay: source label %q", src)
	}

	// project overlay present → beats dojoDir.
	projDoc := "# Project Overlay\nproject rules"
	writeFile(t, filepath.Join(proj, "DOJO.md"), projDoc)
	doc, src = LoadOverlay(proj, dojo)
	if doc != projDoc {
		t.Errorf("project overlay should win: got %q, want %q", doc, projDoc)
	}
	if src != filepath.Join(proj, "DOJO.md") {
		t.Errorf("project overlay: source label %q", src)
	}
}

func TestLoadOverlay_EmptyFileIgnored(t *testing.T) {
	proj := t.TempDir()
	// A whitespace-only overlay must be treated as absent — a stray empty
	// DOJO.md can never blank out the protocol.
	writeFile(t, filepath.Join(proj, "DOJO.md"), "   \n\t\n")
	doc, _ := LoadOverlay(proj, "")
	if doc != DefaultDoc() {
		t.Error("whitespace-only overlay should be ignored, falling back to default")
	}
}

// ─── BuildSystemContext ───────────────────────────────────────────────────────

func TestBuildSystemContext_DisabledReturnsEmpty(t *testing.T) {
	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: false}}
	if got := BuildSystemContext(cfg); got != "" {
		t.Errorf("disabled config: expected empty, got %d bytes", len(got))
	}
	if got := BuildSystemContext(nil); got != "" {
		t.Errorf("nil config: expected empty, got %d bytes", len(got))
	}
}

func TestBuildSystemContext_EnabledReturnsDefault(t *testing.T) {
	hermetic(t) // empty HOME + cwd → no overlay files anywhere
	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: true}}
	if got := BuildSystemContext(cfg); got != DefaultDoc() {
		t.Error("enabled with no overlay should return the embedded default")
	}
}

func TestBuildSystemContext_ExplicitPathWins(t *testing.T) {
	cwd := hermetic(t)
	// A cwd overlay exists but must be BEATEN by an explicit Path.
	writeFile(t, filepath.Join(cwd, "DOJO.md"), "# cwd overlay (should lose)")

	pathDir := t.TempDir()
	explicit := filepath.Join(pathDir, "custom-protocol.md")
	writeFile(t, explicit, "# Explicit Path Wins")

	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: true, Path: explicit}}
	if got := BuildSystemContext(cfg); got != "# Explicit Path Wins" {
		t.Errorf("explicit Path should win over cwd overlay, got %q", got)
	}
}

func TestBuildSystemContext_UnreadablePathFallsThrough(t *testing.T) {
	cwd := hermetic(t)
	projDoc := "# Project Overlay Fallback"
	writeFile(t, filepath.Join(cwd, "DOJO.md"), projDoc)

	// Path points at a file that does not exist → must degrade to overlay
	// resolution (project DOJO.md), not error or blank.
	cfg := &config.Config{Protocol: config.ProtocolConfig{
		Enabled: true,
		Path:    filepath.Join(t.TempDir(), "nope.md"),
	}}
	if got := BuildSystemContext(cfg); got != projDoc {
		t.Errorf("unreadable Path should fall through to project overlay, got %q", got)
	}
}

// ─── WriteDefaultOverlay ──────────────────────────────────────────────────────

func TestWriteDefaultOverlay_WritesThenDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "DOJO.md")

	// First call writes the embedded default.
	if err := WriteDefaultOverlay(dir); err != nil {
		t.Fatalf("first WriteDefaultOverlay: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overlay after write: %v", err)
	}
	if string(got) != DefaultDoc() {
		t.Fatal("first write should equal DefaultDoc()")
	}

	// Operator edits it.
	edited := "# My Edited Protocol\nmine, not yours"
	writeFile(t, path, edited)

	// Second call must NOT clobber the edit.
	if err := WriteDefaultOverlay(dir); err != nil {
		t.Fatalf("second WriteDefaultOverlay: %v", err)
	}
	got2, _ := os.ReadFile(path)
	if string(got2) != edited {
		t.Error("WriteDefaultOverlay clobbered an existing operator overlay")
	}
}

func TestWriteDefaultOverlay_EmptyDirErrors(t *testing.T) {
	if err := WriteDefaultOverlay(""); err == nil {
		t.Error("expected an error for empty dojoDir")
	}
}

// ─── Injector: the request-builder proof (no live model) ──────────────────────

func TestInjector_Apply_StampsOnceWhenEnabled(t *testing.T) {
	hermetic(t)
	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: true}}
	inj := NewInjector(cfg)
	if !inj.Enabled() {
		t.Fatal("expected injector to be enabled")
	}

	// First turn: the outgoing ChatRequest must carry the protocol in BOTH
	// Message (immediate effect) and SystemPrompt (forward-compat), with the
	// user's original message preserved at the tail.
	req := client.ChatRequest{Message: "hello world"}
	if !inj.Apply(&req) {
		t.Fatal("expected first Apply to stamp")
	}
	if !strings.Contains(req.Message, "Dojo Genius Protocol") {
		t.Error("first-turn Message should contain the protocol text")
	}
	if !strings.HasSuffix(req.Message, "hello world") {
		t.Error("first-turn Message should end with the original user message")
	}
	if req.SystemPrompt != DefaultDoc() {
		t.Error("first-turn SystemPrompt should equal the resolved protocol doc")
	}

	// Second turn: no-op. The gateway threads session context by SessionID, so
	// re-prepending would be pure bloat.
	req2 := client.ChatRequest{Message: "second message"}
	if inj.Apply(&req2) {
		t.Fatal("expected second Apply to be a no-op")
	}
	if req2.Message != "second message" {
		t.Errorf("second-turn Message should be untouched, got %q", req2.Message)
	}
	if req2.SystemPrompt != "" {
		t.Error("second-turn SystemPrompt should stay empty")
	}
	if inj.Enabled() {
		t.Error("Enabled() should be false after the single stamp is spent")
	}
}

func TestInjector_Apply_NoopWhenDisabled(t *testing.T) {
	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: false}}
	inj := NewInjector(cfg)
	if inj.Enabled() {
		t.Fatal("expected injector disabled")
	}
	req := client.ChatRequest{Message: "hello"}
	if inj.Apply(&req) {
		t.Fatal("disabled injector must not stamp")
	}
	if req.Message != "hello" {
		t.Errorf("Message should be untouched when disabled, got %q", req.Message)
	}
	if req.SystemPrompt != "" {
		t.Error("SystemPrompt should stay empty when disabled")
	}
}

// TestInjector_CwdOverlayOverridesDefault proves a cwd ./DOJO.md overrides the
// embedded default in the actually-injected request — the override path that
// matters for real sessions.
func TestInjector_CwdOverlayOverridesDefault(t *testing.T) {
	cwd := hermetic(t)
	custom := "# Custom Project Protocol\nDo the custom thing."
	writeFile(t, filepath.Join(cwd, "DOJO.md"), custom)

	cfg := &config.Config{Protocol: config.ProtocolConfig{Enabled: true}}
	req := client.ChatRequest{Message: "hi"}
	if !NewInjector(cfg).Apply(&req) {
		t.Fatal("expected stamp")
	}
	if req.SystemPrompt != custom {
		t.Errorf("cwd overlay should win: SystemPrompt = %q", req.SystemPrompt)
	}
	if !strings.Contains(req.Message, "Custom Project Protocol") {
		t.Error("Message should carry the cwd overlay text")
	}
	if strings.Contains(req.Message, "Done means verified") {
		t.Error("embedded default must not appear once the cwd overlay overrides it")
	}
}

// ─── JIT tell-triggered injection: TellFor ────────────────────────────────────

// TestTellFor_WiringSignaturesMapToConfigVsCodeGate proves every boundary/
// wiring error class the CLI can observe routes to the one config-vs-code gate.
func TestTellFor_WiringSignaturesMapToConfigVsCodeGate(t *testing.T) {
	cases := []struct {
		name string
		err  string
	}{
		{"connection refused", `Post "http://localhost:9999/v1/chat": dial tcp 127.0.0.1:9999: connect: connection refused`},
		{"no such host", `dial tcp: lookup nope.invalid: no such host`},
		{"dial tcp bare", "dial tcp 10.0.0.1:7340: i/o timeout"},
		{"auth 401", "gateway returned status 401 unauthorized"},
		{"auth 403", "403 Forbidden"},
		{"dead route 404", "404 page not found"},
		{"model not found", `model "claude-x-9" not found`},
		{"case-insensitive", "CONNECTION REFUSED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate, ok := TellFor(tc.err)
			if !ok {
				t.Fatalf("TellFor(%q) ok=false, want true", tc.err)
			}
			if gate != configVsCodeGate {
				t.Errorf("TellFor(%q) gate = %q, want the config-vs-code gate %q", tc.err, gate, configVsCodeGate)
			}
		})
	}
}

// TestTellFor_UnrelatedTextReturnsFalse proves ordinary, non-boundary error
// text (or empty text) never trips the gate — the nudge stays silent.
func TestTellFor_UnrelatedTextReturnsFalse(t *testing.T) {
	for _, s := range []string{
		"",
		"the model produced an incomplete sentence", // has "model", not "not found"
		"rate limit exceeded, please retry shortly",
		"invalid JSON in your message payload",
		"something unexpected happened while thinking",
	} {
		if gate, ok := TellFor(s); ok {
			t.Errorf("TellFor(%q) = (%q, true), want ok=false", s, gate)
		}
	}
}

// ─── JIT tell-triggered injection: Nudger de-dupe ─────────────────────────────

// TestNudger_FiresOncePerDistinctGate proves the fire-once-per-gate-per-session
// guarantee: the first matching error surfaces the gate, and every later error
// mapping to the SAME gate — even a different wiring signature — is suppressed.
func TestNudger_FiresOncePerDistinctGate(t *testing.T) {
	var n Nudger

	gate, ok := n.NudgeFor(`dial tcp 127.0.0.1:9999: connect: connection refused`)
	if !ok {
		t.Fatal("first NudgeFor should fire for a wiring error")
	}
	if gate != configVsCodeGate {
		t.Fatalf("first NudgeFor gate = %q, want %q", gate, configVsCodeGate)
	}

	// Same error again → deduped.
	if g, ok := n.NudgeFor(`dial tcp 127.0.0.1:9999: connect: connection refused`); ok {
		t.Errorf("second NudgeFor for the same error should dedupe, got (%q, true)", g)
	}
	// A DIFFERENT wiring error that maps to the same gate → also deduped
	// (dedupe is per gate, not per error string).
	if g, ok := n.NudgeFor("gateway returned 401 unauthorized"); ok {
		t.Errorf("a different error mapping to an already-shown gate should dedupe, got (%q, true)", g)
	}
}

// TestNudger_NoTellNoFire proves a Nudger stays silent — and records nothing —
// for error text that carries no tell.
func TestNudger_NoTellNoFire(t *testing.T) {
	var n Nudger
	if g, ok := n.NudgeFor("just a normal sentence with no boundary tell"); ok {
		t.Errorf("NudgeFor should not fire without a tell, got (%q, true)", g)
	}
	// And a real tell afterwards must still fire — the no-tell call left no
	// residue that would suppress it.
	if _, ok := n.NudgeFor("connection refused"); !ok {
		t.Error("NudgeFor should still fire for a real tell after a no-tell call")
	}
}

// TestBuildSystemContext_EnvDisabled_Empty ties the DOJO_PROTOCOL_DISABLED env
// override (resolved by config.Load) to an empty injection context — proving the
// escape hatch reaches all the way through the request builder.
func TestBuildSystemContext_EnvDisabled_Empty(t *testing.T) {
	hermetic(t)
	// Clear the other DOJO_* knobs so a stray shell value can't skew Load().
	for _, k := range []string{
		"DOJO_GATEWAY_URL", "DOJO_GATEWAY_TOKEN", "DOJO_PLUGINS_PATH",
		"DOJO_PROVIDER", "DOJO_DISPOSITION", "DOJO_MODEL", "DOJO_USER_ID",
		"DOJO_PROTOCOL_PATH",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("DOJO_PROTOCOL_DISABLED", "1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Protocol.Enabled {
		t.Fatal("DOJO_PROTOCOL_DISABLED=1 should disable the protocol")
	}
	if got := BuildSystemContext(cfg); got != "" {
		t.Errorf("disabled via env: expected empty context, got %d bytes", len(got))
	}
	// And the injector built from it is inert.
	req := client.ChatRequest{Message: "x"}
	if NewInjector(cfg).Apply(&req) || req.Message != "x" {
		t.Error("env-disabled injector should be inert")
	}
}

// TestBuildSystemContext_EnabledByDefault confirms the default-ON posture: a
// config with no protocol block set at all (as when settings.json omits it and
// defaults() pre-seeds Enabled) still injects.
func TestBuildSystemContext_EnabledByDefaultViaLoad(t *testing.T) {
	hermetic(t)
	for _, k := range []string{
		"DOJO_GATEWAY_URL", "DOJO_GATEWAY_TOKEN", "DOJO_PLUGINS_PATH",
		"DOJO_PROVIDER", "DOJO_DISPOSITION", "DOJO_MODEL", "DOJO_USER_ID",
		"DOJO_PROTOCOL_DISABLED", "DOJO_PROTOCOL_PATH",
	} {
		t.Setenv(k, "")
	}
	cfg, err := config.Load() // no settings.json in the temp HOME → pure defaults
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.Protocol.Enabled {
		t.Fatal("protocol should be enabled by default")
	}
	if got := BuildSystemContext(cfg); got != DefaultDoc() {
		t.Error("default-enabled config should inject the embedded default doc")
	}
}

// ─── JIT tell-triggered injection: TellFor debugging class ────────────────────

// TestTellFor_BuildTestSignaturesMapToDebuggingGate proves build/test/compile/
// panic error text routes to the debug-by-disproof gate — the second tell class.
func TestTellFor_BuildTestSignaturesMapToDebuggingGate(t *testing.T) {
	cases := []struct {
		name string
		err  string
	}{
		{"go test per-test marker", "--- FAIL: TestFoo (0.00s)"},
		{"go test summary line", "FAIL\tgithub.com/DojoGenesis/cli/internal/repl\t0.412s"},
		{"panic header", "panic: runtime error: index out of range [3] with length 2"},
		{"test failed prose", "1 test failed in package ./internal/protocol"},
		{"build failed prose", "build failed: two errors"},
		{"undefined symbol", "./repl.go:12:9: undefined: fooBar"},
		{"cannot use type mismatch", "cannot use x (variable of type int) as string value in assignment"},
		{"compile", "internal compiler error: could not compile package"},
		{"assertion", "assertion failed: expected three, got four"},
		{"case-insensitive", "PANIC: nil pointer dereference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate, ok := TellFor(tc.err)
			if !ok {
				t.Fatalf("TellFor(%q) ok=false, want true", tc.err)
			}
			if gate != debuggingGate {
				t.Errorf("TellFor(%q) gate = %q, want the debugging gate %q", tc.err, gate, debuggingGate)
			}
		})
	}
}

// TestTellFor_DebuggingNearMissesStaySilent guards the build/test class against
// overreach: prose that merely contains "fail" (but none of the tokened shapes)
// must NOT trip the gate.
func TestTellFor_DebuggingNearMissesStaySilent(t *testing.T) {
	for _, s := range []string{
		"the request failed for an unknown reason", // "failed", but not "test failed" / "fail\t" / "--- fail"
		"your prompt failed our safety filter",
		"we could not fulfill that ask",
	} {
		if gate, ok := TellFor(s); ok {
			t.Errorf("TellFor(%q) = (%q, true), want ok=false", s, gate)
		}
	}
}

// TestTellFor_WiringBeatsDebuggingWhenBothPresent proves the precedence: if an
// error text somehow carries both a boundary signature and a build/test one, it
// keeps the config-vs-code framing (wiring is checked first), so a connection
// failure is never re-labeled a logic bug.
func TestTellFor_WiringBeatsDebuggingWhenBothPresent(t *testing.T) {
	gate, ok := TellFor("build failed: dial tcp 127.0.0.1:7340: connection refused")
	if !ok {
		t.Fatal("expected a tell to match")
	}
	if gate != configVsCodeGate {
		t.Errorf("gate = %q, want config-vs-code (wiring precedence)", gate)
	}
}

// TestNudger_DebuggingGateDeDupesAndIsIndependent proves the debugging gate, like
// the config-vs-code gate, surfaces at most once — and that the two are
// independent: showing one does not suppress the other.
func TestNudger_DebuggingGateDeDupesAndIsIndependent(t *testing.T) {
	var n Nudger

	// A wiring error fires config-vs-code.
	if g, ok := n.NudgeFor("connection refused"); !ok || g != configVsCodeGate {
		t.Fatalf("wiring NudgeFor = (%q,%v), want config-vs-code,true", g, ok)
	}
	// A build error still fires — debugging is a distinct gate/key.
	if g, ok := n.NudgeFor("panic: boom"); !ok || g != debuggingGate {
		t.Fatalf("build NudgeFor = (%q,%v), want debugging,true", g, ok)
	}
	// A second, differently-worded build error is deduped (per gate, not string).
	if g, ok := n.NudgeFor("--- FAIL: TestX"); ok {
		t.Errorf("second debugging-class error should dedupe, got (%q,true)", g)
	}
}

// TestNudger_NudgeDebugging_SharesLedgerWithNudgeFor proves the direct
// NudgeDebugging entry point de-dupes against the text-driven NudgeFor path:
// whichever fires first spends the single debugging-gate slot for the session.
func TestNudger_NudgeDebugging_SharesLedgerWithNudgeFor(t *testing.T) {
	// Direct-first: NudgeDebugging fires, then a build-tell NudgeFor is deduped.
	var a Nudger
	if g, ok := a.NudgeDebugging(); !ok || g != debuggingGate {
		t.Fatalf("first NudgeDebugging = (%q,%v), want debugging,true", g, ok)
	}
	if g, ok := a.NudgeDebugging(); ok {
		t.Errorf("second NudgeDebugging should dedupe, got (%q,true)", g)
	}
	if g, ok := a.NudgeFor("panic: later"); ok {
		t.Errorf("NudgeFor debugging class after NudgeDebugging should dedupe, got (%q,true)", g)
	}

	// Text-first: a build-tell NudgeFor fires, then NudgeDebugging is deduped.
	var b Nudger
	if g, ok := b.NudgeFor("build failed"); !ok || g != debuggingGate {
		t.Fatalf("build NudgeFor = (%q,%v), want debugging,true", g, ok)
	}
	if g, ok := b.NudgeDebugging(); ok {
		t.Errorf("NudgeDebugging after a debugging-class NudgeFor should dedupe, got (%q,true)", g)
	}
	// The config-vs-code gate is still available on b — independence holds.
	if g, ok := b.NudgeFor("connection refused"); !ok || g != configVsCodeGate {
		t.Errorf("config-vs-code gate should remain available, got (%q,%v)", g, ok)
	}
}
