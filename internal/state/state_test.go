package state

import (
	"fmt"
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

	// Verify that the file doesn't need to exist for state ops to work — the
	// file may or may not exist; this call just ensures no panic.
	_, _ = os.Stat(filepath.Join(os.Getenv("HOME"), ".dojo", "state.json"))
}

// ─── Session history (W5-RESUME) ────────────────────────────────────────────
//
// These tests cover State.RecordSession / State.History directly (append,
// cap, de-dup, ordering) plus the two integration points the rest of the
// resume feature depends on: Save()'s auto-record hook (so repl.go's direct
// `st.LastSessionID = id; st.Save()` — untouched by this change — still
// produces history) and SaveSession's wiring (what cmd/dojo/main.go's
// --session flag and internal/commands/cmd_session.go both call through).

// TestHistoryEmptyByDefault verifies a freshly loaded state (no file, no
// sessions recorded yet) reports an empty history, not an error.
func TestHistoryEmptyByDefault(t *testing.T) {
	overrideStateDir(t)

	s, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if got := s.History(); len(got) != 0 {
		t.Errorf("History() len = %d; want 0, got %+v", len(got), got)
	}
}

// TestRecordSessionAppendsMostRecentFirst verifies recording distinct
// session IDs builds a most-recent-first list with valid RFC3339 timestamps.
func TestRecordSessionAppendsMostRecentFirst(t *testing.T) {
	s := &State{}
	s.RecordSession("sess-1")
	s.RecordSession("sess-2")
	s.RecordSession("sess-3")

	hist := s.History()
	if len(hist) != 3 {
		t.Fatalf("History() len = %d; want 3", len(hist))
	}
	wantOrder := []string{"sess-3", "sess-2", "sess-1"}
	for i, want := range wantOrder {
		if hist[i].ID != want {
			t.Errorf("History()[%d].ID = %q; want %q", i, hist[i].ID, want)
		}
	}
	for i, e := range hist {
		if _, err := time.Parse(time.RFC3339, e.SavedAt); err != nil {
			t.Errorf("History()[%d].SavedAt = %q not RFC3339: %v", i, e.SavedAt, err)
		}
	}
}

// TestRecordSessionDedup verifies re-recording an existing ID moves it to
// the front instead of duplicating it.
func TestRecordSessionDedup(t *testing.T) {
	s := &State{}
	s.RecordSession("sess-a")
	s.RecordSession("sess-b")
	s.RecordSession("sess-c")

	// sess-a becomes active again — should move to front, not duplicate.
	s.RecordSession("sess-a")

	hist := s.History()
	if len(hist) != 3 {
		t.Fatalf("History() len = %d; want 3 (no duplicate entries)", len(hist))
	}
	if hist[0].ID != "sess-a" {
		t.Errorf("History()[0].ID = %q; want sess-a (moved to front)", hist[0].ID)
	}
	seen := 0
	for _, e := range hist {
		if e.ID == "sess-a" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("sess-a appears %d times in History(); want exactly 1", seen)
	}
}

// TestRecordSessionCap verifies the history is capped at maxSessionHistory,
// dropping the oldest entries first.
func TestRecordSessionCap(t *testing.T) {
	s := &State{}
	const total = maxSessionHistory + 5
	for i := 0; i < total; i++ {
		s.RecordSession(fmt.Sprintf("sess-%02d", i))
	}

	hist := s.History()
	if len(hist) != maxSessionHistory {
		t.Fatalf("History() len = %d; want %d (capped)", len(hist), maxSessionHistory)
	}
	wantNewest := fmt.Sprintf("sess-%02d", total-1)
	if hist[0].ID != wantNewest {
		t.Errorf("History()[0].ID = %q; want %q (most recently recorded)", hist[0].ID, wantNewest)
	}
	for _, e := range hist {
		if e.ID == "sess-00" || e.ID == "sess-04" {
			t.Errorf("History() still contains %q; want it evicted (oldest, over cap)", e.ID)
		}
	}
}

// TestRecordSessionEmptyIDNoop verifies an empty session ID is ignored
// rather than recorded as a blank history entry.
func TestRecordSessionEmptyIDNoop(t *testing.T) {
	s := &State{}
	s.RecordSession("")
	if got := s.History(); len(got) != 0 {
		t.Errorf("History() len = %d after RecordSession(\"\"); want 0", len(got))
	}
}

// TestSaveRecordsHistoryFromLastSessionID verifies Save() alone — with no
// explicit RecordSession call — still populates history from whatever
// LastSessionID is set to. This is the hook that lets repl.go's direct
// `st.LastSessionID = id; st.Save()` pattern (internal/repl/repl.go, out of
// scope for this change) participate in history without repl.go needing to
// call RecordSession itself.
func TestSaveRecordsHistoryFromLastSessionID(t *testing.T) {
	overrideStateDir(t)

	s := &State{LastSessionID: "sess-direct-assign"}
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	hist := loaded.History()
	if len(hist) != 1 || hist[0].ID != "sess-direct-assign" {
		t.Fatalf("History() = %+v; want single entry sess-direct-assign", hist)
	}
}

// TestHistoryBackwardCompatibleMissingKey verifies a pre-history state.json
// (written before SessionHistory existed, so no "history" key at all) loads
// cleanly with an empty History() — no error, exactly the contract W5-RESUME
// requires.
func TestHistoryBackwardCompatibleMissingKey(t *testing.T) {
	path := overrideStateDir(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	oldFormat := `{"last_session_id":"pre-history-session","setup_complete":true,"agents":{}}`
	if err := os.WriteFile(path, []byte(oldFormat), 0600); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	s, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil for a pre-history state.json", err)
	}
	if s.LastSessionID != "pre-history-session" {
		t.Errorf("LastSessionID = %q; want pre-history-session", s.LastSessionID)
	}
	if got := s.History(); len(got) != 0 {
		t.Errorf("History() len = %d; want 0 for a pre-history state.json, got %+v", len(got), got)
	}
}

// TestSaveSessionWiring verifies the mechanism cmd/dojo/main.go's --session
// flag relies on: SaveSession(id) persists id as LastSessionID (and records
// it in history), so a subsequent Load() — exactly what repl.New(resume=true)
// performs internally — returns that same id. This is what lets --session
// <id> resume the SPECIFIC session requested rather than whatever was
// previously last active, without repl.New's signature changing.
func TestSaveSessionWiring(t *testing.T) {
	overrideStateDir(t)

	// An existing "last active" session before --session is used.
	SaveSession("previously-active-session")

	// Simulates `dojo --session targeted-session`: main.go calls SaveSession
	// with the requested id before constructing the REPL.
	SaveSession("targeted-session")

	// Simulates repl.New(resume=true)'s own load-and-restore.
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LastSessionID != "targeted-session" {
		t.Errorf("LastSessionID = %q; want targeted-session", loaded.LastSessionID)
	}
	hist := loaded.History()
	if len(hist) == 0 || hist[0].ID != "targeted-session" {
		t.Fatalf("History()[0] = %+v; want targeted-session at front", hist)
	}
	found := false
	for _, e := range hist {
		if e.ID == "previously-active-session" {
			found = true
		}
	}
	if !found {
		t.Errorf("History() = %+v; want previously-active-session still retained (demoted, not dropped)", hist)
	}
}
