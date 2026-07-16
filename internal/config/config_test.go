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
