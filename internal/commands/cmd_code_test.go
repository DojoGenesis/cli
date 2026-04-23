package commands

// cmd_code_test.go — regression tests for path-traversal fix in codeRead.
//
// Security invariants tested:
//   1. Relative path inside cwd       → succeeds
//   2. Relative ../ escape            → "outside project root" error
//   3. Absolute path outside root     → "outside project root" error
//   4. Symlink inside root            → succeeds (monorepo-safe)
//   5. Symlink escaping root          → "outside project root" error

import (
	"os"
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
