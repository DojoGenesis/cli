package repl

// renderer_guardrail_test.go — tests for StreamGuard (renderer.go), the
// advisory repeated-tool-failure watch over classified agent-stream events.
// NEW file per the advancement-wave ownership rules; renderer_test.go and the
// other existing test files are not modified.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guardrail"
)

func guardCfg(warnAfter, hardAfter int, enabled bool) *config.Config {
	return &config.Config{
		Guardrails: config.GuardrailsConfig{Enabled: enabled, WarnAfter: warnAfter, HardAfter: hardAfter},
	}
}

// toolCallEv classifies a synthetic tool_call chunk for the named tool.
func toolCallEv(tool string) RenderEvent {
	return ClassifyChunk(client.SSEChunk{Event: "tool_call", Data: fmt.Sprintf(`{"name":%q}`, tool)})
}

// toolResultEv classifies a synthetic tool_result chunk carrying raw content.
func toolResultEv(content string) RenderEvent {
	return ClassifyChunk(client.SSEChunk{Event: "tool_result", Data: content})
}

func streamWarnMsg(key string, n int) string {
	return fmt.Sprintf("guardrail: %s has failed %d times consecutively with the same signature — consider a different approach", key, n)
}

func streamHardMsg(key string, n int) string {
	return fmt.Sprintf("guardrail: %s is stuck (%d consecutive identical failures) — stop and change strategy, or check config vs code (see debugging gate)", key, n)
}

// TestStreamGuard_WarnAndHardThresholds feeds repeated failing ToolResults
// with an identical signature and asserts the advisory Warning appears
// exactly at WarnAfter, then at and after HardAfter — and that the original
// events pass through Observe untouched.
func TestStreamGuard_WarnAndHardThresholds(t *testing.T) {
	g := NewStreamGuard(guardCfg(3, 5, true))

	failure := "Error: connection refused"
	key := "tool:web_fetch|" + guardrail.Signature(failure)

	// The tool_call itself must never produce advice.
	call := toolCallEv("web_fetch")
	if _, ok := g.Observe(call); ok {
		t.Fatal("Observe(tool_call) produced advice; want none")
	}

	wants := []struct {
		name    string
		wantMsg string // "" = no advice
		wantLvl EventType
	}{
		{"failure 1", "", EventEmpty},
		{"failure 2", "", EventEmpty},
		{"failure 3: warn at WarnAfter", streamWarnMsg(key, 3), EventWarning},
		{"failure 4: between", "", EventEmpty},
		{"failure 5: hard at HardAfter", streamHardMsg(key, 5), EventWarning},
		{"failure 6: hard keeps firing", streamHardMsg(key, 6), EventWarning},
	}
	for _, step := range wants {
		ev := toolResultEv(failure)
		orig := RenderEvent{Type: ev.Type, Content: ev.Content, Meta: ev.Meta}
		adv, ok := g.Observe(ev)
		if !reflect.DeepEqual(ev, orig) {
			t.Fatalf("%s: Observe mutated the input event: %+v -> %+v", step.name, orig, ev)
		}
		if step.wantMsg == "" {
			if ok {
				t.Fatalf("%s: unexpected advice %+v", step.name, adv)
			}
			continue
		}
		if !ok {
			t.Fatalf("%s: no advice; want %q", step.name, step.wantMsg)
		}
		if adv.Type != EventWarning {
			t.Fatalf("%s: advice type = %s; want warning (must reuse the Warning rendering path)", step.name, adv.Type)
		}
		if adv.Content != step.wantMsg {
			t.Fatalf("%s: advice = %q; want %q", step.name, adv.Content, step.wantMsg)
		}
		// The advisory line renders through the existing Warning path.
		if plain := adv.Render(true); plain != "[warning] "+step.wantMsg {
			t.Fatalf("%s: plain render = %q; want %q", step.name, plain, "[warning] "+step.wantMsg)
		}
	}
}

// TestStreamGuard_ResetOnSuccess verifies a healthy-looking ToolResult for a
// tool resets that tool's remembered failing streak (via the last-failure
// signature bookkeeping), and that a success with no tracked failure is a
// harmless no-op.
func TestStreamGuard_ResetOnSuccess(t *testing.T) {
	g := NewStreamGuard(guardCfg(2, 4, true))

	// Success before any failure: nothing tracked, nothing fires, no panic.
	g.Observe(toolCallEv("web_fetch"))
	if _, ok := g.Observe(toolResultEv("fetched 200 OK")); ok {
		t.Fatal("success with no tracked failure produced advice")
	}

	failure := "Error: connection refused"
	key := "tool:web_fetch|" + guardrail.Signature(failure)

	g.Observe(toolResultEv(failure)) // streak 1
	if adv, ok := g.Observe(toolResultEv(failure)); !ok || adv.Content != streamWarnMsg(key, 2) {
		t.Fatalf("2nd failure: advice = %+v ok=%v; want warn %q", adv, ok, streamWarnMsg(key, 2))
	}

	// Success resets the streak under the remembered failing key.
	if _, ok := g.Observe(toolResultEv("fetched 200 OK")); ok {
		t.Fatal("resetting success produced advice")
	}

	// After reset the streak restarts: 1 then warn again at 2.
	if _, ok := g.Observe(toolResultEv(failure)); ok {
		t.Fatal("1st failure after reset produced advice; streak did not reset")
	}
	if adv, ok := g.Observe(toolResultEv(failure)); !ok || adv.Content != streamWarnMsg(key, 2) {
		t.Fatalf("2nd failure after reset: advice = %+v ok=%v; want warn %q", adv, ok, streamWarnMsg(key, 2))
	}
}

// TestStreamGuard_FailureHeuristic tables the conservative failure detector:
// case-insensitive "error"/"failed"/"exception" within the first 200 bytes.
func TestStreamGuard_FailureHeuristic(t *testing.T) {
	tests := []struct {
		name    string
		content string
		failing bool
	}{
		{"lowercase error", "error: no route to host", true},
		{"capitalized Error", "Error: connection refused", true},
		{"uppercase FAILED", "FAILED to connect after 3 attempts", true},
		{"mixed-case Exception", "Unhandled Exception in worker", true},
		{"healthy result", "fetched 3 documents, 200 OK", false},
		{"marker beyond the 200-byte window", strings.Repeat("a", 200) + " error", false},
		{"marker inside the 200-byte window", strings.Repeat("a", 190) + " error", true},
		{"empty result", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh guard with warnAfter=1 so a single detected failure fires
			// Warn immediately — turning the detector itself into the assert.
			g := NewStreamGuard(guardCfg(1, 3, true))
			g.Observe(toolCallEv("web_fetch"))
			_, ok := g.Observe(toolResultEv(tc.content))
			if ok != tc.failing {
				t.Fatalf("content %q: detected failing = %v; want %v", tc.content, ok, tc.failing)
			}
		})
	}
}

// TestStreamGuard_ToolAttribution verifies keys carry the tool name from the
// preceding ToolCall, different tools track independently, and a result with
// no preceding call falls back to "unknown".
func TestStreamGuard_ToolAttribution(t *testing.T) {
	failure := "error: boom"

	// No preceding tool_call → attributed to "unknown".
	g := NewStreamGuard(guardCfg(1, 3, true))
	adv, ok := g.Observe(toolResultEv(failure))
	wantKey := "tool:unknown|" + guardrail.Signature(failure)
	if !ok || adv.Content != streamWarnMsg(wantKey, 1) {
		t.Fatalf("orphan result: advice = %+v ok=%v; want warn %q", adv, ok, streamWarnMsg(wantKey, 1))
	}

	// Two tools with identical error text keep independent streaks.
	g2 := NewStreamGuard(guardCfg(2, 4, true))
	g2.Observe(toolCallEv("alpha"))
	g2.Observe(toolResultEv(failure)) // alpha streak 1
	g2.Observe(toolCallEv("beta"))
	if _, ok := g2.Observe(toolResultEv(failure)); ok { // beta streak 1
		t.Fatal("beta's first failure fired advice; tools must track independently")
	}
	g2.Observe(toolCallEv("alpha"))
	adv2, ok2 := g2.Observe(toolResultEv(failure)) // alpha streak 2 → warn
	alphaKey := "tool:alpha|" + guardrail.Signature(failure)
	if !ok2 || adv2.Content != streamWarnMsg(alphaKey, 2) {
		t.Fatalf("alpha 2nd failure: advice = %+v ok=%v; want warn %q", adv2, ok2, streamWarnMsg(alphaKey, 2))
	}
}

// TestStreamGuard_DisabledNilCfgNilReceiver verifies every inert mode: config
// disabled, nil config, and nil *StreamGuard receiver (bare REPL literals in
// existing tests have a nil streamGuard field).
func TestStreamGuard_DisabledNilCfgNilReceiver(t *testing.T) {
	failure := "error: boom"

	for _, tc := range []struct {
		name string
		g    *StreamGuard
	}{
		{"disabled via config", NewStreamGuard(guardCfg(1, 2, false))},
		{"nil config", NewStreamGuard(nil)},
		{"nil receiver", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.g.Observe(toolCallEv("web_fetch"))
			for i := 0; i < 5; i++ {
				if adv, ok := tc.g.Observe(toolResultEv(failure)); ok {
					t.Fatalf("inert guard produced advice %+v", adv)
				}
			}
		})
	}
}

// TestStreamGuard_NonToolEventsIgnored verifies text/thinking/warning/done
// events flow past the guard without advice or state changes.
func TestStreamGuard_NonToolEventsIgnored(t *testing.T) {
	g := NewStreamGuard(guardCfg(1, 3, true))
	events := []RenderEvent{
		{Type: EventText, Content: "error in your code is likely"}, // text mentioning "error" is NOT a tool failure
		{Type: EventThinking, Content: "thinking..."},
		{Type: EventWarning, Content: "some upstream warning"},
		{Type: EventError, Content: "gateway exploded"},
		{Type: EventDone},
		{Type: EventEmpty},
	}
	for _, ev := range events {
		if adv, ok := g.Observe(ev); ok {
			t.Fatalf("non-tool event %s produced advice %+v", ev.Type, adv)
		}
	}
}
