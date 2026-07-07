package project

// Tests for internal/project/project.go.
//
// All tests are hermetic: t.Setenv("HOME", t.TempDir()) redirects
// config.DojoDir() — which calls os.UserHomeDir() — away from the real ~/.dojo.
// No network, no external processes, no mutations to the developer's home dir.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// isoHome sets HOME to a fresh temp directory so config.DojoDir() (and thus
// ProjectsDir()) resolves under an isolated sandbox for the duration of t.
func isoHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// ---------------------------------------------------------------------------
// slugify (exercised indirectly through Create, since it is unexported and
// Create appends a collision suffix only when the slug dir already exists —
// so the FIRST Create of a given name yields the bare slug).
// ---------------------------------------------------------------------------

func TestCreate_SlugifiesName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"My Project", "my-project"},
		{"my_project_name", "my-project-name"},
		{"my-project-name", "my-project-name"},
		{"Hello   World", "hello-world"},               // multiple spaces collapse to one dash
		{"Weird!@# Chars$%^", "weird-chars"},           // non-alphanumerics stripped
		{"  -leading-trailing-  ", "leading-trailing"}, // leading/trailing dashes trimmed
		{"!!!", "project"},                             // empty result falls back to "project"
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isoHome(t)
			p, err := Create(tc.name, "desc")
			if err != nil {
				t.Fatalf("Create(%q): %v", tc.name, err)
			}
			if p.ID != tc.want {
				t.Errorf("Create(%q).ID = %q; want %q", tc.name, p.ID, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// In-memory mutation helpers — no FS needed.
// ---------------------------------------------------------------------------

func TestSetPhase(t *testing.T) {
	p := &Project{Phase: PhaseInitialized}
	p.SetPhase(PhaseScouting)

	if p.Phase != PhaseScouting {
		t.Errorf("Phase = %q; want %q", p.Phase, PhaseScouting)
	}
	if len(p.ActivityLog) != 1 {
		t.Fatalf("ActivityLog len = %d; want 1", len(p.ActivityLog))
	}
	entry := p.ActivityLog[0]
	if entry.Action != "phase_change" {
		t.Errorf("ActivityLog[0].Action = %q; want %q", entry.Action, "phase_change")
	}
	if !strings.Contains(entry.Summary, "initialized") || !strings.Contains(entry.Summary, "scouting") {
		t.Errorf("ActivityLog[0].Summary = %q; want it to mention both phases", entry.Summary)
	}
}

func TestAddTrack(t *testing.T) {
	p := &Project{}

	t1 := p.AddTrack("first track", nil)
	if t1.ID != 1 {
		t.Errorf("first AddTrack ID = %d; want 1", t1.ID)
	}
	if t1.Status != TrackPending {
		t.Errorf("first AddTrack Status = %q; want %q", t1.Status, TrackPending)
	}
	if t1.Name != "first track" {
		t.Errorf("first AddTrack Name = %q; want %q", t1.Name, "first track")
	}
	if t1.Dependencies != nil {
		t.Errorf("first AddTrack Dependencies = %v; want nil", t1.Dependencies)
	}

	t2 := p.AddTrack("second track", []int{1})
	if t2.ID != 2 {
		t.Errorf("second AddTrack ID = %d; want 2", t2.ID)
	}
	if len(t2.Dependencies) != 1 || t2.Dependencies[0] != 1 {
		t.Errorf("second AddTrack Dependencies = %v; want [1]", t2.Dependencies)
	}

	if len(p.Tracks) != 2 {
		t.Fatalf("len(p.Tracks) = %d; want 2", len(p.Tracks))
	}
	if len(p.ActivityLog) != 2 {
		t.Fatalf("len(p.ActivityLog) = %d; want 2", len(p.ActivityLog))
	}
	for i, e := range p.ActivityLog {
		if e.Action != "add_track" {
			t.Errorf("ActivityLog[%d].Action = %q; want %q", i, e.Action, "add_track")
		}
	}
}

func TestAddDecision(t *testing.T) {
	p := &Project{}
	p.AddDecision("use postgres")

	if len(p.Decisions) != 1 {
		t.Fatalf("len(p.Decisions) = %d; want 1", len(p.Decisions))
	}
	d := p.Decisions[0]
	if d.Summary != "use postgres" {
		t.Errorf("Decision.Summary = %q; want %q", d.Summary, "use postgres")
	}
	if d.CreatedAt == "" {
		t.Error("Decision.CreatedAt is empty; want non-empty RFC3339 timestamp")
	}

	if len(p.ActivityLog) != 1 {
		t.Fatalf("len(p.ActivityLog) = %d; want 1", len(p.ActivityLog))
	}
	if p.ActivityLog[0].Action != "add_decision" {
		t.Errorf("ActivityLog[0].Action = %q; want %q", p.ActivityLog[0].Action, "add_decision")
	}
	if p.ActivityLog[0].Summary != "use postgres" {
		t.Errorf("ActivityLog[0].Summary = %q; want %q", p.ActivityLog[0].Summary, "use postgres")
	}
}

func TestAddArtifact(t *testing.T) {
	p := &Project{}
	p.AddArtifact("scouts/tension.md")

	if len(p.Artifacts) != 1 || p.Artifacts[0] != "scouts/tension.md" {
		t.Fatalf("p.Artifacts = %v; want [scouts/tension.md]", p.Artifacts)
	}
	if len(p.ActivityLog) != 1 {
		t.Fatalf("len(p.ActivityLog) = %d; want 1", len(p.ActivityLog))
	}
	entry := p.ActivityLog[0]
	if entry.Action != "add_artifact" {
		t.Errorf("ActivityLog[0].Action = %q; want %q", entry.Action, "add_artifact")
	}
	if entry.Summary != "scouts/tension.md" {
		t.Errorf("ActivityLog[0].Summary = %q; want %q", entry.Summary, "scouts/tension.md")
	}
}

func TestSuggestNext(t *testing.T) {
	cases := []struct {
		phase Phase
		want  string
	}{
		{PhaseInitialized, PhaseNextAction[PhaseInitialized]},
		{PhaseImplementing, PhaseNextAction[PhaseImplementing]},
		{PhaseArchived, PhaseNextAction[PhaseArchived]},
		{Phase("unknown-phase"), "No suggestion available for this phase"},
		{Phase(""), "No suggestion available for this phase"},
	}
	for _, tc := range cases {
		t.Run(string(tc.phase), func(t *testing.T) {
			p := &Project{Phase: tc.phase}
			got := p.SuggestNext()
			if got != tc.want {
				t.Errorf("SuggestNext() with phase %q = %q; want %q", tc.phase, got, tc.want)
			}
		})
	}
}

func TestAppendActivity_CapsAt50AndKeepsNewest(t *testing.T) {
	p := &Project{}

	const total = 60
	for i := 0; i < total; i++ {
		p.AddArtifact(entrySummary(i))
	}

	if len(p.ActivityLog) != maxActivityEntries {
		t.Fatalf("len(p.ActivityLog) = %d; want %d", len(p.ActivityLog), maxActivityEntries)
	}

	// The newest entry (last appended, index total-1) must be present as the
	// final element.
	last := p.ActivityLog[len(p.ActivityLog)-1]
	if last.Summary != entrySummary(total-1) {
		t.Errorf("last ActivityLog entry = %q; want %q", last.Summary, entrySummary(total-1))
	}

	// The oldest entries (e.g. index 0) must have been evicted.
	for _, e := range p.ActivityLog {
		if e.Summary == entrySummary(0) {
			t.Errorf("ActivityLog still contains evicted early entry %q", entrySummary(0))
		}
	}

	// The first surviving entry should be index (total - maxActivityEntries).
	wantFirstSurvivor := entrySummary(total - maxActivityEntries)
	if p.ActivityLog[0].Summary != wantFirstSurvivor {
		t.Errorf("ActivityLog[0].Summary = %q; want %q", p.ActivityLog[0].Summary, wantFirstSurvivor)
	}
}

func entrySummary(i int) string {
	return "artifact-" + strconv.Itoa(i)
}

// ---------------------------------------------------------------------------
// Filesystem lifecycle under an isolated HOME.
// ---------------------------------------------------------------------------

func TestCreate_PersistsAndActivates(t *testing.T) {
	isoHome(t)

	p, err := Create("Test Project", "a description")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Phase != PhaseInitialized {
		t.Errorf("Phase = %q; want %q", p.Phase, PhaseInitialized)
	}

	// Load round-trips the project.
	loaded, err := Load(p.ID)
	if err != nil {
		t.Fatalf("Load(%q): %v", p.ID, err)
	}
	if loaded == nil {
		t.Fatalf("Load(%q) = nil; want project", p.ID)
	}
	if loaded.Name != "Test Project" {
		t.Errorf("loaded.Name = %q; want %q", loaded.Name, "Test Project")
	}
	if loaded.Description != "a description" {
		t.Errorf("loaded.Description = %q; want %q", loaded.Description, "a description")
	}

	// Global state shows it active and registered.
	gs, err := LoadGlobalState()
	if err != nil {
		t.Fatalf("LoadGlobalState: %v", err)
	}
	if gs.ActiveProjectID != p.ID {
		t.Errorf("gs.ActiveProjectID = %q; want %q", gs.ActiveProjectID, p.ID)
	}
	found := false
	for _, id := range gs.ProjectIDs {
		if id == p.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("gs.ProjectIDs = %v; want to contain %q", gs.ProjectIDs, p.ID)
	}
}

func TestLoad_NonExistentReturnsNilNil(t *testing.T) {
	isoHome(t)

	p, err := Load("does-not-exist")
	if err != nil {
		t.Fatalf("Load: unexpected error %v", err)
	}
	if p != nil {
		t.Errorf("Load(non-existent) = %+v; want nil", p)
	}
}

func TestGetProject_NonExistentReturnsError(t *testing.T) {
	isoHome(t)

	_, err := GetProject("does-not-exist")
	if err == nil {
		t.Fatal("GetProject(non-existent): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("GetProject error = %q; want it to contain %q", err.Error(), "not found")
	}
}

func TestSwitch_UpdatesActiveProject(t *testing.T) {
	isoHome(t)

	p1, err := Create("Project One", "")
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	p2, err := Create("Project Two", "")
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// After creating p2, it should already be active (Create sets active).
	gs, err := LoadGlobalState()
	if err != nil {
		t.Fatalf("LoadGlobalState: %v", err)
	}
	if gs.ActiveProjectID != p2.ID {
		t.Fatalf("sanity check failed: ActiveProjectID = %q; want %q (p2)", gs.ActiveProjectID, p2.ID)
	}

	// Switch back to p1.
	if err := Switch(p1.ID); err != nil {
		t.Fatalf("Switch(%q): %v", p1.ID, err)
	}
	gs, err = LoadGlobalState()
	if err != nil {
		t.Fatalf("LoadGlobalState after Switch: %v", err)
	}
	if gs.ActiveProjectID != p1.ID {
		t.Errorf("ActiveProjectID after Switch = %q; want %q", gs.ActiveProjectID, p1.ID)
	}
}

func TestSwitch_NonExistentReturnsError(t *testing.T) {
	isoHome(t)

	if err := Switch("does-not-exist"); err == nil {
		t.Fatal("Switch(non-existent): expected error, got nil")
	}
}

func TestListAll_ExcludesArchivedUnlessRequested(t *testing.T) {
	isoHome(t)

	active, err := Create("Active Project", "")
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	archived, err := Create("Archived Project", "")
	if err != nil {
		t.Fatalf("Create archived: %v", err)
	}
	if err := Archive(archived.ID); err != nil {
		t.Fatalf("Archive(%q): %v", archived.ID, err)
	}

	withoutArchived, err := ListAll(false)
	if err != nil {
		t.Fatalf("ListAll(false): %v", err)
	}
	for _, p := range withoutArchived {
		if p.ID == archived.ID {
			t.Errorf("ListAll(false) unexpectedly included archived project %q", archived.ID)
		}
	}
	foundActive := false
	for _, p := range withoutArchived {
		if p.ID == active.ID {
			foundActive = true
		}
	}
	if !foundActive {
		t.Errorf("ListAll(false) missing active project %q", active.ID)
	}

	withArchived, err := ListAll(true)
	if err != nil {
		t.Fatalf("ListAll(true): %v", err)
	}
	foundArchived := false
	for _, p := range withArchived {
		if p.ID == archived.ID {
			foundArchived = true
		}
	}
	if !foundArchived {
		t.Errorf("ListAll(true) missing archived project %q", archived.ID)
	}
}

func TestArchive_SetsPhaseArchived(t *testing.T) {
	isoHome(t)

	p, err := Create("To Archive", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Archive(p.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	loaded, err := Load(p.ID)
	if err != nil {
		t.Fatalf("Load after Archive: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load after Archive returned nil")
	}
	if loaded.Phase != PhaseArchived {
		t.Errorf("Phase after Archive = %q; want %q", loaded.Phase, PhaseArchived)
	}
}

func TestSave_CreatesStandardSubdirsAndBumpsUpdatedAt(t *testing.T) {
	isoHome(t)

	p, err := Create("Subdir Project", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dir := projectDir(p.ID)
	for _, sub := range []string{"scouts", "specs", "tracks", "prompts", "retros", "artifacts"} {
		full := filepath.Join(dir, sub)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("expected subdir %q to exist: %v", full, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", full)
		}
	}

	firstUpdatedAt := p.UpdatedAt
	if firstUpdatedAt == "" {
		t.Fatal("UpdatedAt is empty after Create/Save")
	}

	// Mutate and Save again; UpdatedAt must be refreshed (non-empty; content
	// may coincide if the clock hasn't ticked, so just assert it's still set
	// and the save succeeds without error).
	p.AddDecision("bump updated_at")
	if err := p.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if p.UpdatedAt == "" {
		t.Error("UpdatedAt is empty after second Save")
	}
}
