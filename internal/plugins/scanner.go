// Package plugins scans a plugins directory for CoworkPlugins-format plugin directories.
package plugins

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Plugin holds the metadata and hook rules discovered for a single plugin.
type Plugin struct {
	Name        string
	Description string
	Version     string
	Path        string // absolute path to plugin directory
	HookRules   []HookRule
	AgentCount  int
	SkillCount  int
}

// HookRule is a single entry in hooks.json — one event + one list of hook definitions.
type HookRule struct {
	Event   string
	Matcher string
	If      string
	Hooks   []HookDef
	// Blocking marks this rule as requiring synchronous completion (as
	// opposed to the per-HookDef Async flag, which controls fire-and-forget
	// vs. block-until-done for one individual hook action within the rule).
	// Field only for now: Fire() (internal/hooks/runner.go) does not yet
	// read it, and loadHooks below does not yet populate it from
	// hooks.json's "blocking" key — both are left to whoever implements the
	// blocking-hook behavior.
	Blocking bool `json:"blocking,omitempty"`
}

// HookDef is an individual hook action within a rule.
type HookDef struct {
	Type    string `json:"type"`    // "command", "prompt", "agent", "http"
	Command string `json:"command"` // shell command string (type=command)
	Prompt  string `json:"prompt"`  // prompt text (type=prompt)
	Model   string `json:"model"`   // model override (type=prompt)
	URL     string `json:"url"`     // target URL (type=http)
	Async   bool   `json:"async"`
}

// pluginMeta is the JSON shape of plugin.json (or .claude-plugin/plugin.json).
type pluginMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// hookEntry is one element inside an event's array in hooks.json.
type hookEntry struct {
	Matcher string    `json:"matcher"`
	If      string    `json:"if"`
	Hooks   []HookDef `json:"hooks"`
}

// Scan reads a plugins root directory and returns all discovered plugins.
// Each subdirectory is checked for a plugin.json (or .claude-plugin/plugin.json).
// Missing or unreadable files are skipped with a best-effort approach.
func Scan(pluginsRoot string) ([]Plugin, error) {
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var plugins []Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pluginDir := filepath.Join(pluginsRoot, e.Name())
		p, ok := scanOne(pluginDir)
		if ok {
			plugins = append(plugins, p)
		}
	}
	return plugins, nil
}

// scanOne attempts to parse one plugin directory. Returns (plugin, true) on success.
func scanOne(dir string) (Plugin, bool) {
	// Locate plugin.json — check .claude-plugin/ first, then root.
	meta, ok := loadPluginMeta(dir)
	if !ok {
		return Plugin{}, false
	}

	p := Plugin{
		Name:        meta.Name,
		Description: meta.Description,
		Version:     meta.Version,
		Path:        dir,
	}

	// Fall back to directory name if plugin.json has no name.
	if p.Name == "" {
		p.Name = filepath.Base(dir)
	}

	// Load hooks/hooks.json
	p.HookRules = loadHooks(dir, p.Name)

	// Count agents (agents/*.md)
	p.AgentCount = countFiles(filepath.Join(dir, "agents"), "*.md")

	// Count skills (skills/*/SKILL.md)
	p.SkillCount = countSkills(filepath.Join(dir, "skills"))

	return p, true
}

// loadPluginMeta looks for plugin.json in .claude-plugin/ then root.
func loadPluginMeta(dir string) (pluginMeta, bool) {
	candidates := []string{
		filepath.Join(dir, ".claude-plugin", "plugin.json"),
		filepath.Join(dir, "plugin.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m pluginMeta
		if err := json.Unmarshal(data, &m); err != nil {
			log.Printf("[plugins] warning: skipping %s — failed to parse plugin.json: %v", path, err)
			continue
		}
		return m, true
	}
	return pluginMeta{}, false
}

// Dojo-cli's own hook event names, duplicated here (not imported) because
// internal/hooks already imports internal/plugins — importing the other
// direction would create a cycle. Keep these in sync with the Event*
// constants in internal/hooks/runner.go.
const (
	dojoEventPreCommand   = "PreCommand"
	dojoEventPostCommand  = "PostCommand"
	dojoEventPostSkill    = "PostSkill"
	dojoEventPostAgent    = "PostAgent"
	dojoEventSessionStart = "SessionStart"
	dojoEventSessionEnd   = "SessionEnd"
)

// loadHooks reads hooks/hooks.json and converts it into []HookRule.
//
// Two schemas are supported:
//
//   - Flat (dojo-native, back-compat): {"<event>": [{matcher,if,hooks}, ...], ...}
//     directly at the top level. Event names are used verbatim — unchanged
//     from this function's original, wrapper-unaware behavior.
//
//   - Wrapped (Claude-Code-native): {"hooks": {"<event>": [...], ...}} — a
//     single top-level "hooks" key whose value is itself an event-name-keyed
//     object. This is the shape Claude Code's own hook files use (see e.g.
//     kata-harness's plugin/hooks/hooks.json), so it's what plugin authors
//     porting an existing Claude Code hooks.json actually hand us. Event
//     names found here are translated via ccEventToDojo; a Claude-Code
//     event with no honest dojo equivalent is skipped (logged) rather than
//     mismapped or bolted on as a literal "hooks" pseudo-event.
//
// Before this function grew wrapper support, feeding it a wrapped file
// meant unmarshalling {"hooks": {...}} (an object) into map[string][]hookEntry
// (expects every value to be an array) — a hard type-mismatch error, so
// loadHooks logged a warning and returned zero rules. The plugin's hooks
// were silently inert either way; this rewrite makes the wrapped shape a
// first-class input instead of a parse failure.
func loadHooks(pluginDir, pluginName string) []HookRule {
	hooksPath := filepath.Join(pluginDir, "hooks", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		return nil
	}

	eventMap, wrapped, err := parseHooksJSON(data)
	if err != nil {
		log.Printf("[plugins] warning: skipping hooks for plugin at %s — failed to parse hooks.json: %v", pluginDir, err)
		return nil
	}

	var rules []HookRule
	for event, entries := range eventMap {
		ruleEvent := event
		if wrapped {
			dojoEvent, ok := ccEventToDojo(event)
			if !ok {
				log.Printf("[plugins] note: plugin %q hooks.json — Claude-Code event %q has no dojo equivalent, skipping %d hook(s)", pluginName, event, len(entries))
				continue
			}
			ruleEvent = dojoEvent
		}
		if strings.EqualFold(ruleEvent, "hooks") {
			// Defense in depth: a literal top-level "hooks" key that isn't
			// the wrapper (e.g. the degenerate flat shape {"hooks": [...]})
			// would otherwise slip through as a pseudo-event that can never
			// match any real dojo event — exactly the trap that made
			// wrapped hooks.json files silently inert before this fix.
			// Refuse to reproduce it under any input shape.
			log.Printf("[plugins] warning: plugin %q hooks.json has a top-level %q key that isn't a real event — skipping %d hook(s); wrapped Claude-Code hooks belong under {\"hooks\": {\"<Event>\": [...]}}", pluginName, event, len(entries))
			continue
		}
		for _, entry := range entries {
			rules = append(rules, HookRule{
				Event:   ruleEvent,
				Matcher: entry.Matcher,
				If:      entry.If,
				Hooks:   entry.Hooks,
			})
		}
	}
	return rules
}

// parseHooksJSON parses the raw bytes of a hooks.json file, trying the
// Claude-Code wrapper schema first and falling back to the flat dojo-native
// schema — see loadHooks for the shape of each. Returns the parsed
// event->entries map, whether the wrapped schema matched (so the caller
// knows to run Claude-Code -> dojo event translation), and any parse error.
func parseHooksJSON(data []byte) (eventMap map[string][]hookEntry, wrapped bool, err error) {
	var probe struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	if probeErr := json.Unmarshal(data, &probe); probeErr == nil && len(probe.Hooks) > 0 {
		var inner map[string][]hookEntry
		if innerErr := json.Unmarshal(probe.Hooks, &inner); innerErr == nil {
			return inner, true, nil
		}
		// "hooks" was present but wasn't an event-name-keyed object (e.g.
		// some other shape entirely) — fall through and try the flat schema
		// below; if that also fails, its error is what gets reported.
	}

	var flat map[string][]hookEntry
	if flatErr := json.Unmarshal(data, &flat); flatErr != nil {
		return nil, false, flatErr
	}
	return flat, false, nil
}

// ccEventToDojo translates a hook event name found inside the Claude-Code
// wrapper schema to its dojo-cli equivalent. Two kinds of input are
// accepted: dojo's own event names (identity passthrough, in case a wrapped
// hooks.json already uses dojo's vocabulary) and Claude Code's hook event
// names, mapped to their closest dojo counterpart:
//
//	PreToolUse   -> PreCommand   (about to run a tool/command)
//	PostToolUse  -> PostCommand  (a tool/command just finished)
//	SubagentStop -> PostAgent    (a dispatched subagent finished)
//	SessionStart -> SessionStart (same concept, dojo has it natively —
//	                              covered by the identity case below)
//	SessionEnd   -> SessionEnd   (same concept, dojo has it natively —
//	                              covered by the identity case below)
//
// SessionStart used to be deliberately unmapped: dojo-cli had no "beginning
// of session" event among its 5 (PreCommand/PostCommand/PostSkill/PostAgent/
// SessionEnd), and SessionEnd was a false friend for it, not "the closest" —
// mapping SessionStart -> SessionEnd would have fired a startup hook (e.g.
// kata-harness's roll-status-injector, whose entire job is injecting status
// at session start) at the END of the session instead — wrong, not just
// imprecise. Now that dojo-cli fires its own EventSessionStart at REPL
// startup (see internal/repl.REPL.fireSessionStart), the identity mapping
// below is honest, not a guess.
//
// Still deliberately NOT mapped, for the same reason SessionStart used to be
// (no honest dojo lifecycle counterpart): Notification, UserPromptSubmit,
// Stop, PreCompact. Callers report these as "no dojo equivalent" and skip
// them rather than silently mismapping them.
func ccEventToDojo(event string) (string, bool) {
	switch event {
	case dojoEventPreCommand, dojoEventPostCommand, dojoEventPostSkill, dojoEventPostAgent, dojoEventSessionStart, dojoEventSessionEnd:
		return event, true
	case "PreToolUse":
		return dojoEventPreCommand, true
	case "PostToolUse":
		return dojoEventPostCommand, true
	case "SubagentStop":
		return dojoEventPostAgent, true
	default:
		return "", false
	}
}

// countFiles counts files matching a glob pattern inside dir.
func countFiles(dir, pattern string) int {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return 0
	}
	return len(matches)
}

// countSkills counts subdirectories of skillsDir that contain a SKILL.md file.
func countSkills(skillsDir string) int {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMD := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			count++
		}
	}
	return count
}
