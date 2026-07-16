package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Load defaults ────────────────────────────────────────────────────────────

func TestLoad_NoSettingsFile_ReturnsDefaults(t *testing.T) {
	// Point the dojo dir at a temp directory that has no settings.json.
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	// Clear env overrides that might bleed in from the test environment.
	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.Gateway.URL != "http://localhost:7340" {
		t.Errorf("default Gateway.URL: got %q, want %q", cfg.Gateway.URL, "http://localhost:7340")
	}
	if cfg.Gateway.Timeout != "60s" {
		t.Errorf("default Gateway.Timeout: got %q, want %q", cfg.Gateway.Timeout, "60s")
	}
	if cfg.Defaults.Disposition != "balanced" {
		t.Errorf("default Disposition: got %q, want %q", cfg.Defaults.Disposition, "balanced")
	}
}

// ─── Load with file overrides ─────────────────────────────────────────────────

func TestLoad_WithSettingsFile_AppliesOverrides(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	// Ensure no env overrides interfere.
	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	// Create ~/.dojo/settings.json with custom values.
	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	overrides := map[string]any{
		"gateway": map[string]any{
			"url":     "http://custom-gateway:8080",
			"timeout": "30s",
			"token":   "mytoken",
		},
		"defaults": map[string]any{
			"provider":    "openai",
			"disposition": "focused",
			"model":       "gpt-4",
		},
	}
	data, _ := json.Marshal(overrides)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Gateway.URL != "http://custom-gateway:8080" {
		t.Errorf("Gateway.URL: got %q, want %q", cfg.Gateway.URL, "http://custom-gateway:8080")
	}
	if cfg.Gateway.Timeout != "30s" {
		t.Errorf("Gateway.Timeout: got %q, want %q", cfg.Gateway.Timeout, "30s")
	}
	if cfg.Gateway.Token != "mytoken" {
		t.Errorf("Gateway.Token: got %q, want %q", cfg.Gateway.Token, "mytoken")
	}
	if cfg.Defaults.Provider != "openai" {
		t.Errorf("Defaults.Provider: got %q, want %q", cfg.Defaults.Provider, "openai")
	}
	if cfg.Defaults.Model != "gpt-4" {
		t.Errorf("Defaults.Model: got %q, want %q", cfg.Defaults.Model, "gpt-4")
	}
}

// ─── DojoDir ─────────────────────────────────────────────────────────────────

func TestDojoDir_ContainsDotDojo(t *testing.T) {
	d := DojoDir()
	if !strings.HasSuffix(d, ".dojo") {
		t.Errorf("DojoDir() = %q; expected it to end with '.dojo'", d)
	}
	// Must be an absolute path rooted at the real home dir.
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(d, home) {
		t.Errorf("DojoDir() = %q; expected prefix %q", d, home)
	}
}

// ─── Env var overrides ────────────────────────────────────────────────────────

func TestLoad_EnvVar_GatewayURL(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "http://env-override:9999")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Gateway.URL != "http://env-override:9999" {
		t.Errorf("Gateway.URL after env override: got %q, want %q", cfg.Gateway.URL, "http://env-override:9999")
	}
}

func TestLoad_EnvVar_Token(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "env-token-xyz")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Gateway.Token != "env-token-xyz" {
		t.Errorf("Gateway.Token: got %q, want %q", cfg.Gateway.Token, "env-token-xyz")
	}
}

func TestLoad_EnvVar_Provider(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "anthropic")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Defaults.Provider != "anthropic" {
		t.Errorf("Defaults.Provider: got %q, want %q", cfg.Defaults.Provider, "anthropic")
	}
}

func TestLoad_EnvVar_PluginsPath(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	customPluginsPath := filepath.Join(tmp, "myplugins")
	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", customPluginsPath)
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Plugins.Path != customPluginsPath {
		t.Errorf("Plugins.Path: got %q, want %q", cfg.Plugins.Path, customPluginsPath)
	}
}

// ─── SettingsPath ─────────────────────────────────────────────────────────────

func TestSettingsPath_EndsWithSettingsJSON(t *testing.T) {
	p := SettingsPath()
	if !strings.HasSuffix(p, "settings.json") {
		t.Errorf("SettingsPath() = %q; expected suffix 'settings.json'", p)
	}
}

// ─── Invalid JSON in settings file ───────────────────────────────────────────

func TestLoad_InvalidJSON_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON settings file, got nil")
	}
}

// ─── Validate ────────────────────────────────────────────────────────────────

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "balanced",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
}

func TestValidate_BadURL(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "not a url",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "balanced",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate() to fail for bad URL, got nil")
	}
	if !strings.Contains(err.Error(), "gateway.url") {
		t.Errorf("error should mention gateway.url, got: %v", err)
	}
}

func TestValidate_BadTimeout(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "xyz",
		},
		Defaults: DefaultsConfig{
			Disposition: "balanced",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate() to fail for bad timeout, got nil")
	}
	if !strings.Contains(err.Error(), "gateway.timeout") {
		t.Errorf("error should mention gateway.timeout, got: %v", err)
	}
}

func TestValidate_BadDisposition(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "chaotic",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate() to fail for bad disposition, got nil")
	}
	if !strings.Contains(err.Error(), "defaults.disposition") {
		t.Errorf("error should mention defaults.disposition, got: %v", err)
	}
}

func TestValidate_EmptyDisposition(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty disposition should be valid, got: %v", err)
	}
}

// ─── Env var overrides: DOJO_DISPOSITION and DOJO_MODEL ─────────────────────

func TestLoad_DispositionEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")
	t.Setenv("DOJO_DISPOSITION", "focused")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Defaults.Disposition != "focused" {
		t.Errorf("Defaults.Disposition: got %q, want %q", cfg.Defaults.Disposition, "focused")
	}
}

func TestLoad_ModelEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_USER_ID", "")
	t.Setenv("DOJO_MODEL", "claude-sonnet-4-6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Defaults.Model != "claude-sonnet-4-6" {
		t.Errorf("Defaults.Model: got %q, want %q", cfg.Defaults.Model, "claude-sonnet-4-6")
	}
}

// ─── Load() disposition graceful degradation (never brick startup) ──────────

func TestLoad_UnknownDisposition_DegradesToDefault(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A disposition that resolves nowhere: not a builtin, not a config
	// profile, and (no dispositions dir exists) not a file preset either.
	// Before the fix, this made Load() return an error — bricking every
	// subsequent `dojo` invocation until settings.json was hand-edited.
	settings := map[string]any{
		"defaults": map[string]any{"disposition": "typo-d-name"},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should degrade, not error, on an unknown disposition: %v", err)
	}
	if cfg.Defaults.Disposition != DefaultDisposition {
		t.Errorf("Defaults.Disposition after degrade: got %q, want %q", cfg.Defaults.Disposition, DefaultDisposition)
	}
}

func TestLoad_FilePresetDisposition_ResolvesWithoutReset(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	// A disposition saved only as a file-based preset (~/.dojo/dispositions/*.json)
	// — NOT a builtin, NOT in DispositionProfiles. validateDisposition() alone
	// would reject this; Load() must accept it via IsKnownDisposition instead
	// of silently overwriting it with the default. This is the other half of
	// the brick fix: a legitimate file-preset name must resolve cleanly, not
	// just avoid a hard error.
	if err := SaveDispositionPreset(DispositionPreset{
		Name: "custom-file-preset", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive",
	}); err != nil {
		t.Fatalf("SaveDispositionPreset() error: %v", err)
	}

	dojoDir := filepath.Join(tmp, ".dojo")
	settings := map[string]any{
		"defaults": map[string]any{"disposition": "custom-file-preset"},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error for a valid file-preset disposition: %v", err)
	}
	if cfg.Defaults.Disposition != "custom-file-preset" {
		t.Errorf("Defaults.Disposition: got %q, want %q (should not have been reset to the default)", cfg.Defaults.Disposition, "custom-file-preset")
	}
}

func TestLoad_BadGatewayURL_StillHardFails(t *testing.T) {
	// Gateway errors are real wiring problems and must still stop startup —
	// the degrade-gracefully behavior is specific to disposition.
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "")

	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"gateway": map[string]any{"url": "not a url"},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("Load() should still fail on a malformed gateway.url — only disposition degrades")
	}
}

// ─── IsKnownDisposition ───────────────────────────────────────────────────────

func TestIsKnownDisposition_Builtin(t *testing.T) {
	if !IsKnownDisposition("balanced", nil) {
		t.Error(`IsKnownDisposition("balanced", nil) should be true — it's a builtin`)
	}
}

func TestIsKnownDisposition_Empty(t *testing.T) {
	if !IsKnownDisposition("", nil) {
		t.Error(`IsKnownDisposition("", nil) should be true — empty means "unset"`)
	}
}

func TestIsKnownDisposition_ConfigProfile(t *testing.T) {
	profiles := map[string]DispositionPreset{
		"sprint": {Name: "sprint", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive"},
	}
	if !IsKnownDisposition("sprint", profiles) {
		t.Error("IsKnownDisposition should recognize a config-resident profile")
	}
}

func TestIsKnownDisposition_FilePreset(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	if err := SaveDispositionPreset(DispositionPreset{
		Name: "file-only", Pacing: "measured", Depth: "thorough", Tone: "warm", Initiative: "proactive",
	}); err != nil {
		t.Fatalf("SaveDispositionPreset() error: %v", err)
	}

	if !IsKnownDisposition("file-only", nil) {
		t.Error("IsKnownDisposition should recognize a file-based preset under ~/.dojo/dispositions/")
	}
}

func TestIsKnownDisposition_Unknown(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	if IsKnownDisposition("totally-made-up", nil) {
		t.Error("IsKnownDisposition should reject a name that matches nothing")
	}
}

// ─── EffectiveString ─────────────────────────────────────────────────────────

func TestEffectiveString_ContainsAllFields(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
			Token:   "tok_abcdefgh1234",
		},
		Plugins: PluginsConfig{
			Path: "/home/user/.dojo/plugins",
		},
		Defaults: DefaultsConfig{
			Provider:    "anthropic",
			Model:       "claude-sonnet-4-6",
			Disposition: "balanced",
		},
	}
	out := cfg.EffectiveString()

	for _, want := range []string{
		"gateway.url = http://localhost:7340",
		"gateway.timeout = 60s",
		"gateway.token = tok_****1234",
		"defaults.provider = anthropic",
		"defaults.model = claude-sonnet-4-6",
		"defaults.disposition = balanced",
		"plugins.path = /home/user/.dojo/plugins",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("EffectiveString() missing %q\ngot:\n%s", want, out)
		}
	}
}

// ─── Protocol block ──────────────────────────────────────────────────────────

// clearProtocolEnv wipes every DOJO_* knob so a stray shell value can't skew a
// Load() under test, and points HOME at a fresh temp so there is no settings.json.
func clearProtocolEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{
		"DOJO_GATEWAY_URL", "DOJO_GATEWAY_TOKEN", "DOJO_PLUGINS_PATH",
		"DOJO_PROVIDER", "DOJO_DISPOSITION", "DOJO_MODEL", "DOJO_USER_ID",
		"DOJO_PROTOCOL_DISABLED", "DOJO_PROTOCOL_PATH", "DOJO_VERIFY_AFTER_AGENT",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Protocol_EnabledByDefault(t *testing.T) {
	clearProtocolEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Protocol.Enabled {
		t.Error("Protocol.Enabled should default to true when settings.json omits the block")
	}
	if cfg.Protocol.Path != "" {
		t.Errorf("Protocol.Path should default empty, got %q", cfg.Protocol.Path)
	}
}

func TestLoad_Protocol_EnvDisables(t *testing.T) {
	clearProtocolEnv(t)
	t.Setenv("DOJO_PROTOCOL_DISABLED", "1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Protocol.Enabled {
		t.Error("DOJO_PROTOCOL_DISABLED=1 should disable the protocol")
	}
}

func TestLoad_Protocol_EnvPathOverride(t *testing.T) {
	clearProtocolEnv(t)
	t.Setenv("DOJO_PROTOCOL_PATH", "/tmp/custom-protocol.md")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Protocol.Path != "/tmp/custom-protocol.md" {
		t.Errorf("Protocol.Path: got %q, want %q", cfg.Protocol.Path, "/tmp/custom-protocol.md")
	}
	// Path override alone must not flip Enabled off.
	if !cfg.Protocol.Enabled {
		t.Error("setting DOJO_PROTOCOL_PATH should leave the protocol enabled")
	}
}

// A settings.json that omits "enabled" inside a present "protocol" block must
// still resolve to enabled — the defaults()+merge contract that ProtocolConfig
// depends on. This is the subtle case that a plain zero-value bool would break.
func TestLoad_Protocol_PresentBlockWithoutEnabled_StaysOn(t *testing.T) {
	clearProtocolEnv(t)
	dojoDir := filepath.Join(os.Getenv("HOME"), ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"protocol": map[string]any{"path": "/some/where.md"}, // no "enabled" key
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Protocol.Enabled {
		t.Error("a protocol block without an 'enabled' key must keep the default (true)")
	}
	if cfg.Protocol.Path != "/some/where.md" {
		t.Errorf("Protocol.Path from settings: got %q", cfg.Protocol.Path)
	}
}

func TestLoad_Protocol_SettingsDisables(t *testing.T) {
	clearProtocolEnv(t)
	dojoDir := filepath.Join(os.Getenv("HOME"), ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"protocol": map[string]any{"enabled": false},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Protocol.Enabled {
		t.Error(`settings.json "protocol":{"enabled":false} should disable`)
	}
}

func TestEffectiveString_IncludesProtocol(t *testing.T) {
	cfg := &Config{
		Gateway:  GatewayConfig{URL: "http://localhost:7340", Timeout: "60s"},
		Defaults: DefaultsConfig{Disposition: "balanced"},
		Protocol: ProtocolConfig{Enabled: true},
	}
	out := cfg.EffectiveString()
	for _, want := range []string{"protocol.enabled = true", "protocol.path = (embedded default)"} {
		if !strings.Contains(out, want) {
			t.Errorf("EffectiveString() missing %q\ngot:\n%s", want, out)
		}
	}
}

// ─── Verify block (W6-PROTOCOL-ADV) ──────────────────────────────────────────

// TestLoad_Verify_DefaultsFalse proves the opt-in verify loop is OFF by default:
// a settings.json that omits the whole block leaves Verify.AfterAgent false.
func TestLoad_Verify_DefaultsFalse(t *testing.T) {
	clearProtocolEnv(t) // fresh HOME (no settings.json) + all DOJO_* knobs cleared
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Verify.AfterAgent {
		t.Error("Verify.AfterAgent should default to false when settings.json omits the block")
	}
}

// TestLoad_Verify_MissingBlockStaysFalse proves backward compatibility: an
// existing settings.json written before the verify block existed (no "verify"
// key at all) loads with the loop disabled, not with some accidental default.
func TestLoad_Verify_MissingBlockStaysFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)

	// A pre-existing config with no "verify" key.
	settings := map[string]any{
		"gateway": map[string]any{"url": "http://localhost:7340", "timeout": "60s"},
	}
	data, _ := json.Marshal(settings)
	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Verify.AfterAgent {
		t.Error("a settings.json with no verify block should load AfterAgent=false")
	}
}

// TestLoad_Verify_EnvEnables proves DOJO_VERIFY_AFTER_AGENT (any non-empty
// value) flips the loop on for the run.
func TestLoad_Verify_EnvEnables(t *testing.T) {
	clearProtocolEnv(t)
	t.Setenv("DOJO_VERIFY_AFTER_AGENT", "1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Verify.AfterAgent {
		t.Error("DOJO_VERIFY_AFTER_AGENT=1 should enable the post-agent verify loop")
	}
}

// TestSave_StripsEnvOverride_VerifyAfterAgent proves the run-scoped env override
// is not baked into settings.json — same transient-override contract that
// protects DOJO_PROTOCOL_DISABLED (see envOverride and Config.Save()).
func TestSave_StripsEnvOverride_VerifyAfterAgent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)
	t.Setenv("DOJO_VERIFY_AFTER_AGENT", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Verify.AfterAgent {
		t.Fatal("sanity check failed: DOJO_VERIFY_AFTER_AGENT=1 should have enabled the verify loop")
	}

	// A command saves config for an unrelated reason (e.g. /model set).
	cfg.Defaults.Model = "claude-sonnet-4-6"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Re-load WITHOUT the env var: the transient override must not have persisted.
	t.Setenv("DOJO_VERIFY_AFTER_AGENT", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload Load() error: %v", err)
	}
	if reloaded.Verify.AfterAgent {
		t.Error("DOJO_VERIFY_AFTER_AGENT leaked into settings.json — Save() should have stripped the run-scoped override")
	}
	if reloaded.Defaults.Model != "claude-sonnet-4-6" {
		t.Errorf("the real edit should persist: Defaults.Model = %q, want %q", reloaded.Defaults.Model, "claude-sonnet-4-6")
	}
}

// TestEffectiveString_IncludesVerify proves the verify state shows up in the
// /settings effective dump alongside the other blocks.
func TestEffectiveString_IncludesVerify(t *testing.T) {
	cfg := &Config{
		Gateway:  GatewayConfig{URL: "http://localhost:7340", Timeout: "60s"},
		Defaults: DefaultsConfig{Disposition: "balanced"},
		Verify:   VerifyConfig{AfterAgent: true},
	}
	if want := "verify.after_agent = true"; !strings.Contains(cfg.EffectiveString(), want) {
		t.Errorf("EffectiveString() missing %q\ngot:\n%s", want, cfg.EffectiveString())
	}
}

// ─── Auth.UserID env override ───────────────────────────────────────────────

func TestLoad_UserIDEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	t.Setenv("DOJO_GATEWAY_URL", "")
	t.Setenv("DOJO_GATEWAY_TOKEN", "")
	t.Setenv("DOJO_PLUGINS_PATH", "")
	t.Setenv("DOJO_PROVIDER", "")
	t.Setenv("DOJO_DISPOSITION", "")
	t.Setenv("DOJO_MODEL", "")
	t.Setenv("DOJO_USER_ID", "user-abc-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Auth.UserID != "user-abc-123" {
		t.Errorf("Auth.UserID: got %q, want %q", cfg.Auth.UserID, "user-abc-123")
	}
}

// ─── EffectiveString includes auth ──────────────────────────────────────────

func TestEffectiveString_IncludesAuth(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "balanced",
		},
		Auth: AuthConfig{
			UserID: "user-xyz-789",
		},
	}
	out := cfg.EffectiveString()
	want := "auth.user_id = user-xyz-789"
	if !strings.Contains(out, want) {
		t.Errorf("EffectiveString() missing %q\ngot:\n%s", want, out)
	}
}

func TestEffectiveString_AuthNotSet(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:7340",
			Timeout: "60s",
		},
		Defaults: DefaultsConfig{
			Disposition: "balanced",
		},
	}
	out := cfg.EffectiveString()
	want := "auth.user_id = (not set)"
	if !strings.Contains(out, want) {
		t.Errorf("EffectiveString() missing %q\ngot:\n%s", want, out)
	}
}

// ─── Save() strips transient env overrides ───────────────────────────────────
//
// Regression coverage for a real incident: Load() applies env var overrides
// directly onto the runtime *Config it returns. Several commands (/model
// set, /disposition set) later call cfg.Save() on that same *Config for an
// unrelated reason. Before this fix, Save() serialized whatever the struct
// held — so a purely run-scoped override like DOJO_PROTOCOL_DISABLED=1 got
// baked into settings.json permanently the moment any save happened,
// silently disabling the protocol on every later run even with the env var
// unset. See envOverride and Config.Save() for the fix.

func clearAllConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"DOJO_GATEWAY_URL", "DOJO_GATEWAY_TOKEN", "DOJO_PLUGINS_PATH",
		"DOJO_PROVIDER", "DOJO_DISPOSITION", "DOJO_MODEL", "DOJO_USER_ID",
		"DOJO_PROTOCOL_DISABLED", "DOJO_PROTOCOL_PATH", "DOJO_VERIFY_AFTER_AGENT",
	} {
		t.Setenv(k, "")
	}
}

func TestSave_StripsEnvOverride_ProtocolDisabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)
	t.Setenv("DOJO_PROTOCOL_DISABLED", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Protocol.Enabled {
		t.Fatal("sanity check failed: DOJO_PROTOCOL_DISABLED=1 should have disabled the protocol")
	}

	// Simulate a command that saves config for an unrelated reason — this is
	// the actual trigger that leaked the env override to disk in the field
	// (e.g. /model set calling cfg.Save() after changing Defaults.Model).
	cfg.Defaults.Model = "claude-sonnet-4-6"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Reload with the SAME settings.json but WITHOUT the env var. Before the
	// fix this came back false — permanently stuck disabled.
	t.Setenv("DOJO_PROTOCOL_DISABLED", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if !reloaded.Protocol.Enabled {
		t.Error("Protocol.Enabled should be back to true after reload without DOJO_PROTOCOL_DISABLED — Save() must not persist a still-in-effect env override")
	}
	// The unrelated explicit change made before Save() must still have
	// persisted normally.
	if reloaded.Defaults.Model != "claude-sonnet-4-6" {
		t.Errorf("Defaults.Model should have been saved: got %q, want %q", reloaded.Defaults.Model, "claude-sonnet-4-6")
	}
}

func TestSave_StripsEnvOverride_GatewayURL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)
	t.Setenv("DOJO_GATEWAY_URL", "http://env-transient:9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Gateway.URL != "http://env-transient:9999" {
		t.Fatalf("sanity check failed: Gateway.URL = %q, want the env override", cfg.Gateway.URL)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	t.Setenv("DOJO_GATEWAY_URL", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Gateway.URL != DefaultGatewayURL {
		t.Errorf("Gateway.URL after reload: got %q, want default %q — Save() must not persist a still-in-effect env override", reloaded.Gateway.URL, DefaultGatewayURL)
	}
}

// The realistic version of the incident: settings.json already holds a real,
// non-default value on disk. Save() must restore THAT value, not just the
// compiled-in default, when stripping a still-in-effect env override.
func TestSave_StripsEnvOverride_RevertsToFileValueNotJustDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)

	dojoDir := filepath.Join(tmp, ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seeded := map[string]any{"gateway": map[string]any{"url": "http://my-real-gateway:7340", "timeout": "60s"}}
	data, _ := json.Marshal(seeded)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// A one-off run overrides the gateway via env (e.g. pointing at a local
	// test instance)...
	t.Setenv("DOJO_GATEWAY_URL", "http://one-off-test-gateway:1234")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Gateway.URL != "http://one-off-test-gateway:1234" {
		t.Fatalf("sanity check failed: Gateway.URL = %q", cfg.Gateway.URL)
	}

	// ...and that same run happens to also save config for an unrelated
	// reason (/disposition set).
	cfg.Defaults.Disposition = "focused"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Next run, without the env var: must see the REAL gateway from the
	// file, not the one-off test value.
	t.Setenv("DOJO_GATEWAY_URL", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Gateway.URL != "http://my-real-gateway:7340" {
		t.Errorf("Gateway.URL after reload: got %q, want the pre-existing file value %q", reloaded.Gateway.URL, "http://my-real-gateway:7340")
	}
	if reloaded.Defaults.Disposition != "focused" {
		t.Errorf("Defaults.Disposition should have persisted the explicit change: got %q", reloaded.Defaults.Disposition)
	}
}

// The other half of the fix: an explicit change to the SAME field an env var
// overrode must win, not get reverted. This is what /model set does — set
// Defaults.Model directly, then Save() — and it must not be treated as
// "still env-controlled" just because DOJO_MODEL happened to be set too.
func TestSave_ExplicitChangeToEnvOverriddenField_Wins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	clearAllConfigEnv(t)
	t.Setenv("DOJO_MODEL", "env-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Defaults.Model != "env-model" {
		t.Fatalf("sanity check failed: Defaults.Model = %q", cfg.Defaults.Model)
	}

	// Exactly what cmd_model.go's /model set does: mutate the same field the
	// env var had overridden, then Save().
	cfg.Defaults.Model = "explicitly-chosen-model"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	t.Setenv("DOJO_MODEL", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Defaults.Model != "explicitly-chosen-model" {
		t.Errorf("Defaults.Model: got %q, want the explicitly-set value %q — an explicit change to an env-overridden field must survive Save(), not get reverted", reloaded.Defaults.Model, "explicitly-chosen-model")
	}
}

// A Config built directly (not via Load()) has no envOverrides recorded at
// all — Save() must be a plain, unmodified round-trip in that case, exactly
// as it always was. Guards against the fix accidentally touching fields it
// has no override record for.
func TestSave_NoEnvOverrides_PlainRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := &Config{
		Gateway:  GatewayConfig{URL: "http://literal:7340", Timeout: "60s"},
		Defaults: DefaultsConfig{Disposition: "balanced", Model: "literal-model"},
		Protocol: ProtocolConfig{Enabled: false},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	clearAllConfigEnv(t)
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Gateway.URL != "http://literal:7340" {
		t.Errorf("Gateway.URL: got %q, want %q", reloaded.Gateway.URL, "http://literal:7340")
	}
	if reloaded.Defaults.Model != "literal-model" {
		t.Errorf("Defaults.Model: got %q, want %q", reloaded.Defaults.Model, "literal-model")
	}
	if reloaded.Protocol.Enabled {
		t.Error("Protocol.Enabled: got true, want false (the literal value written) — no env override was ever in play")
	}
}
