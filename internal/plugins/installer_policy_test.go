package plugins

// installer_policy_test.go — tests for the Rider C2 integrity gate:
// InstallPolicy's source allowlist (checkSourceAllowed/sourceOrigin) and the
// optional sha256 pin (HashPluginTree/verifyPinnedHash). New file: the
// pre-existing installer_test.go is left untouched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── sourceOrigin / checkSourceAllowed ─────────────────────────────────────

func TestSourceOrigin(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantHost   string
		wantOwner  string
		wantParsed bool
	}{
		{"github https", "https://github.com/DojoGenesis/plugins.git", "github.com", "DojoGenesis", true},
		{"github bare (no scheme)", "github.com/TresPies-source/foo", "github.com", "TresPies-source", true},
		{"github subdir tree URL", "https://github.com/anthropics/claude-plugins-official/tree/main/plugins/agent-sdk-dev", "github.com", "anthropics", true},
		{"gitlab", "https://gitlab.com/group/project.git", "gitlab.com", "group", true},
		{"local absolute path", "/tmp/some/local/repo.git", "", "", true},
		{"scp-style unparseable", "git@github.com:user/repo.git", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, owner, parsed := sourceOrigin(tc.input)
			if parsed != tc.wantParsed {
				t.Fatalf("sourceOrigin(%q) parsed = %v, want %v", tc.input, parsed, tc.wantParsed)
			}
			if !tc.wantParsed {
				return
			}
			if host != tc.wantHost {
				t.Errorf("sourceOrigin(%q) host = %q, want %q", tc.input, host, tc.wantHost)
			}
			if owner != tc.wantOwner {
				t.Errorf("sourceOrigin(%q) owner = %q, want %q", tc.input, owner, tc.wantOwner)
			}
		})
	}
}

func TestCheckSourceAllowed_DefaultAllowsHouseOrigins(t *testing.T) {
	policy := DefaultInstallPolicy()
	for _, u := range []string{
		"https://github.com/DojoGenesis/dojo-ke.git",
		"https://github.com/TresPies-source/some-plugin",
		"github.com/DojoGenesis/other-repo",
	} {
		if err := checkSourceAllowed(u, policy); err != nil {
			t.Errorf("checkSourceAllowed(%q) = %v, want nil (house origin)", u, err)
		}
	}
}

func TestCheckSourceAllowed_DefaultRefusesOtherOrigins(t *testing.T) {
	policy := DefaultInstallPolicy()
	for _, u := range []string{
		"https://github.com/some-random-org/plugin.git",
		"https://gitlab.com/group/project.git",
		"https://github.com/anthropics/claude-plugins-official/tree/main/plugins/agent-sdk-dev",
	} {
		err := checkSourceAllowed(u, policy)
		if err == nil {
			t.Errorf("checkSourceAllowed(%q) = nil, want a refusal error", u)
			continue
		}
		if !containsAll(err.Error(), "refused", "allowlist") {
			t.Errorf("checkSourceAllowed(%q) error = %v, want it to mention refused+allowlist", u, err)
		}
	}
}

func TestCheckSourceAllowed_LocalPathAllowed(t *testing.T) {
	// A bare local filesystem path has no remote origin to vet — the
	// allowlist must not refuse it (this is also what keeps
	// TestPermGatePluginInstallAllowlistAllow, in internal/commands, green:
	// it installs from a nonexistent local tmp path and expects the
	// failure to come from `git clone`, not the allowlist).
	policy := DefaultInstallPolicy()
	local := filepath.Join(t.TempDir(), "no-such-repo.git")
	if err := checkSourceAllowed(local, policy); err != nil {
		t.Errorf("checkSourceAllowed(%q) = %v, want nil (local path)", local, err)
	}
}

func TestCheckSourceAllowed_UnparseableRefused(t *testing.T) {
	policy := DefaultInstallPolicy()
	err := checkSourceAllowed("git@github.com:user/repo.git", policy)
	if err == nil {
		t.Fatal("expected a refusal for an unparseable source, got nil")
	}
}

func TestCheckSourceAllowed_AllowAnySourceBypasses(t *testing.T) {
	policy := InstallPolicy{AllowAnySource: true}
	for _, u := range []string{
		"https://github.com/some-random-org/plugin.git",
		"git@github.com:user/repo.git",
	} {
		if err := checkSourceAllowed(u, policy); err != nil {
			t.Errorf("checkSourceAllowed(%q) with AllowAnySource = %v, want nil", u, err)
		}
	}
}

func TestCheckSourceAllowed_NotBypassedByNoConfirm(t *testing.T) {
	// InstallConfirmedWithPolicy must refuse a disallowed origin even with
	// noConfirm=true (the --yes path) — noConfirm only pre-answers the y/N
	// prompt, never the integrity gate. git is never invoked: if the
	// allowlist were bypassed, this would instead fail with a "git clone
	// failed" error (or hang) since example.invalid is unreachable.
	destDir := t.TempDir()
	_, err := InstallConfirmedWithPolicy("https://example.invalid/some-plugin.git", destDir, true, DefaultInstallPolicy())
	if err == nil {
		t.Fatal("expected the allowlist to refuse a non-house origin, got nil error")
	}
	if !containsAll(err.Error(), "refused", "allowlist") {
		t.Errorf("error = %v, want an allowlist refusal (not a git/network error)", err)
	}
	entries, readErr := os.ReadDir(destDir)
	if readErr != nil {
		t.Fatalf("reading destDir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("destDir should stay empty after a refused install, has %d entries", len(entries))
	}
}

// ─── HashPluginTree / verifyPinnedHash ─────────────────────────────────────

func TestHashPluginTree_DeterministicAndContentSensitive(t *testing.T) {
	build := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "skills", "roll"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skills", "roll", "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		// VCS cruft that must not affect the digest.
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	dirA := build(t, "# roll v1")
	dirA2 := build(t, "# roll v1") // identical content, different tmp path
	dirB := build(t, "# roll v2 — different content")

	hashA, err := HashPluginTree(dirA)
	if err != nil {
		t.Fatalf("HashPluginTree(dirA): %v", err)
	}
	hashA2, err := HashPluginTree(dirA2)
	if err != nil {
		t.Fatalf("HashPluginTree(dirA2): %v", err)
	}
	hashB, err := HashPluginTree(dirB)
	if err != nil {
		t.Fatalf("HashPluginTree(dirB): %v", err)
	}

	if hashA != hashA2 {
		t.Errorf("identical trees hashed differently: %s vs %s", hashA, hashA2)
	}
	if hashA == hashB {
		t.Errorf("different trees produced the same hash: %s", hashA)
	}
}

func TestVerifyPinnedHash_MismatchDetected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashPluginTree(dir)
	if err != nil {
		t.Fatalf("HashPluginTree: %v", err)
	}

	results := []InstallResult{{Name: "x", Path: dir}}
	if err := verifyPinnedHash(results, got); err != nil {
		t.Errorf("verifyPinnedHash with the correct pin = %v, want nil", err)
	}
	if err := verifyPinnedHash(results, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("verifyPinnedHash with a wrong pin = nil, want a mismatch error")
	}
}

// containsAll reports whether s contains every substring in subs.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
