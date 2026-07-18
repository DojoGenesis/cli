package commands

// cmd_code.go — /code command for file operations and build tooling during REPL sessions.
// Enables the CLI to inspect its own code and verify changes without leaving the REPL.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DojoGenesis/cli/internal/permissions"
	gcolor "github.com/gookit/color"
)

// ─── permissions gate (shared by cmd_code.go, cmd_craft.go, cmd_plugin.go) ────

// permissionsModeAllowed returns the permission mode and allowlist from the
// registry config, tolerating a nil registry or nil cfg (zero Registry values
// appear in tests) by falling back to default-mode semantics.
func (r *Registry) permissionsModeAllowed() (string, []string) {
	if r == nil || r.cfg == nil {
		return "", nil
	}
	return r.cfg.Permissions.Mode, r.cfg.Permissions.Allowed
}

// permissionGate evaluates action (a dot-path like "code.undo") against
// cfg.Permissions and reports whether the caller may proceed. It implements
// the standard gate flow for risky write/exec commands:
//
//   - Allow (yolo, or matched by permissions.allowed): proceed silently.
//   - Deny (allowlist mode, unmatched): print the one-line deny message and
//     stop — callers return nil, the same clean exit other handlers use for
//     user-facing cancellations.
//   - Confirm (default mode, unmatched): prompt via
//     permissions.ConfirmInteractive; a declined or non-interactive (no TTY)
//     prompt prints the same deny message.
//
// preConfirmed marks consent the user already gave inline (e.g. the
// --yes / -y flag on /plugin install): it satisfies the Confirm case without
// re-prompting, but never overrides a Deny — an allowlist-mode refusal is
// policy, not a question.
func (r *Registry) permissionGate(action, detail string, preConfirmed bool) bool {
	mode, allowed := r.permissionsModeAllowed()
	switch permissions.Check(mode, allowed, action) {
	case permissions.Allow:
		return true
	case permissions.Confirm:
		if preConfirmed || permissions.ConfirmInteractive(action, detail) {
			return true
		}
	}
	// Deny, or a Confirm that was declined / had no terminal to ask on.
	fmt.Println()
	fmt.Println("  " + permissions.Explain(action))
	fmt.Println()
	return false
}

func (r *Registry) codeCmd() Command {
	return Command{
		Name:    "code",
		Aliases: []string{"c"},
		Usage:   "/code [read <file>|diff [file]|test [pkg]|build|vet|gate|undo]",
		Short:   "File operations and build tooling for self-build workflows",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /code [read <file>|diff [file]|test [pkg]|build|vet|gate|undo]")
			}
			sub := strings.ToLower(args[0])

			switch sub {
			case "read", "cat":
				return codeRead(args[1:])
			case "diff":
				return codeDiff(args[1:])
			case "test":
				// pkgArgs recomputes the same default-package logic codeTest
				// applies internally, so the JSON step label names the
				// package actually run rather than a generic placeholder.
				pkgArgs := args[1:]
				err := codeTest(pkgArgs)
				if r.out.JSON() && err == nil {
					pkg := "./..."
					if len(pkgArgs) > 0 {
						pkg = pkgArgs[0]
					}
					r.out.Data(codeGateResult{
						Steps:  []codeGateStep{{Step: "go test " + pkg + " -count=1 -race", Passed: true}},
						Passed: true,
					})
				}
				return err
			case "build":
				err := codeBuild()
				if r.out.JSON() && err == nil {
					r.out.Data(codeGateResult{
						Steps:  []codeGateStep{{Step: "go build ./...", Passed: true}},
						Passed: true,
					})
				}
				return err
			case "vet":
				err := codeVet()
				if r.out.JSON() && err == nil {
					r.out.Data(codeGateResult{
						Steps:  []codeGateStep{{Step: "go vet ./...", Passed: true}},
						Passed: true,
					})
				}
				return err
			case "gate":
				return r.codeGate()
			case "undo":
				// Permission gate: "code.undo" reverts working-tree changes.
				// Allow (allowlisted / yolo) skips the in-function y/N prompt
				// too — the gate already carries the consent; Confirm asks
				// once here and then proceeds prompt-free, so no mode ever
				// double-prompts. Direct codeUndo() calls (tests, future
				// callers) keep the legacy prompt-below-preview behavior.
				cwd, cwdErr := os.Getwd()
				if cwdErr != nil || cwd == "" {
					cwd = "the current directory"
				}
				// Headless callers get a clean refusal instead of a silent
				// decline — checked before the gate, same as /craft's
				// permission-gated writes.
				if err := r.headlessRefuse("undo file change"); err != nil {
					return err
				}
				if !r.permissionGate("code.undo", "revert all unstaged changes in "+cwd, false) {
					return nil
				}
				return codeUndoPreconfirmed()
			default:
				return fmt.Errorf("unknown subcommand %q — try: read, diff, test, build, vet, gate, undo", sub)
			}
		},
	}
}

// codeRead reads a file and displays it with line numbers.
func codeRead(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /code read <file> [start:end]")
	}
	path := args[0]

	// Resolve relative paths from CWD.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get cwd: %w", err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	// Resolve symlinks so that a symlink escaping the project root is caught.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %s: %w", path, err)
	}

	// Enforce project-root boundary: the resolved path must be cwd itself or
	// a descendant.  filepath.EvalSymlinks also resolves cwd so we evaluate it
	// as well to handle symlinked working directories correctly.
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return fmt.Errorf("could not resolve cwd: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return fmt.Errorf("path outside project root: %s", resolved)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")

	// Optional line range: /code read file.go 10:30
	startLine, endLine := 1, len(lines)
	if len(args) >= 2 {
		if parts := strings.SplitN(args[1], ":", 2); len(parts) == 2 {
			if n, err := fmt.Sscanf(parts[0], "%d", &startLine); n == 1 && err == nil {
				// Valid start; also attempt to parse the end of the range.
				_, _ = fmt.Sscanf(parts[1], "%d", &endLine)
			}
		}
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  %s", path))
	if startLine > 1 || endLine < len(lines) {
		gcolor.HEX("#94a3b8").Printf(" (lines %d-%d of %d)", startLine, endLine, len(lines))
	} else {
		gcolor.HEX("#94a3b8").Printf(" (%d lines)", len(lines))
	}
	fmt.Println()
	fmt.Println()

	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		lineNo := gcolor.HEX("#64748b").Sprintf("%4d", i+1)
		fmt.Printf("  %s  %s\n", lineNo, lines[i])
	}
	fmt.Println()
	return nil
}

// codeDiff shows git diff (staged + unstaged).
func codeDiff(args []string) error {
	cmdArgs := []string{"diff", "--stat"}
	if len(args) > 0 && args[0] == "--full" {
		cmdArgs = []string{"diff"}
		args = args[1:]
	}
	cmdArgs = append(cmdArgs, args...)

	return runGitCmd(cmdArgs...)
}

// codeTest runs go test for a package or all packages.
func codeTest(args []string) error {
	pkg := "./..."
	if len(args) > 0 {
		pkg = args[0]
	}
	return runGoCmd("test", pkg, "-count=1", "-race")
}

// codeBuild runs go build ./...
func codeBuild() error {
	return runGoCmd("build", "./...")
}

// codeVet runs go vet ./...
func codeVet() error {
	return runGoCmd("vet", "./...")
}

// codeGateStep is one pass/fail record within a /code build|test|vet|gate
// JSON-mode result.
type codeGateStep struct {
	Step   string `json:"step"`
	Passed bool   `json:"passed"`
}

// codeGateResult is the JSON-mode payload for /code build, /code test,
// /code vet, and /code gate — Steps holds one entry per gate that ran (one
// for a single-step command, up to three for the combined gate), in run
// order. Only ever emitted on overall success: a failing step's error already
// carries the failure detail via the envelope's `error` field, and
// Registry.RunHeadless's envelopeFor discards any Data set on a command that
// returns a non-nil error — so there is no partial/failed Steps entry to
// report here.
type codeGateResult struct {
	Steps  []codeGateStep `json:"steps"`
	Passed bool           `json:"passed"`
}

// codeGate runs the full build gate: build + test + vet.
func (r *Registry) codeGate() error {
	fmt.Println()
	steps := []struct {
		label string
		fn    func() error
	}{
		{"go build ./...", codeBuild},
		{"go test ./... -count=1 -race", func() error { return codeTest(nil) }},
		{"go vet ./...", codeVet},
	}

	var passed []codeGateStep
	for _, step := range steps {
		gcolor.HEX("#e8b04a").Printf("  Running: %s\n", step.label)
		if err := step.fn(); err != nil {
			gcolor.HEX("#ef4444").Printf("  FAILED: %s\n\n", step.label)
			return err
		}
		gcolor.HEX("#22c55e").Printf("  PASSED: %s\n\n", step.label)
		passed = append(passed, codeGateStep{Step: step.label, Passed: true})
	}

	gcolor.Bold.Print(gcolor.HEX("#22c55e").Sprint("  All gates passed.\n\n"))
	if r.out.JSON() {
		r.out.Data(codeGateResult{Steps: passed, Passed: true})
	}
	return nil
}

// codeUndo previews unstaged working-tree changes to tracked files and,
// after explicit confirmation, reverts them. It is the git-backed safety
// net for a self-build session — undoing a bad edit without touching
// staged content, commit history, or untracked files.
//
// Preview and revert both use the "." pathspec, so they act on the same
// scope: the current directory and below (the "project root" boundary
// codeRead enforces for file reads). Neither step ever leaves that scope
// or the enclosing git repository.
func codeUndo() error {
	return codeUndoRun(false)
}

// codeUndoPreconfirmed is codeUndo with the in-function y/N prompt skipped:
// the /code dispatch path calls it after the "code.undo" permission gate has
// already carried the user's consent (Allow via allowlist/yolo, or an
// answered Confirm). Skipping the prompt here is what keeps every mode at
// one prompt maximum.
func codeUndoPreconfirmed() error {
	return codeUndoRun(true)
}

// codeUndoRun holds the shared preview + revert flow. preconfirmed=false is
// the legacy direct-call behavior: prompt below the preview via craftConfirm.
func codeUndoRun(preconfirmed bool) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found in PATH — /code undo requires git")
	}
	if err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return fmt.Errorf("not a git repository (or any parent up to /)")
	}

	// Capture (rather than stream) the diff so we can decide whether there's
	// anything to do, and so the confirmation prompt sits directly below the
	// exact change-set it describes.
	stat, err := exec.Command("git", "diff", "--stat", "--", ".").Output()
	if err != nil {
		return fmt.Errorf("could not read git diff: %w", err)
	}

	if len(strings.TrimSpace(string(stat))) == 0 {
		fmt.Println()
		gcolor.HEX("#94a3b8").Print("  Nothing to revert — no unstaged changes to tracked files.")
		fmt.Println()
		fmt.Println()
		return nil
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  /code undo — would revert these unstaged changes:"))
	fmt.Println()
	fmt.Println()
	fmt.Print(string(stat))
	fmt.Println()
	gcolor.HEX("#94a3b8").Print("  Staged changes and commit history are untouched; untracked files are left alone.")
	fmt.Println()
	fmt.Println()

	// Reuses the same y/N stdin idiom as plugins.InstallConfirmed (see
	// cmd_plugin.go) — craftConfirm lives in cmd_craft.go but is
	// package-visible, so no duplicate confirm helper is needed here.
	// Skipped when the "code.undo" permission gate already confirmed.
	if !preconfirmed && !craftConfirm("Revert these unstaged changes?") {
		gcolor.HEX("#94a3b8").Print("  Cancelled — no changes made.")
		fmt.Println()
		fmt.Println()
		return nil
	}

	// git restore (no --staged) rewrites the worktree from the index only:
	// staged content and history are structurally untouched, and untracked
	// files are structurally out of scope (nothing to restore them from).
	if err := runGitCmd("restore", "--", "."); err != nil {
		return fmt.Errorf("git restore failed: %w", err)
	}

	fmt.Println()
	gcolor.HEX("#22c55e").Print("  Reverted. Working tree now matches the index.")
	fmt.Println()
	fmt.Println()
	return nil
}

// runGoCmd executes a go command and streams output.
func runGoCmd(args ...string) error {
	// Find go.mod to determine project root.
	root, err := findGoModRoot()
	if err != nil {
		return fmt.Errorf("could not find go.mod: %w", err)
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runGitCmd executes a git command and streams output.
func runGitCmd(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findGoModRoot walks up from CWD to find the nearest go.mod.
func findGoModRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found")
		}
		dir = parent
	}
}
