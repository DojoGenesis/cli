package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Human mode: the Emitter is inert. JSON() is false and the structured methods
// no-op, so a command's normal stdout rendering is untouched.
func TestHumanModeIsInert(t *testing.T) {
	var buf bytes.Buffer
	e := New(Human, &buf)
	if e.JSON() {
		t.Fatal("Human emitter reports JSON() true")
	}
	e.Field("k", "v")
	e.Data(map[string]any{"x": 1})
	e.Emit(map[string]any{"stream": "x"})
	if buf.Len() != 0 {
		t.Fatalf("Human mode wrote to stream: %q", buf.String())
	}
	if got := e.Payload(""); got != nil {
		t.Fatalf("Human Payload = %v, want nil", got)
	}
}

// A nil Emitter (the REPL path, where curEmitter is nil) must be safe to call —
// that is what lets the free helpers branch without a guard at every site.
func TestNilEmitterIsSafe(t *testing.T) {
	var e *Emitter
	if e.JSON() {
		t.Fatal("nil emitter reports JSON() true")
	}
	e.Field("k", "v") // must not panic
	e.Data(1)         // must not panic
	e.Emit(1)         // must not panic
	if got := e.Payload("x"); got != nil {
		t.Fatalf("nil Payload = %v, want nil", got)
	}
}

// Field accumulates the data object in JSON mode.
func TestFieldBuildsData(t *testing.T) {
	e := New(JSON, &bytes.Buffer{})
	e.Field("status", "healthy")
	e.Field("version", "3.2.2")
	got, ok := e.Payload("").(map[string]any)
	if !ok {
		t.Fatalf("Payload type = %T, want map", e.Payload(""))
	}
	if got["status"] != "healthy" || got["version"] != "3.2.2" {
		t.Fatalf("Payload = %v", got)
	}
}

// Data sets a wholesale payload that takes precedence over Field entries and
// over the captured-text fallback.
func TestDataPrecedence(t *testing.T) {
	e := New(JSON, &bytes.Buffer{})
	e.Field("ignored", true)
	type row struct {
		Name string `json:"name"`
	}
	e.Data([]row{{Name: "a"}, {Name: "b"}})
	got, ok := e.Payload("captured text").([]row)
	if !ok {
		t.Fatalf("Payload type = %T, want []row", e.Payload(""))
	}
	if len(got) != 2 || got[0].Name != "a" {
		t.Fatalf("Payload = %v", got)
	}
}

// With neither Data nor Field set, captured stdout becomes a {"text": …}
// fallback so a not-yet-converted command still yields usable output.
func TestTextFallback(t *testing.T) {
	e := New(JSON, &bytes.Buffer{})
	got, ok := e.Payload("some human text").(map[string]any)
	if !ok || got["text"] != "some human text" {
		t.Fatalf("Payload = %v, want text fallback", e.Payload("some human text"))
	}
	// Empty capture with no structured data → nil (a clean empty envelope).
	if e.Payload("") != nil {
		t.Fatal("empty Payload should be nil")
	}
}

// Emit writes exactly one NDJSON record (one JSON value + newline) per call to
// the real writer, bypassing any stdout redirect.
func TestEmitWritesNDJSON(t *testing.T) {
	var buf bytes.Buffer
	e := New(JSON, &buf)
	e.Emit(map[string]any{"stream": "run", "node": "plan", "status": "running"})
	e.Emit(map[string]any{"stream": "run", "node": "plan", "status": "ok"})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d NDJSON lines, want 2: %q", len(lines), buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &m); err != nil {
		t.Fatalf("line 2 not valid JSON: %v", err)
	}
	if m["status"] != "ok" {
		t.Fatalf("line 2 = %v", m)
	}
}

// The Envelope marshals to the stable agent-facing shape.
func TestEnvelopeShape(t *testing.T) {
	ok := Envelope{OK: true, Command: "health", Data: map[string]any{"status": "healthy"}}
	b, _ := json.Marshal(ok)
	if !strings.Contains(string(b), `"ok":true`) || !strings.Contains(string(b), `"error":null`) {
		t.Fatalf("ok envelope = %s", b)
	}
	fail := Envelope{OK: false, Command: "skill", Error: &Err{Code: "not_found", Message: "no"}}
	b, _ = json.Marshal(fail)
	if !strings.Contains(string(b), `"ok":false`) || !strings.Contains(string(b), `"code":"not_found"`) {
		t.Fatalf("fail envelope = %s", b)
	}
	if !strings.Contains(string(b), `"data":null`) {
		t.Fatalf("fail envelope should have data:null: %s", b)
	}
}
