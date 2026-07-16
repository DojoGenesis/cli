package commands

// cmd_code_test.go — regression tests for path-traversal fix in codeRead,
// plus coverage for the /code undo safety command.
//
// Security invariants tested (codeRead):
//   1. Relative path inside cwd       → succeeds
//   2. Relative ../ escape            → "outside project root" error
//   3. Absolute path outside root     → "outside project root" error
//   4. Symlink inside root            → succeeds (monorepo-safe)
//   5. Symlink escaping root          → "outside project root" error
//
// Safety invariants tested (codeUndo):
//   1. Not a git repo                 → clean error, nothing touched
//   2. git missing from PATH          → clean error, nothing touched
//   3. Clean tree                     → no-op, no prompt read (would hang if it tried)
//   4. Confirmed ("y")                → unstaged tracked edit reverted;
//                                        staged content + untracked files survive
//   5. Declined ("n")                 → working tree left exactly as-is

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withTempCwd creates a temp directory, changes cwd to it for the duration of
// the test, restores the original cwd on cleanup, and returns the temp path.
func withTempCwd(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not get cwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("could not chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			// Best-effort restore; TempDir cleanup will still happen.
			t.Logf("warning: could not restore cwd to %s: %v", orig, err)
		}
	})
	return tmp
}

// TestCodeReadInsideRoot — Test 1: a file directly in cwd is readable.
func TestCodeReadInsideRoot(t *testing.T) {
	root := withTempCwd(t)

	// Write a small file inside root.
	target := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(target, []byte("hello world\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// codeRead with a relative path should succeed.
	if err := codeRead([]string{"hello.txt"}); err != nil {
		t.Errorf("codeRead inside root: unexpected error: %v", err)
	}
}

// TestCodeReadRelativeTraversal — Test 2: ../ escape returns "outside project root".
func TestCodeReadRelativeTraversal(t *testing.T) {
	// Use a real subdirectory so the traversal target exists on the system.
	withTempCwd(t)

	// Three levels of ../ puts us well above any temp dir and lands in a
	// system directory (/tmp on macOS/Linux).  We use a file that is guaranteed
	// to exist — /etc/hosts — via the path "../../../etc/hosts".
	// Even if that exact path doesn't resolve, EvalSymlinks will fail, which is
	// also an acceptable rejection for our purposes. The important thing is that
	// codeRead never returns nil for a path escaping the root.
	err := codeRead([]string{"../../../etc/hosts"})
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	// Accept either "outside project root" or a resolution/read error — both
	// prove the file was not served.  The primary assertion is "outside project root".
	if !strings.Contains(err.Error(), "outside project root") &&
		!strings.Contains(err.Error(), "could not resolve") {
		t.Errorf("expected 'outside project root' or resolve error; got: %v", err)
	}
}

// TestCodeReadAbsoluteOutsideRoot — Test 3: absolute path outside root is rejected.
func TestCodeReadAbsoluteOutsideRoot(t *testing.T) {
	withTempCwd(t)

	// /etc/hosts exists on macOS and Linux.
	err := codeRead([]string{"/etc/hosts"})
	if err == nil {
		t.Fatal("expected error for absolute path outside root, got nil")
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Errorf("expected 'outside project root'; got: %v", err)
	}
}

// TestCodeReadSymlinkInsideRoot — Test 4: symlink pointing to a file INSIDE root succeeds.
func TestCodeReadSymlinkInsideRoot(t *testing.T) {
	root := withTempCwd(t)

	// Create the real file.
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("internal content\n"), 0o600); err != nil {
		t.Fatalf("setup real file: %v", err)
	}

	// Create a symlink inside root pointing to the real file (also inside root).
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	// Reading via the symlink should succeed.
	if err := codeRead([]string{"link.txt"}); err != nil {
		t.Errorf("codeRead symlink inside root: unexpected error: %v", err)
	}
}

// TestCodeReadSymlinkEscapingRoot — Test 5: symlink pointing outside root is rejected.
func TestCodeReadSymlinkEscapingRoot(t *testing.T) {
	root := withTempCwd(t)

	// Create a symlink inside root that points to /etc/hosts (outside root).
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	err := codeRead([]string{"escape.txt"})
	if err == nil {
		t.Fatal("expected error for symlink escaping root, got nil")
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Errorf("expected 'outside project root'; got: %v", err)
	}
}

// TestCodeReadDeepRelativePath — /code read ./deep/path/file.go must work.
func TestCodeReadDeepRelativePath(t *testing.T) {
	root := withTempCwd(t)

	// Create a nested directory and file.
	dir := filepath.Join(root, "deep", "path")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup dir: %v", err)
	}
	file := filepath.Join(dir, "file.go")
	if err := os.WriteFile(file, []byte("package deep\n"), 0o600); err != nil {
		t.Fatalf("setup file: %v", err)
	}

	if err := codeRead([]string{"./deep/path/file.go"}); err != nil {
		t.Errorf("codeRead deep relative path: unexpected error: %v", err)
	}
}

// ─── /code undo ───────────────────────────────────────────────────────────

// initTempGitRepo creates an isolated temp dir (via withTempCwd), chdirs into
// it, and initializes a git repo with repo-local (non-global) user config so
// commits succeed hermetically regardless of the host's git identity setup.
func initTempGitRepo(t *testing.T) string {
	t.Helper()
	root := withTempCwd(t)

	runGitSetup(t, "init", "-q")
	runGitSetup(t, "config", "user.email", "test@example.com")
	runGitSetup(t, "config", "user.name", "Test")

	return root
}

// runGitSetup runs a git command in the current cwd for test fixture setup,
// failing the test immediately (with output) on error.
func runGitSetup(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// withStdin temporarily replaces os.Stdin with a pipe pre-loaded with input,
// restoring the original on cleanup. Drives craftConfirm's y/N prompt without
// a real terminal.
func withStdin(t *testing.T, input string) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not create stdin pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("could not write stdin pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
}

// TestCodeUndoNotGitRepo — codeUndo outside any git repo errors cleanly and
// never reaches the confirmation prompt.
func TestCodeUndoNotGitRepo(t *testing.T) {
	withTempCwd(t)

	err := codeUndo()
	if err == nil {
		t.Fatal("expected error outside a git repository, got nil")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("expected 'not a git repository'; got: %v", err)
	}
}

// TestCodeUndoGitMissing — codeUndo errors cleanly when git isn't in PATH.
func TestCodeUndoGitMissing(t *testing.T) {
	withTempCwd(t)
	t.Setenv("PATH", "")

	err := codeUndo()
	if err == nil {
		t.Fatal("expected error when git is missing, got nil")
	}
	if !strings.Contains(err.Error(), "git not found") {
		t.Errorf("expected 'git not found'; got: %v", err)
	}
}

// TestCodeUndoNoChanges — a clean working tree is a no-op: nil error, and
// critically, no attempt to read a confirmation from stdin. os.Stdin is left
// at its default here; if codeUndo mistakenly tried to prompt, the read
// would block on empty stdin and this test would hang until timeout — the
// absence of a hang is itself part of the assertion.
func TestCodeUndoNoChanges(t *testing.T) {
	initTempGitRepo(t)

	if err := os.WriteFile("committed.txt", []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runGitSetup(t, "add", "committed.txt")
	runGitSetup(t, "commit", "-q", "-m", "initial")

	if err := codeUndo(); err != nil {
		t.Errorf("codeUndo on a clean tree: unexpected error: %v", err)
	}
}

// TestCodeUndoRevertsUnstagedChanges — the money path: an unstaged edit to a
// tracked file is reverted on "y". A staged addition and an untracked file
// both survive untouched, proving the safety guards actually hold.
func TestCodeUndoRevertsUnstagedChanges(t *testing.T) {
	initTempGitRepo(t)

	// Tracked file, committed at "v1" — then edited unstaged. This is the
	// only change /code undo should touch.
	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("setup tracked.txt: %v", err)
	}
	runGitSetup(t, "add", "tracked.txt")
	runGitSetup(t, "commit", "-q", "-m", "initial")
	if err := os.WriteFile("tracked.txt", []byte("v2 unstaged\n"), 0o600); err != nil {
		t.Fatalf("unstaged edit: %v", err)
	}

	// Staged addition — must survive undo untouched (undo never touches the index).
	if err := os.WriteFile("staged.txt", []byte("staged content\n"), 0o600); err != nil {
		t.Fatalf("setup staged.txt: %v", err)
	}
	runGitSetup(t, "add", "staged.txt")

	// Untracked file — must survive undo untouched (nothing to restore it from).
	if err := os.WriteFile("untracked.txt", []byte("untracked content\n"), 0o600); err != nil {
		t.Fatalf("setup untracked.txt: %v", err)
	}

	withStdin(t, "y\n")
	if err := codeUndo(); err != nil {
		t.Fatalf("codeUndo: unexpected error: %v", err)
	}

	got, err := os.ReadFile("tracked.txt")
	if err != nil {
		t.Fatalf("reading tracked.txt after undo: %v", err)
	}
	if string(got) != "v1\n" {
		t.Errorf("tracked.txt = %q after undo, want reverted to %q", got, "v1\n")
	}

	if _, err := os.Stat("staged.txt"); err != nil {
		t.Errorf("staged.txt should survive undo untouched: %v", err)
	}
	if _, err := os.Stat("untracked.txt"); err != nil {
		t.Errorf("untracked.txt should survive undo untouched: %v", err)
	}

	statusOut, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if !strings.Contains(string(statusOut), "A  staged.txt") {
		t.Errorf("expected staged.txt still staged (A ), got status:\n%s", statusOut)
	}
	if !strings.Contains(string(statusOut), "?? untracked.txt") {
		t.Errorf("expected untracked.txt still untracked (??), got status:\n%s", statusOut)
	}
}

// TestCodeUndoCancelled — answering "n" leaves the working tree exactly as-is.
func TestCodeUndoCancelled(t *testing.T) {
	initTempGitRepo(t)

	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("setup tracked.txt: %v", err)
	}
	runGitSetup(t, "add", "tracked.txt")
	runGitSetup(t, "commit", "-q", "-m", "initial")
	if err := os.WriteFile("tracked.txt", []byte("v2 unstaged\n"), 0o600); err != nil {
		t.Fatalf("unstaged edit: %v", err)
	}

	withStdin(t, "n\n")
	if err := codeUndo(); err != nil {
		t.Fatalf("codeUndo with cancellation: unexpected error: %v", err)
	}

	got, err := os.ReadFile("tracked.txt")
	if err != nil {
		t.Fatalf("reading tracked.txt after cancelled undo: %v", err)
	}
	if string(got) != "v2 unstaged\n" {
		t.Errorf("tracked.txt = %q after cancel, want unchanged %q", got, "v2 unstaged\n")
	}
}

// TestCodeCmdDispatchesUndo — /code undo reaches codeUndo via the registry
// dispatch, not just via a direct function call.
func TestCodeCmdDispatchesUndo(t *testing.T) {
	initTempGitRepo(t)

	r := &Registry{}
	cmd := r.codeCmd()

	// Clean tree → codeUndo's no-op path → nil error, no stdin read.
	if err := cmd.Run(context.Background(), []string{"undo"}); err != nil {
		t.Errorf("/code undo dispatch on clean tree: unexpected error: %v", err)
	}
}
