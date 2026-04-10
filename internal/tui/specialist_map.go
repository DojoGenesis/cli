// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

// SpecialistInfo holds client-side specialist mapping.
// Mirrors DefaultSpecialists() from gateway specialist/registry.go.
type SpecialistInfo struct {
	Name        string
	Plugin      string
	Disposition string
	Skills      []string
}

// IntentToSpecialist maps intent category strings to specialist configs.
// This is a client-side mirror of the gateway's specialist registry.
var IntentToSpecialist = map[string]SpecialistInfo{
	"CodeGeneration": {Name: "forger", Plugin: "skill-forge", Disposition: "rapid", Skills: []string{"skill-creation", "skill-extraction"}},
	"Debugging":      {Name: "researcher", Plugin: "continuous-learning", Disposition: "deliberate", Skills: []string{"deep-research", "wide-research"}},
	"Planning":       {Name: "specifier", Plugin: "specification-driven-development", Disposition: "measured", Skills: []string{"release-spec", "frontend-spec"}},
	"MetaQuery":      {Name: "coordinator", Plugin: "agent-orchestration", Disposition: "measured", Skills: []string{"agent-dispatch-playbook", "workflow-router"}},
	"Explanation":    {Name: "gardener", Plugin: "wisdom-garden", Disposition: "measured", Skills: []string{"memory-garden", "seed-extraction"}},
	"Factual":        {Name: "factual-researcher", Plugin: "continuous-learning", Disposition: "responsive", Skills: []string{"deep-research"}},
}

// GeneralistFallback is the default specialist when no intent matches or
// confidence is below the threshold.
var GeneralistFallback = SpecialistInfo{Name: "generalist", Plugin: "", Disposition: "responsive", Skills: nil}

// LookupSpecialist returns the specialist for an intent category string.
// Returns generalist fallback if no match or confidence < 0.7.
func LookupSpecialist(intent string, confidence float64) SpecialistInfo {
	if confidence < 0.7 {
		return GeneralistFallback
	}
	if info, ok := IntentToSpecialist[intent]; ok {
		return info
	}
	return GeneralistFallback
}
