package plugins

import (
	"path/filepath"
	"testing"
)

// TestLoadHooks_PopulatesBlockingFromJSON proves the "blocking" key in a
// hooks.json rule is parsed into HookRule.Blocking, and that its absence
// defaults to false. This is the wiring the runner's FireChecked relies on to
// know which rules may veto a command.
func TestLoadHooks_PopulatesBlockingFromJSON(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "gatekeeper")
	mustMkdir(t, pluginDir)
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), map[string]any{
		"name":    "gatekeeper",
		"version": "1.0",
	})
	mustMkdir(t, filepath.Join(pluginDir, "hooks"))
	// Two rules on the same event: one blocking, one not (blocking key omitted).
	writeJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), map[string]any{
		"PreCommand": []map[string]any{
			{
				"matcher":  "deploy*",
				"blocking": true,
				"hooks":    []map[string]any{{"type": "command", "command": "guard.sh"}},
			},
			{
				"matcher": "*",
				"hooks":   []map[string]any{{"type": "command", "command": "log.sh"}},
			},
		},
	})

	plugins, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() returned error: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}

	var blocking, nonBlocking *HookRule
	for i := range plugins[0].HookRules {
		r := &plugins[0].HookRules[i]
		switch r.Matcher {
		case "deploy*":
			blocking = r
		case "*":
			nonBlocking = r
		}
	}
	if blocking == nil || nonBlocking == nil {
		t.Fatalf("expected both rules to load; got %+v", plugins[0].HookRules)
	}
	if !blocking.Blocking {
		t.Error(`rule with "blocking": true should have Blocking == true`)
	}
	if nonBlocking.Blocking {
		t.Error("rule with no blocking key should default to Blocking == false")
	}
}

// TestLoadHooks_BlockingUnderWrappedSchema proves the blocking key is also read
// from the Claude-Code wrapper schema ({"hooks": {"<Event>": [...]}}), not only
// the flat dojo-native schema.
func TestLoadHooks_BlockingUnderWrappedSchema(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "wrapped")
	mustMkdir(t, pluginDir)
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), map[string]any{
		"name":    "wrapped",
		"version": "1.0",
	})
	mustMkdir(t, filepath.Join(pluginDir, "hooks"))
	writeJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher":  "*",
					"blocking": true,
					"hooks":    []map[string]any{{"type": "command", "command": "guard.sh"}},
				},
			},
		},
	})

	plugins, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() returned error: %v", err)
	}
	if len(plugins) != 1 || len(plugins[0].HookRules) != 1 {
		t.Fatalf("expected 1 plugin with 1 rule, got %+v", plugins)
	}
	rule := plugins[0].HookRules[0]
	if rule.Event != "PreCommand" { // PreToolUse → PreCommand
		t.Errorf("wrapped event = %q, want PreCommand", rule.Event)
	}
	if !rule.Blocking {
		t.Error("blocking key under the wrapped schema should populate Blocking")
	}
}
