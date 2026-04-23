// Package ioutilx provides filesystem utilities not found in the standard library.
package ioutilx

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path using a write-then-rename strategy so
// that a crash or concurrent invocation mid-write never leaves a truncated or
// partially-written file at path.
//
// The temp file is placed in the same directory as path so the final rename is
// guaranteed to be an atomic same-filesystem operation. The parent directory is
// created (with mode 0755) if it does not already exist.
//
// The final file is created with the given perm. Use 0600 for files that
// contain secrets (tokens, settings), 0644 for world-readable config.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*-"+filepath.Base(path))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// If anything below fails the deferred Remove is a no-op once the rename
	// has already moved the file, but it still cleans up on error paths.
	defer os.Remove(tmpName) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
