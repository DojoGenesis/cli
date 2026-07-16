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
	"strings"

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

// ─── JIT tell-triggered injection ─────────────────────────────────────────────
//
// The protocol above leads a session once, up front. This section is the
// complementary move: surface ONE situated gate at the exact moment a tell
// appears, rather than reciting the whole doctrine. The only tell the CLI can
// observe today is a failed chat turn's error text, and the one gate worth
// interrupting for at that moment is the config-vs-code discriminator.

// configVsCodeGate is the one-line config-vs-code discriminator — the single
// most relevant operating gate when a turn fails at a boundary. It mirrors the
// workspace CLAUDE.md "Config or code?" debugging gate: a total,
// input-independent failure points at wiring (config/env/state), so settings
// and env are the first place to look, not the code path. Surfaced verbatim by
// TellFor.
const configVsCodeGate = "Config or code? Total+input-independent failure → wiring: check settings/env FIRST."

// debuggingGate is the one-line "debug by disproof" gate — the most relevant
// move the moment an error signals a build/test/logic failure rather than a
// wiring boundary. It mirrors the workspace CLAUDE.md debugging protocol: state
// the causal chain and name the cheapest experiment that toggles the bug before
// touching a fix. Surfaced verbatim by TellFor for the build/test tell class.
const debuggingGate = "Debug by disproof: state the causal chain and name the cheapest experiment that toggles the bug on and off before any fix."

// Stable gate keys. The Nudger de-dupes on these — not on the display line — so
// a gate fires at most once per session regardless of which error string (or
// which surface: TellFor vs the REPL's repeated-failure detector) triggered it,
// and the two classes stay independently fire-able because their keys differ.
const (
	gateKeyConfigVsCode = "config-vs-code"
	gateKeyDebugging    = "debugging"
)

// wiringTells are lowercased substrings that mark an error as a boundary/wiring
// failure — connection, DNS, auth, and dead-route signatures. Any match routes
// to configVsCodeGate. Kept deliberately small and literal (no regex, no
// scoring): this is a situated nudge, not a diagnosis engine. The compound
// "model … not found" case is handled separately in TellFor so a bare
// "not found" from some unrelated error can't trip the gate.
var wiringTells = []string{
	"connection refused",     // TCP connect rejected
	"no such host",           // DNS lookup failure
	"no route to host",       // network path down
	"network is unreachable", // network path down
	"dial tcp",               // generic transport dial failure
	"401",                    // unauthorized
	"403",                    // forbidden
	"unauthorized",
	"forbidden",
	"404", // dead route / missing endpoint
}

// debuggingTells are lowercased substrings that mark an error as a build, test,
// or compile/logic failure — the class where the right move is to form a
// disprovable theory, not to check config. Any match routes to debuggingGate.
// Kept small and literal like wiringTells. Several are the exact shapes the Go
// toolchain emits — "panic:", "undefined:", and the "--- fail" / "fail\t"
// markers `go test` prints — so ordinary prose that merely contains the word
// "fail" does not trip the gate. "cannot use"/"compile"/"assertion" carry a
// little more false-positive surface than the tokened forms, so they are the
// last resort here (checked after the wiring class in tellFor) and stay literal.
var debuggingTells = []string{
	"panic:",      // Go runtime panic header
	"--- fail",    // `go test` per-test failure marker (from "--- FAIL")
	"fail\t",      // `go test` summary line "FAIL\t<pkg>" (tab-delimited)
	"test failed", // generic test-runner phrasing
	"build failed",
	"undefined:", // Go compiler: undefined symbol
	"cannot use", // Go compiler: type mismatch ("cannot use x as y")
	"compile",    // "compile error" / "does not compile" / "compilation failed"
	"assertion",  // failed assertion (test frameworks, runtime checks)
}

// TellFor maps a failed turn's error text to the single most relevant protocol
// gate line, or ("", false) when the text carries no recognized tell. Two tell
// classes are recognized: wiring/boundary signatures (connection, auth, dead
// route, unknown model) map to the config-vs-code discriminator, and build/
// test/compile signatures map to the debug-by-disproof gate. The match is
// case-insensitive and substring-based; unrelated text returns ok=false so the
// caller stays silent. Wiring is checked first, so a boundary failure keeps its
// config-vs-code framing even in the rare event its text also mentions a build.
func TellFor(errText string) (gate string, ok bool) {
	_, gate, ok = tellFor(errText)
	return gate, ok
}

// tellFor is the shared matcher behind TellFor and Nudger.NudgeFor. Beyond the
// display line it returns a STABLE gate key (gateKeyConfigVsCode /
// gateKeyDebugging) so the Nudger can de-dupe on gate identity rather than
// wording. Wiring/boundary tells win over build/test tells when both are
// present, preserving TellFor's original config-vs-code precedence.
func tellFor(errText string) (key, gate string, ok bool) {
	lower := strings.ToLower(errText)
	for _, tell := range wiringTells {
		if strings.Contains(lower, tell) {
			return gateKeyConfigVsCode, configVsCodeGate, true
		}
	}
	// A dead/unknown model endpoint reads like a code error ("model X not
	// found") but is a config choice — route it to the config-vs-code gate.
	// Required to co-occur with "model" so a generic "not found" doesn't
	// false-positive.
	if strings.Contains(lower, "model") && strings.Contains(lower, "not found") {
		return gateKeyConfigVsCode, configVsCodeGate, true
	}
	// Build/test/compile signatures — the debug-by-disproof class. Checked last
	// so the wiring class always wins a tie.
	for _, tell := range debuggingTells {
		if strings.Contains(lower, tell) {
			return gateKeyDebugging, debuggingGate, true
		}
	}
	return "", "", false
}

// Nudger tracks which protocol gates have already been surfaced this session so
// a JIT nudge fires at most once per distinct gate — a recurring error never
// nags. The zero value is ready to use; a caller (the REPL) holds one for the
// life of the session. Not safe for concurrent use: the REPL drives it from its
// single read loop.
type Nudger struct {
	shown map[string]bool
}

// NudgeFor returns the gate line to surface for errText and true when it should
// be shown now — i.e. errText matches a tell (tellFor) AND that gate has not
// been shown before. On a true return it records the gate (by its stable key)
// as shown, so any later error mapping to the same gate returns ok=false (the
// fire-once-per-gate guarantee). Errors with no tell always return ("", false)
// and record nothing.
func (n *Nudger) NudgeFor(errText string) (gate string, ok bool) {
	key, g, matched := tellFor(errText)
	if !matched || n.shown[key] {
		return "", false
	}
	n.markShown(key)
	return g, true
}

// NudgeDebugging surfaces the debug-by-disproof gate on demand, de-duped through
// the SAME per-session ledger as NudgeFor. It returns (debuggingGate, true) only
// the first time the debugging gate is requested this session — whether that
// first request came from build/test error text via NudgeFor or from a caller
// that has independently decided the gate applies (the REPL's repeated-failure
// detector) — and ("", false) every time thereafter. This is the entry point
// for "I already know the debugging gate is what's needed"; NudgeFor is the
// entry point for "decide from the error text".
func (n *Nudger) NudgeDebugging() (gate string, ok bool) {
	if n.shown[gateKeyDebugging] {
		return "", false
	}
	n.markShown(gateKeyDebugging)
	return debuggingGate, true
}

// markShown records a gate key as surfaced, lazily creating the ledger. Shared
// by NudgeFor and NudgeDebugging so the fire-once-per-gate bookkeeping lives in
// one place.
func (n *Nudger) markShown(key string) {
	if n.shown == nil {
		n.shown = make(map[string]bool)
	}
	n.shown[key] = true
}
