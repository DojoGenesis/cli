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

	gcolor "github.com/gookit/color"
)

func (r *Registry) codeCmd() Command {
	return Command{
		Name:    "code",
		Aliases: []string{"c"},
		Usage:   "/code [read <file>|diff [file]|test [pkg]|build|vet|gate]",
		Short:   "File operations and build tooling for self-build workflows",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /code [read <file>|diff [file]|test [pkg]|build|vet|gate]")
			}
			sub := strings.ToLower(args[0])

			switch sub {
			case "read", "cat":
				return codeRead(args[1:])
			case "diff":
				return codeDiff(args[1:])
			case "test":
				return codeTest(args[1:])
			case "build":
				return codeBuild()
			case "vet":
				return codeVet()
			case "gate":
				return codeGate()
			default:
				return fmt.Errorf("unknown subcommand %q — try: read, diff, test, build, vet, gate", sub)
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
				if n, err := fmt.Sscanf(parts[1], "%d", &endLine); n == 1 && err == nil {
					// Valid range.
				}
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

// codeGate runs the full build gate: build + test + vet.
func codeGate() error {
	fmt.Println()
	steps := []struct {
		label string
		fn    func() error
	}{
		{"go build ./...", codeBuild},
		{"go test ./... -count=1 -race", func() error { return codeTest(nil) }},
		{"go vet ./...", codeVet},
	}

	for _, step := range steps {
		gcolor.HEX("#e8b04a").Printf("  Running: %s\n", step.label)
		if err := step.fn(); err != nil {
			gcolor.HEX("#ef4444").Printf("  FAILED: %s\n\n", step.label)
			return err
		}
		gcolor.HEX("#22c55e").Printf("  PASSED: %s\n\n", step.label)
	}

	gcolor.Bold.Print(gcolor.HEX("#22c55e").Sprint("  All gates passed.\n\n"))
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
