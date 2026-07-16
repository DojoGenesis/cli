package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
)

// mustMkdirAll creates dir (and any parents), failing the test on error.
// Every call site in this file uses the same 0755 fixture permissions.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// mustWriteFile writes data to path, failing the test on error. Every call
// site in this file uses the same 0644 fixture permissions.
func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ─── writeSettings ────────────────────────────────────────────────────────────

func TestWriteSettings(t *testing.T) {
	dir := t.TempDir()
	opts := Options{GatewayURL: "http://gateway.test:7340"}

	created, err := writeSettings(dir, opts)
	if err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}

	gw, ok := cfg["gateway"].(map[string]any)
	if !ok {
		t.Fatal("missing gateway key")
	}
	if gw["url"] != "http://gateway.test:7340" {
		t.Errorf("expected gateway.url=http://gateway.test:7340, got %v", gw["url"])
	}
	if gw["timeout"] != "60s" {
		t.Errorf("expected gateway.timeout=60s, got %v", gw["timeout"])
	}

	plugins, ok := cfg["plugins"].(map[string]any)
	if !ok {
		t.Fatal("missing plugins key")
	}
	expectedPluginsPath := filepath.Join(dir, "plugins")
	if plugins["path"] != expectedPluginsPath {
		t.Errorf("expected plugins.path=%s, got %v", expectedPluginsPath, plugins["path"])
	}

	defaults, ok := cfg["defaults"].(map[string]any)
	if !ok {
		t.Fatal("missing defaults key")
	}
	if defaults["disposition"] != "balanced" {
		t.Errorf("expected defaults.disposition=balanced, got %v", defaults["disposition"])
	}
}

func TestWriteSettingsDefaultGatewayURL(t *testing.T) {
	dir := t.TempDir()
	opts := Options{} // no GatewayURL

	created, err := writeSettings(dir, opts)
	if err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}

	gw := cfg["gateway"].(map[string]any)
	if gw["url"] != "http://localhost:7340" {
		t.Errorf("expected default gateway URL, got %v", gw["url"])
	}
}

func TestWriteSettingsIdempotent(t *testing.T) {
	dir := t.TempDir()
	opts := Options{GatewayURL: "http://first:7340"}

	// Write once.
	created, err := writeSettings(dir, opts)
	if err != nil {
		t.Fatalf("first writeSettings: %v", err)
	}
	if !created {
		t.Fatal("expected created=true on first call")
	}

	// Write again without Force — should be skipped.
	opts.GatewayURL = "http://second:7340"
	created, err = writeSettings(dir, opts)
	if err != nil {
		t.Fatalf("second writeSettings: %v", err)
	}
	if created {
		t.Fatal("expected created=false on second call (no Force)")
	}

	// Verify original content unchanged.
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.Contains(string(data), "second") {
		t.Error("settings.json was overwritten despite Force=false")
	}
}

func TestWriteSettingsForce(t *testing.T) {
	dir := t.TempDir()
	opts := Options{GatewayURL: "http://first:7340"}

	if _, err := writeSettings(dir, opts); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}

	opts.GatewayURL = "http://second:7340"
	opts.Force = true
	created, err := writeSettings(dir, opts)
	if err != nil {
		t.Fatalf("force writeSettings: %v", err)
	}
	if !created {
		t.Fatal("expected created=true with Force=true")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(data), "second") {
		t.Error("settings.json was NOT overwritten despite Force=true")
	}
}

// ─── copyPlugins ──────────────────────────────────────────────────────────────

// makeFakePluginSource creates a source directory with the named plugins.
// Each plugin gets a plugin.json and a skills/ subdirectory with a stub skill.
func makeFakePluginSource(t *testing.T, names []string) string {
	t.Helper()
	srcRoot := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(srcRoot, name)
		mustMkdirAll(t, filepath.Join(dir, "skills", "example"))

		pluginJSON := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
		mustWriteFile(t, filepath.Join(dir, "plugin.json"), []byte(pluginJSON))
		mustWriteFile(t, filepath.Join(dir, "skills", "example", "SKILL.md"), []byte("# Example\n"))
	}
	return srcRoot
}

func TestCopyPlugins(t *testing.T) {
	dojoDir := t.TempDir()
	srcRoot := makeFakePluginSource(t, []string{"agent-orchestration", "skill-forge"})

	opts := Options{PluginsSource: srcRoot}
	copied, skipped, errs := copyPlugins(dojoDir, opts)

	// 2 found, 6 missing (logged as skipped with errors)
	if copied != 2 {
		t.Errorf("expected copied=2, got %d", copied)
	}
	// 6 plugins not present in source → skipped
	if skipped != 6 {
		t.Errorf("expected skipped=6 (missing sources), got %d", skipped)
	}
	// 6 errors about missing sources
	if len(errs) != 6 {
		t.Errorf("expected 6 source-not-found errors, got %d: %v", len(errs), errs)
	}

	// Verify the two copied plugins landed correctly.
	for _, name := range []string{"agent-orchestration", "skill-forge"} {
		pjPath := filepath.Join(dojoDir, "plugins", name, "plugin.json")
		if _, err := os.Stat(pjPath); err != nil {
			t.Errorf("expected %s to exist after copy", pjPath)
		}
		skillPath := filepath.Join(dojoDir, "plugins", name, "skills", "example", "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("expected %s to exist after copy", skillPath)
		}
	}
}

func TestCopyPluginsSkipExisting(t *testing.T) {
	dojoDir := t.TempDir()
	srcRoot := makeFakePluginSource(t, []string{"agent-orchestration"})

	// Pre-create the destination plugin directory.
	dstPlugin := filepath.Join(dojoDir, "plugins", "agent-orchestration")
	mustMkdirAll(t, dstPlugin)
	mustWriteFile(t, filepath.Join(dstPlugin, "plugin.json"), []byte(`{"name":"old"}`))

	opts := Options{PluginsSource: srcRoot}
	copied, skipped, _ := copyPlugins(dojoDir, opts)

	// agent-orchestration already exists → skipped, not copied.
	if copied != 0 {
		t.Errorf("expected copied=0, got %d", copied)
	}
	// 1 skipped (exists) + 7 missing from source
	expectedSkipped := 8
	if skipped != expectedSkipped {
		t.Errorf("expected skipped=%d, got %d", expectedSkipped, skipped)
	}

	// Verify the destination was NOT overwritten.
	data, _ := os.ReadFile(filepath.Join(dstPlugin, "plugin.json"))
	if !strings.Contains(string(data), "old") {
		t.Error("existing plugin was overwritten despite Force=false")
	}
}

func TestCopyPluginsForce(t *testing.T) {
	dojoDir := t.TempDir()
	srcRoot := makeFakePluginSource(t, []string{"agent-orchestration"})

	// Pre-create the destination with old content.
	dstPlugin := filepath.Join(dojoDir, "plugins", "agent-orchestration")
	mustMkdirAll(t, dstPlugin)
	mustWriteFile(t, filepath.Join(dstPlugin, "plugin.json"), []byte(`{"name":"old"}`))

	opts := Options{PluginsSource: srcRoot, Force: true}
	copied, _, _ := copyPlugins(dojoDir, opts)

	if copied != 1 {
		t.Errorf("expected copied=1 with Force=true, got %d", copied)
	}

	// Verify destination was overwritten.
	data, _ := os.ReadFile(filepath.Join(dstPlugin, "plugin.json"))
	if strings.Contains(string(data), "old") {
		t.Error("plugin was not overwritten despite Force=true")
	}
}

// ─── writeDispositions ────────────────────────────────────────────────────────

func TestWriteDispositions(t *testing.T) {
	dojoDir := t.TempDir()

	written, err := writeDispositions(dojoDir, false)
	if err != nil {
		t.Fatalf("writeDispositions: %v", err)
	}
	if written != 4 {
		t.Errorf("expected 4 files written, got %d", written)
	}

	expected := []string{"focused.yaml", "balanced.yaml", "exploratory.yaml", "deliberate.yaml"}
	for _, name := range expected {
		path := filepath.Join(dojoDir, "dispositions", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("disposition file %s missing: %v", name, err)
			continue
		}
		// Verify it contains the name field.
		stem := strings.TrimSuffix(name, ".yaml")
		if !strings.Contains(string(data), "name: "+stem) {
			t.Errorf("disposition %s missing 'name: %s' field", name, stem)
		}
	}
}

func TestWriteDispositionsIdempotent(t *testing.T) {
	dojoDir := t.TempDir()

	// First write.
	written, err := writeDispositions(dojoDir, false)
	if err != nil {
		t.Fatalf("first writeDispositions: %v", err)
	}
	if written != 4 {
		t.Fatalf("expected 4 written, got %d", written)
	}

	// Second write without force — should skip all.
	written, err = writeDispositions(dojoDir, false)
	if err != nil {
		t.Fatalf("second writeDispositions: %v", err)
	}
	if written != 0 {
		t.Errorf("expected 0 written on second call (no Force), got %d", written)
	}
}

// ─── writeMCPConfig ───────────────────────────────────────────────────────────

func TestWriteMCPConfig(t *testing.T) {
	dojoDir := t.TempDir()

	wrote, err := writeMCPConfig(dojoDir, false)
	if err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	if !wrote {
		t.Fatal("expected written=true")
	}

	data, err := os.ReadFile(filepath.Join(dojoDir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}

	// Validate it's parseable JSON.
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("mcp.json not valid JSON: %v", err)
	}

	servers, ok := cfg["servers"].([]any)
	if !ok {
		t.Fatal("mcp.json missing 'servers' array")
	}
	if len(servers) != 7 {
		t.Errorf("expected 7 servers, got %d", len(servers))
	}

	// Version check.
	if cfg["version"] != "1.0" {
		t.Errorf("expected version=1.0, got %v", cfg["version"])
	}
}

func TestWriteMCPConfigIdempotent(t *testing.T) {
	dojoDir := t.TempDir()

	wrote, _ := writeMCPConfig(dojoDir, false)
	if !wrote {
		t.Fatal("expected written=true on first call")
	}

	// Overwrite file with marker content.
	mustWriteFile(t, filepath.Join(dojoDir, "mcp.json"), []byte(`{"marker":true}`))

	wrote, _ = writeMCPConfig(dojoDir, false)
	if wrote {
		t.Fatal("expected written=false on second call (no Force)")
	}

	// Marker should still be there.
	data, _ := os.ReadFile(filepath.Join(dojoDir, "mcp.json"))
	if !strings.Contains(string(data), "marker") {
		t.Error("mcp.json was overwritten despite Force=false")
	}
}

// ─── plantSeeds ───────────────────────────────────────────────────────────────

// newMockGateway creates a test HTTP server that handles GET /v1/seeds and POST /v1/seeds.
// existingNames is the list of seed names to return on GET.
func newMockGateway(t *testing.T, existingNames []string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/seeds", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			seeds := make([]map[string]any, 0, len(existingNames))
			for _, name := range existingNames {
				seeds = append(seeds, map[string]any{"id": name, "name": name})
			}
			resp := map[string]any{
				"success": true,
				"count":   len(seeds),
				"seeds":   seeds,
			}
			w.Header().Set("Content-Type", "application/json")
			// Runs on the server's own goroutine — t.Errorf (not Fatalf) is
			// the safe way to surface a failure from here.
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("encode seeds response: %v", err)
			}

		case http.MethodPost:
			var req client.CreateSeedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode create-seed request: %v", err)
			}
			seed := map[string]any{
				"seed": map[string]any{
					"id":      "new-" + req.Name,
					"name":    req.Name,
					"content": req.Content,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(seed); err != nil {
				t.Errorf("encode create-seed response: %v", err)
			}

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux)
}

func TestPlantSeeds(t *testing.T) {
	srv := newMockGateway(t, nil) // no existing seeds
	defer srv.Close()

	gw := client.New(srv.URL, "", "10s")
	planted, skipped, errs := plantSeeds(context.Background(), gw)

	if planted != 5 {
		t.Errorf("expected 5 planted, got %d", planted)
	}
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestPlantSeedsSkipDuplicates(t *testing.T) {
	// Pre-populate with all 5 starter seed names.
	existing := make([]string, len(starterSeeds))
	for i, s := range starterSeeds {
		existing[i] = s.Name
	}

	srv := newMockGateway(t, existing)
	defer srv.Close()

	gw := client.New(srv.URL, "", "10s")
	planted, skipped, errs := plantSeeds(context.Background(), gw)

	if planted != 0 {
		t.Errorf("expected 0 planted (all duplicates), got %d", planted)
	}
	if skipped != 5 {
		t.Errorf("expected 5 skipped, got %d", skipped)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestPlantSeedsPartialDuplicates(t *testing.T) {
	// 2 seeds already exist.
	existing := []string{starterSeeds[0].Name, starterSeeds[2].Name}

	srv := newMockGateway(t, existing)
	defer srv.Close()

	gw := client.New(srv.URL, "", "10s")
	planted, skipped, errs := plantSeeds(context.Background(), gw)

	if planted != 3 {
		t.Errorf("expected 3 planted, got %d", planted)
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestPlantSeedsGatewayUnreachable(t *testing.T) {
	// Point at a closed server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	gw := client.New(srv.URL, "", "2s")
	planted, skipped, errs := plantSeeds(context.Background(), gw)

	if planted != 0 {
		t.Errorf("expected 0 planted on error, got %d", planted)
	}
	if skipped != len(starterSeeds) {
		t.Errorf("expected all seeds skipped on error, got %d", skipped)
	}
	if len(errs) == 0 {
		t.Error("expected at least one error when gateway unreachable")
	}
}

// ─── copyDir ──────────────────────────────────────────────────────────────────

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a nested structure with a .git dir (should be skipped).
	mustMkdirAll(t, filepath.Join(src, "sub"))
	mustMkdirAll(t, filepath.Join(src, ".git"))
	mustWriteFile(t, filepath.Join(src, "file.txt"), []byte("hello"))
	mustWriteFile(t, filepath.Join(src, "sub", "nested.txt"), []byte("world"))
	mustWriteFile(t, filepath.Join(src, ".git", "HEAD"), []byte("ref: refs/heads/main"))

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Regular files should exist.
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil || string(data) != "hello" {
		t.Errorf("file.txt not copied correctly: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dst, "sub", "nested.txt"))
	if err != nil || string(data) != "world" {
		t.Errorf("sub/nested.txt not copied correctly: %v", err)
	}

	// .git should be excluded.
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory was copied but should have been skipped")
	}
}
