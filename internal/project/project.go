// Package project manages dojo project lifecycle: phases, tracks, decisions,
// artifacts, and the global project registry stored under ~/.dojo/projects/.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/ioutilx"
)

// Phase represents the current lifecycle stage of a project.
type Phase string

const (
	PhaseInitialized   Phase = "initialized"
	PhaseScouting      Phase = "scouting"
	PhaseSpecifying    Phase = "specifying"
	PhaseDecomposing   Phase = "decomposing"
	PhaseCommissioning Phase = "commissioning"
	PhaseImplementing  Phase = "implementing"
	PhaseRetrospective Phase = "retrospective"
	PhaseArchived      Phase = "archived"
)

// PhaseIndicator maps each phase to a short status glyph shown in the REPL.
var PhaseIndicator = map[Phase]string{
	PhaseInitialized:   "[ ]",
	PhaseScouting:      "[~]",
	PhaseSpecifying:    "[~]",
	PhaseDecomposing:   "[~]",
	PhaseCommissioning: "[~]",
	PhaseImplementing:  "[>]",
	PhaseRetrospective: "[*]",
	PhaseArchived:      "[x]",
}

// PhaseNextAction maps each phase to a short contextual suggestion.
var PhaseNextAction = map[Phase]string{
	PhaseInitialized:   "Run /project scout <tension> to begin exploring",
	PhaseScouting:      "Run /project spec <feature> to write a specification",
	PhaseSpecifying:    "Run /project decompose to break the spec into tracks",
	PhaseDecomposing:   "Run /project commission to assign tracks to agents",
	PhaseCommissioning: "Run /project implement to start executing tracks",
	PhaseImplementing:  "Run /project retro to reflect and close the cycle",
	PhaseRetrospective: "Run /project archive to mark this project complete",
	PhaseArchived:      "This project is archived — start a new one with /project new",
}

// TrackStatus represents the current state of a work track.
type TrackStatus string

const (
	TrackPending    TrackStatus = "pending"
	TrackInProgress TrackStatus = "in_progress"
	TrackCompleted  TrackStatus = "completed"
	TrackBlocked    TrackStatus = "blocked"
)

// Track is a named unit of parallel work within a project.
type Track struct {
	ID           int         `json:"id"`
	Name         string      `json:"name"`
	Status       TrackStatus `json:"status"`
	Dependencies []int       `json:"dependencies,omitempty"`
	Notes        string      `json:"notes,omitempty"`
	UpdatedAt    string      `json:"updated_at"`
}

// Decision records an architectural or strategic decision made during the project.
type Decision struct {
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"` // RFC3339
}

// ActivityEntry is a single entry in the project activity log.
type ActivityEntry struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Summary   string `json:"summary"`
}

const maxActivityEntries = 50

// Project is the top-level project record stored under ~/.dojo/projects/<id>/state.json.
type Project struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Phase       Phase           `json:"phase"`
	Tracks      []Track         `json:"tracks,omitempty"`
	Decisions   []Decision      `json:"decisions,omitempty"`
	Artifacts   []string        `json:"artifacts,omitempty"`
	ActivityLog []ActivityEntry `json:"activity_log,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// GlobalState holds the active project pointer and the ordered list of all project IDs.
type GlobalState struct {
	ActiveProjectID string   `json:"active_project_id,omitempty"`
	ProjectIDs      []string `json:"project_ids,omitempty"`
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// ProjectsDir returns ~/.dojo/projects.
func ProjectsDir() string {
	return filepath.Join(config.DojoDir(), "projects")
}

func globalStatePath() string {
	return filepath.Join(ProjectsDir(), "global.json")
}

func projectDir(id string) string {
	return filepath.Join(ProjectsDir(), id)
}

func projectStatePath(id string) string {
	return filepath.Join(projectDir(id), "state.json")
}

// ---------------------------------------------------------------------------
// Global state
// ---------------------------------------------------------------------------

// LoadGlobalState reads ~/.dojo/projects/global.json.
// Returns an empty state if the file does not exist.
func LoadGlobalState() (*GlobalState, error) {
	gs := &GlobalState{}
	data, err := os.ReadFile(globalStatePath())
	if os.IsNotExist(err) {
		return gs, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, gs); err != nil {
		return nil, err
	}
	return gs, nil
}

// SaveGlobalState writes the global state to ~/.dojo/projects/global.json atomically.
func SaveGlobalState(gs *GlobalState) error {
	data, err := json.MarshalIndent(gs, "", "  ")
	if err != nil {
		return err
	}
	return ioutilx.AtomicWriteFile(globalStatePath(), data, 0600)
}

// ---------------------------------------------------------------------------
// Project persistence
// ---------------------------------------------------------------------------

// Load reads a project by ID. Returns nil, nil if the project directory does
// not exist (caller must decide whether that is an error).
func Load(id string) (*Project, error) {
	data, err := os.ReadFile(projectStatePath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Save writes the project state to disk atomically and creates the standard
// subdirectories.
func (p *Project) Save() error {
	dir := projectDir(p.ID)
	for _, sub := range []string{"scouts", "specs", "tracks", "prompts", "retros", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return err
		}
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return ioutilx.AtomicWriteFile(projectStatePath(p.ID), data, 0600)
}

// ---------------------------------------------------------------------------
// Project lifecycle
// ---------------------------------------------------------------------------

// Create makes a new project, sets it as active, and persists both the project
// and the global state.
func Create(name, description string) (*Project, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := slugify(name)

	// Ensure uniqueness by appending a timestamp suffix when a collision exists.
	if _, err := os.Stat(projectDir(id)); err == nil {
		id = fmt.Sprintf("%s-%d", id, time.Now().UnixMilli())
	}

	p := &Project{
		ID:          id,
		Name:        name,
		Description: description,
		Phase:       PhaseInitialized,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	p.appendActivity("create", fmt.Sprintf("Project %q created", name))

	if err := p.Save(); err != nil {
		return nil, err
	}

	gs, err := LoadGlobalState()
	if err != nil {
		return nil, err
	}
	gs.ActiveProjectID = id
	gs.ProjectIDs = append(gs.ProjectIDs, id)
	if err := SaveGlobalState(gs); err != nil {
		return nil, err
	}

	return p, nil
}

// ActiveProject returns the currently active project, or nil if none is set.
func ActiveProject() (*Project, error) {
	gs, err := LoadGlobalState()
	if err != nil {
		return nil, err
	}
	if gs.ActiveProjectID == "" {
		return nil, nil
	}
	return Load(gs.ActiveProjectID)
}

// GetProject loads a project by ID and returns an error if it is not found.
func GetProject(id string) (*Project, error) {
	p, err := Load(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("project %q not found", id)
	}
	return p, nil
}

// Switch sets the active project in global state.
func Switch(id string) error {
	// Confirm the project exists before updating the pointer.
	if _, err := GetProject(id); err != nil {
		return err
	}
	gs, err := LoadGlobalState()
	if err != nil {
		return err
	}
	gs.ActiveProjectID = id
	return SaveGlobalState(gs)
}

// ListAll returns all projects. Pass includeArchived=false to skip archived ones.
func ListAll(includeArchived bool) ([]*Project, error) {
	gs, err := LoadGlobalState()
	if err != nil {
		return nil, err
	}
	var out []*Project
	for _, id := range gs.ProjectIDs {
		p, err := Load(id)
		if err != nil || p == nil {
			continue
		}
		if !includeArchived && p.Phase == PhaseArchived {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// Archive sets a project's phase to PhaseArchived and saves it.
func Archive(id string) error {
	p, err := GetProject(id)
	if err != nil {
		return err
	}
	p.SetPhase(PhaseArchived)
	return p.Save()
}

// ---------------------------------------------------------------------------
// Project mutation helpers
// ---------------------------------------------------------------------------

// SetPhase updates the project phase and appends an activity entry.
func (p *Project) SetPhase(phase Phase) {
	p.appendActivity("phase_change", fmt.Sprintf("Phase changed from %s to %s", p.Phase, phase))
	p.Phase = phase
}

// AddTrack appends a new track to the project and returns it.
func (p *Project) AddTrack(name string, deps []int) Track {
	id := len(p.Tracks) + 1
	t := Track{
		ID:           id,
		Name:         name,
		Status:       TrackPending,
		Dependencies: deps,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	p.Tracks = append(p.Tracks, t)
	p.appendActivity("add_track", fmt.Sprintf("Track %d %q added", id, name))
	return t
}

// AddDecision appends a decision record to the project.
func (p *Project) AddDecision(summary string) {
	p.Decisions = append(p.Decisions, Decision{
		Summary:   summary,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	p.appendActivity("add_decision", summary)
}

// AddArtifact appends a relative artifact path to the project's artifact list.
func (p *Project) AddArtifact(relPath string) {
	p.Artifacts = append(p.Artifacts, relPath)
	p.appendActivity("add_artifact", relPath)
}

// SuggestNext returns a short actionable suggestion based on the current phase.
func (p *Project) SuggestNext() string {
	if hint, ok := PhaseNextAction[p.Phase]; ok {
		return hint
	}
	return "No suggestion available for this phase"
}

// appendActivity adds an entry to the activity log, capping at maxActivityEntries.
func (p *Project) appendActivity(action, summary string) {
	entry := ActivityEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Action:    action,
		Summary:   summary,
	}
	p.ActivityLog = append(p.ActivityLog, entry)
	if len(p.ActivityLog) > maxActivityEntries {
		p.ActivityLog = p.ActivityLog[len(p.ActivityLog)-maxActivityEntries:]
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9-]`)
var multiDash = regexp.MustCompile(`-+`)

// slugify converts a project name into a URL-safe, lowercase identifier.
// Spaces, hyphens, and underscores are collapsed into a single dash; all
// other non-alphanumeric characters are stripped.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.NewReplacer(" ", "-", "_", "-").Replace(s)
	s = nonAlphaNum.ReplaceAllString(s, "")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "project"
	}
	return s
}
