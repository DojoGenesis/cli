// Package state persists agent IDs and session info across REPL invocations.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/ioutilx"
	"github.com/DojoGenesis/cli/internal/spirit"
)

// GuideProgress tracks the user's active and completed guides.
type GuideProgress struct {
	Active    string   `json:"active,omitempty"`    // guide ID currently in progress
	Step      int      `json:"step"`                // current step index (0-based)
	Completed []string `json:"completed,omitempty"` // IDs of finished guides
}

// SessionEntry records one session's ID and when it was last active. Powers
// `/session ls` and the friendly (non-blocking) validation on
// `/session resume <id>` — see internal/commands/cmd_session.go.
type SessionEntry struct {
	ID      string `json:"id"`
	SavedAt string `json:"saved_at"` // RFC3339, UTC
}

// maxSessionHistory bounds State.SessionHistory. Oldest entries are dropped
// once the cap is exceeded — see RecordSession.
const maxSessionHistory = 20

// State persists across REPL sessions at ~/.dojo/state.json.
type State struct {
	LastSessionID string             `json:"last_session_id,omitempty"`
	SetupComplete bool               `json:"setup_complete,omitempty"`
	Agents        map[string]Agent   `json:"agents,omitempty"` // keyed by agent_id
	Spirit        spirit.SpiritState `json:"spirit,omitempty"`
	Guide         GuideProgress      `json:"guide,omitempty"`
	// SessionHistory holds recent sessions, most-recent-first. A state.json
	// written before this field existed simply has no "history" key, which
	// decodes to a nil slice — empty, never an error. See RecordSession/History.
	SessionHistory []SessionEntry `json:"history,omitempty"`
}

// Agent holds metadata about a known agent.
type Agent struct {
	AgentID   string `json:"agent_id"`
	Mode      string `json:"mode"`       // focused|balanced|exploratory|deliberate
	CreatedAt string `json:"created_at"` // RFC3339
	LastUsed  string `json:"last_used"`  // RFC3339
}

func statePath() string {
	return filepath.Join(config.DojoDir(), "state.json")
}

// Load reads ~/.dojo/state.json. Returns empty state if file doesn't exist.
func Load() (*State, error) {
	s := &State{
		Agents: make(map[string]Agent),
	}
	data, err := os.ReadFile(statePath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	// Ensure the map is initialised even if the JSON had null.
	if s.Agents == nil {
		s.Agents = make(map[string]Agent)
	}
	if s.Spirit.Unlocked == nil {
		s.Spirit.Unlocked = make(map[string]string)
	}
	return s, nil
}

// Save writes the state to ~/.dojo/state.json atomically with 0600 permissions.
// Before marshalling it folds LastSessionID into the session history (see
// RecordSession) so callers that set LastSessionID directly rather than going
// through SaveSession — e.g. repl.go's post-startup
// `st.LastSessionID = r.session; st.Save()` — still produce/refresh a history
// entry without this package needing repl.go to call RecordSession itself.
func (s *State) Save() error {
	if s.LastSessionID != "" {
		s.RecordSession(s.LastSessionID)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return ioutilx.AtomicWriteFile(statePath(), data, 0600)
}

// AddAgent records an agent in the state.
func (s *State) AddAgent(agentID, mode string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.Agents[agentID] = Agent{
		AgentID:   agentID,
		Mode:      mode,
		CreatedAt: now,
		LastUsed:  now,
	}
}

// TouchAgent updates the last_used timestamp for an agent.
func (s *State) TouchAgent(agentID string) {
	if a, ok := s.Agents[agentID]; ok {
		a.LastUsed = time.Now().UTC().Format(time.RFC3339)
		s.Agents[agentID] = a
	}
}

// SaveSession updates the last session ID and saves state.
func SaveSession(sessionID string) {
	st, err := Load()
	if err != nil {
		st = &State{}
	}
	st.LastSessionID = sessionID
	_ = st.Save()
}

// RecordSession records sessionID as the most-recently-active session. An
// existing entry for the same ID is moved to the front (with a refreshed
// timestamp) rather than duplicated, and the list is capped at
// maxSessionHistory entries, dropping the oldest first. A no-op for an empty
// ID. Called automatically from Save whenever LastSessionID is set, so most
// callers never need to call this directly.
func (s *State) RecordSession(sessionID string) {
	if sessionID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)

	entries := make([]SessionEntry, 0, len(s.SessionHistory)+1)
	entries = append(entries, SessionEntry{ID: sessionID, SavedAt: now})
	for _, e := range s.SessionHistory {
		if e.ID != sessionID {
			entries = append(entries, e)
		}
	}
	if len(entries) > maxSessionHistory {
		entries = entries[:maxSessionHistory]
	}
	s.SessionHistory = entries
}

// History returns the recorded session history, most-recent-first. Empty (a
// nil slice, safe to range over) when state.json predates this field or no
// session has been recorded yet — never an error.
func (s *State) History() []SessionEntry {
	return s.SessionHistory
}

// RecentAgents returns agents sorted by last_used (newest first), max n.
func (s *State) RecentAgents(n int) []Agent {
	agents := make([]Agent, 0, len(s.Agents))
	for _, a := range s.Agents {
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].LastUsed > agents[j].LastUsed
	})
	if n > 0 && len(agents) > n {
		agents = agents[:n]
	}
	return agents
}
