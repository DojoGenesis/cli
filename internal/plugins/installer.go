// Package plugins — installer.go provides Install/Uninstall for git-based plugin management.
package plugins

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallResult holds the path and name of one installed plugin.
type InstallResult struct {
	Name string
	Path string
}

// InstallPolicy controls the integrity checks InstallConfirmedWithPolicy
// enforces before (source allowlist) and after (hash pin) cloning an
// external plugin. Installed plugins run arbitrary command-hook shell (see
// internal/hooks), so "eyeball the URL + y/N" is not, by itself, a
// meaningful trust boundary — this is the minimal-but-real check ahead of
// full artifact signing (out of scope here).
type InstallPolicy struct {
	// AllowedSources lists "host/owner" origins a plugin's git URL must
	// match — e.g. "github.com/DojoGenesis". Matching is case-insensitive
	// and exact (no wildcards); see DefaultInstallPolicy for the house
	// default. Ignored when AllowAnySource is true.
	AllowedSources []string
	// AllowAnySource disables the source allowlist entirely. This is a
	// separate, explicitly-named opt-out — it is NEVER implied by
	// InstallConfirmed's noConfirm/--yes, which only pre-answers the
	// interactive y/N prompt. A caller has to deliberately set this field
	// to bypass the allowlist.
	AllowAnySource bool
	// ExpectedSHA256 optionally pins the installed plugin tree to a known
	// content digest (see HashPluginTree). Empty skips the pin check. When
	// set and a clone yields more than one plugin (the monorepo case),
	// EVERY resulting plugin must match — the pin is meant for a single,
	// known-good artifact (a root repo or a GitHub subdirectory URL), not
	// an "any of these" match across an unrelated set.
	ExpectedSHA256 string
}

// DefaultInstallPolicy returns the house allowlist: first-party DojoGenesis
// and TresPies-source origins only. Callers needing a broader or narrower
// policy build their own InstallPolicy and call InstallConfirmedWithPolicy
// directly; InstallConfirmed (the pre-existing entry point) always applies
// this default so the real /plugin install call path is protected without
// any caller-side change.
func DefaultInstallPolicy() InstallPolicy {
	return InstallPolicy{
		AllowedSources: []string{"github.com/DojoGenesis", "github.com/TresPies-source"},
	}
}

// Install clones a git URL and installs one or more plugins into destDir.
// Three cases are handled in order:
//  1. GitHub subdirectory URL (https://github.com/{o}/{r}/tree/{branch}/{path}) —
//     sparse-checkout of the subpath only.
//  2. Monorepo — full clone, no root plugin.json found, scan subdirs up to
//     depth 2 for plugin.json and extract each as its own plugin.
//  3. Root plugin — existing behaviour: clone root, validate plugin.json.
func Install(gitURL, destDir string) ([]InstallResult, error) {
	normalized := normalizeURL(gitURL)

	// Case 1: GitHub subdirectory URL.
	if cloneURL, subpath, branch, ok := parseGitHubSubdirURL(normalized); ok {
		return installSparse(cloneURL, subpath, branch, destDir)
	}

	// Cases 2 & 3: full clone then inspect.
	return installFull(normalized, destDir)
}

// InstallConfirmed is the interactive entry point for CLI use.
// It prints a security warning to stderr listing the URL, then — unless
// noConfirm is true — prompts the user for explicit y/n confirmation before
// proceeding. Pass noConfirm=true to skip the prompt (e.g. --yes flag).
//
// This is now a thin wrapper over InstallConfirmedWithPolicy using
// DefaultInstallPolicy() — additive: the signature and behavior for any
// already-allowlisted source are unchanged, but every call now also passes
// through the source-allowlist gate (Rider C2). Callers that need a
// different policy (a wider allowlist, AllowAnySource, or a sha256 pin) call
// InstallConfirmedWithPolicy directly.
func InstallConfirmed(gitURL, destDir string, noConfirm bool) ([]InstallResult, error) {
	return InstallConfirmedWithPolicy(gitURL, destDir, noConfirm, DefaultInstallPolicy())
}

// InstallConfirmedWithPolicy is InstallConfirmed with an explicit
// InstallPolicy. It prints the same security warning, then enforces the
// policy's source allowlist UNCONDITIONALLY — before the y/N prompt and
// regardless of noConfirm — since noConfirm/--yes only pre-answers the
// confirmation question, never the integrity gate. On success, when
// policy.ExpectedSHA256 is set, every installed result's tree is hashed
// (HashPluginTree) and compared; a mismatch removes everything Install just
// wrote and returns an error rather than leaving a partially-verified
// plugin on disk.
func InstallConfirmedWithPolicy(gitURL, destDir string, noConfirm bool, policy InstallPolicy) ([]InstallResult, error) {
	fmt.Fprintf(os.Stderr, "\n  WARNING: Installing plugin from external source. Verify trust before proceeding.\n")
	fmt.Fprintf(os.Stderr, "  URL: %s\n\n", gitURL)

	if err := checkSourceAllowed(gitURL, policy); err != nil {
		return nil, err
	}

	if !noConfirm {
		fmt.Fprint(os.Stderr, "  Continue? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			return nil, fmt.Errorf("plugin install cancelled by user")
		}
	}

	results, err := Install(gitURL, destDir)
	if err != nil {
		return nil, err
	}

	if policy.ExpectedSHA256 != "" {
		if err := verifyPinnedHash(results, policy.ExpectedSHA256); err != nil {
			for _, res := range results {
				_ = os.RemoveAll(res.Path)
			}
			return nil, err
		}
	}

	return results, nil
}

// checkSourceAllowed enforces policy's source allowlist ahead of any clone —
// refusing a non-allowlisted origin here means a malicious or unvetted git
// URL never reaches exec.Command("git", "clone", ...). A URL that parses
// with no host at all (a local filesystem path, e.g. from a dev workflow
// installing a plugin already on disk) is let through: the allowlist exists
// to vet third-party REMOTE origins, and a bare local path carries no
// supply-chain risk of that kind — the caller already has whatever access
// to it that installing would grant. A URL that fails to parse at all is
// refused (fail closed): it cannot be verified as either a safe local path
// or an allowlisted remote origin.
func checkSourceAllowed(gitURL string, policy InstallPolicy) error {
	if policy.AllowAnySource {
		return nil
	}
	host, owner, parsed := sourceOrigin(gitURL)
	if !parsed {
		return fmt.Errorf("plugin install refused: could not parse source %q (set InstallPolicy.AllowAnySource to override)", gitURL)
	}
	if host == "" {
		return nil // local filesystem path — no remote origin to vet
	}
	candidate := host
	if owner != "" {
		candidate = host + "/" + owner
	}
	for _, allowed := range policy.AllowedSources {
		if strings.EqualFold(candidate, allowed) {
			return nil
		}
	}
	return fmt.Errorf("plugin install refused: source %q is not in the allowlist (%s) — pass an InstallPolicy with AllowAnySource, or an expanded AllowedSources, to override",
		candidate, strings.Join(policy.AllowedSources, ", "))
}

// sourceOrigin extracts the (host, owner) pair from a plugin git URL for
// allowlist matching, e.g. "https://github.com/DojoGenesis/plugins.git" ->
// ("github.com", "DojoGenesis"). owner is always the first path segment,
// which is correct for both a root repo URL and the
// "/tree/{branch}/{path}" subdirectory form parseGitHubSubdirURL handles —
// the owner sits before "/tree/" either way, so no special-casing of that
// shape is needed here. parsed is false only when normalizeURL's result
// fails to parse as a URL at all (see checkSourceAllowed for how the two
// failure/no-host cases are treated differently).
func sourceOrigin(gitURL string) (host, owner string, parsed bool) {
	u, err := url.Parse(normalizeURL(gitURL))
	if err != nil {
		return "", "", false
	}
	if u.Host == "" {
		return "", "", true
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		owner = parts[0]
	}
	return u.Host, owner, true
}

// HashPluginTree computes a deterministic sha256 digest over a plugin
// directory's file contents and relative paths, for InstallPolicy's
// ExpectedSHA256 pin. filepath.WalkDir visits entries in lexical order, and
// each file's relative path (as a NUL-separated, slash-normalized prefix) is
// hashed ahead of its content, so neither a reordered walk nor a
// same-content-different-name file can produce a matching digest by
// accident. Skips the same VCS/cache cruft the install/copy paths already
// ignore (.git, .DS_Store, __pycache__) so the pin survives incidental
// clone artifacts rather than breaking on them.
func HashPluginTree(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if name == ".git" || name == ".DS_Store" || name == "__pycache__" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // path walks a just-cloned plugin tree under our own destDir
		if readErr != nil {
			return readErr
		}
		// Writing to a hash never errors; discard explicitly for errcheck.
		_, _ = fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyPinnedHash hashes each installed plugin's tree and compares it
// against policy's ExpectedSHA256 pin (case-insensitive hex compare).
func verifyPinnedHash(results []InstallResult, expected string) error {
	expected = strings.TrimSpace(expected)
	for _, res := range results {
		got, err := HashPluginTree(res.Path)
		if err != nil {
			return fmt.Errorf("hash %s for sha256 pin check: %w", res.Path, err)
		}
		if !strings.EqualFold(got, expected) {
			return fmt.Errorf("sha256 pin mismatch for %s: got %s, want %s", res.Name, got, expected)
		}
	}
	return nil
}

// Uninstall removes a plugin directory.
func Uninstall(pluginName, pluginsRoot string) error {
	dest := filepath.Join(pluginsRoot, pluginName)

	info, err := os.Stat(dest)
	if err != nil {
		return fmt.Errorf("plugin %q not found at %s", pluginName, dest)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dest)
	}

	if !hasPluginJSON(dest) {
		return fmt.Errorf("%s does not appear to be a plugin (no plugin.json found)", dest)
	}

	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("failed to remove plugin: %w", err)
	}
	return nil
}

// installFull clones the repository and handles root-plugin vs monorepo detection.
func installFull(gitURL, destDir string) ([]InstallResult, error) {
	name := repoName(gitURL)
	if name == "" {
		return nil, fmt.Errorf("cannot extract repository name from URL %q", gitURL)
	}

	dest := filepath.Join(destDir, name)
	if _, err := os.Stat(dest); err == nil {
		return nil, fmt.Errorf("plugin already installed at %s", dest)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	cmd := exec.Command("git", "clone", "--depth", "1", gitURL, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git clone failed: %s\n%s", err, strings.TrimSpace(string(out)))
	}

	// Case 3: root-level plugin.json — single plugin.
	if hasPluginJSON(dest) {
		return []InstallResult{{Name: name, Path: dest}}, nil
	}

	// Case 2: monorepo — scan subdirs for plugin.json.
	results, err := extractMonorepoPlugins(dest, destDir)
	if err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}
	if len(results) == 0 {
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("cloned repository contains no plugin.json — not a plugin repo (removed %s)", dest)
	}

	// Remove the full clone after extraction (each plugin now lives at its own path).
	_ = os.RemoveAll(dest)
	return results, nil
}

// extractMonorepoPlugins scans cloneDir up to depth 2 for plugin.json files,
// copies each matching subdir into destDir as an independent plugin, and
// returns one InstallResult per plugin found.
func extractMonorepoPlugins(cloneDir, destDir string) ([]InstallResult, error) {
	var results []InstallResult

	// Scan immediate children.
	top, err := os.ReadDir(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read cloned repo: %w", err)
	}

	for _, e := range top {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		level1 := filepath.Join(cloneDir, e.Name())

		// Depth 1: plugin.json directly inside a child dir.
		if hasPluginJSON(level1) {
			r, err := movePlugin(level1, destDir)
			if err != nil {
				return nil, err
			}
			results = append(results, r)
			continue
		}

		// Depth 2: plugin.json inside grandchild dirs.
		subs, err := os.ReadDir(level1)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if !sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
				continue
			}
			level2 := filepath.Join(level1, sub.Name())
			if hasPluginJSON(level2) {
				r, err := movePlugin(level2, destDir)
				if err != nil {
					return nil, err
				}
				results = append(results, r)
			}
		}
	}
	return results, nil
}

// movePlugin copies srcDir into destDir/{dirName} and returns the InstallResult.
// Uses os.CopyFS (Go 1.23+) where available; falls back to a recursive copy.
func movePlugin(srcDir, destDir string) (InstallResult, error) {
	name := filepath.Base(srcDir)
	dst := filepath.Join(destDir, name)

	if _, err := os.Stat(dst); err == nil {
		return InstallResult{}, fmt.Errorf("plugin already installed at %s", dst)
	}

	if err := copyDir(srcDir, dst); err != nil {
		_ = os.RemoveAll(dst)
		return InstallResult{}, fmt.Errorf("failed to copy plugin %q: %w", name, err)
	}
	return InstallResult{Name: name, Path: dst}, nil
}

// copyDir recursively copies src into dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// installSparse performs a sparse-checkout of a single subdirectory from a git repo.
func installSparse(cloneURL, subpath, branch, destDir string) ([]InstallResult, error) {
	name := filepath.Base(subpath)
	if name == "" || name == "." {
		return nil, fmt.Errorf("cannot determine plugin name from subpath %q", subpath)
	}

	dest := filepath.Join(destDir, name)
	if _, err := os.Stat(dest); err == nil {
		return nil, fmt.Errorf("plugin already installed at %s", dest)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	// Step 1: init an empty repo.
	if out, err := exec.Command("git", "init", dest).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git init failed: %s\n%s", err, strings.TrimSpace(string(out)))
	}

	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dest
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %s\n%s", args[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// Step 2: configure sparse-checkout.
	if err := run("remote", "add", "origin", cloneURL); err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}
	if err := run("sparse-checkout", "init", "--cone"); err != nil {
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("sparse-checkout init failed (git 2.25+ required): %w", err)
	}
	if err := run("sparse-checkout", "set", subpath); err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}

	// Step 3: fetch only the target branch, depth 1.
	if err := run("fetch", "--depth", "1", "origin", branch); err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}
	if err := run("checkout", branch); err != nil {
		// Try FETCH_HEAD as fallback.
		if err2 := run("checkout", "FETCH_HEAD"); err2 != nil {
			_ = os.RemoveAll(dest)
			return nil, fmt.Errorf("checkout failed: %w (also tried FETCH_HEAD: %v)", err, err2)
		}
	}

	// After sparse checkout, the plugin lives at dest/{subpath}.
	// Promote it: copy the contents up to dest, then remove the scaffold.
	pluginSrc := filepath.Join(dest, subpath)
	if _, err := os.Stat(pluginSrc); err != nil {
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("sparse checkout did not produce %s", pluginSrc)
	}

	// Temp rename dest, copy subdir back to dest.
	tmp := dest + ".__tmp"
	if err := os.Rename(dest, tmp); err != nil {
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("failed to stage sparse checkout: %w", err)
	}
	if err := copyDir(filepath.Join(tmp, subpath), dest); err != nil {
		_ = os.RemoveAll(tmp)
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("failed to promote sparse subpath: %w", err)
	}
	_ = os.RemoveAll(tmp)

	if !hasPluginJSON(dest) {
		_ = os.RemoveAll(dest)
		return nil, fmt.Errorf("subdirectory %q does not contain plugin.json", subpath)
	}

	return []InstallResult{{Name: name, Path: dest}}, nil
}

// parseGitHubSubdirURL detects a GitHub web URL pointing to a subdirectory:
//
//	https://github.com/{owner}/{repo}/tree/{branch}/{path...}
//
// Returns (cloneURL, subpath, branch, true) on match.
func parseGitHubSubdirURL(rawURL string) (cloneURL, subpath, branch string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "github.com" {
		return "", "", "", false
	}
	// Path segments: ["", owner, repo, "tree", branch, path...]
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", "", "", false
	}
	cloneURL = fmt.Sprintf("https://github.com/%s/%s", parts[0], parts[1])
	branch = parts[3]
	subpath = strings.Join(parts[4:], "/")
	return cloneURL, subpath, branch, true
}

// repoName extracts a directory name from a git URL.
//
//	"https://github.com/DojoGenesis/plugins.git" -> "plugins"
//	"github.com/foo/bar" -> "bar"
func repoName(gitURL string) string {
	gitURL = strings.TrimRight(gitURL, "/")

	var pathStr string
	if u, err := url.Parse(gitURL); err == nil && u.Path != "" {
		pathStr = u.Path
	} else {
		pathStr = gitURL
	}

	name := filepath.Base(pathStr)
	name = strings.TrimSuffix(name, ".git")

	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

// normalizeURL prepends "https://" if the URL has no scheme.
func normalizeURL(gitURL string) string {
	gitURL = strings.TrimSpace(gitURL)
	if strings.Contains(gitURL, "://") {
		return gitURL
	}
	return "https://" + gitURL
}

// hasPluginJSON checks whether a directory contains plugin.json at
// the root or inside .claude-plugin/.
func hasPluginJSON(dir string) bool {
	candidates := []string{
		filepath.Join(dir, "plugin.json"),
		filepath.Join(dir, ".claude-plugin", "plugin.json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
