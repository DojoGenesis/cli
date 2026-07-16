package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/DojoGenesis/cli/internal/ioutilx"
)

// DispositionPreset defines an ADA disposition configuration.
//
// Pacing/Depth/Tone/Initiative are pure interaction style — how fast, how
// deep, how warm, how eagerly the agent acts. Discipline is a separate axis:
// a short note on which cognitive gate(s) this preset leans on harder or
// lighter than the default (orchestrator-binding, output-channel discipline,
// the debugging gate, etc. — see the workspace CLAUDE.md's Operating Gates).
// Style says how it talks; discipline says what it's stricter or looser
// about while doing so.
//
// Discipline is additive: a preset loaded from JSON/YAML written before this
// field existed simply unmarshals it to "" (Go's zero value for string), so
// old file-based presets under ~/.dojo/dispositions/*.json keep loading
// exactly as before — an empty Discipline just means "no note; behave like
// the default gate set."
type DispositionPreset struct {
	Name       string `json:"name"`
	Pacing     string `json:"pacing"`
	Depth      string `json:"depth"`
	Tone       string `json:"tone"`
	Initiative string `json:"initiative"`
	Discipline string `json:"discipline,omitempty"`
}

// BuiltinPresets returns the four canonical disposition presets.
func BuiltinPresets() []DispositionPreset {
	return []DispositionPreset{
		{
			Name: "focused", Pacing: "swift", Depth: "concise", Tone: "direct", Initiative: "reactive",
			Discipline: "tighten output-channel discipline: route big or reusable output to a file with a short status+path, keep chat lean, no exploratory detours",
		},
		{
			Name: "balanced", Pacing: "measured", Depth: "thorough", Tone: "balanced", Initiative: "proactive",
			Discipline: "default gates: apply the standard operating gates as written, no loosening or tightening",
		},
		{
			Name: "exploratory", Pacing: "measured", Depth: "exhaustive", Tone: "warm", Initiative: "autonomous",
			Discipline: "relax orchestrator-binding: widen search, favor breadth and inline investigation over immediately dispatching agents",
		},
		{
			Name: "deliberate", Pacing: "deliberate", Depth: "exhaustive", Tone: "direct", Initiative: "proactive",
			Discipline: "enforce the debugging gate hard: no fix lands without a stated causal chain and a repro that toggles the bug on and off",
		},
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
