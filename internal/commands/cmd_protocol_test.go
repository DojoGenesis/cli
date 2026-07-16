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
	"strings"
	"testing"

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
