package commands

// cmd_protocol_test.go — tests for /protocol: portable catalog resolution with
// an embedded fallback, status/harnesses rendering, and the install safety
// gates (unratified and not-locally-available both route to operator guidance,
// never a crash and never a ke shell-out).
//
// HOME is isolated per-test (in addition to the package-wide TestMain) so the
// home-relative catalog/plugin candidates can never resolve to a real workspace
// checkout on the developer's machine.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/protocol"
	gcolor "github.com/gookit/color"
)

// captureProtocolStdout redirects both os.Stdout (for fmt.*) and the
// gookit/color writer (for gcolor.*.Print, which writes to the library's own
// cached output, not os.Stdout) for the duration of fn, and returns everything
// written.
func captureProtocolStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = wp
	gcolor.SetOutput(wp)
	runErr := fn()
	gcolor.ResetOutput()
	os.Stdout = old
	_ = wp.Close()
	data, _ := io.ReadAll(rp)
	_ = rp.Close()
	return string(data), runErr
}

func protoWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// clearProtocolEnv neutralizes every env var the command reads so a test starts
// from a known "no override, no workspace" baseline.
func clearProtocolEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KE_CATALOG_PATH", "")
	t.Setenv("KE_HARNESS_SOURCE", "")
	t.Setenv("DOJO_PLUGINS_SOURCE", "")
}

// ─── portable catalog resolution ──────────────────────────────────────────────

func TestProtocolLoadHarnessesEmbeddedFallback(t *testing.T) {
	clearProtocolEnv(t) // KE_CATALOG_PATH unset + HOME has no dojo-ke checkout

	list, source := loadHarnesses()
	if len(list) != len(embeddedHarnesses) {
		t.Fatalf("expected %d embedded harnesses, got %d", len(embeddedHarnesses), len(list))
	}
	if !strings.Contains(source, "embedded snapshot") {
		t.Errorf("expected embedded-snapshot source, got %q", source)
	}

	var kata *harnessInfo
	for i := range list {
		if list[i].ID == "kata-harness" {
			kata = &list[i]
		}
	}
	if kata == nil {
		t.Fatal("kata-harness missing from embedded snapshot")
	}
	if !kata.Ratified {
		t.Error("kata-harness must be ratified in the embedded snapshot")
	}
}

func TestProtocolLoadHarnessesMissingOverrideFallsBack(t *testing.T) {
	clearProtocolEnv(t)
	// Point the override at a file that does not exist → still fall back.
	t.Setenv("KE_CATALOG_PATH", filepath.Join(t.TempDir(), "does-not-exist.json"))

	list, source := loadHarnesses()
	if len(list) != len(embeddedHarnesses) {
		t.Fatalf("expected embedded fallback (%d), got %d", len(embeddedHarnesses), len(list))
	}
	if !strings.Contains(source, "embedded snapshot") {
		t.Errorf("expected embedded snapshot on a missing override, got %q", source)
	}
}

func TestProtocolLoadHarnessesReadsCatalogOverride(t *testing.T) {
	clearProtocolEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	protoWriteFile(t, path, `[
      {"id":"kata-harness","name":"Kata Harness","kind":"harness","status":"draft","tagline":"timed rolls","design_record":{"ratified":true}},
      {"id":"dojo-build-dag","name":"Build-DAG","kind":"harness","status":"draft","tagline":"gate build","design_record":{"ratified":false}},
      {"id":"dojo-tirada","name":"Dojo Tirada","kind":"edition","status":"published","tagline":"issue 1"}
    ]`)
	t.Setenv("KE_CATALOG_PATH", path)

	list, source := loadHarnesses()
	if len(list) != 3 {
		t.Fatalf("expected 3 rows parsed from catalog, got %d", len(list))
	}
	if !strings.Contains(source, path) {
		t.Errorf("source should name the catalog path, got %q", source)
	}
	// design_record.ratified drives the flag; a published edition is ratified too.
	byID := map[string]harnessInfo{}
	for _, h := range list {
		byID[h.ID] = h
	}
	if !byID["kata-harness"].Ratified {
		t.Error("kata-harness (design_record.ratified=true) should be ratified")
	}
	if byID["dojo-build-dag"].Ratified {
		t.Error("dojo-build-dag (design_record.ratified=false) should be unratified")
	}
	if !byID["dojo-tirada"].Ratified {
		t.Error("published edition dojo-tirada should count as ratified")
	}
}

func TestProtocolLoadHarnessesMalformedCatalogFallsBack(t *testing.T) {
	clearProtocolEnv(t)
	path := filepath.Join(t.TempDir(), "catalog.json")
	protoWriteFile(t, path, `{ this is not valid json `)
	t.Setenv("KE_CATALOG_PATH", path)

	list, source := loadHarnesses()
	if len(list) != len(embeddedHarnesses) {
		t.Fatalf("malformed catalog should fall back to embedded (%d), got %d", len(embeddedHarnesses), len(list))
	}
	if !strings.Contains(source, "malformed") {
		t.Errorf("expected a 'malformed' note in the source, got %q", source)
	}
}

// ─── status / harnesses rendering ─────────────────────────────────────────────

func TestProtocolStatusRenders(t *testing.T) {
	clearProtocolEnv(t)
	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")
	r.cfg.Protocol.Enabled = true

	out, err := captureProtocolStdout(t, r.protocolStatus)
	if err != nil {
		t.Fatalf("/protocol status returned error: %v", err)
	}
	if !strings.Contains(out, "kata-harness") {
		t.Errorf("status output should mention kata-harness; got:\n%s", out)
	}
	// Route through Dispatch too, to prove registration + the default subcommand.
	if err := r.Dispatch(context.Background(), "protocol"); err != nil {
		t.Fatalf("/protocol dispatch returned error: %v", err)
	}
	if err := r.Dispatch(context.Background(), "protocol status"); err != nil {
		t.Fatalf("/protocol status dispatch returned error: %v", err)
	}
}

func TestProtocolHarnessesRenders(t *testing.T) {
	clearProtocolEnv(t)
	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")

	out, err := captureProtocolStdout(t, r.protocolHarnesses)
	if err != nil {
		t.Fatalf("/protocol harnesses returned error: %v", err)
	}
	for _, want := range []string{"kata-harness", "dojo-build-dag", "not installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("harnesses output missing %q; got:\n%s", want, out)
		}
	}
	if err := r.Dispatch(context.Background(), "protocol harnesses"); err != nil {
		t.Fatalf("/protocol harnesses dispatch returned error: %v", err)
	}
}

// ─── install safety gates ─────────────────────────────────────────────────────

func TestProtocolInstallUnratifiedGivesGuidance(t *testing.T) {
	clearProtocolEnv(t)
	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")

	// dojo-build-dag is draft + unratified in the embedded snapshot.
	out, err := captureProtocolStdout(t, func() error {
		return r.protocolInstall(context.Background(), "dojo-build-dag", true)
	})
	if err != nil {
		t.Fatalf("unratified install should print guidance, not error: %v", err)
	}
	if !strings.Contains(out, "operator-gated") {
		t.Errorf("expected operator-gated guidance; got:\n%s", out)
	}
	if strings.Contains(out, "Installed") {
		t.Errorf("unratified harness must not report a successful install; got:\n%s", out)
	}
}

func TestProtocolInstallRatifiedButNoLocalPluginGivesGuidance(t *testing.T) {
	clearProtocolEnv(t) // HOME temp + no source envs → nothing resolves locally
	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")

	// kata-harness is ratified, but no plugin exists on this synthetic machine.
	out, err := captureProtocolStdout(t, func() error {
		return r.protocolInstall(context.Background(), "kata-harness", true)
	})
	if err != nil {
		t.Fatalf("ratified-but-not-local install should give guidance, not error: %v", err)
	}
	if !strings.Contains(out, "operator-gated") || !strings.Contains(out, "no local plugin") {
		t.Errorf("expected 'no local plugin' operator guidance; got:\n%s", out)
	}
}

func TestProtocolInstallUnknownHarnessErrors(t *testing.T) {
	clearProtocolEnv(t)
	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")

	err := r.Dispatch(context.Background(), "protocol install nope-not-real --yes")
	if err == nil {
		t.Fatal("expected an error for an unknown harness")
	}
	if !strings.Contains(err.Error(), "unknown harness") {
		t.Errorf("expected 'unknown harness' error, got %v", err)
	}
}

func TestProtocolInstallCopiesLocalRatifiedPlugin(t *testing.T) {
	clearProtocolEnv(t)

	// Build a fake, local, ratified kata-harness plugin the resolver will find
	// via KE_HARNESS_SOURCE (candidate = <src>/<id>).
	srcRoot := t.TempDir()
	pluginDir := filepath.Join(srcRoot, "kata-harness")
	protoWriteFile(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		`{"name":"kata-harness","version":"0.1.0"}`)
	protoWriteFile(t, filepath.Join(pluginDir, "skills", "roll", "SKILL.md"), "# roll")
	// A .git dir that must be skipped by the copy.
	protoWriteFile(t, filepath.Join(pluginDir, ".git", "config"), "[core]")
	t.Setenv("KE_HARNESS_SOURCE", srcRoot)

	r, _ := testRegistry()
	r.cfg.Plugins.Path = filepath.Join(t.TempDir(), "plugins")

	out, err := captureProtocolStdout(t, func() error {
		return r.protocolInstall(context.Background(), "kata-harness", true) // --yes
	})
	if err != nil {
		t.Fatalf("install of a ratified local plugin should succeed: %v", err)
	}
	if !strings.Contains(out, "Installed") {
		t.Errorf("expected an install-success line; got:\n%s", out)
	}

	// Manifest + nested content copied; .git skipped.
	dst := filepath.Join(r.cfg.Plugins.Path, "kata-harness")
	if _, statErr := os.Stat(filepath.Join(dst, ".claude-plugin", "plugin.json")); statErr != nil {
		t.Fatalf("expected copied manifest under %s: %v", dst, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dst, "skills", "roll", "SKILL.md")); statErr != nil {
		t.Errorf("expected nested skill file to be copied: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dst, ".git")); statErr == nil {
		t.Error(".git should have been skipped by the copy, but it exists in the destination")
	}

	// Now /protocol status must observe the install.
	if !r.harnessInstalled("kata-harness") {
		t.Error("harnessInstalled should report kata-harness installed after copy")
	}

	// A second install attempt should refuse to clobber, not re-copy.
	out2, err2 := captureProtocolStdout(t, func() error {
		return r.protocolInstall(context.Background(), "kata-harness", true)
	})
	if err2 != nil {
		t.Fatalf("second install should be a no-op, not an error: %v", err2)
	}
	if !strings.Contains(out2, "already installed") {
		t.Errorf("expected an 'already installed' message on re-install; got:\n%s", out2)
	}
}

// isPluginDir must accept both manifest locations and reject a bare directory.
func TestProtocolIsPluginDir(t *testing.T) {
	root := t.TempDir()

	nested := filepath.Join(root, "nested")
	protoWriteFile(t, filepath.Join(nested, ".claude-plugin", "plugin.json"), `{"name":"x"}`)
	if !isPluginDir(nested) {
		t.Error("expected isPluginDir true for a .claude-plugin/plugin.json layout")
	}

	rooted := filepath.Join(root, "rooted")
	protoWriteFile(t, filepath.Join(rooted, "plugin.json"), `{"name":"y"}`)
	if !isPluginDir(rooted) {
		t.Error("expected isPluginDir true for a root plugin.json layout")
	}

	bare := filepath.Join(root, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	if isPluginDir(bare) {
		t.Error("expected isPluginDir false for a directory with no manifest")
	}
}

// ─── /protocol show + /protocol edit test helpers ─────────────────────────────

// protocolHermeticCwd composes clearProtocolEnv with a fresh, empty cwd so
// /protocol show and /protocol edit resolution sees no project ./DOJO.md and
// no ~/.dojo/DOJO.md unless the test writes one. Returns the (unresolved)
// temp cwd. t.Chdir auto-restores at test end (same contract as t.Setenv), so
// — like every other test in this file — this cannot run in parallel.
func protocolHermeticCwd(t *testing.T) string {
	t.Helper()
	clearProtocolEnv(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	return cwd
}

// withColorDisabled forces gcolor.Enable off for the duration of a test so
// mdrender.RenderMarkdown returns raw markdown verbatim instead of
// glamour-styled ANSI output (see internal/mdrender/mdrender_test.go),
// letting assertions match literal doc text. Restores the previous value on
// cleanup since gcolor.Enable is shared package-level state.
func withColorDisabled(t *testing.T) {
	t.Helper()
	prev := gcolor.Enable
	gcolor.Enable = false
	t.Cleanup(func() { gcolor.Enable = prev })
}

// stubEditorScript writes a POSIX shell script to dir that appends every
// argument it receives, one per line, to markerPath — a minimal stand-in for
// a real $EDITOR that lets a test observe exactly what protocolEdit invoked
// it with, without opening a real interactive program. exitCode controls the
// script's own exit status so failure propagation can be exercised too.
func stubEditorScript(t *testing.T, dir, markerPath string, exitCode int) string {
	t.Helper()
	script := filepath.Join(dir, "fake-editor.sh")
	body := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> \"" + markerPath + "\"; done\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub editor script: %v", err)
	}
	return script
}

// ─── /protocol show ────────────────────────────────────────────────────────────

func TestProtocolShowEmbeddedDefault(t *testing.T) {
	protocolHermeticCwd(t) // empty HOME + empty cwd → no overlay anywhere
	withColorDisabled(t)
	r, _ := testRegistry()
	r.cfg.Protocol.Enabled = true

	out, err := captureProtocolStdout(t, r.protocolShow)
	if err != nil {
		t.Fatalf("/protocol show returned error: %v", err)
	}
	if !strings.Contains(out, "embedded default") {
		t.Errorf("expected an 'embedded default' source label; got:\n%s", out)
	}
	if !strings.Contains(out, "Dojo Genius Protocol") {
		t.Errorf("expected the embedded doc's heading to be rendered; got:\n%s", out)
	}
	if !strings.Contains(out, protocol.DefaultDoc()) {
		t.Errorf("expected the raw embedded doc body verbatim (color disabled); got:\n%s", out)
	}
}

func TestProtocolShowProjectOverlay(t *testing.T) {
	cwd := protocolHermeticCwd(t)
	withColorDisabled(t)
	const custom = "# My Custom Protocol\n\nDo the thing my way.\n"
	protoWriteFile(t, filepath.Join(cwd, "DOJO.md"), custom)

	r, _ := testRegistry()
	out, err := captureProtocolStdout(t, r.protocolShow)
	if err != nil {
		t.Fatalf("/protocol show returned error: %v", err)
	}

	// os.Getwd() (called inside protocolShow right after t.Chdir(cwd)) returns
	// the same string t.TempDir() produced here — verified empirically; Go's
	// Getwd on this platform does not re-resolve the /var vs /private/var
	// symlink once the process has already chdir'd into it.
	wantPath := filepath.Join(cwd, "DOJO.md")
	if !strings.Contains(out, wantPath) {
		t.Errorf("expected source line to name %q; got:\n%s", wantPath, out)
	}
	if !strings.Contains(out, "My Custom Protocol") {
		t.Errorf("expected the project overlay's own content, not the embedded default; got:\n%s", out)
	}
	if strings.Contains(out, "Dojo Genius Protocol") {
		t.Errorf("project overlay should shadow the embedded default entirely; got:\n%s", out)
	}
}

func TestProtocolShowDisabledNotesInjection(t *testing.T) {
	protocolHermeticCwd(t)
	withColorDisabled(t)
	r, _ := testRegistry()
	r.cfg.Protocol.Enabled = false

	out, err := captureProtocolStdout(t, r.protocolShow)
	if err != nil {
		t.Fatalf("/protocol show returned error: %v", err)
	}
	if !strings.Contains(out, "protocol.enabled = false") {
		t.Errorf("expected a disabled-protocol note; got:\n%s", out)
	}
}

func TestProtocolShowDispatch(t *testing.T) {
	protocolHermeticCwd(t)
	withColorDisabled(t)
	r, _ := testRegistry()

	if err := r.Dispatch(context.Background(), "protocol show"); err != nil {
		t.Fatalf("/protocol show dispatch returned error: %v", err)
	}
}

// ─── protocolResolveEditTarget ─────────────────────────────────────────────────

func TestProtocolResolveEditTargetProjectOverlayWins(t *testing.T) {
	cwd := protocolHermeticCwd(t)
	projectPath := filepath.Join(cwd, "DOJO.md")
	protoWriteFile(t, projectPath, "# Project override\n")
	dojoDir := filepath.Join(t.TempDir(), ".dojo")

	got, err := protocolResolveEditTarget(cwd, dojoDir)
	if err != nil {
		t.Fatalf("protocolResolveEditTarget returned error: %v", err)
	}
	if got != projectPath {
		t.Errorf("got %q; want project overlay %q", got, projectPath)
	}
	if _, statErr := os.Stat(filepath.Join(dojoDir, "DOJO.md")); statErr == nil {
		t.Error("dojoDir/DOJO.md should not have been created when the project overlay already wins")
	}
}

func TestProtocolResolveEditTargetCreatesDefaultOverlay(t *testing.T) {
	cwd := protocolHermeticCwd(t) // no ./DOJO.md in cwd
	dojoDir := filepath.Join(t.TempDir(), ".dojo")

	got, err := protocolResolveEditTarget(cwd, dojoDir)
	if err != nil {
		t.Fatalf("protocolResolveEditTarget returned error: %v", err)
	}
	want := filepath.Join(dojoDir, "DOJO.md")
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	data, readErr := os.ReadFile(want)
	if readErr != nil {
		t.Fatalf("expected %s to have been created: %v", want, readErr)
	}
	if string(data) != protocol.DefaultDoc() {
		t.Error("newly created overlay should contain the embedded default doc verbatim")
	}
}

func TestProtocolResolveEditTargetPreservesExistingDojoOverlay(t *testing.T) {
	cwd := protocolHermeticCwd(t) // no ./DOJO.md in cwd
	dojoDir := filepath.Join(t.TempDir(), ".dojo")
	dojoOverlay := filepath.Join(dojoDir, "DOJO.md")
	const custom = "# Already customized\n"
	protoWriteFile(t, dojoOverlay, custom)

	got, err := protocolResolveEditTarget(cwd, dojoDir)
	if err != nil {
		t.Fatalf("protocolResolveEditTarget returned error: %v", err)
	}
	if got != dojoOverlay {
		t.Errorf("got %q; want %q", got, dojoOverlay)
	}
	data, readErr := os.ReadFile(dojoOverlay)
	if readErr != nil {
		t.Fatalf("re-reading %s: %v", dojoOverlay, readErr)
	}
	if string(data) != custom {
		t.Errorf("existing overlay must never be clobbered; got %q, want %q", string(data), custom)
	}
}

// ─── /protocol edit ─────────────────────────────────────────────────────────────

func TestProtocolEditNoEditorPrintsGuidance(t *testing.T) {
	protocolHermeticCwd(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	r, _ := testRegistry()

	out, err := captureProtocolStdout(t, func() error {
		return r.protocolEdit(context.Background())
	})
	if err != nil {
		t.Fatalf("no $EDITOR/$VISUAL should print guidance, not error: %v", err)
	}
	if !strings.Contains(out, "set $EDITOR") {
		t.Errorf("expected guidance to set $EDITOR; got:\n%s", out)
	}
	// dojoDir is HOME-derived (os.Getenv passthrough, no symlink resolution),
	// so — unlike the cwd/os.Getwd() case in TestProtocolShowProjectOverlay —
	// this string is guaranteed to match byte-for-byte with what protocolEdit
	// printed.
	wantPath := filepath.Join(config.DojoDir(), "DOJO.md")
	if !strings.Contains(out, wantPath) {
		t.Errorf("expected the resolved overlay path %q in guidance; got:\n%s", wantPath, out)
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Errorf("expected the overlay to have been created even without an editor: %v", statErr)
	}
}

func TestProtocolEditDispatchNoEditor(t *testing.T) {
	protocolHermeticCwd(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	r, _ := testRegistry()

	if err := r.Dispatch(context.Background(), "protocol edit"); err != nil {
		t.Fatalf("/protocol edit dispatch with no editor set should not error: %v", err)
	}
}

func TestProtocolEditLaunchesEditorWithResolvedTarget(t *testing.T) {
	protocolHermeticCwd(t)
	marker := filepath.Join(t.TempDir(), "editor-args.log")
	script := stubEditorScript(t, t.TempDir(), marker, 0)
	t.Setenv("EDITOR", script)
	t.Setenv("VISUAL", "")

	r, _ := testRegistry()
	out, err := captureProtocolStdout(t, func() error {
		return r.protocolEdit(context.Background())
	})
	if err != nil {
		t.Fatalf("protocolEdit with a working stub editor should not error: %v", err)
	}
	if !strings.Contains(out, "next session") {
		t.Errorf("expected a 'takes effect next session' note; got:\n%s", out)
	}

	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("expected the stub editor to have run and logged its args: %v", readErr)
	}
	gotArgs := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wantTarget := filepath.Join(config.DojoDir(), "DOJO.md")
	if len(gotArgs) != 1 || gotArgs[0] != wantTarget {
		t.Errorf("stub editor args = %v; want exactly [%s]", gotArgs, wantTarget)
	}
}

func TestProtocolEditSplitsMultiWordEditor(t *testing.T) {
	protocolHermeticCwd(t)
	marker := filepath.Join(t.TempDir(), "editor-args.log")
	script := stubEditorScript(t, t.TempDir(), marker, 0)
	// A common real-world shape ("code --wait") — the binary and its flags
	// must be split apart from the target path, not passed as one argv token.
	t.Setenv("EDITOR", script+" --wait --flag2")
	t.Setenv("VISUAL", "")

	r, _ := testRegistry()
	if _, err := captureProtocolStdout(t, func() error {
		return r.protocolEdit(context.Background())
	}); err != nil {
		t.Fatalf("protocolEdit with a multi-word $EDITOR should not error: %v", err)
	}

	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("expected the stub editor to have run: %v", readErr)
	}
	gotArgs := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wantTarget := filepath.Join(config.DojoDir(), "DOJO.md")
	wantArgs := []string{"--wait", "--flag2", wantTarget}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("stub editor args = %v; want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Errorf("arg[%d] = %q; want %q", i, gotArgs[i], wantArgs[i])
		}
	}
}

func TestProtocolEditEditorFailurePropagates(t *testing.T) {
	protocolHermeticCwd(t)
	marker := filepath.Join(t.TempDir(), "editor-args.log")
	script := stubEditorScript(t, t.TempDir(), marker, 7)
	t.Setenv("EDITOR", script)
	t.Setenv("VISUAL", "")

	r, _ := testRegistry()
	_, err := captureProtocolStdout(t, func() error {
		return r.protocolEdit(context.Background())
	})
	if err == nil {
		t.Fatal("expected protocolEdit to surface a nonzero-exit editor as an error")
	}
	if !strings.Contains(err.Error(), "launch editor") {
		t.Errorf("expected a 'launch editor' error; got: %v", err)
	}
}

func TestProtocolEditDispatchWithStubEditor(t *testing.T) {
	protocolHermeticCwd(t)
	marker := filepath.Join(t.TempDir(), "editor-args.log")
	script := stubEditorScript(t, t.TempDir(), marker, 0)
	t.Setenv("EDITOR", script)
	t.Setenv("VISUAL", "")

	r, _ := testRegistry()
	if err := r.Dispatch(context.Background(), "protocol edit"); err != nil {
		t.Fatalf("/protocol edit dispatch with a working stub editor should not error: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("expected the stub editor to have run via Dispatch: %v", statErr)
	}
}
