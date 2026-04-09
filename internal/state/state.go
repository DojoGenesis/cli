// Package state persists agent IDs and session info across REPL invocations.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DojoGenesis/dojo-cli/internal/config"
)

// State persists across REPL sessions at ~/.dojo/state.json.
type State struct {
	LastSessionID string           `json:"last_session_id,omitempty"`
	Agents        map[string]Agent `json:"agents,omitempty"` // keyed by agent_id
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
	return s, nil
}

// Save writes the state to ~/.dojo/state.json with 0600 permissions.
func (s *State) Save() error {
	dir := config.DojoDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0600)
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
