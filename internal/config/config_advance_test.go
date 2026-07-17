package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file covers the advancement-wave config sections (Permissions,
// Delegation, Guardrails, Skills) added alongside the contracts for the
// 2026-07-17 dojo-cli-advance feature wave. Every test isolates HOME to a
// fresh t.TempDir() (see commit 3a7cd64 and clearProtocolEnv/clearAllConfigEnv
// above in config_test.go) so `go test ./...` never reads or writes the
// developer's real ~/.dojo/settings.json.

// clearAdvanceEnv points HOME at a fresh temp dir (so there is no
// settings.json) and wipes every env var this file's Load() calls could pick
// up — the existing DOJO_* knobs via clearAllConfigEnv (config_test.go) plus
// the two new ones added for this wave.
func clearAdvanceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	clearAllConfigEnv(t)
	t.Setenv("DOJO_PERMISSIONS_MODE", "")
	t.Setenv("DOJO_DELEGATION_MODEL", "")
}

// ─── Defaults ────────────────────────────────────────────────────────────────

func TestLoad_Permissions_DefaultsToDefaultMode(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Permissions.Mode != "default" {
		t.Errorf("Permissions.Mode: got %q, want %q", cfg.Permissions.Mode, "default")
	}
	if len(cfg.Permissions.Allowed) != 0 {
		t.Errorf("Permissions.Allowed: got %v, want empty", cfg.Permissions.Allowed)
	}
}

func TestLoad_Delegation_DefaultsToEmptyModel(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Delegation.Model != "" {
		t.Errorf("Delegation.Model: got %q, want empty", cfg.Delegation.Model)
	}
}

func TestLoad_Guardrails_Defaults(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Guardrails.Enabled {
		t.Error("Guardrails.Enabled should default to true when settings.json omits the block")
	}
	if cfg.Guardrails.WarnAfter != 3 {
		t.Errorf("Guardrails.WarnAfter: got %d, want 3", cfg.Guardrails.WarnAfter)
	}
	if cfg.Guardrails.HardAfter != 5 {
		t.Errorf("Guardrails.HardAfter: got %d, want 5", cfg.Guardrails.HardAfter)
	}
}

func TestLoad_Skills_DefaultsToClaudeSkillsDir(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Skills.ExternalDirs) != 1 || cfg.Skills.ExternalDirs[0] != ".claude/skills" {
		t.Errorf("Skills.ExternalDirs: got %v, want [%q]", cfg.Skills.ExternalDirs, ".claude/skills")
	}
}

// ─── settings.json merge semantics (absent key keeps the default) ───────────

func TestLoad_Guardrails_PresentBlockWithoutEnabled_StaysOn(t *testing.T) {
	clearAdvanceEnv(t)

	dojoDir := filepath.Join(os.Getenv("HOME"), ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"guardrails": map[string]any{"warn_after": 7}, // no "enabled" key
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Guardrails.Enabled {
		t.Error("a guardrails block without an 'enabled' key must keep the default (true)")
	}
	if cfg.Guardrails.WarnAfter != 7 {
		t.Errorf("Guardrails.WarnAfter from settings: got %d, want 7", cfg.Guardrails.WarnAfter)
	}
	if cfg.Guardrails.HardAfter != 5 {
		t.Errorf("Guardrails.HardAfter should keep its default: got %d, want 5", cfg.Guardrails.HardAfter)
	}
}

func TestLoad_Guardrails_SettingsDisables(t *testing.T) {
	clearAdvanceEnv(t)

	dojoDir := filepath.Join(os.Getenv("HOME"), ".dojo")
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"guardrails": map[string]any{"enabled": false},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dojoDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Guardrails.Enabled {
		t.Error(`settings.json "guardrails":{"enabled":false} should disable`)
	}
}

// ─── Round trip (Save then Load) ─────────────────────────────────────────────

func TestSaveLoad_Permissions_RoundTrip(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Permissions.Mode = "allowlist"
	cfg.Permissions.Allowed = []string{"code.undo", "plugin.install", "craft.*"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Permissions.Mode != "allowlist" {
		t.Errorf("Permissions.Mode: got %q, want %q", reloaded.Permissions.Mode, "allowlist")
	}
	want := []string{"code.undo", "plugin.install", "craft.*"}
	if len(reloaded.Permissions.Allowed) != len(want) {
		t.Fatalf("Permissions.Allowed length: got %d, want %d", len(reloaded.Permissions.Allowed), len(want))
	}
	for i, p := range want {
		if reloaded.Permissions.Allowed[i] != p {
			t.Errorf("Permissions.Allowed[%d]: got %q, want %q", i, reloaded.Permissions.Allowed[i], p)
		}
	}
}

func TestSaveLoad_Delegation_RoundTrip(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Delegation.Model = "claude-haiku-cheap-lane"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Delegation.Model != "claude-haiku-cheap-lane" {
		t.Errorf("Delegation.Model: got %q, want %q", reloaded.Delegation.Model, "claude-haiku-cheap-lane")
	}
}

func TestSaveLoad_Guardrails_RoundTrip(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Guardrails.Enabled = false
	cfg.Guardrails.WarnAfter = 10
	cfg.Guardrails.HardAfter = 20
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Guardrails.Enabled {
		t.Error("Guardrails.Enabled explicit false should survive round trip")
	}
	if reloaded.Guardrails.WarnAfter != 10 {
		t.Errorf("Guardrails.WarnAfter: got %d, want 10", reloaded.Guardrails.WarnAfter)
	}
	if reloaded.Guardrails.HardAfter != 20 {
		t.Errorf("Guardrails.HardAfter: got %d, want 20", reloaded.Guardrails.HardAfter)
	}
}

func TestSaveLoad_Skills_RoundTrip(t *testing.T) {
	clearAdvanceEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Skills.ExternalDirs = []string{".claude/skills", "custom/skills-dir"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	want := []string{".claude/skills", "custom/skills-dir"}
	if len(reloaded.Skills.ExternalDirs) != len(want) {
		t.Fatalf("Skills.ExternalDirs length: got %d, want %d", len(reloaded.Skills.ExternalDirs), len(want))
	}
	for i, d := range want {
		if reloaded.Skills.ExternalDirs[i] != d {
			t.Errorf("Skills.ExternalDirs[%d]: got %q, want %q", i, reloaded.Skills.ExternalDirs[i], d)
		}
	}
}

// ─── Env overrides ────────────────────────────────────────────────────────────

func TestLoad_EnvVar_PermissionsMode(t *testing.T) {
	clearAdvanceEnv(t)
	t.Setenv("DOJO_PERMISSIONS_MODE", "yolo")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Permissions.Mode != "yolo" {
		t.Errorf("Permissions.Mode: got %q, want %q", cfg.Permissions.Mode, "yolo")
	}
}

func TestLoad_EnvVar_DelegationModel(t *testing.T) {
	clearAdvanceEnv(t)
	t.Setenv("DOJO_DELEGATION_MODEL", "claude-sonnet-cheap")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Delegation.Model != "claude-sonnet-cheap" {
		t.Errorf("Delegation.Model: got %q, want %q", cfg.Delegation.Model, "claude-sonnet-cheap")
	}
}

// Env override discipline (b89b9c5): a transient env-sourced value must not
// get baked into settings.json by an unrelated Save().

func TestSave_StripsEnvOverride_PermissionsMode(t *testing.T) {
	clearAdvanceEnv(t)
	t.Setenv("DOJO_PERMISSIONS_MODE", "yolo")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Permissions.Mode != "yolo" {
		t.Fatalf("sanity check failed: Permissions.Mode = %q", cfg.Permissions.Mode)
	}

	// An unrelated save (e.g. /model set) must not bake the transient
	// env-sourced mode into settings.json.
	cfg.Defaults.Model = "claude-sonnet-4-6"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	t.Setenv("DOJO_PERMISSIONS_MODE", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Permissions.Mode != "default" {
		t.Errorf("Permissions.Mode after reload: got %q, want default %q — Save() must not persist a still-in-effect env override", reloaded.Permissions.Mode, "default")
	}
	if reloaded.Defaults.Model != "claude-sonnet-4-6" {
		t.Errorf("the real edit should persist: Defaults.Model = %q, want %q", reloaded.Defaults.Model, "claude-sonnet-4-6")
	}
}

func TestSave_StripsEnvOverride_DelegationModel(t *testing.T) {
	clearAdvanceEnv(t)
	t.Setenv("DOJO_DELEGATION_MODEL", "env-transient-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Delegation.Model != "env-transient-model" {
		t.Fatalf("sanity check failed: Delegation.Model = %q", cfg.Delegation.Model)
	}

	cfg.Defaults.Model = "claude-sonnet-4-6"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	t.Setenv("DOJO_DELEGATION_MODEL", "")
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if reloaded.Delegation.Model != "" {
		t.Errorf("Delegation.Model after reload: got %q, want empty — Save() must not persist a still-in-effect env override", reloaded.Delegation.Model)
	}
}

// ─── Validate() ───────────────────────────────────────────────────────────────

func TestValidate_Permissions_ValidModes(t *testing.T) {
	for _, mode := range []string{"", "default", "allowlist", "yolo"} {
		cfg := &Config{
			Gateway:     GatewayConfig{URL: "http://localhost:7340", Timeout: "60s"},
			Defaults:    DefaultsConfig{Disposition: "balanced"},
			Permissions: PermissionsConfig{Mode: mode},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with permissions.mode=%q: unexpected error: %v", mode, err)
		}
	}
}

func TestValidate_Permissions_InvalidMode(t *testing.T) {
	cfg := &Config{
		Gateway:     GatewayConfig{URL: "http://localhost:7340", Timeout: "60s"},
		Defaults:    DefaultsConfig{Disposition: "balanced"},
		Permissions: PermissionsConfig{Mode: "sudo-everything"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate() to fail for invalid permissions.mode, got nil")
	}
	if !strings.Contains(err.Error(), "permissions.mode") {
		t.Errorf("error should mention permissions.mode, got: %v", err)
	}
}
