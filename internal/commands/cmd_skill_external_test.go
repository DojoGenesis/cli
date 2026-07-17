package commands

// cmd_skill_external_test.go — tests for the read-only external-skill surface
// of /skill: the supplementary "External (read-only)" section on /skill ls,
// the explicit ext:<name> namespace on /skill get, the external fallback when
// the gateway tag lookup misses, and the preserved miss behavior when both
// sides miss.
//
// Seams reused from existing test files (same package, no edits there):
// captureProtocolStdout (cmd_protocol_test.go) for stdout+gcolor capture and
// protoWriteFile (cmd_protocol_test.go) for fixture files. The gateway is
// faked with httptest, mirroring internal/client's own test style.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/plugins"
)

// newExternalSkillRegistry builds a Registry wired to gatewayURL (empty =>
// nil client; only safe for code paths that never touch the gateway) with
// cfg.Skills.ExternalDirs set. Mirrors commands_test.go's testRegistry.
func newExternalSkillRegistry(gatewayURL string, externalDirs []string) *Registry {
	session := "skill-external-test-session"
	cfg := &config.Config{
		Gateway: config.GatewayConfig{URL: gatewayURL, Timeout: "5s"},
		Skills:  config.SkillsConfig{ExternalDirs: externalDirs},
	}
	var gw *client.Client
	if gatewayURL != "" {
		gw = client.New(gatewayURL, "", "5s")
	}
	r := &Registry{
		cfg:     cfg,
		gw:      gw,
		cmds:    make(map[string]Command),
		plgs:    []plugins.Plugin{},
		session: &session,
	}
	r.register()
	return r
}

// oneSkillGateway serves GET /api/skills with a single gateway skill (total=1
// so client.SkillsAll stops after one page) and 404 for everything else.
func oneSkillGateway(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/skills") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"skills":[{"id":"s1","name":"gw-skill","description":"a gateway skill","category":"engineering","inputs":[],"outputs":[]}],"total":1,"limit":100,"offset":0}`))
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// missGateway 404s every request — CAS tag resolution always misses.
func missGateway(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeExternalFixture creates <root>/<child>/SKILL.md and returns (root, path).
func writeExternalFixture(t *testing.T, child, frontmatter string) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, child, "SKILL.md")
	protoWriteFile(t, path, frontmatter)
	return root, path
}

// ─── /skill ls — External (read-only) section ────────────────────────────────

func TestSkillLsAppendsExternalSection(t *testing.T) {
	srv := oneSkillGateway(t)
	root, _ := writeExternalFixture(t, "ext-alpha",
		"---\nname: ext-alpha\ndescription: External alpha does alpha things.\n---\n# ext-alpha\nbody\n")
	r := newExternalSkillRegistry(srv.URL, []string{root})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill ls")
	})
	if err != nil {
		t.Fatalf("skill ls: %v", err)
	}
	// Existing gateway listing still renders first: bare `/skill ls` shows
	// the category summary (categories, not skill names).
	if !strings.Contains(out, "Skills  1 total") || !strings.Contains(out, "engineering") {
		t.Errorf("gateway category summary missing from output:\n%s", out)
	}
	if !strings.Contains(out, "External (read-only)") {
		t.Errorf("External (read-only) section header missing:\n%s", out)
	}
	if !strings.Contains(out, "ext:ext-alpha") {
		t.Errorf("ext:<name> line missing:\n%s", out)
	}
	if !strings.Contains(out, "External alpha does alpha things.") {
		t.Errorf("external description missing:\n%s", out)
	}
	if !strings.Contains(out, "("+root+")") {
		t.Errorf("SourceDir suffix missing:\n%s", out)
	}
	// Section must come after the gateway listing.
	if strings.Index(out, "External (read-only)") < strings.Index(out, "engineering") {
		t.Errorf("External section rendered before the gateway listing:\n%s", out)
	}
}

func TestSkillLsEmptyScanPrintsNothingExtra(t *testing.T) {
	srv := oneSkillGateway(t)
	// ExternalDirs points at a dir that exists but holds no SKILL.md.
	r := newExternalSkillRegistry(srv.URL, []string{t.TempDir()})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill ls")
	})
	if err != nil {
		t.Fatalf("skill ls: %v", err)
	}
	if strings.Contains(out, "External (read-only)") {
		t.Errorf("empty scan must print no External section:\n%s", out)
	}
	if strings.Contains(out, "ext:") {
		t.Errorf("empty scan must print no ext: lines:\n%s", out)
	}
}

func TestSkillLsGatewayErrorUnchanged(t *testing.T) {
	// Gateway unreachable: /skill ls must surface the existing error
	// unchanged, with no External section around it.
	root, _ := writeExternalFixture(t, "ext-alpha", "---\nname: ext-alpha\n---\n")
	srv := missGateway(t) // /api/skills 404s → Skills() errors
	r := newExternalSkillRegistry(srv.URL, []string{root})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill ls")
	})
	if err == nil {
		t.Fatal("expected the existing gateway error, got nil")
	}
	if !strings.Contains(err.Error(), "could not fetch skills") {
		t.Errorf("gateway error text changed: %v", err)
	}
	if strings.Contains(out, "External (read-only)") {
		t.Errorf("External section must not render on the gateway error path:\n%s", out)
	}
}

// ─── /skill get ext:<name> ───────────────────────────────────────────────────

func TestSkillGetExtPrefixRendersWithReadOnlyHeader(t *testing.T) {
	root, path := writeExternalFixture(t, "ext-alpha",
		"---\nname: ext-alpha\ndescription: External alpha does alpha things.\n---\n# Alpha Heading\nalpha body text\n")
	// Gateway never touched on the ext: path — nil client proves it.
	r := newExternalSkillRegistry("", []string{root})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill get ext:ext-alpha")
	})
	if err != nil {
		t.Fatalf("skill get ext:ext-alpha: %v", err)
	}
	if !strings.Contains(out, "external skill (read-only): "+path) {
		t.Errorf("read-only header with path missing:\n%s", out)
	}
	if !strings.Contains(out, "alpha body text") {
		t.Errorf("rendered SKILL.md body missing:\n%s", out)
	}
}

func TestSkillGetExtPrefixCaseInsensitive(t *testing.T) {
	root, _ := writeExternalFixture(t, "ext-alpha", "---\nname: ext-alpha\n---\nbody\n")
	r := newExternalSkillRegistry("", []string{root})

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill get ext:EXT-ALPHA")
	})
	if err != nil {
		t.Fatalf("case-insensitive ext: lookup failed: %v", err)
	}
}

func TestSkillGetExtPrefixMiss(t *testing.T) {
	r := newExternalSkillRegistry("", []string{t.TempDir()})

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill get ext:nope")
	})
	if err == nil {
		t.Fatal("expected error for unknown external skill, got nil")
	}
	if !strings.Contains(err.Error(), `external skill "nope" not found`) {
		t.Errorf("unexpected ext: miss error: %v", err)
	}
}

// ─── /skill get <name> — gateway first, external fallback ────────────────────

func TestSkillGetPlainNameFallsBackToExternalOnGatewayMiss(t *testing.T) {
	srv := missGateway(t) // CASResolveTag misses
	root, path := writeExternalFixture(t, "ext-alpha",
		"---\nname: ext-alpha\n---\nfallback body\n")
	r := newExternalSkillRegistry(srv.URL, []string{root})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill get ext-alpha")
	})
	if err != nil {
		t.Fatalf("expected external fallback to succeed, got: %v", err)
	}
	if !strings.Contains(out, "external skill (read-only): "+path) {
		t.Errorf("read-only header missing on fallback render:\n%s", out)
	}
	if !strings.Contains(out, "fallback body") {
		t.Errorf("fallback body missing:\n%s", out)
	}
}

func TestSkillGetMissesBothPrintsExistingMiss(t *testing.T) {
	srv := missGateway(t)
	root, _ := writeExternalFixture(t, "ext-alpha", "---\nname: ext-alpha\n---\nbody\n")
	r := newExternalSkillRegistry(srv.URL, []string{root})

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "skill get definitely-not-anywhere")
	})
	if err == nil {
		t.Fatal("expected the existing miss error, got nil")
	}
	if !strings.Contains(err.Error(), `could not resolve tag "definitely-not-anywhere"`) {
		t.Errorf("existing miss behavior changed: %v", err)
	}
	if strings.Contains(out, "external skill (read-only)") {
		t.Errorf("no external render expected on a double miss:\n%s", out)
	}
}
