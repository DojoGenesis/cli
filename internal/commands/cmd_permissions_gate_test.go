package commands

// cmd_permissions_gate_test.go — end-to-end tests for the permissions gate on
// risky write/exec commands (code.undo, plugin.install, plugin.rm,
// craft.scaffold). New file per the phase-2 track rules: existing test files
// are never edited; shared helpers from them are reused —
// testRegistry (commands_test.go), initTempGitRepo / runGitSetup /
// withTempCwd / withStdin (cmd_code_test.go), and captureProtocolStdout
// (cmd_protocol_test.go), which also captures the gookit/color writer.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/config"
)

// permTestRegistry builds the standard test registry and applies the given
// permissions config to it.
func permTestRegistry(t *testing.T, mode string, allowed []string) *Registry {
	t.Helper()
	r, _ := testRegistry()
	r.cfg.Permissions = config.PermissionsConfig{Mode: mode, Allowed: allowed}
	return r
}

// dirtyGitRepo creates a temp git repo (chdir'd into, cleaned up
// automatically), commits tracked.txt at "v1\n", then leaves an unstaged edit
// "v2 unstaged\n" — the exact state /code undo exists to revert.
func dirtyGitRepo(t *testing.T) {
	t.Helper()
	initTempGitRepo(t)
	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("setup tracked.txt: %v", err)
	}
	runGitSetup(t, "add", "tracked.txt")
	runGitSetup(t, "commit", "-q", "-m", "initial")
	if err := os.WriteFile("tracked.txt", []byte("v2 unstaged\n"), 0o600); err != nil {
		t.Fatalf("unstaged edit: %v", err)
	}
}

func mustReadTracked(t *testing.T) string {
	t.Helper()
	got, err := os.ReadFile("tracked.txt")
	if err != nil {
		t.Fatalf("reading tracked.txt: %v", err)
	}
	return string(got)
}

// ─── code.undo ────────────────────────────────────────────────────────────────

// TestPermGateCodeUndoAllowlistDeny — allowlist mode without "code.undo"
// listed refuses the revert: the Explain message reaches output, the handler
// exits cleanly (nil error), and the working tree is untouched.
func TestPermGateCodeUndoAllowlistDeny(t *testing.T) {
	dirtyGitRepo(t)
	// An unrelated pattern proves deny comes from non-matching, not an empty list.
	r := permTestRegistry(t, "allowlist", []string{"craft.*"})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "code undo")
	})
	if err != nil {
		t.Fatalf("denied /code undo should exit cleanly, got error: %v", err)
	}
	if !strings.Contains(out, `permission denied: "code.undo"`) {
		t.Errorf("output missing Explain message for code.undo:\n%s", out)
	}
	if got := mustReadTracked(t); got != "v2 unstaged\n" {
		t.Errorf("tracked.txt = %q after denied undo, want untouched %q", got, "v2 unstaged\n")
	}
}

// TestPermGateCodeUndoAllowlistAllow — with "code.undo" allowlisted the gate
// lets the handler proceed AND skips the interactive prompt entirely: no
// stdin is provided here, so if any prompt were still attempted the answer
// would read as empty/cancel and the file would not revert.
func TestPermGateCodeUndoAllowlistAllow(t *testing.T) {
	dirtyGitRepo(t)
	r := permTestRegistry(t, "allowlist", []string{"code.undo"})

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "code undo")
	})
	if err != nil {
		t.Fatalf("allowlisted /code undo: unexpected error: %v", err)
	}
	if got := mustReadTracked(t); got != "v1\n" {
		t.Errorf("tracked.txt = %q after allowlisted undo, want reverted %q", got, "v1\n")
	}
}

// TestPermGateCodeUndoDefaultNonTTYRefuses — in default (Confirm) mode with a
// non-terminal stdin, ConfirmInteractive must short-circuit to refusal
// WITHOUT reading stdin: an affirmative "y" sits unread in the pipe, and the
// tree must stay untouched with the Explain message printed.
func TestPermGateCodeUndoDefaultNonTTYRefuses(t *testing.T) {
	dirtyGitRepo(t)
	r := permTestRegistry(t, "default", nil)

	withStdin(t, "y\n") // a pipe is never a terminal; the "y" must not be consumed

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "code undo")
	})
	if err != nil {
		t.Fatalf("non-TTY refused /code undo should exit cleanly, got error: %v", err)
	}
	if !strings.Contains(out, `permission denied: "code.undo"`) {
		t.Errorf("output missing Explain message for non-TTY confirm refusal:\n%s", out)
	}
	if got := mustReadTracked(t); got != "v2 unstaged\n" {
		t.Errorf("tracked.txt = %q, want untouched %q — the piped 'y' must not authorize a revert", got, "v2 unstaged\n")
	}
}

// ─── plugin.install ───────────────────────────────────────────────────────────

// TestPermGatePluginInstallAllowlistDeny — allowlist mode without
// "plugin.install" listed refuses before any clone activity: Explain reaches
// output, no "Cloning" line is printed, and the handler exits cleanly.
func TestPermGatePluginInstallAllowlistDeny(t *testing.T) {
	r := permTestRegistry(t, "allowlist", []string{"code.undo"})
	r.cfg.Plugins.Path = t.TempDir()

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "plugin install https://example.invalid/some-plugin.git")
	})
	if err != nil {
		t.Fatalf("denied /plugin install should exit cleanly, got error: %v", err)
	}
	if !strings.Contains(out, `permission denied: "plugin.install"`) {
		t.Errorf("output missing Explain message for plugin.install:\n%s", out)
	}
	if strings.Contains(out, "Cloning") {
		t.Errorf("denied install must not reach the clone step, but output shows Cloning:\n%s", out)
	}
	entries, readErr := os.ReadDir(r.cfg.Plugins.Path)
	if readErr != nil {
		t.Fatalf("reading plugins dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("plugins dir should stay empty after a denied install, has %d entries", len(entries))
	}
}

// TestPermGatePluginInstallAllowlistAllow — with "plugin.install" allowlisted
// the gate lets the handler proceed to the actual clone (no prompt: stdin is
// not consulted). The clone itself fails — the URL points at a nonexistent
// local path — which is exactly the proof that execution got PAST the gate:
// the failure is git's, not the permission system's.
func TestPermGatePluginInstallAllowlistAllow(t *testing.T) {
	r := permTestRegistry(t, "allowlist", []string{"plugin.install"})
	r.cfg.Plugins.Path = t.TempDir()
	bogusURL := filepath.Join(t.TempDir(), "no-such-repo.git") // local path: fails fast, no network

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "plugin install "+bogusURL)
	})
	if err == nil {
		t.Fatal("expected a git clone failure error, got nil")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("error = %v; want a 'git clone failed' error proving the gate was passed", err)
	}
	if strings.Contains(out, "permission denied") {
		t.Errorf("allowlisted install must not print a permission denial:\n%s", out)
	}
	if !strings.Contains(out, "Cloning") {
		t.Errorf("allowlisted install should reach the Cloning step:\n%s", out)
	}
}

// ─── plugin.rm ────────────────────────────────────────────────────────────────

// TestPermGatePluginRmAllowlistDeny — the gate fires before Uninstall even
// looks for the plugin: a denied rm of a nonexistent plugin returns nil (no
// "not found" error) with the Explain message printed.
func TestPermGatePluginRmAllowlistDeny(t *testing.T) {
	r := permTestRegistry(t, "allowlist", nil)
	r.cfg.Plugins.Path = t.TempDir()

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "plugin rm ghost-plugin")
	})
	if err != nil {
		t.Fatalf("denied /plugin rm should exit cleanly, got error: %v", err)
	}
	if !strings.Contains(out, `permission denied: "plugin.rm"`) {
		t.Errorf("output missing Explain message for plugin.rm:\n%s", out)
	}
}

// TestPermGatePluginRmYoloAllow — yolo mode passes the gate for everything;
// the surviving "not found" error from Uninstall proves the handler executed
// past the gate.
func TestPermGatePluginRmYoloAllow(t *testing.T) {
	r := permTestRegistry(t, "yolo", nil)
	r.cfg.Plugins.Path = t.TempDir()

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "plugin rm ghost-plugin")
	})
	if err == nil {
		t.Fatal("expected 'not found' error from Uninstall after passing the yolo gate, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v; want the Uninstall 'not found' error proving the gate was passed", err)
	}
	if strings.Contains(out, "permission denied") {
		t.Errorf("yolo mode must not print a permission denial:\n%s", out)
	}
}

// ─── craft.scaffold ───────────────────────────────────────────────────────────

// TestPermGateCraftScaffoldAllowlistDeny — a denied scaffold prints Explain
// after the create-plan listing and writes nothing.
func TestPermGateCraftScaffoldAllowlistDeny(t *testing.T) {
	withTempCwd(t)
	r := permTestRegistry(t, "allowlist", []string{"plugin.install"})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "craft scaffold minimal")
	})
	if err != nil {
		t.Fatalf("denied /craft scaffold should exit cleanly, got error: %v", err)
	}
	if !strings.Contains(out, `permission denied: "craft.scaffold"`) {
		t.Errorf("output missing Explain message for craft.scaffold:\n%s", out)
	}
	if _, statErr := os.Stat("go.mod"); statErr == nil {
		t.Error("go.mod was created despite the scaffold being denied")
	}
	if _, statErr := os.Stat("src"); statErr == nil {
		t.Error("src/ was created despite the scaffold being denied")
	}
}

// TestPermGateCraftScaffoldGlobAllow — "craft.*" in the allowlist covers
// craft.scaffold via the glob form; the scaffold proceeds prompt-free and
// actually writes its files.
func TestPermGateCraftScaffoldGlobAllow(t *testing.T) {
	withTempCwd(t)
	r := permTestRegistry(t, "allowlist", []string{"craft.*"})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "craft scaffold minimal")
	})
	if err != nil {
		t.Fatalf("glob-allowlisted /craft scaffold: unexpected error: %v", err)
	}
	if strings.Contains(out, "permission denied") {
		t.Errorf("glob-allowlisted scaffold must not print a permission denial:\n%s", out)
	}
	if _, statErr := os.Stat("go.mod"); statErr != nil {
		t.Errorf("go.mod missing after allowed scaffold: %v", statErr)
	}
	if info, statErr := os.Stat("src"); statErr != nil || !info.IsDir() {
		t.Errorf("src/ missing after allowed scaffold (err=%v)", statErr)
	}
}
