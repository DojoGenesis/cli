package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestSink builds a Sink whose base URL points at the given test server.
func newTestSink(t *testing.T, serverURL string) *Sink {
	t.Helper()
	t.Setenv("DOJO_TELEMETRY_URL", serverURL)
	return New("test-session-abc")
}

// ---- New / constructor -------------------------------------------------------

func TestNew_DefaultsAndFields(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "")
	s := New("session-1")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.sessionID != "session-1" {
		t.Errorf("sessionID: want %q, got %q", "session-1", s.sessionID)
	}
	if s.baseURL != "https://dojo-telemetry.trespiesdesign.workers.dev" {
		t.Errorf("unexpected baseURL: %s", s.baseURL)
	}
	if s.buffer == nil {
		t.Error("buffer should not be nil")
	}
	if s.client == nil {
		t.Error("http.Client should not be nil")
	}
}

func TestNew_EnvOverridesBaseURL(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://localhost:9999/")
	s := New("s2")
	// Trailing slash should be stripped.
	if strings.HasSuffix(s.baseURL, "/") {
		t.Errorf("baseURL should not have trailing slash: %s", s.baseURL)
	}
	if !strings.Contains(s.baseURL, "localhost:9999") {
		t.Errorf("baseURL should contain env value, got: %s", s.baseURL)
	}
}

// ---- Ingest -----------------------------------------------------------------

func TestIngest_AppendsEvent(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	s := New("s3")

	s.Ingest("test.event", 1000, map[string]any{"key": "value"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buffer) != 1 {
		t.Fatalf("expected 1 buffered event, got %d", len(s.buffer))
	}
	ev := s.buffer[0]
	if ev.Type != "test.event" {
		t.Errorf("event Type: want %q, got %q", "test.event", ev.Type)
	}
	if ev.Ts != 1000 {
		t.Errorf("event Ts: want 1000, got %d", ev.Ts)
	}
	if ev.Data["key"] != "value" {
		t.Errorf("event Data[key]: want %q, got %v", "value", ev.Data["key"])
	}
}

func TestIngest_MultipleEvents(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	s := New("s4")

	s.Ingest("ev.a", 1, nil)
	s.Ingest("ev.b", 2, nil)
	s.Ingest("ev.c", 3, nil)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buffer) != 3 {
		t.Fatalf("expected 3 buffered events, got %d", len(s.buffer))
	}
}

func TestIngest_ConcurrentlySafe(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	s := New("s-concurrent")

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			s.Ingest("concurrent.event", int64(i), nil)
		}(i)
	}
	wg.Wait()

	s.mu.Lock()
	count := len(s.buffer)
	s.mu.Unlock()

	if count != n {
		t.Errorf("expected %d events after concurrent ingests, got %d", n, count)
	}
}

// ---- Flush ------------------------------------------------------------------

func TestFlush_EmptyBuffer_ReturnsNil(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	s := New("s-empty")

	if err := s.Flush(); err != nil {
		t.Errorf("Flush on empty buffer should return nil, got %v", err)
	}
}

func TestFlush_PostsCorrectPayload(t *testing.T) {
	var (
		receivedBody []byte
		mu           sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/telemetry/ingest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	s.Ingest("flush.test", 42, map[string]any{"foo": "bar"})

	if err := s.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedBody) == 0 {
		t.Fatal("server received no body")
	}

	var payload ingestPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("could not unmarshal payload: %v", err)
	}
	if payload.SessionID != "test-session-abc" {
		t.Errorf("SessionID: want %q, got %q", "test-session-abc", payload.SessionID)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("expected 1 event in payload, got %d", len(payload.Events))
	}
	ev := payload.Events[0]
	if ev.Type != "flush.test" {
		t.Errorf("event Type: want %q, got %q", "flush.test", ev.Type)
	}
	if ev.Ts != 42 {
		t.Errorf("event Ts: want 42, got %d", ev.Ts)
	}
}

func TestFlush_DrainsClearBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	s.Ingest("ev", 1, nil)
	s.Ingest("ev", 2, nil)

	if err := s.Flush(); err != nil {
		t.Fatalf("first Flush error: %v", err)
	}

	// Buffer should be empty after flush.
	s.mu.Lock()
	remaining := len(s.buffer)
	s.mu.Unlock()
	if remaining != 0 {
		t.Errorf("buffer should be empty after Flush, got %d events", remaining)
	}

	// Second flush of empty buffer must also succeed.
	if err := s.Flush(); err != nil {
		t.Errorf("second Flush (empty buffer) returned error: %v", err)
	}
}

func TestFlush_MultipleEventsInOneBatch(t *testing.T) {
	var receivedCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload ingestPayload
		_ = json.Unmarshal(body, &payload)
		receivedCount = len(payload.Events)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	for i := 0; i < 5; i++ {
		s.Ingest("batch.event", int64(i), nil)
	}

	if err := s.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	if receivedCount != 5 {
		t.Errorf("expected 5 events in batch, server got %d", receivedCount)
	}
}

// ---- Flush error handling ---------------------------------------------------

func TestFlush_HTTPError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	s.Ingest("err.event", 1, nil)

	err := s.Flush()
	if err == nil {
		t.Fatal("expected an error from non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code 500, got: %v", err)
	}
}

func TestFlush_NetworkError_ReturnsError(t *testing.T) {
	// Port 1 is always closed on any OS — connection refused guaranteed.
	t.Setenv("DOJO_TELEMETRY_URL", "http://127.0.0.1:1")
	s := New("s-net-err")
	s.Ingest("fail.event", 999, nil)

	err := s.Flush()
	if err == nil {
		t.Fatal("expected a network error, got nil")
	}
}

// ---- Close ------------------------------------------------------------------

func TestClose_FlushesFinalBatch(t *testing.T) {
	var (
		mu       sync.Mutex
		received int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload ingestPayload
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		received += len(payload.Events)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	s.Ingest("close.event", 1, nil)
	s.Close()

	mu.Lock()
	got := received
	mu.Unlock()
	if got != 1 {
		t.Errorf("Close should flush remaining events; expected 1, got %d", got)
	}
}

func TestClose_IdempotentSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	// Calling Close twice must not panic.
	s.Close()
	s.Close()
}

func TestClose_EmptyBuffer_DoesNotPost(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	// Close with nothing buffered — no HTTP call should be made.
	s.Close()

	if calls != 0 {
		t.Errorf("Close on empty buffer should not POST; got %d calls", calls)
	}
}

// ---- Start / background goroutine -------------------------------------------

func TestStart_ContextCancel_Exits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())

	s.Start(ctx)
	// Cancel immediately — goroutine should exit without blocking.
	cancel()

	// Give the goroutine a moment to see the cancellation, then Close.
	// Close itself is synchronous and should not deadlock.
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()

	select {
	case <-done:
		// passed
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked after context cancel — possible goroutine leak")
	}
}

// ---- DOJO_TELEMETRY_DISABLED kill switch -------------------------------------

func TestNew_DisabledEnv_SetsDisabledField(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	t.Setenv("DOJO_TELEMETRY_DISABLED", "1")
	s := New("s-disabled")
	if !s.disabled {
		t.Error("New() with DOJO_TELEMETRY_DISABLED set should produce a disabled Sink")
	}
}

func TestNew_DisabledEnvUnset_NotDisabled(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	t.Setenv("DOJO_TELEMETRY_DISABLED", "")
	s := New("s-enabled")
	if s.disabled {
		t.Error("New() without DOJO_TELEMETRY_DISABLED should not produce a disabled Sink")
	}
}

func TestIngest_Disabled_DoesNotBuffer(t *testing.T) {
	t.Setenv("DOJO_TELEMETRY_URL", "http://unused")
	t.Setenv("DOJO_TELEMETRY_DISABLED", "1")
	s := New("s-disabled-ingest")

	s.Ingest("should.not.buffer", 1, nil)

	s.mu.Lock()
	n := len(s.buffer)
	s.mu.Unlock()
	if n != 0 {
		t.Errorf("Ingest() on a disabled Sink should not buffer events, got %d buffered", n)
	}
}

func TestFlush_Disabled_NeverPosts(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("DOJO_TELEMETRY_URL", srv.URL)
	t.Setenv("DOJO_TELEMETRY_DISABLED", "1")
	s := New("s-disabled-flush")

	// Populate the buffer directly (bypassing Ingest's own no-op) so this
	// test isolates Flush()'s no-op as the backstop, not Ingest()'s.
	s.mu.Lock()
	s.buffer = append(s.buffer, TelemetryEvent{Type: "manual", Ts: 1})
	s.mu.Unlock()

	if err := s.Flush(); err != nil {
		t.Errorf("Flush() on a disabled Sink should return nil, got %v", err)
	}
	if calls != 0 {
		t.Errorf("Flush() on a disabled Sink should never POST; server got %d calls", calls)
	}
}

func TestStart_Disabled_NoGoroutineNoPost(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("DOJO_TELEMETRY_URL", srv.URL)
	t.Setenv("DOJO_TELEMETRY_DISABLED", "1")
	s := New("s-disabled-start")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	s.Ingest("ignored", 1, nil)

	// Give a disabled ticker (if one were mistakenly started anyway) a
	// window to fire before asserting nothing happened.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 0 {
		t.Errorf("Start() on a disabled Sink should never POST; server got %d calls", got)
	}
}

func TestStart_ManualFlushAfterStart(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTestSink(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	s.Ingest("started.event", 1, nil)
	if err := s.Flush(); err != nil {
		t.Fatalf("manual flush after Start returned error: %v", err)
	}

	mu.Lock()
	got := calls
	mu.Unlock()
	if got < 1 {
		t.Errorf("expected at least 1 server call, got %d", got)
	}

	s.Close()
}
