// Package client provides a typed HTTP+SSE client for the Dojo Genesis AgenticGateway.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DojoGenesis/cli/internal/trace"
)

// streamStallTimeout is the maximum gap allowed between SSE chunks before the
// stream is considered dead. It is a stall window, NOT a blanket deadline: a
// slow-but-alive generation resets the watchdog on every chunk and survives
// indefinitely. Declared as a var (not const) so tests can shorten it.
var streamStallTimeout = 120 * time.Second

// Client talks to an AgenticGateway instance.
type Client struct {
	base       string
	token      string
	http       *http.Client
	streamHTTP *http.Client // no timeout — for SSE streaming
}

// New creates a Client. timeout is a Go duration string (e.g. "60s").
func New(baseURL, token, timeout string) *Client {
	d, err := time.ParseDuration(timeout)
	if err != nil {
		d = 60 * time.Second
	}
	return &Client{
		base:  strings.TrimRight(baseURL, "/"),
		token: token,
		http: &http.Client{
			Timeout:   d,
			Transport: trace.NewRoundTripper(nil, nil),
		},
		streamHTTP: &http.Client{
			Transport: trace.NewRoundTripper(nil, nil),
		},
	}
}

// BaseURL returns the gateway base URL this client targets.
func (c *Client) BaseURL() string { return c.base }

// FriendlyError turns a raw transport/HTTP error into a concise, actionable
// message. Connection failures name the gateway URL and the three ways to point
// the CLI at a live one; auth failures name the token knobs. Anything it does
// not recognise is passed through unchanged so no detail is lost.
func (c *Client) FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(msg, "401"),
		strings.Contains(msg, "403"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "forbidden"):
		return "gateway rejected auth — check gateway.token in ~/.dojo/settings.json, DOJO_GATEWAY_TOKEN, or --token"

	case strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no such host"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "dial tcp"),
		strings.Contains(lower, "connect: "),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "deadline exceeded"),
		strings.Contains(lower, "eof"):
		url := c.base
		if url == "" {
			url = "(unset)"
		}
		return fmt.Sprintf("cannot reach gateway at %s — is it running? set gateway.url in ~/.dojo/settings.json, DOJO_GATEWAY_URL, or --gateway", url)

	default:
		return msg
	}
}

// ─── Health ──────────────────────────────────────────────────────────────────

// HealthResponse is the /health response.
// Server: server/handle_health.go → HealthResponse
type HealthResponse struct {
	Status            string            `json:"status"`
	Version           string            `json:"version"`
	Timestamp         string            `json:"timestamp"`
	Providers         map[string]string `json:"providers"`
	Dependencies      map[string]string `json:"dependencies"`
	UptimeSeconds     int64             `json:"uptime_seconds"`
	RequestsProcessed int64             `json:"requests_processed"`
}

// Health fetches GET /health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var h HealthResponse
	if err := c.get(ctx, "/health", &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ─── Models / Providers ──────────────────────────────────────────────────────

// ProviderInfo carries optional metadata about a provider's capabilities.
// Mirrors provider.ProviderInfo fields that are relevant to the client.
type ProviderInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Provider is a single entry in the /v1/providers response.
// Server: server/handlers/models.go → ProviderStatus
type Provider struct {
	Name   string        `json:"name"`
	Status string        `json:"status"`
	Info   *ProviderInfo `json:"info,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// providersEnvelope is the top-level wrapper for GET /v1/providers.
// Server returns: {"providers": [...], "count": N}
type providersEnvelope struct {
	Providers []Provider `json:"providers"`
	Count     int        `json:"count"`
}

// Providers fetches GET /v1/providers.
func (c *Client) Providers(ctx context.Context) ([]Provider, error) {
	var r providersEnvelope
	if err := c.get(ctx, "/v1/providers", &r); err != nil {
		return nil, err
	}
	return r.Providers, nil
}

// Model is a single entry in the /v1/models response.
type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// openAIModelsEnvelope matches the gateway's OpenAI-compatible /v1/models response.
// Server returns: {"object": "list", "data": [{"id": "...", "owned_by": "..."}]}
type openAIModelsEnvelope struct {
	Data []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// Models fetches GET /v1/models and maps the OpenAI-compatible response to Model.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var r openAIModelsEnvelope
	if err := c.get(ctx, "/v1/models", &r); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(r.Data))
	for _, m := range r.Data {
		models = append(models, Model{
			ID:       m.ID,
			Provider: m.OwnedBy,
			Name:     m.ID,
		})
	}
	return models, nil
}

// ─── Tools ───────────────────────────────────────────────────────────────────

// Tool is a single entry in the tools list.
// Server: server/handle_gateway.go → handleGatewayListTools
// Returns: {"tools": [{name, description, parameters, namespace}], "count": N}
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Namespace   string         `json:"namespace"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// toolsEnvelope is the top-level wrapper for GET /v1/gateway/tools.
// Note: gateway uses "count", not "total".
type toolsEnvelope struct {
	Tools []Tool `json:"tools"`
	Count int    `json:"count"`
}

// ToolsResponse exposes the parsed tools list to callers.
type ToolsResponse struct {
	Tools []Tool
	Count int
}

// Tools fetches GET /v1/gateway/tools.
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var r toolsEnvelope
	if err := c.get(ctx, "/v1/gateway/tools", &r); err != nil {
		// fallback to /v1/tools (handlers/tools.go HandleListTools)
		// That endpoint returns: {"success":true,"count":N,"tools":[{name,description,parameters}]}
		var fallback struct {
			Tools []Tool `json:"tools"`
			Count int    `json:"count"`
		}
		if err2 := c.get(ctx, "/v1/tools", &fallback); err2 != nil {
			return nil, fmt.Errorf("both tool endpoints failed: %w; fallback: %v", err, err2)
		}
		return fallback.Tools, nil
	}
	return r.Tools, nil
}

// ─── Agents ──────────────────────────────────────────────────────────────────

// AgentDisposition carries the disposition fields embedded in agent list entries.
type AgentDisposition struct {
	Pacing     string `json:"pacing"`
	Depth      string `json:"depth"`
	Tone       string `json:"tone"`
	Initiative string `json:"initiative"`
}

// Agent is a single entry in the /v1/gateway/agents response.
// Server: server/handle_gateway.go → handleGatewayListAgents
// Returns entries with: {agent_id, status, config?, disposition?, channels?}
type Agent struct {
	AgentID     string            `json:"agent_id"`
	Status      string            `json:"status"`
	Disposition *AgentDisposition `json:"disposition,omitempty"`
	Channels    []string          `json:"channels,omitempty"`
}

// agentsEnvelope is the top-level wrapper for GET /v1/gateway/agents.
// Server returns: {"agents": [...], "total": N, "limit": N, "offset": N}
type agentsEnvelope struct {
	Agents []Agent `json:"agents"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// Agents fetches GET /v1/gateway/agents.
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	var r agentsEnvelope
	if err := c.get(ctx, "/v1/gateway/agents", &r); err != nil {
		return nil, err
	}
	return r.Agents, nil
}

// ─── Skills ──────────────────────────────────────────────────────────────────

// PortDefinition mirrors skill.PortDefinition from the server.
type PortDefinition struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// Skill is a single entry in the /api/skills response.
// Server: workflow/api/handler.go → skillEntry
// Returns: {id, name, description, version, category, plugin, inputs, outputs, cas_hash}
type Skill struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Version     string           `json:"version,omitempty"`
	Category    string           `json:"category,omitempty"`
	Plugin      string           `json:"plugin,omitempty"`
	Inputs      []PortDefinition `json:"inputs"`
	Outputs     []PortDefinition `json:"outputs"`
	CASHash     string           `json:"cas_hash,omitempty"`
}

// skillsEnvelope is the top-level wrapper for GET /api/skills.
// Server returns: {"skills": [...], "total": N, "limit": N, "offset": N}
type skillsEnvelope struct {
	Skills []Skill `json:"skills"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// Skills fetches all skills from the gateway, paginating automatically.
// The server caps responses at 50 by default; this exhausts all pages.
func (c *Client) Skills(ctx context.Context) ([]Skill, error) {
	return c.SkillsAll(ctx)
}

// SkillsAll fetches every skill by walking server-side pages.
// It requests 100 per page and stops when all skills are collected.
func (c *Client) SkillsAll(ctx context.Context) ([]Skill, error) {
	const pageSize = 100
	var all []Skill
	offset := 0
	for {
		var r skillsEnvelope
		path := fmt.Sprintf("/api/skills?limit=%d&offset=%d", pageSize, offset)
		if err := c.get(ctx, path, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Skills...)
		// Stop when we've collected everything or the server returned an empty page.
		if len(r.Skills) == 0 || (r.Total > 0 && len(all) >= r.Total) {
			break
		}
		offset += len(r.Skills)
	}
	return all, nil
}

// SearchSkills searches skills by query string, matching against name, description, and trigger fields.
// Paginates automatically in case the server returns partial results.
func (c *Client) SearchSkills(ctx context.Context, query string) ([]Skill, error) {
	const pageSize = 100
	var all []Skill
	offset := 0
	for {
		var r skillsEnvelope
		path := fmt.Sprintf("/api/skills?q=%s&limit=%d&offset=%d", url.QueryEscape(query), pageSize, offset)
		if err := c.get(ctx, path, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Skills...)
		if len(r.Skills) == 0 || (r.Total > 0 && len(all) >= r.Total) {
			break
		}
		offset += len(r.Skills)
	}
	return all, nil
}

// ─── Seeds / Garden ──────────────────────────────────────────────────────────

// Seed is a single entry in the /v1/seeds response.
// Server: server/handlers/memory.go → memory.Seed
// Returns: {id, name, description, trigger, content, usage_count, last_used?, created_at, updated_at}
type Seed struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Trigger     string     `json:"trigger"`
	Content     string     `json:"content"`
	UsageCount  int        `json:"usage_count"`
	LastUsed    *time.Time `json:"last_used,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// seedsEnvelope is the top-level wrapper for GET /v1/seeds.
// Server returns: {"success": true, "count": N, "seeds": [...]}
type seedsEnvelope struct {
	Success bool   `json:"success"`
	Count   int    `json:"count"`
	Seeds   []Seed `json:"seeds"`
}

// Seeds fetches GET /v1/seeds.
func (c *Client) Seeds(ctx context.Context) ([]Seed, error) {
	var r seedsEnvelope
	if err := c.get(ctx, "/v1/seeds", &r); err != nil {
		return nil, err
	}
	return r.Seeds, nil
}

// GardenStats fetches GET /v1/garden/stats.
func (c *Client) GardenStats(ctx context.Context) (map[string]any, error) {
	var r map[string]any
	if err := c.get(ctx, "/v1/garden/stats", &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ─── Chat (streaming SSE) ────────────────────────────────────────────────────

// ChatRequest mirrors the /v1/chat request body.
// Server: server/handlers/chat.go → ChatRequest
type ChatRequest struct {
	Message       string `json:"message"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Stream        bool   `json:"stream"`
	SessionID     string `json:"session_id"`
	UserID        string `json:"user_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	WorkspaceRoot string `json:"workspace_root,omitempty"` // User's CWD for file tool path resolution
}

// SSEChunk is a parsed line from the SSE stream.
type SSEChunk struct {
	Event string
	Data  string
}

// ChatStream opens a streaming POST /v1/chat and calls onChunk for each SSE data line.
// Returns when the stream ends or ctx is cancelled.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onChunk func(SSEChunk)) error {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.streamHTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return parseSSE(resp.Body, onChunk)
}

// parseSSE reads an SSE stream and calls onChunk for each data line.
// Per SSE spec, the event field resets on blank lines (event dispatch boundary),
// not after each data line. Buffer is raised to 1MB to handle large JSON payloads.
//
// A stall watchdog guards against a gateway that connects but then goes silent
// (no bytes ever arrive): if no line is read for streamStallTimeout, the stream
// is force-closed and a "gateway stalled" error is returned. The watchdog resets
// on every line, so a slow-but-alive generation is never killed. When r is not an
// io.Closer (e.g. a test strings.Reader) the watchdog is inert — it only matters
// for real network streams that can block a Read indefinitely.
func parseSSE(r io.Reader, onChunk func(SSEChunk)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB lines

	var stalled atomic.Bool
	var watchdog *time.Timer
	if closer, ok := r.(io.Closer); ok {
		watchdog = time.AfterFunc(streamStallTimeout, func() {
			stalled.Store(true)
			_ = closer.Close() // unblock the in-flight Read
		})
		defer watchdog.Stop()
	}

	var event string
	for scanner.Scan() {
		if watchdog != nil {
			watchdog.Reset(streamStallTimeout)
		}
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return nil
			}
			onChunk(SSEChunk{Event: event, Data: data})
		case line == "":
			// SSE dispatch boundary — reset event for the next block
			event = ""
		}
	}
	if stalled.Load() {
		return fmt.Errorf("gateway stalled (no data for %s)", streamStallTimeout)
	}
	return scanner.Err()
}

// ─── Seeds — create ──────────────────────────────────────────────────────────

// CreateSeedRequest is the body for POST /v1/seeds.
// Server: server/handlers/memory.go → CreateSeedRequest
type CreateSeedRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Trigger     string `json:"trigger,omitempty"`
	Content     string `json:"content"`
}

// CreateSeed posts a new seed to /v1/seeds.
func (c *Client) CreateSeed(ctx context.Context, req CreateSeedRequest) (*Seed, error) {
	var wrapper struct {
		Seed Seed `json:"seed"`
	}
	if err := c.post(ctx, "/v1/seeds", req, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Seed, nil
}

// ─── Memory ──────────────────────────────────────────────────────────────────

// Memory represents an entry from GET /v1/memory.
type Memory struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	Type      string `json:"type"`
}

type memoriesResponse struct {
	Memories []Memory `json:"memories"`
	Total    int      `json:"total"`
}

// Memories fetches GET /v1/memory.
func (c *Client) Memories(ctx context.Context) ([]Memory, error) {
	var r memoriesResponse
	if err := c.get(ctx, "/v1/memory", &r); err != nil {
		return nil, err
	}
	return r.Memories, nil
}

// ─── Pilot — live SSE stream ─────────────────────────────────────────────────

// PilotStream opens GET /events and calls onChunk for each SSE event.
// It runs until ctx is cancelled or the stream closes.
func (c *Client) PilotStream(ctx context.Context, clientID string, onChunk func(SSEChunk)) error {
	u := c.base + "/events?" + url.Values{"client_id": {clientID}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return parseSSE(resp.Body, onChunk)
}

// ─── Agent creation (Spec 2) ─────────────────────────────────────────────────

// CreateAgentRequest is the body for POST /v1/gateway/agents.
type CreateAgentRequest struct {
	WorkspaceRoot string `json:"workspace_root"`
	ActiveMode    string `json:"active_mode,omitempty"` // "focused"|"balanced"|"exploratory"|"deliberate"
}

// CreateAgentResponse is the response from POST /v1/gateway/agents.
type CreateAgentResponse struct {
	AgentID     string            `json:"agent_id"`
	Status      string            `json:"status"`
	Disposition *AgentDisposition `json:"disposition,omitempty"`
}

// CreateAgent creates a new agent in the gateway and returns its ID.
func (c *Client) CreateAgent(ctx context.Context, req CreateAgentRequest) (*CreateAgentResponse, error) {
	if req.WorkspaceRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			wd = "."
		}
		req.WorkspaceRoot = wd
	}
	var r CreateAgentResponse
	if err := c.post(ctx, "/v1/gateway/agents", req, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ─── Agent chat streaming (Spec 2) ───────────────────────────────────────────

// AgentChatRequest is the body for POST /v1/gateway/agents/:id/chat.
type AgentChatRequest struct {
	Message         string `json:"message"`
	UserID          string `json:"user_id,omitempty"`
	DocumentID      string `json:"document_id,omitempty"`
	DocumentContent string `json:"document_content,omitempty"`
	Stream          bool   `json:"stream"`
}

// AgentChatStream sends a message to a specific agent and streams the SSE response.
// agentID is the UUID returned by CreateAgent. Same SSEChunk callback as ChatStream.
func (c *Client) AgentChatStream(ctx context.Context, agentID string, req AgentChatRequest, onChunk func(SSEChunk)) error {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	path := "/v1/gateway/agents/" + agentID + "/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.streamHTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return parseSSE(resp.Body, onChunk)
}

// ─── Orchestration submission (Spec 3) ───────────────────────────────────────

// ToolInvocation is a single node in an orchestration DAG.
type ToolInvocation struct {
	ID        string         `json:"id"`
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
	DependsOn []string       `json:"depends_on,omitempty"`
}

// ExecutionPlan is the plan submitted to /v1/gateway/orchestrate.
type ExecutionPlan struct {
	ID   string           `json:"id"`
	Name string           `json:"name"`
	DAG  []ToolInvocation `json:"dag"`
}

// OrchestrateRequest is the body for POST /v1/gateway/orchestrate.
type OrchestrateRequest struct {
	Plan   ExecutionPlan `json:"plan"`
	UserID string        `json:"user_id,omitempty"`
}

// OrchestrationStatus is returned by POST /v1/gateway/orchestrate.
type OrchestrationStatus struct {
	ExecutionID string `json:"execution_id"`
	PlanID      string `json:"plan_id"`
	Status      string `json:"status"` // "submitted"
}

// Orchestrate submits an execution plan to the gateway (async — returns immediately).
func (c *Client) Orchestrate(ctx context.Context, req OrchestrateRequest) (*OrchestrationStatus, error) {
	var r OrchestrationStatus
	if err := c.post(ctx, "/v1/gateway/orchestrate", req, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ─── DAG status polling (Spec 3) ─────────────────────────────────────────────

// DAGStatus is the response from GET /v1/gateway/orchestrate/:id/dag.
type DAGStatus struct {
	ExecutionID string           `json:"execution_id"`
	Status      string           `json:"status"` // "running"|"completed"|"failed"
	PlanID      string           `json:"plan_id"`
	Nodes       []map[string]any `json:"nodes,omitempty"`
}

// OrchestrationDAG polls the DAG state for an execution.
func (c *Client) OrchestrationDAG(ctx context.Context, executionID string) (*DAGStatus, error) {
	var r DAGStatus
	if err := c.get(ctx, "/v1/gateway/orchestrate/"+executionID+"/dag", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) put(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

func (c *Client) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

func (c *Client) getRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return io.ReadAll(resp.Body)
}

// ─── Memory — CRUD ──────────────────────────────────────────────────────────

// StoreMemoryRequest is the body for POST /v1/memory.
type StoreMemoryRequest struct {
	Content string `json:"content"`
	Type    string `json:"type,omitempty"` // e.g. "observation", "fact"
}

// StoreMemory creates a new memory entry.
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) (*Memory, error) {
	var wrapper struct {
		Memory Memory `json:"memory"`
	}
	if err := c.post(ctx, "/v1/memory", req, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Memory, nil
}

// UpdateMemoryRequest is the body for PUT /v1/memory/:id.
type UpdateMemoryRequest struct {
	Content string `json:"content"`
}

// UpdateMemory updates an existing memory entry.
func (c *Client) UpdateMemory(ctx context.Context, id string, req UpdateMemoryRequest) error {
	return c.put(ctx, "/v1/memory/"+id, req)
}

// DeleteMemory deletes a memory entry.
func (c *Client) DeleteMemory(ctx context.Context, id string) error {
	return c.delete(ctx, "/v1/memory/"+id)
}

// SearchMemoriesRequest is the body for POST /v1/memory/search.
type SearchMemoriesRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// SearchMemories performs a semantic search across memories.
func (c *Client) SearchMemories(ctx context.Context, query string) ([]Memory, error) {
	req := SearchMemoriesRequest{Query: query, Limit: 20}
	var r memoriesResponse
	if err := c.post(ctx, "/v1/memory/search", req, &r); err != nil {
		return nil, err
	}
	return r.Memories, nil
}

// DeleteSeed deletes a seed by ID.
func (c *Client) DeleteSeed(ctx context.Context, id string) error {
	return c.delete(ctx, "/v1/seeds/"+id)
}

// ─── Snapshots ──────────────────────────────────────────────────────────────

// Snapshot represents a memory snapshot.
type Snapshot struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	CreatedAt string `json:"created_at"`
	Size      int    `json:"size,omitempty"`
}

type snapshotsEnvelope struct {
	Snapshots []Snapshot `json:"snapshots"`
	Total     int        `json:"total"`
}

// ListSnapshots fetches snapshots for a session.
func (c *Client) ListSnapshots(ctx context.Context, session string) ([]Snapshot, error) {
	var r snapshotsEnvelope
	if err := c.get(ctx, "/v1/snapshots/"+session, &r); err != nil {
		return nil, err
	}
	return r.Snapshots, nil
}

// CreateSnapshotRequest is the body for POST /v1/snapshots.
type CreateSnapshotRequest struct {
	SessionID string `json:"session_id"`
}

// CreateSnapshot creates a new memory snapshot.
func (c *Client) CreateSnapshot(ctx context.Context, session string) (*Snapshot, error) {
	req := CreateSnapshotRequest{SessionID: session}
	var wrapper struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.post(ctx, "/v1/snapshots", req, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Snapshot, nil
}

// RestoreSnapshot restores a prior snapshot.
func (c *Client) RestoreSnapshot(ctx context.Context, snapshotID string) error {
	var r map[string]any
	return c.get(ctx, "/v1/snapshots/restore/"+snapshotID, &r)
}

// DeleteSnapshot deletes a snapshot.
func (c *Client) DeleteSnapshot(ctx context.Context, id string) error {
	return c.delete(ctx, "/v1/snapshots/"+id)
}

// ExportSnapshot exports a snapshot as raw bytes.
func (c *Client) ExportSnapshot(ctx context.Context, id string) ([]byte, error) {
	return c.getRaw(ctx, "/v1/snapshots/export/"+id)
}

// ─── Traces ─────────────────────────────────────────────────────────────────

// GetTrace fetches trace data for an execution.
func (c *Client) GetTrace(ctx context.Context, traceID string) (map[string]any, error) {
	var r map[string]any
	if err := c.get(ctx, "/v1/gateway/traces/"+traceID, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ─── Provider Settings ──────────────────────────────────────────────────────

// SetProviderKeyRequest is the body for POST /v1/settings/providers.
// Note: the gateway handler uses field name "key", not "api_key".
type SetProviderKeyRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"key"`
}

// SetProviderKey sets an API key for a provider.
func (c *Client) SetProviderKey(ctx context.Context, provider, apiKey string) error {
	req := SetProviderKeyRequest{Provider: provider, APIKey: apiKey}
	var r map[string]any
	return c.post(ctx, "/v1/settings/providers", req, &r)
}

// GetProviderSettings fetches provider settings.
func (c *Client) GetProviderSettings(ctx context.Context) (map[string]any, error) {
	var r map[string]any
	if err := c.get(ctx, "/v1/settings/providers", &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ─── MCP Apps ───────────────────────────────────────────────────────────────

// App represents a running MCP app.
type App struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Tools  int    `json:"tools,omitempty"`
}

type appsEnvelope struct {
	Apps []App `json:"apps"`
}

// LaunchAppRequest is the body for POST /v1/gateway/apps/launch.
type LaunchAppRequest struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

// LaunchApp starts an MCP app server.
func (c *Client) LaunchApp(ctx context.Context, name string, config map[string]any) error {
	req := LaunchAppRequest{Name: name, Config: config}
	var r map[string]any
	return c.post(ctx, "/v1/gateway/apps/launch", req, &r)
}

// CloseAppRequest is the body for POST /v1/gateway/apps/close.
type CloseAppRequest struct {
	Name string `json:"name"`
}

// CloseApp stops an MCP app server.
func (c *Client) CloseApp(ctx context.Context, name string) error {
	req := CloseAppRequest{Name: name}
	var r map[string]any
	return c.post(ctx, "/v1/gateway/apps/close", req, &r)
}

// ListApps fetches running MCP apps.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var r appsEnvelope
	if err := c.get(ctx, "/v1/gateway/apps", &r); err != nil {
		return nil, err
	}
	return r.Apps, nil
}

// AppStatus fetches MCP app connection status.
func (c *Client) AppStatus(ctx context.Context) (map[string]any, error) {
	var r map[string]any
	if err := c.get(ctx, "/v1/gateway/apps/status", &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ProxyToolCallRequest is the body for POST /v1/gateway/apps/tool-call.
type ProxyToolCallRequest struct {
	App      string         `json:"app"`
	ToolName string         `json:"tool_name"`
	Input    map[string]any `json:"input"`
}

// ProxyToolCall invokes a tool through an MCP app.
func (c *Client) ProxyToolCall(ctx context.Context, app, tool string, input map[string]any) (map[string]any, error) {
	req := ProxyToolCallRequest{App: app, ToolName: tool, Input: input}
	var r map[string]any
	if err := c.post(ctx, "/v1/gateway/apps/tool-call", req, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ─── Agent Channels ─────────────────────────────────────────────────────────

// BindChannelsRequest is the body for POST /v1/gateway/agents/:id/channels.
type BindChannelsRequest struct {
	Channels []string `json:"channels"`
}

// BindAgentChannels binds an agent to one or more channels.
func (c *Client) BindAgentChannels(ctx context.Context, agentID string, channels []string) error {
	req := BindChannelsRequest{Channels: channels}
	var r map[string]any
	return c.post(ctx, "/v1/gateway/agents/"+agentID+"/channels", req, &r)
}

// ListAgentChannels fetches channels bound to an agent.
func (c *Client) ListAgentChannels(ctx context.Context, agentID string) ([]string, error) {
	var r struct {
		Channels []string `json:"channels"`
	}
	if err := c.get(ctx, "/v1/gateway/agents/"+agentID+"/channels", &r); err != nil {
		return nil, err
	}
	return r.Channels, nil
}

// UnbindAgentChannel removes a channel binding from an agent.
func (c *Client) UnbindAgentChannel(ctx context.Context, agentID, channel string) error {
	return c.delete(ctx, "/v1/gateway/agents/"+agentID+"/channels/"+channel)
}

// AgentDetail is the full detail response for GET /v1/gateway/agents/:id.
type AgentDetail struct {
	AgentID     string            `json:"agent_id"`
	Status      string            `json:"status"`
	Disposition *AgentDisposition `json:"disposition,omitempty"`
	Channels    []string          `json:"channels,omitempty"`
	Config      map[string]any    `json:"config,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
}

// GetAgent fetches full details for a specific agent.
func (c *Client) GetAgent(ctx context.Context, agentID string) (*AgentDetail, error) {
	var r AgentDetail
	if err := c.get(ctx, "/v1/gateway/agents/"+agentID, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ─── Workflows ──────────────────────────────────────────────────────────────

// ExecuteWorkflowRequest is the body for POST /api/workflows/:name/execute.
type ExecuteWorkflowRequest struct {
	Input map[string]any `json:"input,omitempty"`
}

// ExecuteWorkflowResponse is the response from workflow execution.
type ExecuteWorkflowResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// ExecuteWorkflow starts a named workflow and returns the run ID.
func (c *Client) ExecuteWorkflow(ctx context.Context, name string, input map[string]any) (*ExecuteWorkflowResponse, error) {
	req := ExecuteWorkflowRequest{Input: input}
	var r ExecuteWorkflowResponse
	if err := c.post(ctx, "/api/workflows/"+name+"/execute", req, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WorkflowExecutionStream streams workflow execution events via SSE.
func (c *Client) WorkflowExecutionStream(ctx context.Context, runID string, onChunk func(SSEChunk)) error {
	path := "/api/workflows/" + runID + "/execution"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return parseSSE(resp.Body, onChunk)
}

// ─── CAS (Content-Addressable Storage) ──────────────────────────────────────

// CASTag represents a named tag in the CAS.
type CASTag struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Ref     string `json:"ref"`
}

type casTagsEnvelope struct {
	Tags []CASTag `json:"tags"`
}

// CASListTags fetches all CAS tags.
func (c *Client) CASListTags(ctx context.Context) ([]CASTag, error) {
	var r casTagsEnvelope
	if err := c.get(ctx, "/api/cas/tags", &r); err != nil {
		return nil, err
	}
	return r.Tags, nil
}

// CASResolveTag resolves a tag to its content ref.
func (c *Client) CASResolveTag(ctx context.Context, name, version string) (*CASTag, error) {
	var r CASTag
	if err := c.get(ctx, "/api/cas/tags/"+name+"/"+version, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CASGetContent fetches raw content by ref.
func (c *Client) CASGetContent(ctx context.Context, ref string) ([]byte, error) {
	return c.getRaw(ctx, "/api/cas/content/"+ref)
}

// CASPutContent stores content and returns the ref hash.
func (c *Client) CASPutContent(ctx context.Context, content []byte) (string, error) {
	var r struct {
		Ref string `json:"ref"`
	}
	body := struct {
		Content []byte `json:"content"`
	}{Content: content}
	if err := c.post(ctx, "/api/cas/content", body, &r); err != nil {
		return "", err
	}
	return r.Ref, nil
}

// CASCreateTag creates or updates a CAS tag linking a name+version to a content ref.
func (c *Client) CASCreateTag(ctx context.Context, name, version, ref string) error {
	body := map[string]string{
		"name":    name,
		"version": version,
		"ref":     ref,
	}
	var r struct{} // gateway returns 201 with empty body or tag echo
	err := c.post(ctx, "/api/cas/tags", body, &r)
	// post() decodes JSON into r — if body is empty, the decode will fail
	// with io.EOF. That's OK for 201 Created with no body.
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		return err
	}
	return nil
}

// ─── Documents ──────────────────────────────────────────────────────────────

// GetDocument fetches a document by ID.
func (c *Client) GetDocument(ctx context.Context, id string) (map[string]any, error) {
	var r map[string]any
	if err := c.get(ctx, "/v1/gateway/documents/"+id, &r); err != nil {
		return nil, err
	}
	return r, nil
}
