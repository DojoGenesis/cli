// Package protocol carries the workspace's "genius protocol" — a compact,
// behavior-changing operating doctrine — onto every dojo chat/agent turn by
// default, overridably, without prompt bloat.
//
// The L0 doc (protocol.md) is embedded at build time and served by DefaultDoc.
// An operator can override it per project (./DOJO.md), per machine
// (~/.dojo/DOJO.md), or by an explicit path, and can disable it entirely via
// config or DOJO_PROTOCOL_DISABLED. Injection is done once per session: the doc
// leads the first turn's message (for immediate effect against a gateway that
// has no system_prompt field) and is also set on ChatRequest.SystemPrompt for
// forward-compat once the gateway grows the field.
package protocol

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
)

// defaultDoc is the embedded L0 genius-protocol doc. Authored in protocol.md so
// it can be edited as prose and refined without touching Go; go:embed folds it
// into the binary at build time so the CLI never depends on the file at runtime.
//
//go:embed protocol.md
var defaultDoc string

// overlayFilename is the basename of a protocol override file, looked up in the
// project cwd first, then the ~/.dojo dir.
const overlayFilename = "DOJO.md"

// injectDelimiter separates the prepended protocol context from the user's
// message on the first turn. It mirrors the inline-prepend style used by
// /craft (cmd_craft.go) so the gateway sees a consistent shape.
const injectDelimiter = "\n\n---\n\n"

// DefaultDoc returns the embedded L0 genius-protocol doc.
func DefaultDoc() string {
	return defaultDoc
}

// LoadOverlay resolves the active protocol doc by precedence:
//
//	project ./DOJO.md  >  dojoDir/DOJO.md  >  DefaultDoc()
//
// It returns the doc text and a human-readable source label (the file path, or
// "(embedded default)"). An empty or whitespace-only override file is treated
// as absent so a stray empty DOJO.md can never blank out the protocol. A cwd or
// dojoDir of "" is skipped rather than joined, so callers may pass "" to opt a
// tier out of the search.
func LoadOverlay(cwd, dojoDir string) (doc string, source string) {
	for _, dir := range []string{cwd, dojoDir} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, overlayFilename)
		if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return string(b), path
		}
	}
	return DefaultDoc(), "(embedded default)"
}

// BuildSystemContext returns the protocol context that should lead a session,
// or "" when the protocol is disabled (cfg nil or Protocol.Enabled false) — an
// empty return is the signal to inject nothing.
//
// Precedence when enabled: an explicit cfg.Protocol.Path (readable, non-empty)
// wins outright; otherwise LoadOverlay resolves project ./DOJO.md, then
// ~/.dojo/DOJO.md, then the embedded default. An unreadable explicit path falls
// through to overlay resolution rather than erroring — the protocol degrades to
// the default instead of bricking a turn.
func BuildSystemContext(cfg *config.Config) string {
	if cfg == nil || !cfg.Protocol.Enabled {
		return ""
	}
	if p := cfg.Protocol.Path; p != "" {
		if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return string(b)
		}
	}
	cwd, _ := os.Getwd()
	doc, _ := LoadOverlay(cwd, config.DojoDir())
	return doc
}

// WriteDefaultOverlay writes dojoDir/DOJO.md from DefaultDoc() only if the file
// does not already exist — it NEVER clobbers an operator's edited overlay. A
// nil error with no write happens when the file is already present. Called by
// /init so a fresh workspace gets a visible, editable copy of the protocol.
func WriteDefaultOverlay(dojoDir string) error {
	if dojoDir == "" {
		return fmt.Errorf("protocol: WriteDefaultOverlay: empty dojoDir")
	}
	path := filepath.Join(dojoDir, overlayFilename)
	if _, err := os.Stat(path); err == nil {
		return nil // already exists — never clobber
	} else if !os.IsNotExist(err) {
		return err // a real stat error (permissions, etc.) — surface it
	}
	if err := os.MkdirAll(dojoDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultDoc()), 0o644)
}

// Injector stamps the protocol onto outgoing ChatRequests exactly once per
// session. Construct one per session with NewInjector; call Apply on every turn
// — it is a no-op after the first stamp and when the protocol is disabled.
type Injector struct {
	context  string
	injected bool
}

// NewInjector builds a session Injector from config. When the protocol is
// disabled the resulting Injector's context is empty and Apply is inert.
func NewInjector(cfg *config.Config) *Injector {
	return &Injector{context: BuildSystemContext(cfg)}
}

// Enabled reports whether Apply would inject anything (protocol on and context
// resolved). False once the single stamp has been spent.
func (in *Injector) Enabled() bool {
	return in != nil && in.context != "" && !in.injected
}

// Apply stamps the protocol onto req exactly once. On the first call with a
// non-empty context it prepends the doc to req.Message (immediate effect — the
// gateway /v1/chat endpoint has no system_prompt field yet) AND sets
// req.SystemPrompt (forward-compat, harmless if the gateway ignores it). It
// returns true only on the call that actually stamped.
//
// Later turns are deliberately left untouched: the gateway threads session
// context by SessionID, so the protocol only needs to lead the first turn —
// re-prepending every turn would be pure prompt bloat.
func (in *Injector) Apply(req *client.ChatRequest) bool {
	if in == nil || in.context == "" || in.injected || req == nil {
		return false
	}
	req.SystemPrompt = in.context
	req.Message = in.context + injectDelimiter + req.Message
	in.injected = true
	return true
}
