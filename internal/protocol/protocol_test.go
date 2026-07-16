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
