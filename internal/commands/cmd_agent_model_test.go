package commands

// cmd_agent_model_test.go — tests for /agent dispatch and /agent chat's
// --model flag: both forms (--model x, --model=x), position independence,
// stripping the flag from the outgoing message, the flag > delegation
// default > empty precedence, the empty-value usage-error gate, and that the
// resolved model actually lands on the wire in CreateAgentRequest.Model /
// AgentChatRequest.Model.
//
// Seams reused from existing test files (same package, no edits there):
// captureProtocolStdout (cmd_protocol_test.go) for stdout+gcolor capture,
// mirroring cmd_skill_external_test.go's httptest-fake-gateway style and its
// newExternalSkillRegistry-shaped Registry builder.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/plugins"
)

// agentModelCapture records the decoded JSON bodies the fake gateway received
// for the two endpoints /agent dispatch and /agent chat exercise, guarded by
// a mutex since the httptest.Server handler runs on its own goroutine.
type agentModelCapture struct {
	mu     sync.Mutex
	create map[string]any
	chat   map[string]any
}

func (c *agentModelCapture) setCreate(body []byte) {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	c.mu.Lock()
	c.create = m
	c.mu.Unlock()
}

func (c *agentModelCapture) setChat(body []byte) {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	c.mu.Lock()
	c.chat = m
	c.mu.Unlock()
}

func (c *agentModelCapture) Create() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.create
}

func (c *agentModelCapture) Chat() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chat
}

// newAgentModelGateway fakes POST /v1/gateway/agents (agent creation) and
// POST /v1/gateway/agents/:id/chat (SSE chat) — the two endpoints /agent
// dispatch and /agent chat drive — capturing each request's decoded JSON
// body for assertions. The chat endpoint always ends the stream immediately
// with "data: [DONE]" so callers never block on the streamStallTimeout
// watchdog.
func newAgentModelGateway(t *testing.T) (*httptest.Server, *agentModelCapture) {
	t.Helper()
	capture := &agentModelCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/gateway/agents":
			capture.setCreate(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"agent_id":"agent-under-test","status":"created"}`))
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/chat"):
			capture.setChat(body)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

// newAgentModelRegistry builds a Registry wired to gatewayURL with
// cfg.Delegation.Model set to delegationModel. Mirrors
// cmd_skill_external_test.go's newExternalSkillRegistry. HOME is isolated
// per-test (on top of the package-wide TestMain in commands_test.go) since
// /agent dispatch and /agent chat both persist to ~/.dojo/state.json.
func newAgentModelRegistry(t *testing.T, gatewayURL, delegationModel string) *Registry {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	session := "agent-model-test-session"
	cfg := &config.Config{
		Gateway:    config.GatewayConfig{URL: gatewayURL, Timeout: "5s"},
		Delegation: config.DelegationConfig{Model: delegationModel},
	}
	r := &Registry{
		cfg:     cfg,
		gw:      client.New(gatewayURL, "", "5s"),
		cmds:    make(map[string]Command),
		plgs:    []plugins.Plugin{},
		session: &session,
	}
	r.register()
	return r
}

// ─── /agent dispatch — flag parsing (both forms) ─────────────────────────────

func TestAgentDispatchModelFlagSpaceForm(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch --model gpt-5-cheap do the thing")
	})
	if err != nil {
		t.Fatalf("agent dispatch: %v", err)
	}
	if got := capture.Create()["model"]; got != "gpt-5-cheap" {
		t.Errorf("CreateAgentRequest.Model = %v, want gpt-5-cheap", got)
	}
	if got := capture.Chat()["model"]; got != "gpt-5-cheap" {
		t.Errorf("AgentChatRequest.Model = %v, want gpt-5-cheap", got)
	}
	msg, _ := capture.Chat()["message"].(string)
	if strings.Contains(msg, "--model") {
		t.Errorf("--model flag leaked into the outgoing message: %q", msg)
	}
	if !strings.Contains(msg, "do the thing") {
		t.Errorf("message missing expected words, got: %q", msg)
	}
	if !strings.Contains(out, "model: gpt-5-cheap (flag)") {
		t.Errorf("banner line missing from output:\n%s", out)
	}
}

func TestAgentDispatchModelFlagEqualsForm(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch --model=gpt-5-cheap do the thing")
	})
	if err != nil {
		t.Fatalf("agent dispatch: %v", err)
	}
	if got := capture.Create()["model"]; got != "gpt-5-cheap" {
		t.Errorf("CreateAgentRequest.Model = %v, want gpt-5-cheap", got)
	}
	msg, _ := capture.Chat()["message"].(string)
	if strings.Contains(msg, "--model") {
		t.Errorf("--model= flag leaked into the outgoing message: %q", msg)
	}
	if !strings.Contains(out, "model: gpt-5-cheap (flag)") {
		t.Errorf("banner line missing from output:\n%s", out)
	}
}

func TestAgentDispatchModelFlagPositionIndependent(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"flag_before_mode", "agent dispatch --model gpt-5-cheap focused do the thing"},
		{"flag_after_mode_mid_message", "agent dispatch focused do the --model gpt-5-cheap thing"},
		{"flag_at_end", "agent dispatch focused do the thing --model gpt-5-cheap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, capture := newAgentModelGateway(t)
			r := newAgentModelRegistry(t, srv.URL, "")

			out, err := captureProtocolStdout(t, func() error {
				return r.Dispatch(context.Background(), tc.input)
			})
			if err != nil {
				t.Fatalf("agent dispatch: %v", err)
			}
			if got := capture.Create()["active_mode"]; got != "focused" {
				t.Errorf("ActiveMode = %v, want focused (mode-detection broke around the flag)", got)
			}
			if got := capture.Create()["model"]; got != "gpt-5-cheap" {
				t.Errorf("CreateAgentRequest.Model = %v, want gpt-5-cheap", got)
			}
			msg, _ := capture.Chat()["message"].(string)
			if strings.Contains(msg, "--model") {
				t.Errorf("--model flag leaked into the outgoing message: %q", msg)
			}
			if !strings.Contains(msg, "do the thing") {
				t.Errorf("message reconstruction broke around the flag, got: %q", msg)
			}
			if !strings.Contains(out, "Creating agent (mode: focused)") {
				t.Errorf("mode banner missing or wrong:\n%s", out)
			}
		})
	}
}

// ─── precedence: flag > delegation default > empty ──────────────────────────

func TestAgentDispatchPrecedenceDelegationDefault(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "sonnet-delegate")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch do the thing")
	})
	if err != nil {
		t.Fatalf("agent dispatch: %v", err)
	}
	if got := capture.Create()["model"]; got != "sonnet-delegate" {
		t.Errorf("CreateAgentRequest.Model = %v, want sonnet-delegate (delegation default)", got)
	}
	if !strings.Contains(out, "model: sonnet-delegate (delegation default)") {
		t.Errorf("banner line missing from output:\n%s", out)
	}
}

func TestAgentDispatchPrecedenceFlagOverridesDelegationDefault(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "sonnet-delegate")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch --model opus-frontier do the thing")
	})
	if err != nil {
		t.Fatalf("agent dispatch: %v", err)
	}
	if got := capture.Create()["model"]; got != "opus-frontier" {
		t.Errorf("CreateAgentRequest.Model = %v, want opus-frontier (flag must win over delegation default)", got)
	}
	if !strings.Contains(out, "model: opus-frontier (flag)") {
		t.Errorf("banner line missing or wrong source:\n%s", out)
	}
}

func TestAgentDispatchPrecedenceEmptyOmitsModel(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch do the thing")
	})
	if err != nil {
		t.Fatalf("agent dispatch: %v", err)
	}
	if _, ok := capture.Create()["model"]; ok {
		t.Errorf("CreateAgentRequest.Model should be omitted (empty+omitempty), got present: %v", capture.Create()["model"])
	}
	if _, ok := capture.Chat()["model"]; ok {
		t.Errorf("AgentChatRequest.Model should be omitted (empty+omitempty), got present: %v", capture.Chat()["model"])
	}
	if strings.Contains(out, "model:") {
		t.Errorf("no model banner expected when nothing resolved:\n%s", out)
	}
}

// ─── empty-value usage error ─────────────────────────────────────────────────

func TestAgentDispatchModelFlagEmptyValueSpaceForm(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch --model")
	})
	if err == nil {
		t.Fatal("expected a usage error for a trailing --model with no value")
	}
	if !strings.Contains(err.Error(), "usage:") || !strings.Contains(err.Error(), "--model") {
		t.Errorf("unexpected error text: %v", err)
	}
	if capture.Create() != nil {
		t.Errorf("gateway must not be called on a malformed --model flag, got: %v", capture.Create())
	}
}

func TestAgentDispatchModelFlagEmptyValueEqualsForm(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent dispatch --model= do the thing")
	})
	if err == nil {
		t.Fatal("expected a usage error for --model= with nothing after the equals")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("unexpected error text: %v", err)
	}
	if capture.Create() != nil {
		t.Errorf("gateway must not be called on a malformed --model= flag, got: %v", capture.Create())
	}
}

// ─── /agent chat — same flag surface on a different subcommand ──────────────

func TestAgentChatModelFlagAndPrecedence(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "sonnet-delegate")

	out, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent chat --model opus-frontier existing-agent-id hello there")
	})
	if err != nil {
		t.Fatalf("agent chat: %v", err)
	}
	if got := capture.Chat()["model"]; got != "opus-frontier" {
		t.Errorf("AgentChatRequest.Model = %v, want opus-frontier (flag must win over delegation default)", got)
	}
	msg, _ := capture.Chat()["message"].(string)
	if msg != "hello there" {
		t.Errorf("message = %q, want %q (flag and agent-id must both be stripped)", msg, "hello there")
	}
	if !strings.Contains(out, "model: opus-frontier (flag)") {
		t.Errorf("banner line missing from output:\n%s", out)
	}
	if capture.Create() != nil {
		t.Errorf("/agent chat must not create a new agent, got a CreateAgent call: %v", capture.Create())
	}
}

func TestAgentChatModelFlagEmptyValueUsageError(t *testing.T) {
	srv, capture := newAgentModelGateway(t)
	r := newAgentModelRegistry(t, srv.URL, "")

	_, err := captureProtocolStdout(t, func() error {
		return r.Dispatch(context.Background(), "agent chat --model= existing-agent-id hello")
	})
	if err == nil {
		t.Fatal("expected a usage error for --model= with nothing after the equals")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("unexpected error text: %v", err)
	}
	if capture.Chat() != nil {
		t.Errorf("gateway must not be called on a malformed --model flag, got: %v", capture.Chat())
	}
}
