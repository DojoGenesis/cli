package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeSkillMD creates (with parent dirs) a SKILL.md at dir/SKILL.md with
// the given raw content.
func writeSkillMD(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanExternal_DirLevelLayout(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "solo-skill")
	writeSkillMD(t, skillDir, "---\nname: Alpha\ndescription: Does alpha things.\n---\nbody\n")

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (got=%+v)", len(got), got)
	}
	want := ExternalSkill{
		Name:        "Alpha",
		Description: "Does alpha things.",
		Path:        filepath.Join(skillDir, "SKILL.md"),
		SourceDir:   skillDir,
	}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

func TestScanExternal_ChildLevelLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	writeSkillMD(t, filepath.Join(root, "foo"), "---\nname: Foo\ndescription: foo skill\n---\n")
	writeSkillMD(t, filepath.Join(root, "bar"), "---\nname: Bar\ndescription: bar skill\n---\n")

	got := ScanExternal([]string{root})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (got=%+v)", len(got), got)
	}
	// (SourceDir, Name) sort => Bar before Foo (same SourceDir, "Bar" < "Foo").
	if got[0].Name != "Bar" || got[1].Name != "Foo" {
		t.Fatalf("got names [%s, %s], want [Bar, Foo]", got[0].Name, got[1].Name)
	}
	for _, sk := range got {
		if sk.SourceDir != root {
			t.Errorf("SourceDir = %q, want %q", sk.SourceDir, root)
		}
	}
}

func TestScanExternal_NoRecursionBeyondOneChildLevel(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	// Grandchild SKILL.md must NOT be discovered -- depth cap is one child level.
	writeSkillMD(t, filepath.Join(root, "child", "grandchild"), "---\nname: TooDeep\n---\n")

	got := ScanExternal([]string{root})
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 (grandchild SKILL.md should not be found): %+v", len(got), got)
	}
}

func TestScanExternal_MissingDirSkipped(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	writeSkillMD(t, valid, "---\nname: Valid\n---\n")
	missing := filepath.Join(root, "does-not-exist")

	got := ScanExternal([]string{missing, valid})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (got=%+v)", len(got), got)
	}
	if got[0].Name != "Valid" {
		t.Fatalf("got name %q, want Valid", got[0].Name)
	}
}

func TestScanExternal_TildeExpansion(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	skillDir := filepath.Join(fakeHome, ".agents", "skills")
	writeSkillMD(t, skillDir, "---\nname: Tilde\n---\n")

	got := ScanExternal([]string{"~/.agents/skills"})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (got=%+v)", len(got), got)
	}
	if got[0].SourceDir != "~/.agents/skills" {
		t.Errorf("SourceDir = %q, want the pre-expansion literal %q", got[0].SourceDir, "~/.agents/skills")
	}
	wantPath := filepath.Join(skillDir, "SKILL.md")
	if got[0].Path != wantPath {
		t.Errorf("Path = %q, want %q", got[0].Path, wantPath)
	}
}

func TestScanExternal_FrontmatterPresentWithNestedDecoy(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "quoted")
	content := "---\n" +
		"name: \"Quoted Name\"\n" +
		"description: A thing that does stuff.\n" +
		"  name: this-is-nested-and-must-be-ignored\n" +
		"tags:\n" +
		"  - one\n" +
		"  - two\n" +
		"---\n" +
		"# Quoted Name\nBody text.\n"
	writeSkillMD(t, skillDir, content)

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "Quoted Name" {
		t.Errorf("Name = %q, want %q (quotes trimmed, nested decoy ignored)", got[0].Name, "Quoted Name")
	}
	if got[0].Description != "A thing that does stuff." {
		t.Errorf("Description = %q, want %q", got[0].Description, "A thing that does stuff.")
	}
}

func TestScanExternal_FrontmatterAbsent(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "plain-dir")
	writeSkillMD(t, skillDir, "# Just a heading\n\nNo frontmatter here.\n")

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "plain-dir" {
		t.Errorf("Name = %q, want fallback to containing dir name %q", got[0].Name, "plain-dir")
	}
	if got[0].Description != "" {
		t.Errorf("Description = %q, want empty", got[0].Description)
	}
}

func TestScanExternal_FrontmatterMalformedUnclosed(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "broken-dir")
	// Opening "---" with no closing "---" before EOF.
	writeSkillMD(t, skillDir, "---\nname: Broken\ndescription: Never closed\n")

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "broken-dir" {
		t.Errorf("Name = %q, want fallback to containing dir name %q (unclosed frontmatter treated as absent)", got[0].Name, "broken-dir")
	}
	if got[0].Description != "" {
		t.Errorf("Description = %q, want empty", got[0].Description)
	}
}

func TestScanExternal_MultilineDescriptionMarker(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "multiline")
	content := "---\nname: Multiline\ndescription: |\n  line one\n  line two\n---\n"
	writeSkillMD(t, skillDir, content)

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Description != "" {
		t.Errorf("Description = %q, want empty string for a `|` block scalar marker", got[0].Description)
	}
}

func TestScanExternal_DescriptionTruncation(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "long-desc")
	// Multi-byte rune repeated so a byte-based (rather than rune-based)
	// truncation would produce a visibly different, shorter rune count.
	long := strings.Repeat("é", 250)
	writeSkillMD(t, skillDir, "---\nname: LongDesc\ndescription: "+long+"\n---\n")

	got := ScanExternal([]string{skillDir})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	gotRunes := utf8.RuneCountInString(got[0].Description)
	if gotRunes != 200 {
		t.Fatalf("rune count = %d, want 200", gotRunes)
	}
	wantRunes := []rune(long)[:200]
	if got[0].Description != string(wantRunes) {
		t.Errorf("Description does not match first 200 runes of source")
	}
}

func TestFindExternal_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, filepath.Join(root, "mixed"), "---\nname: MixedCase\n---\n")
	dirs := []string{root}

	for _, query := range []string{"mixedcase", "MIXEDCASE", "MixedCase", "mIxEdCaSe"} {
		sk := FindExternal(dirs, query)
		if sk == nil {
			t.Fatalf("FindExternal(%q) = nil, want a match", query)
		}
		if sk.Name != "MixedCase" {
			t.Errorf("FindExternal(%q).Name = %q, want MixedCase", query, sk.Name)
		}
	}

	if sk := FindExternal(dirs, "nonexistent"); sk != nil {
		t.Errorf("FindExternal(nonexistent) = %+v, want nil", sk)
	}
}

func TestScanExternal_SortStability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	// Three child dirs that all resolve to the SAME Name (and SourceDir),
	// named so alphabetical directory-read order is unambiguous. A stable
	// sort must preserve this discovery order for the tied keys.
	for _, child := range []string{"a-first", "m-mid", "z-last"} {
		writeSkillMD(t, filepath.Join(root, child), "---\nname: Tied\n---\n")
	}

	got := ScanExternal([]string{root})
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	wantOrder := []string{"a-first", "m-mid", "z-last"}
	for i, want := range wantOrder {
		gotChild := filepath.Base(filepath.Dir(got[i].Path))
		if gotChild != want {
			t.Errorf("position %d: child dir = %q, want %q (order=%v)", i, gotChild, want, childDirsOf(got))
		}
	}
}

func childDirsOf(skills []ExternalSkill) []string {
	out := make([]string, len(skills))
	for i, sk := range skills {
		out[i] = filepath.Base(filepath.Dir(sk.Path))
	}
	return out
}

func TestScanExternal_Cap(t *testing.T) {
	orig := maxExternalSkills
	maxExternalSkills = 2
	defer func() { maxExternalSkills = orig }()

	root := filepath.Join(t.TempDir(), "skills")
	for _, child := range []string{"s1", "s2", "s3", "s4", "s5"} {
		writeSkillMD(t, filepath.Join(root, child), "---\nname: "+child+"\n---\n")
	}

	got := ScanExternal([]string{root})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (cap not enforced)", len(got))
	}
}
