package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/DojoGenesis/cli/internal/ioutilx"
)

// DispositionPreset defines an ADA disposition configuration.
type DispositionPreset struct {
	Name       string `json:"name"`
	Pacing     string `json:"pacing"`
	Depth      string `json:"depth"`
	Tone       string `json:"tone"`
	Initiative string `json:"initiative"`
}

// BuiltinPresets returns the four canonical disposition presets.
func BuiltinPresets() []DispositionPreset {
	return []DispositionPreset{
		{Name: "focused", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive"},
		{Name: "balanced", Pacing: "measured", Depth: "thorough", Tone: "balanced", Initiative: "proactive"},
		{Name: "exploratory", Pacing: "measured", Depth: "exhaustive", Tone: "warm", Initiative: "autonomous"},
		{Name: "deliberate", Pacing: "deliberate", Depth: "exhaustive", Tone: "direct", Initiative: "proactive"},
	}
}

// LoadDispositionPresets reads user-defined presets from DispositionsDir(),
// merging them with builtins so that user presets override builtins by name.
func LoadDispositionPresets() ([]DispositionPreset, error) {
	dir := DispositionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return BuiltinPresets(), nil
		}
		return nil, err
	}
	var presets []DispositionPreset
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p DispositionPreset
		if json.Unmarshal(data, &p) == nil && p.Name != "" {
			presets = append(presets, p)
		}
	}
	return mergeBuiltins(presets), nil
}

// SaveDispositionPreset writes a preset to DispositionsDir()/<name>.json atomically.
func SaveDispositionPreset(p DispositionPreset) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return ioutilx.AtomicWriteFile(filepath.Join(DispositionsDir(), p.Name+".json"), data, 0600)
}

// MergeConfigProfiles overlays config-resident profiles on top of the
// file-based preset list. Config profiles win when names collide.
// The returned slice is unsorted; callers must not rely on order.
func MergeConfigProfiles(configProfiles map[string]DispositionPreset, filePresets []DispositionPreset) []DispositionPreset {
	if len(configProfiles) == 0 {
		return filePresets
	}
	byName := make(map[string]DispositionPreset, len(filePresets)+len(configProfiles))
	for _, p := range filePresets {
		byName[p.Name] = p
	}
	for name, p := range configProfiles {
		if p.Name == "" {
			p.Name = name
		}
		byName[p.Name] = p
	}
	result := make([]DispositionPreset, 0, len(byName))
	for _, p := range byName {
		result = append(result, p)
	}
	return result
}

// mergeBuiltins appends any builtin presets not already present in loaded.
func mergeBuiltins(loaded []DispositionPreset) []DispositionPreset {
	names := make(map[string]bool)
	for _, p := range loaded {
		names[p.Name] = true
	}
	for _, b := range BuiltinPresets() {
		if !names[b.Name] {
			loaded = append(loaded, b)
		}
	}
	return loaded
}
