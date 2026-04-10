// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DojoGenesis/dojo-cli/internal/client"
)

// ─── Event Classification ───────────────────────────────────────────────────

// EventCategory groups events by their functional domain.
type EventCategory int

const (
	CategoryCore          EventCategory = iota // chat lifecycle events
	CategoryTrace                              // observability spans
	CategoryArtifact                           // file / diagram outputs
	CategoryOrchestration                      // DAG execution events
)

// EventSeverity indicates visual urgency for color-coding.
type EventSeverity int

const (
	SeverityInfo    EventSeverity = iota // default / dim
	SeveritySuccess                      // green
	SeverityWarning                      // yellow
	SeverityError                        // red
)

// ─── Parsed Event ───────────────────────────────────────────────────────────

// ParsedEvent is a typed, human-readable representation of a raw SSE event.
// It replaces the former opaque eventEntry in the Pilot dashboard.
type ParsedEvent struct {
	Time      string         // HH:MM:SS
	EventType string         // raw event type string
	Category  EventCategory  // functional domain
	Severity  EventSeverity  // visual urgency
	Summary   string         // human-readable one-liner
	RawData   string         // original JSON for drill-down
	Parsed    map[string]any // decoded JSON payload
}

// ─── Parser ─────────────────────────────────────────────────────────────────

// ParseSSEEvent transforms a raw client.SSEChunk into a typed, summarised
// ParsedEvent ready for display in the Pilot dashboard.
func ParseSSEEvent(chunk client.SSEChunk) ParsedEvent {
	pe := ParsedEvent{
		Time:      time.Now().Format("15:04:05"),
		EventType: chunk.Event,
		RawData:   chunk.Data,
		Parsed:    make(map[string]any),
	}

	// Attempt JSON decode; non-fatal if it fails.
	_ = json.Unmarshal([]byte(chunk.Data), &pe.Parsed)

	switch chunk.Event {

	// ── Core chat lifecycle ─────────────────────────────────────────────

	case "intent_classified":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		intent := getStr(pe.Parsed, "intent")
		confidence := getFloat(pe.Parsed, "confidence")
		pe.Summary = fmt.Sprintf("Intent: %s (%.0f%%)", intent, confidence*100)

	case "provider_selected":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		provider := getStr(pe.Parsed, "provider")
		model := getStr(pe.Parsed, "model")
		pe.Summary = fmt.Sprintf("Provider: %s/%s", provider, model)

	case "tool_invoked":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		tool := getStr(pe.Parsed, "tool")
		pe.Summary = fmt.Sprintf("\u25b6 Tool: %s", tool)

	case "tool_completed":
		pe.Category = CategoryCore
		tool := getStr(pe.Parsed, "tool")
		dur := getFloat(pe.Parsed, "duration_ms")
		if isTruthy(pe.Parsed, "error") || getStr(pe.Parsed, "status") == "failed" {
			pe.Severity = SeverityError
			pe.Summary = fmt.Sprintf("\u2717 Tool: %s FAILED", tool)
		} else {
			pe.Severity = SeveritySuccess
			pe.Summary = fmt.Sprintf("\u2713 Tool: %s (%.0fms)", tool, dur)
		}

	case "thinking":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		msg := getStr(pe.Parsed, "message")
		if len(msg) > 80 {
			msg = msg[:80]
		}
		pe.Summary = fmt.Sprintf("\u2026 %s", msg)

	case "response_chunk":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		content := getStr(pe.Parsed, "content")
		pe.Summary = fmt.Sprintf("\u21b3 chunk (%d chars)", len(content))

	case "memory_retrieved":
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		count := getFloat(pe.Parsed, "memories_found")
		pe.Summary = fmt.Sprintf("Memory: %.0f items retrieved", count)

	case "complete":
		pe.Category = CategoryCore
		pe.Severity = SeveritySuccess
		tokIn := getFloat(pe.Parsed, "tokens_in")
		tokOut := getFloat(pe.Parsed, "tokens_out")
		pe.Summary = fmt.Sprintf("Complete: %.0f\u2192%.0f tokens", tokIn, tokOut)

	case "error":
		pe.Category = CategoryCore
		pe.Severity = SeverityError
		code := getStr(pe.Parsed, "error_code")
		msg := getStr(pe.Parsed, "error")
		pe.Summary = fmt.Sprintf("ERROR [%s]: %s", code, msg)

	// ── Trace / observability ───────────────────────────────────────────

	case "trace_span_start":
		pe.Category = CategoryTrace
		pe.Severity = SeverityInfo
		name := getStr(pe.Parsed, "name")
		pe.Summary = fmt.Sprintf("Span: %s started", name)

	case "trace_span_end":
		pe.Category = CategoryTrace
		name := getStr(pe.Parsed, "name")
		dur := getFloat(pe.Parsed, "duration_ms")
		status := getStr(pe.Parsed, "status")
		if status == "error" || status == "failed" {
			pe.Severity = SeverityError
		} else {
			pe.Severity = SeveritySuccess
		}
		pe.Summary = fmt.Sprintf("Span: %s (%.0fms) [%s]", name, dur, status)

	// ── Artifacts ────────────────────────────────────────────────────────

	case "artifact_created":
		pe.Category = CategoryArtifact
		pe.Severity = SeveritySuccess
		name := getStr(pe.Parsed, "artifact_name")
		atype := getStr(pe.Parsed, "artifact_type")
		pe.Summary = fmt.Sprintf("Artifact: %s (%s)", name, atype)

	case "artifact_updated":
		pe.Category = CategoryArtifact
		pe.Severity = SeverityInfo
		name := getStr(pe.Parsed, "artifact_name")
		ver := getStr(pe.Parsed, "version")
		pe.Summary = fmt.Sprintf("Artifact: %s v%s", name, ver)

	case "project_switched":
		pe.Category = CategoryArtifact
		pe.Severity = SeverityInfo
		proj := getStr(pe.Parsed, "project_name")
		pe.Summary = fmt.Sprintf("Project: %s", proj)

	case "diagram_rendered":
		pe.Category = CategoryArtifact
		pe.Severity = SeveritySuccess
		dtype := getStr(pe.Parsed, "diagram_type")
		dfmt := getStr(pe.Parsed, "format")
		pe.Summary = fmt.Sprintf("Diagram: %s (%s)", dtype, dfmt)

	case "patch_intent":
		pe.Category = CategoryArtifact
		pe.Severity = SeverityInfo
		op := getStr(pe.Parsed, "operation")
		desc := getStr(pe.Parsed, "description")
		if len(desc) > 60 {
			desc = desc[:60]
		}
		pe.Summary = fmt.Sprintf("Patch: %s \u2014 %s", op, desc)

	// ── Orchestration / DAG ─────────────────────────────────────────────

	case "orchestration_plan_created":
		pe.Category = CategoryOrchestration
		pe.Severity = SeverityInfo
		nodes := getFloat(pe.Parsed, "node_count")
		cost := getFloat(pe.Parsed, "estimated_cost")
		pe.Summary = fmt.Sprintf("DAG: %.0f nodes, est $%.4f", nodes, cost)

	case "orchestration_node_start":
		pe.Category = CategoryOrchestration
		pe.Severity = SeverityInfo
		tool := getStr(pe.Parsed, "tool_name")
		pe.Summary = fmt.Sprintf("DAG\u25b6 %s", tool)

	case "orchestration_node_end":
		pe.Category = CategoryOrchestration
		tool := getStr(pe.Parsed, "tool_name")
		dur := getFloat(pe.Parsed, "duration_ms")
		if isTruthy(pe.Parsed, "error") || getStr(pe.Parsed, "status") == "failed" {
			pe.Severity = SeverityError
			pe.Summary = fmt.Sprintf("DAG\u2717 %s FAILED", tool)
		} else {
			pe.Severity = SeveritySuccess
			pe.Summary = fmt.Sprintf("DAG\u2713 %s (%.0fms)", tool, dur)
		}

	case "orchestration_replanning":
		pe.Category = CategoryOrchestration
		pe.Severity = SeverityWarning
		reason := getStr(pe.Parsed, "reason")
		pe.Summary = fmt.Sprintf("DAG\u21bb replanning: %s", reason)

	case "orchestration_complete":
		pe.Category = CategoryOrchestration
		pe.Severity = SeveritySuccess
		succ := getFloat(pe.Parsed, "success_nodes")
		total := getFloat(pe.Parsed, "total_nodes")
		dur := getFloat(pe.Parsed, "duration_ms")
		pe.Summary = fmt.Sprintf("DAG\u2713 %.0f/%.0f nodes (%.0fms)", succ, total, dur)

	case "orchestration_failed":
		pe.Category = CategoryOrchestration
		pe.Severity = SeverityError
		reason := getStr(pe.Parsed, "reason")
		pe.Summary = fmt.Sprintf("DAG\u2717 FAILED: %s", reason)

	// ── Unknown event type — fall back to raw data ──────────────────────

	default:
		pe.Category = CategoryCore
		pe.Severity = SeverityInfo
		preview := chunk.Data
		if len(preview) > 80 {
			preview = preview[:80]
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		if chunk.Event != "" {
			pe.Summary = fmt.Sprintf("[%s] %s", chunk.Event, preview)
		} else {
			pe.Summary = preview
		}
	}

	return pe
}

// ─── JSON helpers ───────────────────────────────────────────────────────────

// getStr safely extracts a string value from a decoded JSON map.
func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// getFloat safely extracts a numeric value from a decoded JSON map.
// JSON numbers are decoded as float64 by encoding/json.
func getFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, ok := v.(float64)
	if ok {
		return f
	}
	return 0
}

// isTruthy returns true when the key exists and is a non-empty, non-false value.
func isTruthy(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != ""
	case float64:
		return val != 0
	case nil:
		return false
	default:
		return true
	}
}
