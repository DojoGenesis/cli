// Package activity records a timestamped NDJSON activity log at ~/.dojo/activity.log.
package activity

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/DojoGenesis/cli/internal/config"
)

// EntryType classifies the kind of activity recorded.
type EntryType string

const (
	CommandRun      EntryType = "command_run"
	SkillInvoked    EntryType = "skill_invoked"
	ArtifactSaved   EntryType = "artifact_saved"
	SessionStarted  EntryType = "session_started"
	ModelChanged    EntryType = "model_changed"
	AgentDispatched EntryType = "agent_dispatched"
	ProjectCreated  EntryType = "project_created"
	PhaseAdvanced   EntryType = "phase_advanced"
	ErrorOccurred   EntryType = "error_occurred"
)

// Entry is a single activity log record.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	Type       EntryType `json:"type"`
	Summary    string    `json:"summary"`
	Details    string    `json:"details,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
}

// LogPath returns the path to the activity log file (~/.dojo/activity.log).
func LogPath() string {
	return filepath.Join(config.DojoDir(), "activity.log")
}

// Append encodes e as a JSON line and appends it to the activity log.
// The ~/.dojo directory is created if it does not already exist.
func Append(e Entry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	dir := config.DojoDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.OpenFile(LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // write result already captured via f.Write below; close failure on a best-effort activity log is not actionable

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	_, err = f.Write(line)
	return err
}

// Log records a minimal activity entry with the current timestamp.
// Errors from Append are silently ignored so call-sites stay clean.
func Log(t EntryType, summary string) {
	_ = Append(Entry{
		Timestamp: time.Now().UTC(),
		Type:      t,
		Summary:   summary,
	})
}

// LogWithDetails records an activity entry that includes an optional detail string.
func LogWithDetails(t EntryType, summary, details string) {
	_ = Append(Entry{
		Timestamp: time.Now().UTC(),
		Type:      t,
		Summary:   summary,
		Details:   details,
	})
}

// Recent reads the activity log and returns the newest-first entries, up to n.
// If n <= 0 all entries are returned.
// If the log file does not exist, Recent returns nil, nil (not an error).
func Recent(n int) ([]Entry, error) {
	data, err := os.Open(LogPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer data.Close() //nolint:errcheck // read-only fd; all data already consumed via scanner, close failure has nothing left to affect

	var entries []Entry
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			// Skip malformed lines rather than aborting.
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reverse in-place to get newest-first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if n > 0 && len(entries) > n {
		entries = entries[:n]
	}
	return entries, nil
}

// Clear truncates the activity log to zero bytes.
// If the file does not exist, Clear is a no-op.
func Clear() error {
	f, err := os.OpenFile(LogPath(), os.O_TRUNC|os.O_WRONLY, 0600)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}
