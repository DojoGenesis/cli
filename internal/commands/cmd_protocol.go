package commands

// cmd_protocol.go — /protocol: make the KE harness ecosystem discoverable and
// installable from the CLI, and surface the genius-protocol state, without ever
// bypassing ke's operator-gated publish pipeline.
//
//	/protocol            — focused status: protocol enabled/source + is kata-harness installed
//	/protocol status     — same as above
//	/protocol harnesses  — list the KE catalog: name · status · ratified/draft · installed?
//	/protocol install <name> [--yes]
//	                     — copy a ratified, locally-available harness plugin into the
//	                       plugins path (with confirmation). Draft/unratified or
//	                       not-locally-available harnesses print operator guidance.
//
// SAFETY model (why this file is read-mostly):
//   - The ONLY write is copying an already-local, ratified plugin directory into
//     cfg.Plugins.Path on an explicit install + confirmation. Nothing else mutates
//     state, calls a network endpoint, or shells out.
//   - No operator-gated action is ever taken: this command never runs `ke promote`
//     or `ke publish`, never shells out to bin/ke. Unratified or not-yet-pulled
//     harnesses route to printed instructions, per "the store discovers, ke installs".
//   - No machine-specific hardcoded path is the ONLY source. The catalog resolves
//     via $KE_CATALOG_PATH → a home-relative workspace default → an embedded
//     snapshot; plugin sources resolve via $KE_HARNESS_SOURCE / $DOJO_PLUGINS_SOURCE
//     / home-relative candidates. It works on ANY machine, even with no checkout.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/plugins"
	gcolor "github.com/gookit/color"
)

// harnessInfo is one row of the KE harness ecosystem: enough to discover a
// harness, decide whether it is installable (ratified + locally available),
// and show whether it is already installed under the plugins path.
type harnessInfo struct {
	ID       string
	Name     string
	Kind     string // "harness" | "edition"
	Status   string // "published" | "draft"
	Ratified bool
	Desc     string
}

// embeddedHarnesses is the portable fallback the command uses when no workspace
// catalog.json is reachable — the five known KE catalog entries as of
// 2026-07-16. Hardcoded (not go:embed) so the CLI needs no adjacent file and
// works on ANY machine. kata-harness is the only ratified harness; the design
// records of the other three harnesses are not yet operator-ratified, and
// dojo-tirada is a published edition. Ratified here mirrors the catalog's
// design_record.ratified (with a published edition treated as live).
var embeddedHarnesses = []harnessInfo{
	{ID: "dojo-tirada", Name: "Dojo Tirada", Kind: "edition", Status: "published", Ratified: true,
		Desc: "Orchestration & Memory — Issue 1"},
	{ID: "kata-harness", Name: "Kata Harness", Kind: "harness", Status: "draft", Ratified: true,
		Desc: "Run your work as timed rolls — surface, stage, resolve, advance"},
	{ID: "dojo-build-dag", Name: "Build-DAG Harness", Kind: "harness", Status: "draft", Ratified: false,
		Desc: "Gate → ownership-partitioned build → integrate → audit"},
	{ID: "dojo-convergence", Name: "Convergence Harness", Kind: "harness", Status: "draft", Ratified: false,
		Desc: "Parallel read-only audits → adversarial verify → fix tracks"},
	{ID: "memory-garden", Name: "Memory-Garden Harness", Kind: "harness", Status: "draft", Ratified: false,
		Desc: "Don't-forget memory: seeds, index budget, compression ritual"},
}

// catalogRow is the subset of a ke catalog.json entry this command reads.
type catalogRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Tagline      string `json:"tagline"`
	DesignRecord *struct {
		Ratified bool `json:"ratified"`
	} `json:"design_record"`
}

// ratified derives the installable-gate signal for a catalog row: an
// operator-ratified design record, or a published edition (live even without a
// design_record block). A draft harness whose design record is unratified is
// operator-gated and returns false.
func (row catalogRow) ratified() bool {
	if row.DesignRecord != nil && row.DesignRecord.Ratified {
		return true
	}
	return strings.EqualFold(row.Status, "published")
}

// keCatalogPath resolves the catalog.json location portably, without ever
// treating a machine-specific absolute path as the only source:
//  1. $KE_CATALOG_PATH — explicit override (returned as-is so a missing override
//     still names itself in the fallback message rather than being silently ignored).
//  2. ~/ZenflowProjects/dojo-ke/public/catalog.json — the workspace default,
//     mirroring bootstrap.copyPlugins' home-relative convention.
//
// Returns "" when neither yields an on-disk file; the caller falls back to the
// embedded snapshot.
func keCatalogPath() string {
	if v := strings.TrimSpace(os.Getenv("KE_CATALOG_PATH")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	def := filepath.Join(home, "ZenflowProjects", "dojo-ke", "public", "catalog.json")
	if _, statErr := os.Stat(def); statErr == nil {
		return def
	}
	return ""
}

// loadHarnesses returns the harness ecosystem plus a human-readable description
// of where the data came from. It reads the resolved catalog path; on ANY
// failure (no path, unreadable, malformed, empty) it falls back to the embedded
// snapshot so the command always works.
func loadHarnesses() (list []harnessInfo, source string) {
	path := keCatalogPath()
	if path == "" {
		return embeddedHarnesses, "embedded snapshot — no workspace catalog found (set KE_CATALOG_PATH to override)"
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is env/home-derived, read-only
	if err != nil {
		return embeddedHarnesses, fmt.Sprintf("embedded snapshot — %s unreadable (%v)", path, err)
	}
	var rows []catalogRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return embeddedHarnesses, fmt.Sprintf("embedded snapshot — %s malformed (%v)", path, err)
	}
	if len(rows) == 0 {
		return embeddedHarnesses, fmt.Sprintf("embedded snapshot — %s had no entries", path)
	}
	for _, row := range rows {
		list = append(list, harnessInfo{
			ID:       row.ID,
			Name:     row.Name,
			Kind:     row.Kind,
			Status:   row.Status,
			Ratified: row.ratified(),
			Desc:     row.Tagline,
		})
	}
	return list, "ke catalog: " + path
}

// harnessInstalled reports whether a harness id has a directory under the
// plugins path — the same check /doctor's HARNESSES section uses.
func (r *Registry) harnessInstalled(id string) bool {
	info, err := os.Stat(filepath.Join(r.cfg.Plugins.Path, id))
	return err == nil && info.IsDir()
}

// harnessSourceCandidates returns the ordered local directories that might hold
// a harness's installable plugin, all derived portably from an env override or
// $HOME — never a single hardcoded machine-specific path (SAFETY). Order:
// explicit KE_HARNESS_SOURCE, the bootstrap plugin source (DOJO_PLUGINS_SOURCE),
// the home-relative CoworkPlugins source, then the standalone harness repo's
// plugin/ dir (e.g. ~/ZenflowProjects/kata-harness/plugin).
func harnessSourceCandidates(id string) []string {
	var cands []string
	add := func(p string) {
		if p != "" {
			cands = append(cands, p)
		}
	}
	if v := strings.TrimSpace(os.Getenv("KE_HARNESS_SOURCE")); v != "" {
		add(filepath.Join(v, id)) // a directory holding many harness plugins by id
		add(v)                    // or the harness plugin directory itself
	}
	if v := strings.TrimSpace(os.Getenv("DOJO_PLUGINS_SOURCE")); v != "" {
		add(filepath.Join(v, id)) // reuse bootstrap's plugin-source env
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, "ZenflowProjects", "CoworkPluginsByDojoGenesis", "plugins", id))
		add(filepath.Join(home, "ZenflowProjects", id, "plugin"))
	}
	return cands
}

// isPluginDir reports whether dir holds a plugin manifest, checking the same
// two locations plugins.scanOne does (.claude-plugin/plugin.json, then root).
func isPluginDir(dir string) bool {
	for _, m := range []string{
		filepath.Join(dir, ".claude-plugin", "plugin.json"),
		filepath.Join(dir, "plugin.json"),
	} {
		if info, err := os.Stat(m); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// resolveHarnessSource returns the first local plugin source for id, or "" when
// nothing on this machine looks like the harness's plugin.
func resolveHarnessSource(id string) string {
	for _, c := range harnessSourceCandidates(id) {
		if isPluginDir(c) {
			return c
		}
	}
	return ""
}

// copyPluginTree recursively copies a plugin directory, skipping VCS/cache cruft
// with the same filter and permissions bootstrap.copyDir uses. Re-implemented
// here (not imported) to keep this command self-contained, the way cmd_doctor.go
// is. This is the command's only write to disk.
func copyPluginTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if name == ".git" || name == ".DS_Store" || name == "__pycache__" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // path walks a local plugin source
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0644)
	})
}

// ─── /protocol ──────────────────────────────────────────────────────────────

func (r *Registry) protocolCmd() Command {
	return Command{
		Name:  "protocol",
		Usage: "/protocol [status|harnesses|install <name> [--yes]]",
		Short: "Discover + install KE harnesses and show genius-protocol state",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return r.protocolStatus()
			}
			switch strings.ToLower(args[0]) {
			case "status":
				return r.protocolStatus()
			case "harnesses", "harness", "ls", "list":
				return r.protocolHarnesses()
			case "install", "add":
				if len(args) < 2 {
					return fmt.Errorf("usage: /protocol install <name> [--yes]")
				}
				noConfirm := false
				for _, a := range args[2:] {
					if a == "--yes" || a == "-y" {
						noConfirm = true
					}
				}
				return r.protocolInstall(ctx, args[1], noConfirm)
			default:
				return fmt.Errorf("unknown subcommand %q — use status, harnesses, or install <name>", args[0])
			}
		},
	}
}

// ─── status ─────────────────────────────────────────────────────────────────

// protocolStatus renders the focused view: protocol enabled/source (the same
// "overrides must be visible" rule /doctor's PROTOCOL section enforces — a
// silenced protocol WARNs), and whether kata-harness is installed.
func (r *Registry) protocolStatus() error {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Genius Protocol"))
	fmt.Println()
	fmt.Println()

	gcolor.Bold.Print(gcolor.HEX("#f4a261").Sprint("  PROTOCOL"))
	fmt.Println()
	if r.cfg.Protocol.Enabled {
		fmt.Printf("    %s  protocol.enabled = true\n", doctorTag(true))
	} else {
		fmt.Printf("    %s  protocol disabled — set protocol.enabled or unset DOJO_PROTOCOL_DISABLED\n", doctorTag(false))
	}
	source := r.cfg.Protocol.Path
	if source == "" {
		source = "embedded default"
		if cwd, err := os.Getwd(); err == nil {
			if _, statErr := os.Stat(filepath.Join(cwd, "DOJO.md")); statErr == nil {
				source = "embedded default — overridden by project ./DOJO.md"
			}
		}
	}
	fmt.Printf("    %s  source = %s\n", doctorTag(true), source)

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#f4a261").Sprint("  HARNESSES"))
	fmt.Println()
	kataPath := filepath.Join(r.cfg.Plugins.Path, "kata-harness")
	if info, err := os.Stat(kataPath); err == nil && info.IsDir() {
		fmt.Printf("    %s  kata-harness installed at %s\n", doctorTag(true), kataPath)
	} else {
		fmt.Printf("    %s  kata-harness not installed — run /protocol install kata-harness\n", doctorTag(false))
	}

	list, _ := loadHarnesses()
	total, installed := 0, 0
	for _, h := range list {
		if !strings.EqualFold(h.Kind, "harness") {
			continue
		}
		total++
		if r.harnessInstalled(h.ID) {
			installed++
		}
	}
	fmt.Printf("    %s  %d of %d harness(es) installed — /protocol harnesses for the full list\n",
		doctorTag(true), installed, total)
	fmt.Println()
	return nil
}

// ─── harnesses ────────────────────────────────────────────────────────────────

// protocolHarnesses lists the KE catalog: name, status, ratified/draft, and
// whether each is installed under the plugins path.
func (r *Registry) protocolHarnesses() error {
	list, source := loadHarnesses()

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  KE Harnesses"))
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + source))
	fmt.Println()

	fmt.Printf("  %s%s%s%s\n",
		gcolor.HEX("#94a3b8").Sprintf("%-20s", "name"),
		gcolor.HEX("#94a3b8").Sprintf("%-12s", "status"),
		gcolor.HEX("#94a3b8").Sprintf("%-13s", "ratified"),
		gcolor.HEX("#94a3b8").Sprint("installed"),
	)
	for _, h := range list {
		fmt.Printf("  %s%s%s%s\n",
			gcolor.HEX("#f4a261").Sprintf("%-20s", h.ID),
			protocolStatusCell(h.Status),
			protocolRatifiedCell(h.Ratified),
			protocolInstalledCell(r.harnessInstalled(h.ID)),
		)
		if h.Desc != "" {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("    " + h.Desc))
		}
	}
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  install a ratified, locally-available harness:  /protocol install <name>"))
	fmt.Println()
	return nil
}

// protocolStatusCell colors a status token: published sage, draft amber.
func protocolStatusCell(status string) string {
	c := "#e8b04a" // draft = warm-amber
	if strings.EqualFold(status, "published") {
		c = "#7fb88c" // published = soft-sage
	}
	return gcolor.HEX(c).Sprintf("%-12s", orDefault(status, "unknown"))
}

// protocolRatifiedCell colors the ratified/unratified token.
func protocolRatifiedCell(ratified bool) string {
	if ratified {
		return gcolor.HEX("#7fb88c").Sprintf("%-13s", "ratified")
	}
	return gcolor.HEX("#94a3b8").Sprintf("%-13s", "unratified")
}

// protocolInstalledCell colors the installed/not-installed token.
func protocolInstalledCell(installed bool) string {
	if installed {
		return gcolor.HEX("#7fb88c").Sprint("installed")
	}
	return gcolor.HEX("#94a3b8").Sprint("not installed")
}

// ─── install ──────────────────────────────────────────────────────────────────

// protocolInstall copies a ratified, locally-available harness plugin into the
// plugins path after confirmation. Draft/unratified or not-locally-available
// harnesses print operator guidance and return nil — this command never runs
// ke promote/publish and never shells out to bin/ke.
//
// ctx is accepted for signature parity with the other install commands; the
// install is a local filesystem copy with no gateway call, so it is unused.
func (r *Registry) protocolInstall(ctx context.Context, name string, noConfirm bool) error {
	list, _ := loadHarnesses()
	var target *harnessInfo
	for i := range list {
		if strings.EqualFold(list[i].ID, name) || strings.EqualFold(list[i].Name, name) {
			target = &list[i]
			break
		}
	}
	if target == nil {
		names := make([]string, 0, len(list))
		for _, h := range list {
			names = append(names, h.ID)
		}
		return fmt.Errorf("unknown harness %q — known: %s", name, strings.Join(names, ", "))
	}

	// Gate 1 — must be ratified. Draft/unratified harnesses are operator-gated.
	if !target.Ratified {
		printOperatorGate(target, "it is not yet ratified")
		return nil
	}

	// Gate 2 — must be locally available as a plugin. A ratified harness with no
	// local plugin (an edition, or a harness not yet pulled to this machine)
	// also routes to guidance: the store discovers, ke installs.
	src := resolveHarnessSource(target.ID)
	if src == "" {
		printOperatorGate(target, "no local plugin was found on this machine")
		return nil
	}

	dst := filepath.Join(r.cfg.Plugins.Path, target.ID)
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		fmt.Println()
		fmt.Println(gcolor.HEX("#e8b04a").Sprintf("  %s already installed at %s — remove it first to reinstall", target.ID, dst))
		fmt.Println()
		return nil
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Install harness %q", target.ID))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("    from: " + src))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("    to:   " + dst))
	if !noConfirm {
		fmt.Print(gcolor.HEX("#e8b04a").Sprint("\n  Continue? [y/N]: "))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Install cancelled."))
			fmt.Println()
			return nil
		}
	}

	if err := copyPluginTree(src, dst); err != nil {
		return fmt.Errorf("install %s: %w", target.ID, err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("protocol harness installed: %s → %s", target.ID, dst))

	// Rescan so /protocol status and /doctor reflect the new install immediately.
	if plgs, scanErr := plugins.Scan(r.cfg.Plugins.Path); scanErr == nil {
		r.plgs = plgs
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprintf("  Installed %s → %s", target.ID, dst))
	fmt.Println()
	fmt.Println()
	return nil
}

// printOperatorGate explains why a harness cannot be installed from the CLI and
// what the operator must do — ratify, then publish through ke — without this
// command ever taking those operator-gated actions itself.
func printOperatorGate(h *harnessInfo, reason string) {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  %s is operator-gated", h.ID))
	fmt.Println()
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Not installable here — %s (%s).", reason, protocolStatusLabel(h)))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Harnesses ship through the ke pipeline, not this CLI. To make it installable:"))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("    1. Ratify the design record — the operator writes RATIFIED.md."))
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("    2. Package + publish it:  ke publish <YYYY.NN> --line %s", h.ID))
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("    3. Re-run /protocol install %s once its plugin is local.", h.ID))
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  This command never runs ke promote/publish for you — that stays an operator action."))
	fmt.Println()
}

// protocolStatusLabel is a compact "status · ratified" tag for guidance text.
func protocolStatusLabel(h *harnessInfo) string {
	rat := "unratified"
	if h.Ratified {
		rat = "ratified"
	}
	return fmt.Sprintf("%s · %s", orDefault(h.Status, "unknown"), rat)
}
