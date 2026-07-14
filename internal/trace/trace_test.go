package trace

// Tests for internal/trace/trace.go.
//
// All tests are hermetic: no network, no filesystem writes, no env mutation.
// The span-recording path inside RoundTrip is gated by traceEnabled, which is
// set once at package init() from DOJO_TRACE and cannot be flipped afterward
// within a normal `go test` run — so that branch is intentionally not
// exercised here. This file covers only the always-on header-propagation
// path and the pure/default-wiring surfaces.

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// randomHex — pure property test.
// ---------------------------------------------------------------------------

func TestRandomHex(t *testing.T) {
	cases := []int{4, 8}

	for _, n := range cases {
		n := n
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			s := randomHex(n)
			if len(s) != 2*n {
				t.Fatalf("randomHex(%d) len = %d; want %d", n, len(s), 2*n)
			}
			if _, err := hex.DecodeString(s); err != nil {
				t.Errorf("randomHex(%d) = %q is not valid hex: %v", n, s, err)
			}
		})
	}
}

func TestRandomHex_DifferentCallsDiffer(t *testing.T) {
	a := randomHex(8)
	b := randomHex(8)
	if a == b {
		t.Errorf("randomHex(8) returned the same value twice: %q — overwhelmingly unlikely, check the RNG source", a)
	}
}

func TestRandomHex_Zero(t *testing.T) {
	s := randomHex(0)
	if s != "" {
		t.Errorf("randomHex(0) = %q; want empty string", s)
	}
}

// ---------------------------------------------------------------------------
// NewRoundTripper — nil-default wiring.
// ---------------------------------------------------------------------------

func TestNewRoundTripper_NilInnerDefaultsToDefaultTransport(t *testing.T) {
	rt := NewRoundTripper(nil, nil)

	concrete, ok := rt.(*roundTripper)
	if !ok {
		t.Fatalf("NewRoundTripper returned %T; want *roundTripper", rt)
	}
	if concrete.inner != http.DefaultTransport {
		t.Errorf("inner = %v; want http.DefaultTransport", concrete.inner)
	}
}

func TestNewRoundTripper_NilSinkDefaultsToLogSink(t *testing.T) {
	rt := NewRoundTripper(nil, nil)

	concrete, ok := rt.(*roundTripper)
	if !ok {
		t.Fatalf("NewRoundTripper returned %T; want *roundTripper", rt)
	}
	if _, ok := concrete.sink.(LogSink); !ok {
		t.Errorf("sink = %T; want LogSink{}", concrete.sink)
	}
}

func TestNewRoundTripper_NonNilInnerAndSinkPreserved(t *testing.T) {
	inner := &fakeRoundTripper{}
	sink := &fakeSink{}

	rt := NewRoundTripper(inner, sink)

	concrete, ok := rt.(*roundTripper)
	if !ok {
		t.Fatalf("NewRoundTripper returned %T; want *roundTripper", rt)
	}
	if concrete.inner != http.RoundTripper(inner) {
		t.Errorf("inner not preserved: got %v", concrete.inner)
	}
	if concrete.sink != SpanSink(sink) {
		t.Errorf("sink not preserved: got %v", concrete.sink)
	}
}

// ---------------------------------------------------------------------------
// RoundTrip — always-on X-Trace-ID propagation (fires regardless of
// traceEnabled since it happens before the traceEnabled check).
// ---------------------------------------------------------------------------

// fakeRoundTripper captures the request it receives and returns a canned
// response, so tests can inspect what the "inner" transport actually saw.
type fakeRoundTripper struct {
	gotReq *http.Request
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

// fakeSink is a no-op SpanSink used only to satisfy NewRoundTripper's nil
// checks in tests that don't care about span content (traceEnabled is off).
type fakeSink struct {
	spans []Span
}

func (f *fakeSink) Emit(s Span) {
	f.spans = append(f.spans, s)
}

func TestRoundTrip_GeneratesTraceIDWhenAbsent(t *testing.T) {
	inner := &fakeRoundTripper{}
	rt := NewRoundTripper(inner, &fakeSink{})

	origReq, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	resp, err := rt.RoundTrip(origReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("resp.StatusCode = %d; want 200", resp.StatusCode)
	}

	// The inner transport must have seen a generated 16-char hex trace ID
	// (randomHex(8) => 2*8 = 16 chars).
	if inner.gotReq == nil {
		t.Fatal("inner RoundTripper was never invoked")
	}
	gotID := inner.gotReq.Header.Get(headerTraceID)
	if len(gotID) != 16 {
		t.Errorf("inner saw X-Trace-ID = %q (len %d); want 16-char hex", gotID, len(gotID))
	}
	if _, err := hex.DecodeString(gotID); err != nil {
		t.Errorf("inner saw X-Trace-ID = %q; not valid hex: %v", gotID, err)
	}

	// The ORIGINAL request passed by the caller must NOT be mutated — the
	// implementation clones before setting the header.
	if origReq.Header.Get(headerTraceID) != "" {
		t.Errorf("original request was mutated: X-Trace-ID = %q; want empty", origReq.Header.Get(headerTraceID))
	}
}

func TestRoundTrip_PassesThroughExistingTraceID(t *testing.T) {
	inner := &fakeRoundTripper{}
	rt := NewRoundTripper(inner, &fakeSink{})

	const existingID = "deadbeefdeadbeef"
	origReq, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	origReq.Header.Set(headerTraceID, existingID)

	resp, err := rt.RoundTrip(origReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("resp.StatusCode = %d; want 200", resp.StatusCode)
	}

	if inner.gotReq == nil {
		t.Fatal("inner RoundTripper was never invoked")
	}
	if got := inner.gotReq.Header.Get(headerTraceID); got != existingID {
		t.Errorf("inner saw X-Trace-ID = %q; want unchanged %q (no new ID generated)", got, existingID)
	}
}

// ---------------------------------------------------------------------------
// LogSink.Emit — no-panic + independent marshal check.
// ---------------------------------------------------------------------------

func TestLogSink_EmitDoesNotPanic(t *testing.T) {
	span := Span{
		TraceID:    "abcdef0123456789",
		SpanID:     "01234567",
		Method:     http.MethodPost,
		URL:        "http://example.com/path?query=1",
		StatusCode: 500,
		StartTime:  time.Now(),
		DurationMS: 42,
		Error:      "boom",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogSink{}.Emit panicked: %v", r)
		}
	}()

	LogSink{}.Emit(span)

	// Independently verify the same Span marshals cleanly via encoding/json,
	// confirming Emit's internal json.Marshal call has nothing to fail on.
	if _, err := json.Marshal(span); err != nil {
		t.Errorf("json.Marshal(span) failed: %v", err)
	}
}
