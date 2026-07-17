// External-skill discovery: read-only scanning of SKILL.md-bearing
// directories that live outside the gateway-hosted CAS skill set, per
// cfg.Skills.ExternalDirs (internal/config/config.go's SkillsConfig).
//
// Rationale (Hermes analog "skills.external_dirs"): promoted knowledge is
// reference material, not configuration. dojo may READ skills authored for
// foreign agent ecosystems -- Claude Code's ".claude/skills", Cursor's
// ".cursor/skills", the emerging "~/.agents/skills" convention popularized
// by Crush -- but this package never creates, modifies, or deletes anything
// under them.
//
// Frontmatter is hand-parsed rather than pulled in via a YAML library: no
// YAML package is a direct dependency of this module (checked against
// go.mod before writing this file), and the SKILL.md dialect only ever
// needs two top-level scalar keys (name, description), so a small
// hand-rolled scanner avoids adding a dependency for two string fields.
package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxDescriptionRunes caps ExternalSkill.Description length. 200 runes is
// enough for a one-line summary without letting a runaway SKILL.md balloon
// output in list views.
const maxDescriptionRunes = 200

// maxExternalSkills defensively bounds the total number of skills a single
// ScanExternal call will return, in case a misconfigured external dir (e.g.
// a home directory or a symlink loop target) contains far more
// SKILL.md-bearing entries than any real skill set would. It is a package
// var rather than a const so tests can shrink it and exercise the cap
// without staging 500 fixture files.
var maxExternalSkills = 500

// ExternalSkill is one skill discovered under a configured external
// directory (cfg.Skills.ExternalDirs). It is intentionally a much thinner
// shape than client.Skill (the gateway CAS skill entry): external skills
// are foreign, read-only reference material, not first-class dojo skills,
// so there is no ID/version/category/ports to carry.
type ExternalSkill struct {
	// Name is the frontmatter `name:` value, or the containing directory
	// name when frontmatter is absent, malformed, or leaves name empty.
	Name string
	// Description is the frontmatter `description:` value, truncated to
	// maxDescriptionRunes runes. May be empty.
	Description string
	// Path is the absolute path to the SKILL.md file itself.
	Path string
	// SourceDir is the configured external dir this skill was found
	// under, exactly as configured (pre-expansion) -- e.g. "~/.agents/skills",
	// not the $HOME-expanded absolute form.
	SourceDir string
}

// ScanExternal walks each configured dir (missing dirs are silently
// skipped) and returns all discovered skills, stable-sorted by
// (SourceDir, Name).
//
// Two layouts are recognized per configured dir, depth-limited to avoid
// runaway recursion into an unrelated directory tree:
//
//   - <dir>/SKILL.md         -- the configured dir IS a single skill.
//   - <dir>/<child>/SKILL.md -- the configured dir is a collection; each
//     immediate subdirectory may itself be one skill.
//
// Both patterns are checked for every configured dir; there is no
// recursion beyond the one child level (no <dir>/<child>/<grandchild>/...).
//
// Path expansion: a leading "~/" (or a bare "~") expands to $HOME via
// os.UserHomeDir; other relative paths resolve against the process cwd via
// filepath.Abs; absolute paths pass through unchanged.
//
// ScanExternal is READ-ONLY: it never creates, modifies, or deletes
// anything on disk.
func ScanExternal(dirs []string) []ExternalSkill {
	var results []ExternalSkill

	for _, dir := range dirs {
		if len(results) >= maxExternalSkills {
			break
		}

		expanded, err := expandExternalDir(dir)
		if err != nil {
			continue
		}
		info, err := os.Stat(expanded)
		if err != nil || !info.IsDir() {
			// Missing (or not-a-directory) configured dirs are silently
			// skipped -- an unconfigured foreign ecosystem on this
			// machine is the common case, not an error.
			continue
		}

		// Layout: <dir>/SKILL.md -- the dir itself is one skill.
		if sk, ok := loadSkillMD(filepath.Join(expanded, "SKILL.md"), dir); ok {
			results = append(results, sk)
			if len(results) >= maxExternalSkills {
				break
			}
		}

		// Layout: <dir>/<child>/SKILL.md -- one level of skill
		// subdirectories. No recursion past this depth.
		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if len(results) >= maxExternalSkills {
				break
			}
			if !entry.IsDir() {
				continue
			}
			childSkillMD := filepath.Join(expanded, entry.Name(), "SKILL.md")
			if sk, ok := loadSkillMD(childSkillMD, dir); ok {
				results = append(results, sk)
			}
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].SourceDir != results[j].SourceDir {
			return results[i].SourceDir < results[j].SourceDir
		}
		return results[i].Name < results[j].Name
	})
	return results
}

// FindExternal returns the first skill whose Name matches (case-insensitive),
// or nil. "First" follows ScanExternal's (SourceDir, Name) order.
func FindExternal(dirs []string, name string) *ExternalSkill {
	target := strings.ToLower(name)
	for _, sk := range ScanExternal(dirs) {
		if strings.ToLower(sk.Name) == target {
			return &sk
		}
	}
	return nil
}

// expandExternalDir resolves a configured external dir to an absolute path.
// A leading "~/" or a bare "~" expands to $HOME; other relative paths
// resolve against the process cwd; absolute paths pass through unchanged
// (modulo filepath.Clean).
func expandExternalDir(dir string) (string, error) {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if dir == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(dir, "~/")), nil
	}
	return filepath.Abs(dir)
}

// loadSkillMD reads and parses a single SKILL.md at path, attributing it to
// sourceDir (the pre-expansion configured dir). It reports ok=false when
// the file does not exist, is a directory, or cannot be read -- all of
// which are silently-skipped conditions per ScanExternal's contract.
func loadSkillMD(path, sourceDir string) (ExternalSkill, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ExternalSkill{}, false
	}

	name, description := parseFrontmatter(data)
	if name == "" {
		// Containing dir name: for both recognized layouts this is simply
		// the parent directory of the SKILL.md file itself.
		name = filepath.Base(filepath.Dir(path))
	}
	description = truncateRunes(description, maxDescriptionRunes)

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	return ExternalSkill{
		Name:        name,
		Description: description,
		Path:        absPath,
		SourceDir:   sourceDir,
	}, true
}

// parseFrontmatter hand-parses the leading "---" ... "---" block of a
// SKILL.md file and extracts only the top-level `name:` and `description:`
// scalar values. Everything else -- nested keys, arrays, multiline `|`/`>`
// block scalars, unrecognized keys -- is deliberately ignored rather than
// fully parsed as YAML.
//
// If the file has no leading "---" line, or the block is never closed by a
// following "---" before EOF (malformed), no frontmatter is recognized and
// both return values are empty -- callers fall back to the containing
// directory name and an empty description, exactly as if no frontmatter
// existed at all.
func parseFrontmatter(data []byte) (name, description string) {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", ""
	}

	closed := false
	for _, raw := range lines[1:] {
		line := strings.TrimRight(raw, "\r")
		if line == "---" {
			closed = true
			break
		}
		if v, ok := topLevelScalar(line, "name"); ok {
			name = v
		}
		if v, ok := topLevelScalar(line, "description"); ok {
			description = v
		}
	}
	if !closed {
		return "", ""
	}
	return name, description
}

// topLevelScalar reports whether line is a top-level (non-indented)
// "key: value" line for the given key, returning the trimmed, unquoted
// value. A multiline block scalar marker ("|" or ">", with any chomping
// suffix) resolves to an empty string per parseFrontmatter's contract,
// rather than attempting to consume the following indented lines.
func topLevelScalar(line, key string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", false // empty or indented => not a top-level key
	}
	prefix := key + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	v := strings.TrimSpace(line[len(prefix):])
	if strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
		return "", true
	}
	return trimQuotes(v), true
}

// trimQuotes strips one matching pair of surrounding double or single
// quotes, if present.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// truncateRunes cuts s to at most n runes (not bytes), leaving it unchanged
// if it is already within the limit.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
