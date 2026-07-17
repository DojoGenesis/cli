// Package repl provides the interactive read-eval-print loop for the dojo CLI.
// renderer.go contains typed SSE event classification and terminal rendering.
package repl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guardrail"
	gcolor "github.com/gookit/color"
)

// EventType classifies SSE chunks for rendering.
type EventType int

const (
	EventText       EventType = iota // Regular response text
	EventThinking                    // Model reasoning/thinking
	EventToolCall                    // Tool invocation started
	EventToolResult                  // Tool returned result
	EventArtifact                    // Generated artifact
	EventWarning                     // Warning or notice
	EventDone                        // Stream complete
	EventEmpty                       // No content to render
	EventError                       // Gateway/provider error — always rendered visibly
)

// String returns a human-readable label for the event type.
func (et EventType) String() string {
	switch et {
	case EventText:
		return "text"
	case EventThinking:
		return "thinking"
	case EventToolCall:
		return "tool_call"
	case EventToolResult:
		return "tool_result"
	case EventArtifact:
		return "artifact"
	case EventWarning:
		return "warning"
	case EventDone:
		return "done"
	case EventEmpty:
		return "empty"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}

// RenderEvent is a classified, renderable SSE event.
type RenderEvent struct {
	Type    EventType
	Content string
	Meta    map[string]string // e.g. "tool" -> tool name, "id" -> artifact ID
}

// ClassifyChunk parses an SSE chunk into a typed RenderEvent.
// It checks the chunk.Event field first for typed events (thinking, tool_call, etc.),
// then falls back to JSON unwrap logic matching the original extractText behavior.
func ClassifyChunk(chunk client.SSEChunk) RenderEvent {
	data := strings.TrimSpace(chunk.Data)

	// Check SSE event field first — typed events take priority over data parsing.
	// This must come before the empty-data check because event: done may carry no data.
	switch strings.TrimSpace(chunk.Event) {
	case "thinking":
		content := extractContent(data)
		if content == "" {
			content = data
		}
		return RenderEvent{Type: EventThinking, Content: content}

	case "tool_call":
		content, meta := extractToolCall(data)
		return RenderEvent{Type: EventToolCall, Content: content, Meta: meta}

	case "tool_result":
		content := extractContent(data)
		if content == "" {
			content = data
		}
		return RenderEvent{Type: EventToolResult, Content: content}

	case "artifact":
		content, meta := extractArtifact(data)
		return RenderEvent{Type: EventArtifact, Content: content, Meta: meta}

	case "warning":
		content := extractContent(data)
		if content == "" {
			content = data
		}
		return RenderEvent{Type: EventWarning, Content: content}

	case "error":
		// P0: gateway/provider failures (429 quota, 404 dead model, etc.) arrive
		// as event:error. These MUST be surfaced — never swallowed into a blank
		// "success". The error message lives under the "error" key.
		msg := extractError(data)
		if msg == "" {
			msg = "unknown gateway error"
		}
		return RenderEvent{Type: EventError, Content: msg}

	case "intent_classified", "provider_selected":
		// Routing telemetry — useful to the gateway, noise to the user. Drop
		// quietly so plain/pipeline stdout carries only the real answer.
		return RenderEvent{Type: EventEmpty}

	case "done":
		return RenderEvent{Type: EventDone}
	}

	// No typed event field — check for empty data or stream terminator
	if data == "" {
		return RenderEvent{Type: EventEmpty}
	}
	if data == "[DONE]" {
		return RenderEvent{Type: EventDone}
	}

	// Fall back to content extraction (preserves extractText logic)
	text := extractContentFromData(data)
	if text == "" {
		return RenderEvent{Type: EventEmpty}
	}
	return RenderEvent{Type: EventText, Content: text}
}

// RenderJSON formats the event as a JSON line for scripted pipelines.
// Each event is a single-line JSON object with type, content, and meta fields.
func (re RenderEvent) RenderJSON() string {
	// EventThinking is a reasoning placeholder, not answer content — keep it out
	// of the JSON pipeline. EventError DOES flow through: a scripted consumer must
	// see {"type":"error",...} so a failed run is observable, not silently empty.
	if re.Type == EventEmpty || re.Type == EventDone || re.Type == EventThinking {
		return ""
	}
	obj := map[string]any{
		"type":    re.Type.String(),
		"content": re.Content,
	}
	if len(re.Meta) > 0 {
		obj["meta"] = re.Meta
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// Render formats the event for terminal display.
// If plain is true, output is unstyled for piped/CI usage.
func (re RenderEvent) Render(plain bool) string {
	switch re.Type {
	case EventText:
		return re.Content

	case EventThinking:
		// Plain/pipeline mode: the reasoning placeholder ("Processing your
		// request…") is not answer content — suppress it so stdout stays clean.
		// Interactive mode keeps the dim reasoning trace.
		if plain {
			return ""
		}
		return gcolor.HEX("#94a3b8").Sprint(re.Content)

	case EventToolCall:
		name := re.Meta["tool"]
		if name == "" {
			name = "unknown"
		}
		if plain {
			return fmt.Sprintf("[Tool: %s]", name)
		}
		return gcolor.HEX("#e8b04a").Sprintf("[Tool: %s]", name)

	case EventToolResult:
		if plain {
			return re.Content
		}
		return gcolor.HEX("#64748b").Sprint(re.Content)

	case EventArtifact:
		id := re.Meta["id"]
		if id == "" {
			id = "?"
		}
		if plain {
			return fmt.Sprintf("[Artifact: %s]", id)
		}
		return gcolor.HEX("#7fb88c").Sprintf("[Artifact: %s]", id)

	case EventWarning:
		if plain {
			return "[warning] " + re.Content
		}
		return gcolor.HEX("#f4a261").Sprintf("[warning] %s", re.Content)

	case EventError:
		if plain {
			return "error: " + re.Content
		}
		return gcolor.HEX("#ef4444").Sprintf("error: %s", re.Content)

	case EventDone, EventEmpty:
		return ""
	}

	return re.Content
}

// ─── stream guardrail (advisory repeated-tool-failure watch) ─────────────────

// streamGuardWindow is how many leading bytes of a tool result are examined,
// both for the failure heuristic and for the signature handed to
// guardrail.Signature (which further truncates to 80 bytes).
const streamGuardWindow = 200

// failureMarkers are the case-insensitive substrings that mark a tool result
// as failing when any appears within the first streamGuardWindow bytes.
// Deliberately conservative and advisory-only: a missed failure costs nothing,
// while over-triggering would train the user to ignore the guardrail.
var failureMarkers = []string{"error", "failed", "exception"}

// StreamGuard watches classified agent-stream events for repeated identical
// tool failures and escalates advisory notices through a guardrail.Tracker
// (internal/guardrail — warn at WarnAfter, hard at/after HardAfter, per
// cfg.Guardrails). It never mutates, drops, or reorders the original events:
// Observe only ever returns an ADDITIONAL synthetic warning for the caller to
// render through the normal EventWarning path.
//
// Scope choice: chunk classification here is stateless free functions with no
// renderer instance to hang state on, so the guard lives one-per-REPL (a
// field set in repl.New) — one interactive session lifetime, letting failure
// streaks span turns the way the Registry's command guard does. It is driven
// only from the single ChatStream callback goroutine, so the small
// bookkeeping fields need no lock (the Tracker inside is mutex-guarded
// anyway).
type StreamGuard struct {
	tracker *guardrail.Tracker

	// pendingTool is the tool name from the most recent EventToolCall; the
	// next EventToolResult is attributed to it. Results follow their calls on
	// this stream and the SSE protocol carries no call-id on results, so this
	// simple last-call pairing is the best available attribution.
	pendingTool string

	// lastFailSig remembers, per tool, the signature of its most recent
	// failure. Tracker keys embed the failure signature, so a SUCCESSFUL
	// result cannot rebuild the failing key from its own (healthy) content —
	// instead the reset replays the remembered key with failed=false, then
	// forgets it. A success for a tool with nothing remembered is a no-op.
	lastFailSig map[string]string
}

// NewStreamGuard builds a StreamGuard from cfg.Guardrails; a nil cfg is
// treated as disabled (the Tracker short-circuits every Record call).
func NewStreamGuard(cfg *config.Config) *StreamGuard {
	enabled := false
	warnAfter, hardAfter := 0, 0
	if cfg != nil {
		enabled = cfg.Guardrails.Enabled
		warnAfter = cfg.Guardrails.WarnAfter
		hardAfter = cfg.Guardrails.HardAfter
	}
	return &StreamGuard{
		tracker:     guardrail.New(warnAfter, hardAfter, enabled),
		lastFailSig: make(map[string]string),
	}
}

// Observe feeds one classified event through the guard. When the tracker
// escalates it returns (synthetic EventWarning event, true); otherwise
// (zero RenderEvent, false). The input event is never modified. A nil
// receiver is inert, so tests that build bare REPL literals stay safe.
func (g *StreamGuard) Observe(ev RenderEvent) (RenderEvent, bool) {
	if g == nil || g.tracker == nil {
		return RenderEvent{}, false
	}
	switch ev.Type {
	case EventToolCall:
		name := ev.Meta["tool"]
		if name == "" {
			name = "unknown"
		}
		g.pendingTool = name

	case EventToolResult:
		tool := g.pendingTool
		if tool == "" {
			tool = "unknown"
		}
		window := ev.Content
		if len(window) > streamGuardWindow {
			window = window[:streamGuardWindow]
		}
		if looksFailing(window) {
			sig := guardrail.Signature(window)
			g.lastFailSig[tool] = sig
			adv := g.tracker.Record("tool:"+tool+"|"+sig, true)
			if adv.Level != guardrail.None {
				return RenderEvent{Type: EventWarning, Content: adv.Msg}, true
			}
		} else if sig, ok := g.lastFailSig[tool]; ok {
			g.tracker.Record("tool:"+tool+"|"+sig, false)
			delete(g.lastFailSig, tool)
		}
	}
	return RenderEvent{}, false
}

// looksFailing reports whether the already window-truncated tool-result
// prefix carries any failureMarkers substring, case-insensitively.
func looksFailing(window string) bool {
	lower := strings.ToLower(window)
	for _, m := range failureMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// ─── internal content extraction ─────────────────────────────────────────────

// extractContentFromData pulls readable text from a raw SSE data string.
// This preserves the full extractText logic: OpenAI delta format, simple JSON
// keys (text/content/message/response), and plain text fallback.
func extractContentFromData(data string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		// OpenAI delta format: choices[0].delta.content
		if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]any); ok {
				if delta, ok := choice["delta"].(map[string]any); ok {
					if content, ok := delta["content"].(string); ok {
						return content
					}
				}
				// Non-streaming: choices[0].text
				if text, ok := choice["text"].(string); ok {
					return text
				}
			}
		}
		// Simple JSON: {"text": "..."}, {"content": "..."}, etc.
		for _, key := range []string{"text", "content", "message", "response"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}

	// Not JSON — plain text chunk
	return data
}

// extractError pulls a human-readable message from an event:error payload.
// It handles {"error":"msg"}, {"error":{"message":"msg"}}, and bare {"message":"msg"};
// non-JSON payloads are returned verbatim so a failure is never rendered blank.
func extractError(data string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return data
	}
	if v, ok := m["error"]; ok {
		switch e := v.(type) {
		case string:
			if e != "" {
				return e
			}
		case map[string]any:
			for _, key := range []string{"message", "content", "text", "detail"} {
				if s, ok := e[key].(string); ok && s != "" {
					return s
				}
			}
		}
	}
	for _, key := range []string{"message", "content", "text", "detail"} {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return data
}

// extractContent tries to pull a text value from JSON data, falling back to the
// raw data if it is not valid JSON.
func extractContent(data string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return data
	}
	for _, key := range []string{"text", "content", "message", "response"} {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

// extractToolCall parses tool call data, returning content and metadata.
func extractToolCall(data string) (string, map[string]string) {
	meta := make(map[string]string)
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		meta["tool"] = data
		return data, meta
	}
	if name, ok := m["name"].(string); ok {
		meta["tool"] = name
	} else if name, ok := m["tool"].(string); ok {
		meta["tool"] = name
	}
	if id, ok := m["id"].(string); ok {
		meta["id"] = id
	}
	content := extractContent(data)
	if content == "" {
		content = meta["tool"]
	}
	return content, meta
}

// extractArtifact parses artifact data, returning content and metadata.
func extractArtifact(data string) (string, map[string]string) {
	meta := make(map[string]string)
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return data, meta
	}
	if id, ok := m["id"].(string); ok {
		meta["id"] = id
	}
	if atype, ok := m["type"].(string); ok {
		meta["type"] = atype
	}
	content := extractContent(data)
	if content == "" {
		if id, ok := meta["id"]; ok {
			content = id
		} else {
			content = data
		}
	}
	return content, meta
}
