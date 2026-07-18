// Package output is the headless/JSON output seam for dojo slash commands.
//
// Every command handler renders human text straight to stdout and returns only
// an error (see internal/commands.Command). That is fine for the interactive
// REPL, but a non-interactive caller — a script, an agent, CI — needs a
// uniform, machine-parseable result instead of colored human text it has to
// scrape.
//
// The seam works without touching all ~35 command signatures. An *Emitter is
// stashed on the Registry (and in a package-level pointer the free formatting
// helpers consult) for the duration of one headless dispatch. In Human mode the
// Emitter is inert and commands print exactly as before. In JSON mode:
//
//   - Field(k, v) / Data(v) accumulate the envelope's `data` payload,
//   - Emit(v) writes one NDJSON line to the *real* stdout immediately (for
//     streaming commands), and
//   - the caller (Registry.RunHeadless) redirects os.Stdout while the handler
//     runs, so any stray direct fmt.Printf a not-yet-converted command emits is
//     captured as a `data.text` fallback rather than corrupting the JSON stream.
//
// The result of a non-streaming command is one Envelope line:
//
//	{"ok":true,"command":"health","data":{...},"error":null}
//	{"ok":false,"command":"skill get","data":null,"error":{"code":"not_found","message":"…"}}
package output

import (
	"encoding/json"
	"io"
)

// Mode selects how a dispatch produces output.
type Mode int

const (
	// Human renders human text to stdout — the REPL and plain one-shot path.
	// In this mode the Emitter is inert (commands print directly as before).
	Human Mode = iota
	// JSON accumulates a structured payload and emits an Envelope (+ optional
	// NDJSON streaming lines) — the headless/agent path.
	JSON
)

// Envelope is the stable per-command result contract emitted in JSON mode. It
// is intentionally small and uniform so an agent writes one parser for the
// whole surface. Data is nil on error; Error is nil on success.
type Envelope struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Data    any    `json:"data"`
	Error   *Err   `json:"error"`
}

// Err is the structured error carried by a failed Envelope. Code is a stable,
// machine-branchable slug; Message is the human-readable detail.
type Err struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Emitter is the sink a command writes through during a headless dispatch.
// The zero value is not used; construct with New. All methods are nil-safe so
// the free formatting helpers can call them on a nil package pointer in the
// REPL (Human) path without a guard at every site.
type Emitter struct {
	mode Mode
	real io.Writer      // the real stdout, captured before RunHeadless redirects
	enc  *json.Encoder  // NDJSON encoder over real (one JSON value + newline per line)
	data map[string]any // built incrementally via Field
	raw  any            // set wholesale via Data; takes precedence over data
}

// New builds an Emitter for mode, writing NDJSON streaming lines to real (the
// real stdout, captured before any os.Stdout redirect).
func New(mode Mode, real io.Writer) *Emitter {
	return &Emitter{
		mode: mode,
		real: real,
		enc:  json.NewEncoder(real),
		data: make(map[string]any),
	}
}

// JSON reports whether this Emitter is accumulating a JSON payload. Nil-safe:
// a nil Emitter (the REPL/Human path) reports false, so callers can branch with
// `if em.JSON() { … structured … } else { … human … }` without a nil check.
func (e *Emitter) JSON() bool { return e != nil && e.mode == JSON }

// Field records one key/value into the envelope's `data` object. No-op outside
// JSON mode. Later calls with the same key overwrite. Ignored once Data has set
// a wholesale payload.
func (e *Emitter) Field(key string, val any) {
	if !e.JSON() {
		return
	}
	e.data[key] = val
}

// Data sets the entire `data` payload at once (e.g. a typed struct a command
// already fetched). No-op outside JSON mode. Takes precedence over any Field
// entries. The last Data call wins.
func (e *Emitter) Data(v any) {
	if !e.JSON() {
		return
	}
	e.raw = v
}

// Emit writes one NDJSON streaming line to the real stdout immediately — the
// per-event line a streaming command produces before its terminal Envelope.
// No-op outside JSON mode. json.Encoder appends the trailing newline, so each
// call is exactly one NDJSON record.
func (e *Emitter) Emit(v any) {
	if !e.JSON() {
		return
	}
	_ = e.enc.Encode(v)
}

// Payload returns the value for the envelope's `data` field: the wholesale Data
// value if one was set, else the accumulated Field map (when non-empty), else a
// {"text": fallback} object built from stdout captured during the run, else
// nil. This is how a not-yet-converted command still yields usable output
// instead of an empty envelope.
func (e *Emitter) Payload(fallbackText string) any {
	if e == nil {
		return nil
	}
	if e.raw != nil {
		return e.raw
	}
	if len(e.data) > 0 {
		return e.data
	}
	if fallbackText != "" {
		return map[string]any{"text": fallbackText}
	}
	return nil
}
