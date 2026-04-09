package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// overrideStateDir sets the HOME env var so config.DojoDir() resolves to a
// temp directory for the duration of the test.
func overrideStateDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return filepath.Join(tmp, ".dojo", "state.json")
}

// TestLoadNoFile verifies that Load returns an empty state when no file exists.
func TestLoadNoFile(t *testing.T) {
	overrideStateDir(t)

	s, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if s == nil {
		t.Fatal("Load() returned nil state")
	}
	if s.LastSessionID != "" {
		t.Errorf("LastSessionID = %q; want empty", s.LastSessionID)
	}
	if len(s.Agents) != 0 {
		t.Errorf("Agents len = %d; want 0", len(s.Agents))
	}
}

// TestSaveAndLoad writes state to disk and reads it back.
func TestSaveAndLoad(t *testing.T) {
	overrideStateDir(t)

	s := &State{
		LastSessionID: "sess-abc123",
		Agents:        make(map[string]Agent),
	}
	s.AddAgent("agent-001", "focused")

	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LastSessionID != s.LastSessionID {
		t.Errorf("LastSessionID = %q; want %q", loaded.LastSessionID, s.LastSessionID)
	}
	a, ok := loaded.Agents["agent-001"]
	if !ok {
		t.Fatal("agent-001 not found in loaded state")
	}
	if a.Mode != "focused" {
		t.Errorf("Mode = %q; want focused", a.Mode)
	}
	if a.AgentID != "agent-001" {
		t.Errorf("AgentID = %q; want agent-001", a.AgentID)
	}
}

// TestAddAgent verifies the agent is recorded in the map with correct fields.
func TestAddAgent(t *testing.T) {
	overrideStateDir(t)

	s := &State{Agents: make(map[string]Agent)}
	// Truncate to second precision to match RFC3339 format granularity.
	before := time.Now().UTC().Truncate(time.Second)
	s.AddAgent("ag-xyz", "exploratory")
	after := time.Now().UTC().Add(time.Second).Truncate(time.Second)

	a, ok := s.Agents["ag-xyz"]
	if !ok {
		t.Fatal("agent ag-xyz not found after AddAgent")
	}
	if a.AgentID != "ag-xyz" {
		t.Errorf("AgentID = %q; want ag-xyz", a.AgentID)
	}
	if a.Mode != "exploratory" {
		t.Errorf("Mode = %q; want exploratory", a.Mode)
	}
	createdAt, err := time.Parse(time.RFC3339, a.CreatedAt)
	if err != nil {
		t.Fatalf("CreatedAt parse error: %v", err)
	}
	if createdAt.Before(before) || createdAt.After(after) {
		t.Errorf("CreatedAt %v out of expected range [%v, %v]", createdAt, before, after)
	}
}

// TestTouchAgent verifies last_used is updated after Touch.
func TestTouchAgent(t *testing.T) {
	overrideStateDir(t)

	s := &State{Agents: make(map[string]Agent)}
	// Add an agent with a deliberately old timestamp.
	s.Agents["ag-touch"] = Agent{
		AgentID:   "ag-touch",
		Mode:      "balanced",
		CreatedAt: "2020-01-01T00:00:00Z",
		LastUsed:  "2020-01-01T00:00:00Z",
	}

	// Truncate to second precision to match RFC3339 format granularity.
	before := time.Now().UTC().Truncate(time.Second)
	s.TouchAgent("ag-touch")
	after := time.Now().UTC().Add(time.Second).Truncate(time.Second)

	a := s.Agents["ag-touch"]
	lastUsed, err := time.Parse(time.RFC3339, a.LastUsed)
	if err != nil {
		t.Fatalf("LastUsed parse error: %v", err)
	}
	if lastUsed.Before(before) || lastUsed.After(after) {
		t.Errorf("LastUsed %v out of expected range [%v, %v]", lastUsed, before, after)
	}
}

// TestRecentAgents verifies the order is newest-first and the count is capped.
func TestRecentAgents(t *testing.T) {
	overrideStateDir(t)

	s := &State{Agents: make(map[string]Agent)}
	s.Agents["old"] = Agent{
		AgentID:  "old",
		Mode:     "balanced",
		LastUsed: "2023-01-01T00:00:00Z",
	}
	s.Agents["mid"] = Agent{
		AgentID:  "mid",
		Mode:     "focused",
		LastUsed: "2024-06-01T00:00:00Z",
	}
	s.Agents["new"] = Agent{
		AgentID:  "new",
		Mode:     "deliberate",
		LastUsed: "2025-12-01T00:00:00Z",
	}

	recent := s.RecentAgents(2)
	if len(recent) != 2 {
		t.Fatalf("RecentAgents(2) len = %d; want 2", len(recent))
	}
	if recent[0].AgentID != "new" {
		t.Errorf("recent[0] = %q; want new", recent[0].AgentID)
	}
	if recent[1].AgentID != "mid" {
		t.Errorf("recent[1] = %q; want mid", recent[1].AgentID)
	}

	// Passing 0 should return all agents.
	all := s.RecentAgents(0)
	if len(all) != 3 {
		t.Errorf("RecentAgents(0) len = %d; want 3", len(all))
	}

	// Verify that the file doesn't need to exist for state ops to work.
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".dojo", "state.json")); !os.IsNotExist(err) {
		// File may or may not exist — this check just ensures no panic.
	}
}
