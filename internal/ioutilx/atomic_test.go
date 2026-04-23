package ioutilx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestAtomicWriteFile_BasicRoundtrip verifies that a simple write can be read
// back correctly and that no .tmp-* files are left behind.
func TestAtomicWriteFile_BasicRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	want := []byte(`{"key":"value"}`)

	if err := AtomicWriteFile(path, want, 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content mismatch: got %q, want %q", got, want)
	}

	// No temp files must remain.
	assertNoTempFiles(t, dir)
}

// TestAtomicWriteFile_CreatesParentDir ensures MkdirAll is applied so callers
// do not need to pre-create the parent directory.
func TestAtomicWriteFile_CreatesParentDir(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "deep", "nested", "settings.json")

	if err := AtomicWriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// TestAtomicWriteFile_FilePermissions verifies the final file has the exact
// mode passed to AtomicWriteFile.
func TestAtomicWriteFile_FilePermissions(t *testing.T) {
	tests := []struct {
		perm os.FileMode
	}{
		{0600},
		{0644},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("perm=%04o", tc.perm), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.json")
			if err := AtomicWriteFile(path, []byte("{}"), tc.perm); err != nil {
				t.Fatalf("AtomicWriteFile: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			got := info.Mode().Perm()
			if got != tc.perm {
				t.Errorf("permissions: got %04o, want %04o", got, tc.perm)
			}
		})
	}
}

// TestAtomicWriteFile_NoTempFilesOnSuccess verifies that after a successful
// write no .tmp-* files remain in the directory. This is the "crash simulate"
// test: we confirm the rename path cleaned up correctly.
func TestAtomicWriteFile_NoTempFilesOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write A, then overwrite with B — both should leave no temp files.
	for _, payload := range []string{`{"v":1}`, `{"v":2}`} {
		if err := AtomicWriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatalf("AtomicWriteFile: %v", err)
		}
		assertNoTempFiles(t, dir)
	}
}

// TestAtomicWriteFile_ConcurrentWrites writes from 10 goroutines simultaneously.
// The final file must be valid JSON equal to one of the written payloads (not
// torn / partially merged).
func TestAtomicWriteFile_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.json")

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	// Track every payload we actually attempt to write.
	payloads := make([]string, goroutines)
	for i := range goroutines {
		payloads[i] = fmt.Sprintf(`{"writer":%d}`, i)
	}

	for i := range goroutines {
		idx := i
		go func() {
			defer wg.Done()
			_ = AtomicWriteFile(path, []byte(payloads[idx]), 0600)
		}()
	}
	wg.Wait()

	// The final file must be one of the payloads — not a torn write.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after concurrent writes: %v", err)
	}
	found := false
	for _, p := range payloads {
		if string(got) == p {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("file content %q does not match any written payload (torn write?)", got)
	}

	// No leftover temp files.
	assertNoTempFiles(t, dir)
}

// assertNoTempFiles fails the test if any .tmp-* file exists in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file found: %s", e.Name())
		}
	}
}
