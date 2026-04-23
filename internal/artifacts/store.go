// Package artifacts persists skill outputs and workflow results as markdown
// files under ~/.dojo/projects/<projectID>/<artifactType>/.
package artifacts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/ioutilx"
)

// ArtifactType identifies the category of a persisted artifact.
type ArtifactType string

const (
	TypeScout   ArtifactType = "scouts"
	TypeSpec    ArtifactType = "specs"
	TypePrompt  ArtifactType = "prompts"
	TypeRetro   ArtifactType = "retros"
	TypeTrack   ArtifactType = "tracks"
	TypeGeneric ArtifactType = "artifacts"
)

// allTypes lists every known ArtifactType, used by ListAll.
var allTypes = []ArtifactType{
	TypeScout,
	TypeSpec,
	TypePrompt,
	TypeRetro,
	TypeTrack,
	TypeGeneric,
}

// DefaultProjectID is used when no project is active.
const DefaultProjectID = "global"

// ArtifactMeta holds metadata about a saved artifact (no content).
type ArtifactMeta struct {
	Filename     string
	ArtifactType ArtifactType
	Path         string // absolute path
	Size         int64
	ModifiedAt   time.Time
}

// Dir returns the directory for a project+type:
// ~/.dojo/projects/<projectID>/<artifactType>
func Dir(projectID string, at ArtifactType) string {
	return filepath.Join(config.DojoDir(), "projects", projectID, string(at))
}

// ensureMDExt appends ".md" if filename does not already end with ".md".
func ensureMDExt(filename string) string {
	if strings.HasSuffix(filename, ".md") {
		return filename
	}
	return filename + ".md"
}

// Save writes content to ~/.dojo/projects/<projectID>/<artifactType>/<filename>.
// Ensures .md extension. Creates directories as needed.
// Returns the absolute path of the saved file.
func Save(projectID string, at ArtifactType, filename, content string) (string, error) {
	filename = ensureMDExt(filename)
	dir := Dir(projectID, at)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("artifacts: create directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := ioutilx.AtomicWriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("artifacts: write %s: %w", path, err)
	}
	return path, nil
}

// SaveWithTimestamp prefixes filename with YYYYMMDD-HHMMSS-.
// Example: "20260409-143022-scout.md"
func SaveWithTimestamp(projectID string, at ArtifactType, basename, content string) (string, error) {
	ts := time.Now().Format("20060102-150405")
	basename = ensureMDExt(basename)
	// Strip the .md before prepending the timestamp so the result is
	// "<ts>-<basename>.md" rather than "<ts>-<basename>.md.md".
	base := strings.TrimSuffix(basename, ".md")
	filename := ts + "-" + base + ".md"
	return Save(projectID, at, filename, content)
}

// List returns ArtifactMeta for all .md files in the given project+type
// directory. Returns nil, nil if the directory does not exist.
// Results are sorted by ModifiedAt descending (newest first).
func List(projectID string, at ArtifactType) ([]ArtifactMeta, error) {
	dir := Dir(projectID, at)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("artifacts: read directory %s: %w", dir, err)
	}

	var metas []ArtifactMeta
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("artifacts: stat %s: %w", entry.Name(), err)
		}
		metas = append(metas, ArtifactMeta{
			Filename:     entry.Name(),
			ArtifactType: at,
			Path:         filepath.Join(dir, entry.Name()),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime(),
		})
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].ModifiedAt.After(metas[j].ModifiedAt)
	})

	return metas, nil
}

// ListAll returns all artifacts across all types for a project.
func ListAll(projectID string) ([]ArtifactMeta, error) {
	var all []ArtifactMeta
	for _, at := range allTypes {
		metas, err := List(projectID, at)
		if err != nil {
			return nil, err
		}
		all = append(all, metas...)
	}
	// Sort combined slice newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModifiedAt.After(all[j].ModifiedAt)
	})
	return all, nil
}

// Read returns the content of a specific artifact file.
func Read(projectID string, at ArtifactType, filename string) (string, error) {
	filename = ensureMDExt(filename)
	path := filepath.Join(Dir(projectID, at), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("artifacts: read %s: %w", path, err)
	}
	return string(data), nil
}

// Delete removes an artifact file.
func Delete(projectID string, at ArtifactType, filename string) error {
	filename = ensureMDExt(filename)
	path := filepath.Join(Dir(projectID, at), filename)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("artifacts: delete %s: %w", path, err)
	}
	return nil
}
