package commands

// projectCmd implements the /project family of slash commands.
// It wires together the project, artifacts, and activity packages to provide
// full project lifecycle management from the REPL.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/artifacts"
	"github.com/DojoGenesis/cli/internal/project"
	gcolor "github.com/gookit/color"
)

// projectCmd returns the /project command with all subcommands.
//
//	/project init <name> [--desc "..."]       — create a new project, set active
//	/project status [@name]                   — phase indicator, tracks, activity, suggestion
//	/project switch <name>                    — change active project
//	/project list [--all]                     — list all projects with phase indicators
//	/project archive <name>                   — archive a completed project
//	/project phase <phase>                    — set phase manually
//	/project track add <name> [--dep N,...]   — add a track to the active project
//	/project track set <id> <status>          — update track status
//	/project decision <text>                  — record a decision
//	/project artifact <type> <file> <content> — save an artifact

// ─── JSON result types (headless mode) ──────────────────────────────────────

// ProjectStatusResult is the JSON-mode payload for `/project status`. It
// embeds the full project record (tracks, decisions, artifacts, activity
// log) and adds the one derived field — Next — that the human view also
// shows but which isn't itself a stored Project field.
type ProjectStatusResult struct {
	*project.Project
	Next string `json:"next"`
}

// ProjectListEntry is one row of the JSON-mode `/project list` payload: the
// stored project record plus whether it's the active one, which (like Next
// above) lives in GlobalState rather than on Project itself.
type ProjectListEntry struct {
	*project.Project
	Active bool `json:"active"`
}

func (r *Registry) projectCmd() Command {
	return Command{
		Name:    "project",
		Aliases: []string{"proj"},
		Usage:   "/project <subcommand> [args]",
		Short:   "Project lifecycle — phases, tracks, decisions, artifacts",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return r.projectStatus(nil)
			}

			sub := strings.ToLower(args[0])
			rest := args[1:]

			switch sub {
			case "init", "new", "create":
				return projectInit(rest)
			case "status", "st":
				return r.projectStatus(rest)
			case "switch":
				return projectSwitch(rest)
			case "list", "ls":
				return r.projectList(rest)
			case "archive":
				return projectArchive(rest)
			case "phase":
				return projectPhase(rest)
			case "track":
				return projectTrack(rest)
			case "decision", "decide":
				return projectDecision(rest)
			case "artifact", "art":
				return projectArtifact(rest)
			default:
				return fmt.Errorf("unknown /project subcommand %q — use init, status, switch, list, archive, phase, track, decision, artifact", sub)
			}
		},
	}
}

// ─── /project init ────────────────────────────────────────────────────────────

func projectInit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project init <name> [--desc \"...\"]")
	}

	name := args[0]
	desc := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--desc" && i+1 < len(args) {
			i++
			desc = args[i]
		}
	}

	p, err := project.Create(name, desc)
	if err != nil {
		return fmt.Errorf("project init: %w", err)
	}

	activity.Log(activity.ProjectCreated, fmt.Sprintf("Project %q created (id: %s)", p.Name, p.ID))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Project created"))
	fmt.Println()
	printKV("name", p.Name)
	printKV("id", p.ID)
	printKV("phase", string(p.Phase))
	if p.Description != "" {
		printKV("description", p.Description)
	}
	printKV("next", p.SuggestNext())
	fmt.Println()
	return nil
}

// ─── /project status ──────────────────────────────────────────────────────────

func (r *Registry) projectStatus(args []string) error {
	p, err := resolveProjectArg(args)
	if err != nil {
		return err
	}
	if p == nil {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No active project. Run /project init <name> to create one."))
		fmt.Println()
		return nil
	}

	if r.out.JSON() {
		r.out.Data(ProjectStatusResult{Project: p, Next: p.SuggestNext()})
		return nil
	}

	indicator := project.PhaseIndicator[p.Phase]

	fmt.Println()
	gcolor.Bold.Print(
		gcolor.HEX("#e8b04a").Sprintf("  %s %s", indicator, p.Name),
	)
	fmt.Println()
	fmt.Println()

	printKV("id", p.ID)
	printKV("phase", string(p.Phase))
	if p.Description != "" {
		printKV("description", p.Description)
	}
	printKV("created", fmtAgo(p.CreatedAt))
	printKV("updated", fmtAgo(p.UpdatedAt))

	// Tracks table
	if len(p.Tracks) > 0 {
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Tracks"))
		fmt.Println()
		fmt.Println()
		fmt.Printf("    %s  %-20s  %-14s  %s\n",
			gcolor.HEX("#94a3b8").Sprintf("%-4s", "ID"),
			gcolor.HEX("#94a3b8").Sprint("Name"),
			gcolor.HEX("#94a3b8").Sprint("Status"),
			gcolor.HEX("#94a3b8").Sprint("Deps"),
		)
		for _, t := range p.Tracks {
			deps := "—"
			if len(t.Dependencies) > 0 {
				parts := make([]string, len(t.Dependencies))
				for i, d := range t.Dependencies {
					parts[i] = strconv.Itoa(d)
				}
				deps = strings.Join(parts, ", ")
			}
			fmt.Printf("    %-4d  %-20s  %-14s  %s\n",
				t.ID,
				truncate(t.Name, 20),
				colorTrackStatus(string(t.Status)),
				gcolor.HEX("#94a3b8").Sprint(deps),
			)
		}
	}

	// Recent activity (last 5 from project log)
	if len(p.ActivityLog) > 0 {
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Recent activity"))
		fmt.Println()
		fmt.Println()
		limit := 5
		if len(p.ActivityLog) < limit {
			limit = len(p.ActivityLog)
		}
		// Newest first — project log is appended oldest-first, so reverse iterate
		for i := len(p.ActivityLog) - 1; i >= len(p.ActivityLog)-limit; i-- {
			entry := p.ActivityLog[i]
			ts := entry.Timestamp
			if len(ts) >= 16 {
				ts = ts[:16] // trim seconds
			}
			fmt.Printf("  %s  %s  %s\n",
				gcolor.HEX("#94a3b8").Sprintf("%-16s", ts),
				gcolor.HEX("#f4a261").Sprintf("%-16s", entry.Action),
				entry.Summary,
			)
		}
	}

	// Suggested next action
	fmt.Println()
	printKV("next action", p.SuggestNext())
	fmt.Println()
	return nil
}

// ─── /project switch ──────────────────────────────────────────────────────────

func projectSwitch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project switch <name-or-id>")
	}
	id := args[0]

	// Try direct ID first; fall back to searching by name.
	p, _ := project.Load(id)
	if p == nil {
		// Scan all projects for matching name.
		all, err := project.ListAll(true)
		if err != nil {
			return fmt.Errorf("project switch: %w", err)
		}
		for _, candidate := range all {
			if strings.EqualFold(candidate.Name, id) {
				p = candidate
				break
			}
		}
	}
	if p == nil {
		return fmt.Errorf("project %q not found", id)
	}

	if err := project.Switch(p.ID); err != nil {
		return fmt.Errorf("project switch: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("Switched to project %q", p.Name))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Active project updated"))
	fmt.Println()
	printKV("project", p.Name)
	printKV("id", p.ID)
	printKV("phase", string(p.Phase))
	fmt.Println()
	return nil
}

// ─── /project list ────────────────────────────────────────────────────────────

func (r *Registry) projectList(args []string) error {
	includeArchived := false
	for _, a := range args {
		if a == "--all" {
			includeArchived = true
		}
	}

	projects, err := project.ListAll(includeArchived)
	if err != nil {
		return fmt.Errorf("project list: %w", err)
	}

	gs, err := project.LoadGlobalState()
	if err != nil {
		return fmt.Errorf("project list: load state: %w", err)
	}

	if r.out.JSON() {
		entries := make([]ProjectListEntry, len(projects))
		for i, p := range projects {
			entries[i] = ProjectListEntry{Project: p, Active: p.ID == gs.ActiveProjectID}
		}
		r.out.Data(entries)
		return nil
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Projects"))
	fmt.Println()
	fmt.Println()

	if len(projects) == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No projects found. Run /project init <name> to create one."))
		fmt.Println()
		return nil
	}

	for _, p := range projects {
		active := "  "
		if p.ID == gs.ActiveProjectID {
			active = gcolor.HEX("#7fb88c").Sprint("* ")
		}
		indicator := project.PhaseIndicator[p.Phase]
		fmt.Printf("  %s%s %s  %s\n",
			active,
			gcolor.HEX("#f4a261").Sprint(indicator),
			gcolor.Bold.Sprint(p.Name),
			gcolor.HEX("#94a3b8").Sprintf("(%s, updated %s)", p.ID, fmtAgo(p.UpdatedAt)),
		)
	}
	fmt.Println()
	return nil
}

// ─── /project archive ─────────────────────────────────────────────────────────

func projectArchive(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project archive <name-or-id>")
	}
	id := args[0]

	// Resolve name-or-id
	p, _ := project.Load(id)
	if p == nil {
		all, err := project.ListAll(true)
		if err != nil {
			return fmt.Errorf("project archive: %w", err)
		}
		for _, candidate := range all {
			if strings.EqualFold(candidate.Name, id) {
				p = candidate
				break
			}
		}
	}
	if p == nil {
		return fmt.Errorf("project %q not found", id)
	}

	if err := project.Archive(p.ID); err != nil {
		return fmt.Errorf("project archive: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("Archived project %q", p.Name))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Project archived"))
	fmt.Println()
	printKV("project", p.Name)
	printKV("id", p.ID)
	fmt.Println()
	return nil
}

// ─── /project phase ───────────────────────────────────────────────────────────

func projectPhase(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project phase <phase>")
	}

	p, err := requireActiveProject()
	if err != nil {
		return err
	}

	newPhase := project.Phase(args[0])
	validPhases := []project.Phase{
		project.PhaseInitialized,
		project.PhaseScouting,
		project.PhaseSpecifying,
		project.PhaseDecomposing,
		project.PhaseCommissioning,
		project.PhaseImplementing,
		project.PhaseRetrospective,
		project.PhaseArchived,
	}
	valid := false
	for _, ph := range validPhases {
		if ph == newPhase {
			valid = true
			break
		}
	}
	if !valid {
		names := make([]string, len(validPhases))
		for i, ph := range validPhases {
			names[i] = string(ph)
		}
		return fmt.Errorf("unknown phase %q — valid phases: %s", newPhase, strings.Join(names, ", "))
	}

	old := p.Phase
	p.SetPhase(newPhase)
	if err := p.Save(); err != nil {
		return fmt.Errorf("project phase: %w", err)
	}

	activity.Log(activity.PhaseAdvanced, fmt.Sprintf("%s: %s → %s", p.Name, old, newPhase))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Phase updated"))
	fmt.Println()
	printKV("project", p.Name)
	printKV("old phase", string(old))
	printKV("new phase", string(newPhase))
	printKV("next", p.SuggestNext())
	fmt.Println()
	return nil
}

// ─── /project track ───────────────────────────────────────────────────────────

func projectTrack(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project track add <name> [--dep N] | track set <id> <status>")
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "add":
		return projectTrackAdd(rest)
	case "set", "update":
		return projectTrackSet(rest)
	default:
		return fmt.Errorf("unknown track subcommand %q — use add or set", sub)
	}
}

func projectTrackAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project track add <name> [--dep N,...]")
	}

	p, err := requireActiveProject()
	if err != nil {
		return err
	}

	name := args[0]
	var deps []int
	for i := 1; i < len(args); i++ {
		if args[i] == "--dep" && i+1 < len(args) {
			i++
			for _, part := range strings.Split(args[i], ",") {
				part = strings.TrimSpace(part)
				if n, convErr := strconv.Atoi(part); convErr == nil {
					deps = append(deps, n)
				}
			}
		}
	}

	t := p.AddTrack(name, deps)
	if err := p.Save(); err != nil {
		return fmt.Errorf("project track add: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("Added track %d %q to project %q", t.ID, t.Name, p.Name))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Track added"))
	fmt.Println()
	printKV("track id", strconv.Itoa(t.ID))
	printKV("name", t.Name)
	printKV("status", string(t.Status))
	fmt.Println()
	return nil
}

func projectTrackSet(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /project track set <id> <status>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("track id must be a number, got %q", args[0])
	}

	newStatus := project.TrackStatus(args[1])
	validStatuses := []project.TrackStatus{
		project.TrackPending,
		project.TrackInProgress,
		project.TrackCompleted,
		project.TrackBlocked,
	}
	valid := false
	for _, s := range validStatuses {
		if s == newStatus {
			valid = true
			break
		}
	}
	if !valid {
		names := make([]string, len(validStatuses))
		for i, s := range validStatuses {
			names[i] = string(s)
		}
		return fmt.Errorf("unknown status %q — valid: %s", newStatus, strings.Join(names, ", "))
	}

	p, loadErr := requireActiveProject()
	if loadErr != nil {
		return loadErr
	}

	found := false
	for i, t := range p.Tracks {
		if t.ID == id {
			p.Tracks[i].Status = newStatus
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("track %d not found in project %q", id, p.Name)
	}

	if err := p.Save(); err != nil {
		return fmt.Errorf("project track set: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("Track %d in %q set to %s", id, p.Name, newStatus))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Track updated"))
	fmt.Println()
	printKV("track id", strconv.Itoa(id))
	printKV("status", colorTrackStatus(string(newStatus)))
	fmt.Println()
	return nil
}

// ─── /project decision ────────────────────────────────────────────────────────

func projectDecision(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /project decision <text>")
	}

	p, err := requireActiveProject()
	if err != nil {
		return err
	}

	text := strings.Join(args, " ")
	p.AddDecision(text)
	if err := p.Save(); err != nil {
		return fmt.Errorf("project decision: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("Decision recorded in %q: %s", p.Name, truncate(text, 60)))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Decision recorded"))
	fmt.Println()
	printKV("project", p.Name)
	printKV("decision", text)
	fmt.Println()
	return nil
}

// ─── /project artifact ────────────────────────────────────────────────────────

func projectArtifact(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: /project artifact <type> <file> <content>")
	}

	p, err := requireActiveProject()
	if err != nil {
		return err
	}

	artifactType := artifacts.ArtifactType(args[0])
	filename := args[1]
	content := strings.Join(args[2:], " ")

	// Reject path traversal / absolute paths at the command boundary so the
	// user gets a clear, specific error immediately. artifacts.Save enforces
	// the same containment itself (defense-in-depth — this call site is not
	// the only path into Save), so this check is a fast, friendlier front
	// door rather than the sole guard.
	if filepath.IsAbs(filename) || hasParentTraversal(filename) {
		return fmt.Errorf("project artifact: filename %q must be a relative path with no \"..\" segments", filename)
	}

	path, err := artifacts.Save(p.ID, artifactType, filename, content)
	if err != nil {
		return fmt.Errorf("project artifact: %w", err)
	}

	p.AddArtifact(path)
	if err := p.Save(); err != nil {
		return fmt.Errorf("project artifact: save project: %w", err)
	}

	activity.Log(activity.ArtifactSaved, fmt.Sprintf("Saved artifact %s/%s for project %q", artifactType, filename, p.Name))

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Artifact saved"))
	fmt.Println()
	printKV("project", p.Name)
	printKV("type", string(artifactType))
	printKV("file", filename)
	printKV("path", path)
	fmt.Println()
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// hasParentTraversal reports whether p contains a literal ".." path segment
// (as opposed to merely containing the substring ".." inside a longer name,
// e.g. "v1..2-notes", which is a legal filename and must not be rejected).
func hasParentTraversal(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// requireActiveProject returns the active project or a clear error if none is set.
func requireActiveProject() (*project.Project, error) {
	p, err := project.ActiveProject()
	if err != nil {
		return nil, fmt.Errorf("could not load active project: %w", err)
	}
	if p == nil {
		return nil, fmt.Errorf("no active project — run /project init <name> first")
	}
	return p, nil
}

// resolveProjectArg returns the active project if args is empty, or looks up
// a project by name/id from args[0].
func resolveProjectArg(args []string) (*project.Project, error) {
	if len(args) == 0 || args[0] == "" {
		p, err := project.ActiveProject()
		if err != nil {
			return nil, fmt.Errorf("load active project: %w", err)
		}
		return p, nil
	}
	id := args[0]
	// Strip leading @ used as an informal syntax: /project status @myapp
	id = strings.TrimPrefix(id, "@")

	p, _ := project.Load(id)
	if p != nil {
		return p, nil
	}
	// Search by name
	all, err := project.ListAll(true)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	for _, candidate := range all {
		if strings.EqualFold(candidate.Name, id) {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("project %q not found", id)
}

// colorTrackStatus colors a track status string using the sunset palette.
func colorTrackStatus(s string) string {
	switch project.TrackStatus(s) {
	case project.TrackInProgress:
		return gcolor.HEX("#e8b04a").Sprint(s) // warm-amber
	case project.TrackCompleted:
		return gcolor.HEX("#7fb88c").Sprint(s) // soft-sage
	case project.TrackBlocked:
		return gcolor.HEX("#e63946").Sprint(s) // danger-red
	default: // pending
		return gcolor.HEX("#94a3b8").Sprint(s) // cloud-gray
	}
}
