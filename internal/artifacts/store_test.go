package artifacts

// Tests for internal/artifacts/store.go.
//
// All tests are hermetic: t.Setenv("HOME", t.TempDir()) redirects
// config.DojoDir() — which calls os.UserHomeDir() — away from the real ~/.dojo.
// No network, no external processes, no mutations to the developer's home dir.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ensureMDExt — pure string function (unexported, tested from same package)
// ---------------------------------------------------------------------------

func TestEnsureMDExt(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"report", "report.md"},
		{"report.md", "report.md"},
		{"REPORT.MD", "REPORT.MD.md"}, // only lowercase ".md" suffix is recognised
		{"", ".md"},
		{"sub/path/file", "sub/path/file.md"},
		// already ends in ".md" — must not add a second suffix
		{"already.md", "already.md"},
	}
	for _, tc := range cases {
		got := ensureMDExt(tc.input)
		if got != tc.want {
			t.Errorf("ensureMDExt(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Dir — pure path composition
// ---------------------------------------------------------------------------

func TestDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got := Dir("myproject", TypeScout)

	// Must be rooted under the temp home (not the real ~/.dojo).
	if !strings.HasPrefix(got, tmp) {
		t.Errorf("Dir %q does not start with temp HOME %q", got, tmp)
	}
	// Must embed the project ID.
	if !strings.Contains(got, "myproject") {
		t.Errorf("Dir %q does not contain project id", got)
	}
	// Must embed the artifact type.
	if !strings.Contains(got, string(TypeScout)) {
		t.Errorf("Dir %q does not contain artifact type", got)
	}

	// Different projects → different dirs.
	if Dir("alpha", TypeSpec) == Dir("beta", TypeSpec) {
		t.Error("Dir should differ for different project IDs")
	}
	// Different types → different dirs.
	if Dir("alpha", TypeScout) == Dir("alpha", TypeSpec) {
		t.Error("Dir should differ for different artifact types")
	}
}

// ---------------------------------------------------------------------------
// safeArtifactPath — path-traversal containment (security regression)
// ---------------------------------------------------------------------------

func TestSafeArtifactPathRejectsTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := Dir("proj-escape", TypeGeneric)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	// The exact example from the audit: "../../../x" escaping the
	// ~/.dojo/projects/<id>/<type> sandbox.
	_, err := safeArtifactPath(dir, "../../../x")
	if err == nil {
		t.Fatal("safeArtifactPath should reject a '../../../x' traversal filename; got nil error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v; want it to mention the path escaping the artifacts root", err)
	}
}

func TestSafeArtifactPathRejectsDeepTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := Dir("proj-escape-deep", TypeGeneric)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	// Enough ".." segments to walk past HOME entirely, not just past the
	// artifact-type directory — proves the check holds regardless of how
	// far the traversal reaches.
	_, err := safeArtifactPath(dir, strings.Repeat("../", 12)+"etc/passwd")
	if err == nil {
		t.Fatal("safeArtifactPath should reject a deep traversal filename; got nil error")
	}
}

func TestSafeArtifactPathAcceptsNormalFilename(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := Dir("proj-ok", TypeGeneric)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	got, err := safeArtifactPath(dir, "my-file.md")
	if err != nil {
		t.Fatalf("safeArtifactPath rejected a normal filename: %v", err)
	}
	if !strings.HasSuffix(got, "my-file.md") {
		t.Errorf("safeArtifactPath = %q; want it to end in my-file.md", got)
	}
	// Use Contains rather than a HasPrefix(got, dir) check: on macOS dir may
	// be spelled via the /var/folders symlink while safeArtifactPath returns
	// the EvalSymlinks-resolved /private/var/folders form of the same path.
	if !strings.Contains(got, "proj-ok") {
		t.Errorf("safeArtifactPath = %q; want it rooted under the proj-ok artifacts dir", got)
	}
}

func TestSafeArtifactPathAcceptsNestedSubdirFilename(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := Dir("proj-nested", TypeGeneric)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	// Legitimate nested filenames (no "..") must still be allowed — this
	// mirrors ensureMDExt's own test expectation ("sub/path/file").
	got, err := safeArtifactPath(dir, "sub/path/file.md")
	if err != nil {
		t.Fatalf("safeArtifactPath rejected a nested filename: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("sub", "path", "file.md")) {
		t.Errorf("safeArtifactPath = %q; want it to end in sub/path/file.md", got)
	}
}

// ---------------------------------------------------------------------------
// Save / Read / Delete — path-traversal rejection through the public API
// ---------------------------------------------------------------------------

func TestSaveRejectsPathTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := Save("proj-escape-save", TypeGeneric, "../../../x", "malicious content")
	if err == nil {
		t.Fatal("Save with a '../../../x' filename should return an error; got nil")
	}
}

func TestReadRejectsPathTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Seed a legitimate artifact so the project directory exists.
	if _, err := Save("proj-escape-read", TypeGeneric, "legit", "content"); err != nil {
		t.Fatalf("setup Save: %v", err)
	}

	_, err := Read("proj-escape-read", TypeGeneric, "../../../etc/passwd")
	if err == nil {
		t.Fatal("Read with a path-traversal filename should return an error; got nil")
	}
}

func TestDeleteRejectsPathTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Save("proj-escape-delete", TypeGeneric, "legit", "content"); err != nil {
		t.Fatalf("setup Save: %v", err)
	}

	err := Delete("proj-escape-delete", TypeGeneric, "../../../etc/passwd")
	if err == nil {
		t.Fatal("Delete with a path-traversal filename should return an error; got nil")
	}
}

// ---------------------------------------------------------------------------
// Save + Read round-trip
// ---------------------------------------------------------------------------

func TestSaveAndRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const content = "# My Spec\n\nThis is the content."

	path, err := Save("proj1", TypeSpec, "my-spec", content)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("Save returned path %q; want .md suffix", path)
	}

	got, err := Read("proj1", TypeSpec, "my-spec")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != content {
		t.Errorf("Read content = %q; want %q", got, content)
	}
}

func TestSaveAutoAddsExtension(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Saved without ".md" extension.
	if _, err := Save("proj2", TypeScout, "scout-notes", "data"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read without extension — ensureMDExt applied on both sides.
	if _, err := Read("proj2", TypeScout, "scout-notes"); err != nil {
		t.Errorf("Read without extension: %v", err)
	}
	// Read with explicit extension — must also work (idempotent).
	if _, err := Read("proj2", TypeScout, "scout-notes.md"); err != nil {
		t.Errorf("Read with .md extension: %v", err)
	}
}

func TestReadNonExistentReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := Read("no-project", TypeSpec, "no-file")
	if err == nil {
		t.Error("Read of non-existent file should return an error; got nil")
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Save("proj3", TypeRetro, "retro-1", "content"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := Delete("proj3", TypeRetro, "retro-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// File must be gone — Read must fail.
	if _, err := Read("proj3", TypeRetro, "retro-1"); err == nil {
		t.Error("Read after Delete should return an error; got nil")
	}
}

func TestDeleteNonExistentReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := Delete("proj4", TypeSpec, "ghost")
	if err == nil {
		t.Error("Delete of non-existent file should return an error; got nil")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestListMissingDirectoryReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	metas, err := List("no-project", TypeScout)
	if err != nil {
		t.Fatalf("List on missing dir: %v", err)
	}
	if metas != nil {
		t.Errorf("List on missing dir = %v; want nil", metas)
	}
}

func TestListFiltersNonMDFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Save("proj5", TypePrompt, "good", "data"); err != nil {
		t.Fatalf("Save good: %v", err)
	}

	// Manually plant a non-.md file in the same directory.
	dir := Dir("proj5", TypePrompt)
	if err := os.WriteFile(dir+"/ignore.txt", []byte("noise"), 0600); err != nil {
		t.Fatalf("write ignore.txt: %v", err)
	}

	metas, err := List("proj5", TypePrompt)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("List returned %d entries; want 1", len(metas))
	}
	if metas[0].Filename != "good.md" {
		t.Errorf("Filename = %q; want good.md", metas[0].Filename)
	}
	if metas[0].ArtifactType != TypePrompt {
		t.Errorf("ArtifactType = %v; want %v", metas[0].ArtifactType, TypePrompt)
	}
}

func TestListSortedNewestFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Save("proj6", TypeSpec, "alpha", "a"); err != nil {
		t.Fatalf("Save alpha: %v", err)
	}
	if _, err := Save("proj6", TypeSpec, "beta", "b"); err != nil {
		t.Fatalf("Save beta: %v", err)
	}

	// Force alpha to be clearly older so the sort order is deterministic.
	alphaPath := Dir("proj6", TypeSpec) + "/alpha.md"
	older := time.Now().Add(-30 * time.Second)
	if err := os.Chtimes(alphaPath, older, older); err != nil {
		t.Fatalf("Chtimes alpha: %v", err)
	}

	metas, err := List("proj6", TypeSpec)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("List = %d entries; want 2", len(metas))
	}
	if metas[0].Filename != "beta.md" {
		t.Errorf("first = %q; want beta.md (newest)", metas[0].Filename)
	}
	if metas[1].Filename != "alpha.md" {
		t.Errorf("second = %q; want alpha.md (oldest)", metas[1].Filename)
	}
}

func TestListMetaFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const content = "hello world"
	if _, err := Save("proj9", TypeGeneric, "thing", content); err != nil {
		t.Fatalf("Save: %v", err)
	}

	metas, err := List("proj9", TypeGeneric)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("List = %d entries; want 1", len(metas))
	}
	m := metas[0]
	if m.Size != int64(len(content)) {
		t.Errorf("Size = %d; want %d", m.Size, len(content))
	}
	if m.ModifiedAt.IsZero() {
		t.Error("ModifiedAt is zero")
	}
	if !strings.HasPrefix(m.Path, t.TempDir()) {
		// t.TempDir() is the parent; the test-specific subdirs sit beneath it.
		// Relaxed check: path must contain the expected artifact directory.
		if !strings.Contains(m.Path, "proj9") {
			t.Errorf("Path %q does not reference the project", m.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// ListAll
// ---------------------------------------------------------------------------

func TestListAll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	seeds := []struct {
		at       ArtifactType
		filename string
	}{
		{TypeScout, "scout1"},
		{TypeSpec, "spec1"},
		{TypePrompt, "prompt1"},
	}
	for _, s := range seeds {
		if _, err := Save("proj7", s.at, s.filename, "content"); err != nil {
			t.Fatalf("Save %s/%s: %v", s.at, s.filename, err)
		}
	}

	all, err := ListAll("proj7")
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != len(seeds) {
		t.Errorf("ListAll = %d entries; want %d", len(all), len(seeds))
	}

	// Every entry must carry a type that was actually saved.
	knownTypes := map[ArtifactType]bool{TypeScout: true, TypeSpec: true, TypePrompt: true}
	for _, m := range all {
		if !knownTypes[m.ArtifactType] {
			t.Errorf("unexpected ArtifactType %q in ListAll result", m.ArtifactType)
		}
	}
}

func TestListAllEmptyProjectReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	all, err := ListAll("empty-project")
	if err != nil {
		t.Fatalf("ListAll on empty project: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("ListAll on empty project = %d entries; want 0", len(all))
	}
}

// ---------------------------------------------------------------------------
// SaveWithTimestamp
// ---------------------------------------------------------------------------

func TestSaveWithTimestampNoDoubleExtension(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := SaveWithTimestamp("proj8", TypeTrack, "plan", "body")
	if err != nil {
		t.Fatalf("SaveWithTimestamp: %v", err)
	}

	base := path[strings.LastIndex(path, "/")+1:]

	if !strings.HasSuffix(base, ".md") {
		t.Errorf("filename %q does not end in .md", base)
	}
	if strings.HasSuffix(base, ".md.md") {
		t.Errorf("filename %q has a double .md suffix", base)
	}
}

func TestSaveWithTimestampContentRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const body = "track content here"
	if _, err := SaveWithTimestamp("proj8b", TypeTrack, "plan", body); err != nil {
		t.Fatalf("SaveWithTimestamp: %v", err)
	}

	metas, err := List("proj8b", TypeTrack)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("List = %d entries; want 1", len(metas))
	}

	got, err := Read("proj8b", TypeTrack, metas[0].Filename)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != body {
		t.Errorf("content = %q; want %q", got, body)
	}
}
