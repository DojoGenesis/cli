package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── parseSSE ────────────────────────────────────────────────────────────────

func TestParseSSE_BasicChunks(t *testing.T) {
	input := "data: hello\ndata: world\n"
	r := strings.NewReader(input)

	var got []SSEChunk
	err := parseSSE(r, func(c SSEChunk) { got = append(got, c) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Data != "hello" {
		t.Errorf("expected 'hello', got %q", got[0].Data)
	}
	if got[1].Data != "world" {
		t.Errorf("expected 'world', got %q", got[1].Data)
	}
}

func TestParseSSE_DoneTerminator(t *testing.T) {
	// [DONE] should stop processing without calling onChunk.
	input := "data: first\ndata: [DONE]\ndata: should_not_appear\n"
	r := strings.NewReader(input)

	var got []SSEChunk
	err := parseSSE(r, func(c SSEChunk) { got = append(got, c) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk (before [DONE]), got %d", len(got))
	}
	if got[0].Data != "first" {
		t.Errorf("expected 'first', got %q", got[0].Data)
	}
}

func TestParseSSE_EventPrefix(t *testing.T) {
	// event: lines should be captured in SSEChunk.Event and cleared after data.
	input := "event: ping\ndata: payload\n\nevent: pong\ndata: other\n"
	r := strings.NewReader(input)

	var got []SSEChunk
	err := parseSSE(r, func(c SSEChunk) { got = append(got, c) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Event != "ping" {
		t.Errorf("expected event 'ping', got %q", got[0].Event)
	}
	if got[0].Data != "payload" {
		t.Errorf("expected data 'payload', got %q", got[0].Data)
	}
	if got[1].Event != "pong" {
		t.Errorf("expected event 'pong', got %q", got[1].Event)
	}
}

func TestParseSSE_EmptyData(t *testing.T) {
	// Empty stream should produce no chunks and no error.
	r := strings.NewReader("")
	var got []SSEChunk
	err := parseSSE(r, func(c SSEChunk) { got = append(got, c) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(got))
	}
}

func TestParseSSE_DataWithLeadingSpace(t *testing.T) {
	// "data: " prefix includes a space; TrimSpace should handle " value" -> "value".
	input := "data:  trimmed \n"
	r := strings.NewReader(input)

	var got []SSEChunk
	if err := parseSSE(r, func(c SSEChunk) { got = append(got, c) }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Data != "trimmed" {
		t.Errorf("expected trimmed data 'trimmed', got %v", got)
	}
}

// ─── extractText — tested via SSEChunk directly (same logic as repl.extractText) ──

// extractTextLocal mirrors the logic in repl.extractText so we can test JSON
// unwrap behaviour without depending on the repl package (which imports readline).
func extractTextLocal(data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		// OpenAI delta format
		if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]any); ok {
				if delta, ok := choice["delta"].(map[string]any); ok {
					if content, ok := delta["content"].(string); ok {
						return content
					}
				}
				if text, ok := choice["text"].(string); ok {
					return text
				}
			}
		}
		for _, key := range []string{"text", "content", "message", "response"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}
	return data
}

func TestExtractText_PlainText(t *testing.T) {
	got := extractTextLocal("hello world")
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractText_OpenAIDelta(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"hello"}}]}`
	got := extractTextLocal(data)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestExtractText_SimpleText(t *testing.T) {
	data := `{"text":"hello"}`
	got := extractTextLocal(data)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestExtractText_SimpleContent(t *testing.T) {
	data := `{"content":"world"}`
	got := extractTextLocal(data)
	if got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}
}

func TestExtractText_Done(t *testing.T) {
	got := extractTextLocal("[DONE]")
	if got != "" {
		t.Errorf("expected empty string for [DONE], got %q", got)
	}
}

func TestExtractText_Empty(t *testing.T) {
	got := extractTextLocal("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ─── Health ──────────────────────────────────────────────────────────────────

func TestHealth_MockServer(t *testing.T) {
	want := HealthResponse{
		Status:            "ok",
		Version:           "1.2.3",
		Timestamp:         "2026-04-09T00:00:00Z",
		Providers:         map[string]string{"claude": "healthy"},
		Dependencies:      map[string]string{"db": "healthy"},
		UptimeSeconds:     42,
		RequestsProcessed: 100,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() returned error: %v", err)
	}
	if got.Status != want.Status {
		t.Errorf("Status: got %q, want %q", got.Status, want.Status)
	}
	if got.Version != want.Version {
		t.Errorf("Version: got %q, want %q", got.Version, want.Version)
	}
	if got.UptimeSeconds != want.UptimeSeconds {
		t.Errorf("UptimeSeconds: got %d, want %d", got.UptimeSeconds, want.UptimeSeconds)
	}
	if got.RequestsProcessed != want.RequestsProcessed {
		t.Errorf("RequestsProcessed: got %d, want %d", got.RequestsProcessed, want.RequestsProcessed)
	}
	if got.Providers["claude"] != "healthy" {
		t.Errorf("Providers[claude]: got %q, want %q", got.Providers["claude"], "healthy")
	}
}

func TestHealth_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

// ─── Seeds ───────────────────────────────────────────────────────────────────

func TestSeeds_MockServer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	wantSeed := Seed{
		ID:          "seed-1",
		Name:        "test seed",
		Description: "a test seed",
		Trigger:     "testing",
		Content:     "seed content",
		UsageCount:  5,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	envelope := seedsEnvelope{
		Success: true,
		Count:   1,
		Seeds:   []Seed{wantSeed},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/seeds" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	seeds, err := c.Seeds(context.Background())
	if err != nil {
		t.Fatalf("Seeds() returned error: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("expected 1 seed, got %d", len(seeds))
	}
	if seeds[0].ID != wantSeed.ID {
		t.Errorf("ID: got %q, want %q", seeds[0].ID, wantSeed.ID)
	}
	if seeds[0].Name != wantSeed.Name {
		t.Errorf("Name: got %q, want %q", seeds[0].Name, wantSeed.Name)
	}
	if seeds[0].UsageCount != wantSeed.UsageCount {
		t.Errorf("UsageCount: got %d, want %d", seeds[0].UsageCount, wantSeed.UsageCount)
	}
}

func TestSeeds_EmptyEnvelope(t *testing.T) {
	envelope := seedsEnvelope{Success: true, Count: 0, Seeds: nil}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	seeds, err := c.Seeds(context.Background())
	if err != nil {
		t.Fatalf("Seeds() returned error: %v", err)
	}
	if len(seeds) != 0 {
		t.Errorf("expected 0 seeds, got %d", len(seeds))
	}
}

func TestNew_BadTimeout_Defaults(t *testing.T) {
	// A bad timeout string should fall back to 60s without panicking.
	c := New("http://localhost:9999", "", "not-a-duration")
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.http.Timeout != 60*time.Second {
		t.Errorf("expected default 60s timeout, got %v", c.http.Timeout)
	}
}

// ─── Auth header injection ────────────────────────────────────────────────────

func TestAuthHeader_GetRequest(t *testing.T) {
	const wantToken = "test-bearer-token"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}))
	defer srv.Close()

	c := New(srv.URL, wantToken, "5s")
	_, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, want)
	}
}

func TestAuthHeader_PostRequest(t *testing.T) {
	const wantToken = "post-token"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": Memory{ID: "m1", Content: "c"}})
	}))
	defer srv.Close()

	c := New(srv.URL, wantToken, "5s")
	_, err := c.StoreMemory(context.Background(), StoreMemoryRequest{Content: "hello"})
	if err != nil {
		t.Fatalf("StoreMemory() error: %v", err)
	}
	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, want)
	}
}

func TestAuthHeader_NoTokenOmitted(t *testing.T) {
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}))
	defer srv.Close()

	// Empty token — header must be absent.
	c := New(srv.URL, "", "5s")
	_, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestAuthHeader_ChatStream(t *testing.T) {
	const wantToken = "stream-token"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// write a minimal SSE stream
		_, _ = w.Write([]byte("data: hello\ndata: [DONE]\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, wantToken, "5s")
	err := c.ChatStream(context.Background(), ChatRequest{Message: "hi"}, func(SSEChunk) {})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization header on ChatStream: got %q, want %q", gotAuth, want)
	}
}

// ─── Error response handling ─────────────────────────────────────────────────

func TestGet_404_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404, got: %v", err)
	}
}

func TestGet_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Providers(context.Background())
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention 500, got: %v", err)
	}
}

func TestPost_400_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.CreateSeed(context.Background(), CreateSeedRequest{Name: "x", Content: "y"})
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention 400, got: %v", err)
	}
}

func TestPost_422_ErrorBodyIncluded(t *testing.T) {
	const body = "validation failed: name required"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, body, http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.StoreMemory(context.Background(), StoreMemoryRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error for 422, got nil")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("expected 422 in error, got: %v", err)
	}
}

func TestChatStream_4xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.ChatStream(context.Background(), ChatRequest{Message: "hi"}, func(SSEChunk) {})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}
}

func TestAgentChatStream_5xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.AgentChatStream(context.Background(), "agent-xyz", AgentChatRequest{Message: "hello"}, func(SSEChunk) {})
	if err == nil {
		t.Fatal("expected error for 503, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error, got: %v", err)
	}
}

// ─── SSE stream parsing via ChatStream / AgentChatStream ─────────────────────

func TestChatStream_ParsesSSEChunks(t *testing.T) {
	// Server emits three data lines then [DONE].
	sseBody := "data: chunk1\ndata: chunk2\ndata: chunk3\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	err := c.ChatStream(context.Background(), ChatRequest{Message: "test"}, func(chunk SSEChunk) {
		got = append(got, chunk.Data)
	})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	want := []string{"chunk1", "chunk2", "chunk3"}
	if len(got) != len(want) {
		t.Fatalf("expected %d chunks, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("chunk[%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestChatStream_ParsesJSONChunks(t *testing.T) {
	// Server emits JSON-wrapped OpenAI delta chunks followed by [DONE].
	chunk1 := `{"choices":[{"delta":{"content":"Hello"}}]}`
	chunk2 := `{"choices":[{"delta":{"content":" world"}}]}`
	sseBody := "data: " + chunk1 + "\ndata: " + chunk2 + "\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var combined string
	err := c.ChatStream(context.Background(), ChatRequest{Message: "hi"}, func(chunk SSEChunk) {
		combined += extractTextLocal(chunk.Data)
	})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	if combined != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", combined)
	}
}

func TestChatStream_StopsAtDone(t *testing.T) {
	// Data after [DONE] must not be delivered to onChunk.
	sseBody := "data: before\ndata: [DONE]\ndata: after\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	if err := c.ChatStream(context.Background(), ChatRequest{Message: "x"}, func(chunk SSEChunk) {
		got = append(got, chunk.Data)
	}); err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	if len(got) != 1 || got[0] != "before" {
		t.Errorf("expected only [before], got %v", got)
	}
}

func TestAgentChatStream_ParsesSSEChunks(t *testing.T) {
	sseBody := "data: reply1\ndata: reply2\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify routing — path must contain the agent ID.
		if !strings.Contains(r.URL.Path, "agent-abc") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	err := c.AgentChatStream(context.Background(), "agent-abc", AgentChatRequest{Message: "hello"}, func(chunk SSEChunk) {
		got = append(got, chunk.Data)
	})
	if err != nil {
		t.Fatalf("AgentChatStream() error: %v", err)
	}
	want := []string{"reply1", "reply2"}
	if len(got) != len(want) {
		t.Fatalf("expected %d chunks, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("chunk[%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestAgentChatStream_AuthHeader(t *testing.T) {
	const wantToken = "agent-token"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, wantToken, "5s")
	if err := c.AgentChatStream(context.Background(), "a1", AgentChatRequest{Message: "x"}, func(SSEChunk) {}); err != nil {
		t.Fatalf("AgentChatStream() error: %v", err)
	}
	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization: got %q, want %q", gotAuth, want)
	}
}

// ─── Additional GET endpoints — error path ───────────────────────────────────

func TestModels_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Models(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "504") {
		t.Errorf("expected 504 in error, got: %v", err)
	}
}

func TestModels_Success(t *testing.T) {
	// Gateway returns OpenAI-compatible format: {"object":"list","data":[...]}
	resp := openAIModelsEnvelope{
		Data: []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		}{
			{ID: "claude-3", OwnedBy: "anthropic"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "claude-3" || models[0].Provider != "anthropic" {
		t.Errorf("unexpected models: %v", models)
	}
}

func TestProviders_Success(t *testing.T) {
	resp := providersEnvelope{
		Providers: []Provider{{Name: "anthropic", Status: "healthy"}},
		Count:     1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	providers, err := c.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers() error: %v", err)
	}
	if len(providers) != 1 || providers[0].Name != "anthropic" {
		t.Errorf("unexpected providers: %v", providers)
	}
}

func TestMemories_Success(t *testing.T) {
	resp := memoriesResponse{
		Memories: []Memory{{ID: "m1", Content: "remember this", Type: "fact"}},
		Total:    1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memory" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	mems, err := c.Memories(context.Background())
	if err != nil {
		t.Fatalf("Memories() error: %v", err)
	}
	if len(mems) != 1 || mems[0].ID != "m1" {
		t.Errorf("unexpected memories: %v", mems)
	}
}

func TestGardenStats_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"total_seeds": 42})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	stats, err := c.GardenStats(context.Background())
	if err != nil {
		t.Fatalf("GardenStats() error: %v", err)
	}
	if v, ok := stats["total_seeds"].(float64); !ok || int(v) != 42 {
		t.Errorf("unexpected stats: %v", stats)
	}
}

func TestDeleteSeed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	if err := c.DeleteSeed(context.Background(), "seed-1"); err != nil {
		t.Fatalf("DeleteSeed() error: %v", err)
	}
}

func TestDeleteSeed_404_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.DeleteSeed(context.Background(), "missing-seed")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestCreateAgent_Success(t *testing.T) {
	want := CreateAgentResponse{AgentID: "agent-1", Status: "ready"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	got, err := c.CreateAgent(context.Background(), CreateAgentRequest{WorkspaceRoot: "/tmp"})
	if err != nil {
		t.Fatalf("CreateAgent() error: %v", err)
	}
	if got.AgentID != want.AgentID {
		t.Errorf("AgentID: got %q, want %q", got.AgentID, want.AgentID)
	}
}

// ─── Agents endpoint ─────────────────────────────────────────────────────────

func TestAgents_Success(t *testing.T) {
	resp := agentsEnvelope{
		Agents: []Agent{{AgentID: "a1", Status: "running"}, {AgentID: "a2", Status: "idle"}},
		Total:  2,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	agents, err := c.Agents(context.Background())
	if err != nil {
		t.Fatalf("Agents() error: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	if agents[0].AgentID != "a1" {
		t.Errorf("AgentID: got %q, want %q", agents[0].AgentID, "a1")
	}
}

func TestAgents_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Agents(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── Skills / SkillsAll ──────────────────────────────────────────────────────

func TestSkillsAll_SinglePage(t *testing.T) {
	resp := skillsEnvelope{
		Skills: []Skill{{ID: "s1", Name: "skill-one"}, {ID: "s2", Name: "skill-two"}},
		Total:  2,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	skills, err := c.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills() error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].ID != "s1" {
		t.Errorf("ID: got %q, want %q", skills[0].ID, "s1")
	}
}

func TestSkillsAll_EmptyPage_StopsLoop(t *testing.T) {
	// Server returns empty skills list — loop should stop after first page.
	resp := skillsEnvelope{Skills: nil, Total: 0}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	skills, err := c.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills() error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 request, got %d", calls)
	}
}

func TestSkills_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	_, err := c.Skills(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── put helper — covered via UpdateMemory ───────────────────────────────────

func TestUpdateMemory_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	if err := c.UpdateMemory(context.Background(), "m1", UpdateMemoryRequest{Content: "updated"}); err != nil {
		t.Fatalf("UpdateMemory() error: %v", err)
	}
}

func TestUpdateMemory_NoContent_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	if err := c.UpdateMemory(context.Background(), "m2", UpdateMemoryRequest{Content: "x"}); err != nil {
		t.Fatalf("UpdateMemory() error: %v", err)
	}
}

func TestUpdateMemory_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.UpdateMemory(context.Background(), "m1", UpdateMemoryRequest{Content: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── PilotStream ─────────────────────────────────────────────────────────────

func TestPilotStream_ParsesSSEChunks(t *testing.T) {
	sseBody := "data: pilot1\ndata: pilot2\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "client_id") {
			http.Error(w, "missing client_id", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	err := c.PilotStream(context.Background(), "client-123", func(chunk SSEChunk) {
		got = append(got, chunk.Data)
	})
	if err != nil {
		t.Fatalf("PilotStream() error: %v", err)
	}
	if len(got) != 2 || got[0] != "pilot1" || got[1] != "pilot2" {
		t.Errorf("unexpected chunks: %v", got)
	}
}

func TestPilotStream_4xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.PilotStream(context.Background(), "cid", func(SSEChunk) {})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

// ─── WorkflowExecutionStream ─────────────────────────────────────────────────

func TestWorkflowExecutionStream_ParsesSSEChunks(t *testing.T) {
	sseBody := "data: wf-event-1\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "run-xyz") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	err := c.WorkflowExecutionStream(context.Background(), "run-xyz", func(chunk SSEChunk) {
		got = append(got, chunk.Data)
	})
	if err != nil {
		t.Fatalf("WorkflowExecutionStream() error: %v", err)
	}
	if len(got) != 1 || got[0] != "wf-event-1" {
		t.Errorf("unexpected chunks: %v", got)
	}
}

func TestWorkflowExecutionStream_5xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	err := c.WorkflowExecutionStream(context.Background(), "run-1", func(SSEChunk) {})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── Stall watchdog (a gateway that connects then goes silent) ───────────────

func TestChatStream_StallWatchdog(t *testing.T) {
	old := streamStallTimeout
	streamStallTimeout = 150 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Go silent — never send more data or [DONE]. The client watchdog must
		// give up rather than hang forever.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(srv.URL, "", "5s")
	var got []string
	start := time.Now()
	err := c.ChatStream(context.Background(), ChatRequest{Message: "hi"}, func(ch SSEChunk) {
		got = append(got, ch.Data)
	})
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("expected a stall error, got %v", err)
	}
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("expected the single pre-stall chunk [hello], got %v", got)
	}
	if elapsed > 2*time.Second {
		t.Errorf("watchdog fired too slowly (%v) — should trip near the stall window", elapsed)
	}
}

func TestParseSSE_NonCloserReaderNoWatchdog(t *testing.T) {
	// A non-Closer reader (the test path) must not arm the watchdog; a complete
	// stream still parses normally regardless of the stall window.
	old := streamStallTimeout
	streamStallTimeout = 10 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	r := strings.NewReader("data: a\ndata: b\ndata: [DONE]\n")
	var got []string
	if err := parseSSE(r, func(c SSEChunk) { got = append(got, c.Data) }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected [a b], got %v", got)
	}
}

// ─── FriendlyError classifier ────────────────────────────────────────────────

func TestFriendlyError_ConnectionRefused(t *testing.T) {
	c := New("http://localhost:7340", "", "5s")
	msg := c.FriendlyError(errors.New("dial tcp 127.0.0.1:7340: connect: connection refused"))
	if !strings.Contains(msg, "cannot reach gateway at http://localhost:7340") {
		t.Errorf("connection refused: got %q", msg)
	}
}

func TestFriendlyError_NoSuchHost(t *testing.T) {
	c := New("http://test-host", "", "5s")
	msg := c.FriendlyError(errors.New(`Post "http://test-host/v1/chat": dial tcp: lookup test-host: no such host`))
	if !strings.Contains(msg, "cannot reach gateway") {
		t.Errorf("no such host: got %q", msg)
	}
}

func TestFriendlyError_Stalled(t *testing.T) {
	c := New("http://localhost:7340", "", "5s")
	// A bare "gateway stalled" already reads cleanly; the classifier still routes
	// it to the reach-the-gateway guidance because the socket is effectively dead.
	msg := c.FriendlyError(errors.New("gateway stalled (no data for 120s)"))
	if !strings.Contains(msg, "cannot reach gateway") && !strings.Contains(msg, "stalled") {
		t.Errorf("stalled: got %q", msg)
	}
}

func TestFriendlyError_AuthRejected(t *testing.T) {
	c := New("http://localhost:7340", "", "5s")
	for _, in := range []string{
		"gateway returned 401: unauthorized",
		"gateway returned 403: forbidden",
	} {
		msg := c.FriendlyError(errors.New(in))
		if !strings.Contains(strings.ToLower(msg), "auth") {
			t.Errorf("auth error %q: got %q, want auth guidance", in, msg)
		}
	}
}

func TestFriendlyError_Passthrough(t *testing.T) {
	c := New("http://localhost:7340", "", "5s")
	msg := c.FriendlyError(errors.New("unexpected parser state at token 3"))
	if msg != "unexpected parser state at token 3" {
		t.Errorf("passthrough: got %q, want the original message", msg)
	}
}

func TestFriendlyError_Nil(t *testing.T) {
	c := New("http://localhost:7340", "", "5s")
	if msg := c.FriendlyError(nil); msg != "" {
		t.Errorf("nil error: got %q, want empty", msg)
	}
}
