// Package telemetry provides a batched, async telemetry sink that pushes SSE
// events from pilot mode to the D1 telemetry store via the ingest API.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// TelemetryEvent matches the ingest API schema.
type TelemetryEvent struct {
	Type string         `json:"type"`
	Ts   int64          `json:"ts"`
	Data map[string]any `json:"data,omitempty"`
}

// ingestPayload is the POST body sent to the telemetry worker.
type ingestPayload struct {
	SessionID string           `json:"session_id"`
	Events    []TelemetryEvent `json:"events"`
}

// Sink batches telemetry events and POSTs them to the telemetry worker
// on a periodic schedule. All methods are safe for concurrent use.
type Sink struct {
	baseURL   string
	sessionID string
	buffer    []TelemetryEvent
	mu        sync.Mutex
	client    *http.Client
	done      chan struct{}
}

// New creates a Sink that will POST events for the given session ID.
// The telemetry base URL is read from DOJO_TELEMETRY_URL or defaults to
// the production worker endpoint.
func New(sessionID string) *Sink {
	base := os.Getenv("DOJO_TELEMETRY_URL")
	if base == "" {
		base = "https://dojo-telemetry.trespiesdesign.workers.dev"
	}
	base = strings.TrimRight(base, "/")

	return &Sink{
		baseURL:   base,
		sessionID: sessionID,
		buffer:    make([]TelemetryEvent, 0, 64),
		client:    &http.Client{Timeout: 10 * time.Second},
		done:      make(chan struct{}),
	}
}

// Ingest appends a telemetry event to the buffer. It never blocks the caller
// beyond the mutex acquisition.
func (s *Sink) Ingest(eventType string, ts int64, data map[string]any) {
	s.mu.Lock()
	s.buffer = append(s.buffer, TelemetryEvent{
		Type: eventType,
		Ts:   ts,
		Data: data,
	})
	s.mu.Unlock()
}

// Start launches a background goroutine that flushes the event buffer every
// 5 seconds. It stops when ctx is cancelled or Close is called.
func (s *Sink) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.Flush(); err != nil {
					log.Printf("[telemetry] flush warning: %v", err)
				}
			case <-ctx.Done():
				return
			case <-s.done:
				return
			}
		}
	}()
}

// Flush drains the buffer and POSTs all buffered events to the ingest
// endpoint. On HTTP or network errors it logs a warning but never panics.
func (s *Sink) Flush() error {
	// Swap buffer under lock so Ingest() isn't blocked during the POST.
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.buffer
	s.buffer = make([]TelemetryEvent, 0, 64)
	s.mu.Unlock()

	payload := ingestPayload{
		SessionID: s.sessionID,
		Events:    batch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := s.baseURL + "/api/telemetry/ingest"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // idiomatic best-effort close; status code is validated separately below

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned %d", url, resp.StatusCode)
	}
	return nil
}

// Close performs a final flush of any remaining events and signals the
// background goroutine to stop. It is safe to call multiple times.
func (s *Sink) Close() {
	// Signal the background goroutine to stop.
	select {
	case <-s.done:
		// Already closed.
		return
	default:
		close(s.done)
	}

	// Best-effort final flush.
	if err := s.Flush(); err != nil {
		log.Printf("[telemetry] final flush warning: %v", err)
	}
}
