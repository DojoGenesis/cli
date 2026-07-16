package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ─── BuiltinPresets ──────────────────────────────────────────────────────────

func TestBuiltinPresets_ReturnsFour(t *testing.T) {
	presets := BuiltinPresets()
	if len(presets) != 4 {
		t.Fatalf("BuiltinPresets() returned %d presets, want 4", len(presets))
	}
	names := map[string]bool{}
	for _, p := range presets {
		names[p.Name] = true
	}
	for _, want := range []string{"focused", "balanced", "exploratory", "deliberate"} {
		if !names[want] {
			t.Errorf("BuiltinPresets() missing preset %q", want)
		}
	}
}

// ─── LoadDispositionPresets — missing dir ────────────────────────────────────

func TestLoadPresets_MissingDir(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	// No ~/.dojo/dispositions/ directory exists — should return builtins.
	presets, err := LoadDispositionPresets()
	if err != nil {
		t.Fatalf("LoadDispositionPresets() returned error: %v", err)
	}
	if len(presets) != 4 {
		t.Errorf("LoadDispositionPresets() returned %d presets, want 4 builtins", len(presets))
	}
}

// ─── SaveDispositionPreset + LoadDispositionPresets roundtrip ────────────────

func TestSaveAndLoadPreset(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer func() { _ = os.Setenv("HOME", origHome) }() // test cleanup; restore is best-effort, t.Setenv already restores on test end

	custom := DispositionPreset{
		Name:       "custom",
		Pacing:     "swift",
		Depth:      "thorough",
		Tone:       "warm",
		Initiative: "autonomous",
	}
	if err := SaveDispositionPreset(custom); err != nil {
		t.Fatalf("SaveDispositionPreset() error: %v", err)
	}

	// Verify the file was written.
	path := filepath.Join(DispositionsDir(), "custom.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read saved preset file: %v", err)
	}
	var loaded DispositionPreset
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("saved preset is not valid JSON: %v", err)
	}
	if loaded.Name != "custom" || loaded.Tone != "warm" {
		t.Errorf("saved preset mismatch: got %+v", loaded)
	}

	// Load all — should include custom + 4 builtins = 5.
	presets, err := LoadDispositionPresets()
	if err != nil {
		t.Fatalf("LoadDispositionPresets() error: %v", err)
	}
	if len(presets) != 5 {
		t.Errorf("LoadDispositionPresets() returned %d presets, want 5 (1 custom + 4 builtins)", len(presets))
	}

	found := false
	for _, p := range presets {
		if p.Name == "custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom preset not found in loaded presets")
	}
}

// ─── MergeConfigProfiles ────────────────────────────────────────────────────

func TestMergeConfigProfiles_EmptyMap(t *testing.T) {
	filePresets := BuiltinPresets()
	result := MergeConfigProfiles(nil, filePresets)
	if len(result) != len(filePresets) {
		t.Fatalf("MergeConfigProfiles(nil, builtins) returned %d presets, want %d", len(result), len(filePresets))
	}
}

func TestMergeConfigProfiles_ConfigWins(t *testing.T) {
	filePresets := BuiltinPresets()
	// Override builtin "focused" and add a new "custom" preset via config.
	configProfiles := map[string]DispositionPreset{
		"focused": {Name: "focused", Pacing: "slow", Depth: "exhaustive", Tone: "warm", Initiative: "autonomous"},
		"custom":  {Name: "custom", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive"},
	}
	result := MergeConfigProfiles(configProfiles, filePresets)

	// Should have 4 builtins with "focused" overridden + 1 new = 5 total.
	if len(result) != 5 {
		t.Fatalf("MergeConfigProfiles() returned %d presets, want 5", len(result))
	}

	byName := make(map[string]DispositionPreset)
	for _, p := range result {
		byName[p.Name] = p
	}

	if p, ok := byName["focused"]; !ok || p.Pacing != "slow" {
		t.Errorf("config profile did not override builtin focused: got %+v", byName["focused"])
	}
	if _, ok := byName["custom"]; !ok {
		t.Error("custom profile from config not in merged result")
	}
}

func TestMergeConfigProfiles_EmptyNameUsesKey(t *testing.T) {
	// Preset stored in map with empty Name field — key should be used as name.
	configProfiles := map[string]DispositionPreset{
		"mynew": {Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive"},
	}
	result := MergeConfigProfiles(configProfiles, BuiltinPresets())
	byName := make(map[string]DispositionPreset)
	for _, p := range result {
		byName[p.Name] = p
	}
	if p, ok := byName["mynew"]; !ok || p.Pacing != "swift" {
		t.Errorf("preset with empty Name not keyed correctly: %+v", byName)
	}
}

// ─── Config.DispositionProfiles round-trip ───────────────────────────────────

func TestConfig_DispositionProfiles_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := &Config{
		Gateway: GatewayConfig{URL: DefaultGatewayURL, Timeout: "60s"},
		Defaults: DefaultsConfig{
			Disposition: "sprint",
		},
		DispositionProfiles: map[string]DispositionPreset{
			"sprint": {Name: "sprint", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error after save: %v", err)
	}
	if loaded.Defaults.Disposition != "sprint" {
		t.Errorf("Defaults.Disposition = %q, want sprint", loaded.Defaults.Disposition)
	}
	if p, ok := loaded.DispositionProfiles["sprint"]; !ok || p.Pacing != "swift" {
		t.Errorf("DispositionProfiles[sprint] not preserved: %+v", loaded.DispositionProfiles)
	}
}

func TestConfig_Validate_CustomProfileAllowed(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{URL: DefaultGatewayURL, Timeout: "60s"},
		Defaults: DefaultsConfig{Disposition: "myprofile"},
		DispositionProfiles: map[string]DispositionPreset{
			"myprofile": {Name: "myprofile", Pacing: "measured", Depth: "thorough", Tone: "warm", Initiative: "proactive"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() rejected custom profile name: %v", err)
	}
}

func TestConfig_Validate_UnknownDispositionRejected(t *testing.T) {
	cfg := &Config{
		Gateway:  GatewayConfig{URL: DefaultGatewayURL, Timeout: "60s"},
		Defaults: DefaultsConfig{Disposition: "unknownxyz"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject unknown disposition not in profiles")
	}
}

// ─── mergeBuiltins ──────────────────────────────────────────────────────────

func TestMergeBuiltins(t *testing.T) {
	// User override of "focused" — should not duplicate it.
	userPresets := []DispositionPreset{
		{Name: "focused", Pacing: "deliberate", Depth: "exhaustive", Tone: "warm", Initiative: "autonomous"},
	}
	merged := mergeBuiltins(userPresets)

	// 1 user + 3 remaining builtins (balanced, exploratory, deliberate) = 4
	if len(merged) != 4 {
		t.Fatalf("mergeBuiltins() returned %d presets, want 4", len(merged))
	}

	// The user's "focused" should keep user values, not be overwritten.
	for _, p := range merged {
		if p.Name == "focused" {
			if p.Pacing != "deliberate" {
				t.Errorf("user focused.Pacing was overwritten: got %q, want %q", p.Pacing, "deliberate")
			}
			return
		}
	}
	t.Error("focused preset missing from merged results")
}
