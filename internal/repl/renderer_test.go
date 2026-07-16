package repl

import (
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
)

// ─── ClassifyChunk ──────────────────────────────────────────────────────────

func TestClassifyChunk_PlainText(t *testing.T) {
	chunk := client.SSEChunk{Data: "hello world"}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("plain text: got type %s, want text", ev.Type)
	}
	if ev.Content != "hello world" {
		t.Errorf("plain text: got content %q, want %q", ev.Content, "hello world")
	}
}

func TestClassifyChunk_OpenAIDeltaFormat(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"hello"}}]}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("OpenAI delta: got type %s, want text", ev.Type)
	}
	if ev.Content != "hello" {
		t.Errorf("OpenAI delta: got content %q, want %q", ev.Content, "hello")
	}
}

func TestClassifyChunk_SimpleTextJSON(t *testing.T) {
	data := `{"text":"hello"}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("{text}: got type %s, want text", ev.Type)
	}
	if ev.Content != "hello" {
		t.Errorf("{text}: got content %q, want %q", ev.Content, "hello")
	}
}

func TestClassifyChunk_SimpleContentJSON(t *testing.T) {
	data := `{"content":"world"}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("{content}: got type %s, want text", ev.Type)
	}
	if ev.Content != "world" {
		t.Errorf("{content}: got content %q, want %q", ev.Content, "world")
	}
}

func TestClassifyChunk_MessageJSON(t *testing.T) {
	data := `{"message":"msg value"}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("{message}: got type %s, want text", ev.Type)
	}
	if ev.Content != "msg value" {
		t.Errorf("{message}: got content %q, want %q", ev.Content, "msg value")
	}
}

func TestClassifyChunk_ResponseJSON(t *testing.T) {
	data := `{"response":"resp value"}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("{response}: got type %s, want text", ev.Type)
	}
	if ev.Content != "resp value" {
		t.Errorf("{response}: got content %q, want %q", ev.Content, "resp value")
	}
}

func TestClassifyChunk_ChoiceTextFallback(t *testing.T) {
	data := `{"choices":[{"text":"non-streaming text"}]}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventText {
		t.Errorf("choices[0].text: got type %s, want text", ev.Type)
	}
	if ev.Content != "non-streaming text" {
		t.Errorf("choices[0].text: got content %q, want %q", ev.Content, "non-streaming text")
	}
}

func TestClassifyChunk_DoneSentinel(t *testing.T) {
	chunk := client.SSEChunk{Data: "[DONE]"}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventDone {
		t.Errorf("[DONE]: got type %s, want done", ev.Type)
	}
}

func TestClassifyChunk_EmptyData(t *testing.T) {
	chunk := client.SSEChunk{Data: ""}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventEmpty {
		t.Errorf("empty data: got type %s, want empty", ev.Type)
	}
}

func TestClassifyChunk_WhitespaceOnly(t *testing.T) {
	chunk := client.SSEChunk{Data: "   "}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventEmpty {
		t.Errorf("whitespace only: got type %s, want empty", ev.Type)
	}
}

func TestClassifyChunk_UnknownJSONNoTextKeys(t *testing.T) {
	data := `{"unknown_field":"value"}`
	chunk := client.SSEChunk{Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventEmpty {
		t.Errorf("unknown JSON: got type %s, want empty", ev.Type)
	}
}

func TestClassifyChunk_EventThinking_PlainText(t *testing.T) {
	chunk := client.SSEChunk{Event: "thinking", Data: "let me consider..."}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventThinking {
		t.Errorf("thinking event: got type %s, want thinking", ev.Type)
	}
	if ev.Content != "let me consider..." {
		t.Errorf("thinking event: got content %q, want %q", ev.Content, "let me consider...")
	}
}

func TestClassifyChunk_EventThinking_JSON(t *testing.T) {
	chunk := client.SSEChunk{Event: "thinking", Data: `{"content":"reasoning step"}`}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventThinking {
		t.Errorf("thinking JSON: got type %s, want thinking", ev.Type)
	}
	if ev.Content != "reasoning step" {
		t.Errorf("thinking JSON: got content %q, want %q", ev.Content, "reasoning step")
	}
}

func TestClassifyChunk_EventToolCall(t *testing.T) {
	data := `{"name":"search","id":"tc_123"}`
	chunk := client.SSEChunk{Event: "tool_call", Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventToolCall {
		t.Errorf("tool_call: got type %s, want tool_call", ev.Type)
	}
	if ev.Meta["tool"] != "search" {
		t.Errorf("tool_call: got meta[tool] %q, want %q", ev.Meta["tool"], "search")
	}
	if ev.Meta["id"] != "tc_123" {
		t.Errorf("tool_call: got meta[id] %q, want %q", ev.Meta["id"], "tc_123")
	}
}

func TestClassifyChunk_EventToolCall_ToolKey(t *testing.T) {
	data := `{"tool":"calculator"}`
	chunk := client.SSEChunk{Event: "tool_call", Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventToolCall {
		t.Errorf("tool_call (tool key): got type %s, want tool_call", ev.Type)
	}
	if ev.Meta["tool"] != "calculator" {
		t.Errorf("tool_call (tool key): got meta[tool] %q, want %q", ev.Meta["tool"], "calculator")
	}
}

func TestClassifyChunk_EventToolResult(t *testing.T) {
	chunk := client.SSEChunk{Event: "tool_result", Data: `{"content":"42"}`}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventToolResult {
		t.Errorf("tool_result: got type %s, want tool_result", ev.Type)
	}
	if ev.Content != "42" {
		t.Errorf("tool_result: got content %q, want %q", ev.Content, "42")
	}
}

func TestClassifyChunk_EventArtifact(t *testing.T) {
	data := `{"id":"art_001","type":"code","content":"fmt.Println()"}`
	chunk := client.SSEChunk{Event: "artifact", Data: data}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventArtifact {
		t.Errorf("artifact: got type %s, want artifact", ev.Type)
	}
	if ev.Meta["id"] != "art_001" {
		t.Errorf("artifact: got meta[id] %q, want %q", ev.Meta["id"], "art_001")
	}
	if ev.Content != "fmt.Println()" {
		t.Errorf("artifact: got content %q, want %q", ev.Content, "fmt.Println()")
	}
}

func TestClassifyChunk_EventWarning(t *testing.T) {
	chunk := client.SSEChunk{Event: "warning", Data: "rate limit approaching"}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventWarning {
		t.Errorf("warning: got type %s, want warning", ev.Type)
	}
	if ev.Content != "rate limit approaching" {
		t.Errorf("warning: got content %q, want %q", ev.Content, "rate limit approaching")
	}
}

func TestClassifyChunk_EventDone(t *testing.T) {
	chunk := client.SSEChunk{Event: "done", Data: ""}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventDone {
		t.Errorf("done event: got type %s, want done", ev.Type)
	}
}

func TestClassifyChunk_EventDoneWithData(t *testing.T) {
	chunk := client.SSEChunk{Event: "done", Data: "stream finished"}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventDone {
		t.Errorf("done event with data: got type %s, want done", ev.Type)
	}
}

// ─── Render ─────────────────────────────────────────────────────────────────

func TestRender_Text_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventText, Content: "hello"}
	got := ev.Render(true)
	if got != "hello" {
		t.Errorf("text plain: got %q, want %q", got, "hello")
	}
}

func TestRender_Text_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventText, Content: "hello"}
	got := ev.Render(false)
	// Styled text output is content as-is (no wrapping)
	if got != "hello" {
		t.Errorf("text styled: got %q, want %q", got, "hello")
	}
}

func TestRender_Thinking_Plain_Suppressed(t *testing.T) {
	// Plain/pipeline mode suppresses the reasoning placeholder ("Processing your
	// request…") so stdout carries only the real answer (EventText).
	ev := RenderEvent{Type: EventThinking, Content: "hmm"}
	got := ev.Render(true)
	if got != "" {
		t.Errorf("thinking plain: got %q, want %q (suppressed)", got, "")
	}
}

func TestRender_Thinking_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventThinking, Content: "hmm"}
	got := ev.Render(false)
	// Styled output must contain the content (ANSI escapes may be absent in non-TTY)
	if !strings.Contains(got, "hmm") {
		t.Errorf("thinking styled: output %q does not contain 'hmm'", got)
	}
}

func TestRender_ToolCall_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventToolCall, Content: "search", Meta: map[string]string{"tool": "search"}}
	got := ev.Render(true)
	if got != "[Tool: search]" {
		t.Errorf("tool_call plain: got %q, want %q", got, "[Tool: search]")
	}
}

func TestRender_ToolCall_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventToolCall, Content: "search", Meta: map[string]string{"tool": "search"}}
	got := ev.Render(false)
	if !strings.Contains(got, "[Tool: search]") {
		t.Errorf("tool_call styled: output %q does not contain '[Tool: search]'", got)
	}
}

func TestRender_ToolCall_MissingMeta(t *testing.T) {
	ev := RenderEvent{Type: EventToolCall, Content: "data"}
	got := ev.Render(true)
	if got != "[Tool: unknown]" {
		t.Errorf("tool_call no meta plain: got %q, want %q", got, "[Tool: unknown]")
	}
}

func TestRender_ToolResult_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventToolResult, Content: "42"}
	got := ev.Render(true)
	if got != "42" {
		t.Errorf("tool_result plain: got %q, want %q", got, "42")
	}
}

func TestRender_ToolResult_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventToolResult, Content: "42"}
	got := ev.Render(false)
	// Styled output must contain the content (ANSI escapes may be absent in non-TTY)
	if !strings.Contains(got, "42") {
		t.Errorf("tool_result styled: output %q does not contain '42'", got)
	}
}

func TestRender_Artifact_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventArtifact, Content: "code", Meta: map[string]string{"id": "art_1"}}
	got := ev.Render(true)
	if got != "[Artifact: art_1]" {
		t.Errorf("artifact plain: got %q, want %q", got, "[Artifact: art_1]")
	}
}

func TestRender_Artifact_MissingMeta(t *testing.T) {
	ev := RenderEvent{Type: EventArtifact, Content: "data"}
	got := ev.Render(true)
	if got != "[Artifact: ?]" {
		t.Errorf("artifact no meta plain: got %q, want %q", got, "[Artifact: ?]")
	}
}

func TestRender_Artifact_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventArtifact, Content: "code", Meta: map[string]string{"id": "art_1"}}
	got := ev.Render(false)
	if !strings.Contains(got, "[Artifact: art_1]") {
		t.Errorf("artifact styled: output %q does not contain '[Artifact: art_1]'", got)
	}
}

func TestRender_Warning_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventWarning, Content: "slow"}
	got := ev.Render(true)
	if got != "[warning] slow" {
		t.Errorf("warning plain: got %q, want %q", got, "[warning] slow")
	}
}

func TestRender_Warning_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventWarning, Content: "slow"}
	got := ev.Render(false)
	if !strings.Contains(got, "[warning] slow") {
		t.Errorf("warning styled: output %q does not contain '[warning] slow'", got)
	}
}

func TestRender_Done_ReturnsEmpty(t *testing.T) {
	ev := RenderEvent{Type: EventDone}
	got := ev.Render(false)
	if got != "" {
		t.Errorf("done render: got %q, want empty", got)
	}
}

func TestRender_Empty_ReturnsEmpty(t *testing.T) {
	ev := RenderEvent{Type: EventEmpty}
	got := ev.Render(true)
	if got != "" {
		t.Errorf("empty render: got %q, want empty", got)
	}
}

// ─── EventType.String ───────────────────────────────────────────────────────

func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   EventType
		want string
	}{
		{EventText, "text"},
		{EventThinking, "thinking"},
		{EventToolCall, "tool_call"},
		{EventToolResult, "tool_result"},
		{EventArtifact, "artifact"},
		{EventWarning, "warning"},
		{EventDone, "done"},
		{EventEmpty, "empty"},
		{EventError, "error"},
		{EventType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.et.String(); got != tt.want {
			t.Errorf("EventType(%d).String() = %q, want %q", tt.et, got, tt.want)
		}
	}
}

// ─── Backward compatibility: ClassifyChunk produces same text as extractText ─

func TestClassifyChunk_MatchesExtractText(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"OpenAI delta", `{"choices":[{"delta":{"content":"hello"}}]}`, "hello"},
		{"simple text", `{"text":"hello"}`, "hello"},
		{"simple content", `{"content":"world"}`, "world"},
		{"message field", `{"message":"msg value"}`, "msg value"},
		{"response field", `{"response":"resp value"}`, "resp value"},
		{"choices[0].text", `{"choices":[{"text":"non-streaming text"}]}`, "non-streaming text"},
		{"[DONE]", "[DONE]", ""},
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"unknown JSON", `{"unknown_field":"value"}`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk := client.SSEChunk{Data: tc.data}
			ev := ClassifyChunk(chunk)
			rendered := ev.Render(true) // plain mode = no ANSI, matches extractText output
			if rendered != tc.want {
				t.Errorf("ClassifyChunk+Render(true) for %q: got %q, want %q", tc.data, rendered, tc.want)
			}
		})
	}
}

// ─── Error events (P0: failures must be observable, never blank successes) ────

func TestClassifyChunk_EventError_MapsToEventError(t *testing.T) {
	// The failing-gateway repro: event:error / data:{"error":"...429..."}.
	chunk := client.SSEChunk{Event: "error", Data: `{"error":"openai: 429 insufficient_quota"}`}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventError {
		t.Fatalf("event:error: got type %s, want error", ev.Type)
	}
	if ev.Content != "openai: 429 insufficient_quota" {
		t.Errorf("event:error: got content %q, want the extracted error string", ev.Content)
	}
}

func TestClassifyChunk_EventError_NestedMessage(t *testing.T) {
	chunk := client.SSEChunk{Event: "error", Data: `{"error":{"message":"model not found (404)","code":"dead_model"}}`}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventError {
		t.Fatalf("nested error: got type %s, want error", ev.Type)
	}
	if ev.Content != "model not found (404)" {
		t.Errorf("nested error: got content %q, want %q", ev.Content, "model not found (404)")
	}
}

func TestClassifyChunk_EventError_NonJSONRaw(t *testing.T) {
	chunk := client.SSEChunk{Event: "error", Data: "upstream exploded"}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventError {
		t.Fatalf("raw error: got type %s, want error", ev.Type)
	}
	if ev.Content != "upstream exploded" {
		t.Errorf("raw error: got content %q, want raw passthrough", ev.Content)
	}
}

func TestClassifyChunk_EventError_EmptyNeverBlank(t *testing.T) {
	// An error event with no usable message must still classify as EventError
	// with a non-empty, visible message — never a silent EventEmpty.
	chunk := client.SSEChunk{Event: "error", Data: `{"error":""}`}
	ev := ClassifyChunk(chunk)
	if ev.Type != EventError {
		t.Fatalf("empty error: got type %s, want error", ev.Type)
	}
	if ev.Content == "" {
		t.Error("empty error: content must never be blank")
	}
}

func TestRender_Error_Plain(t *testing.T) {
	ev := RenderEvent{Type: EventError, Content: "429 quota exceeded"}
	got := ev.Render(true)
	if got != "error: 429 quota exceeded" {
		t.Errorf("error plain: got %q, want %q", got, "error: 429 quota exceeded")
	}
}

func TestRender_Error_Styled(t *testing.T) {
	ev := RenderEvent{Type: EventError, Content: "429 quota exceeded"}
	got := ev.Render(false)
	if !strings.Contains(got, "error: 429 quota exceeded") {
		t.Errorf("error styled: output %q does not contain the message", got)
	}
}

func TestRenderJSON_Error_Emitted(t *testing.T) {
	ev := RenderEvent{Type: EventError, Content: "boom"}
	got := ev.RenderJSON()
	if !strings.Contains(got, `"type":"error"`) || !strings.Contains(got, `"content":"boom"`) {
		t.Errorf("error JSON: got %q, want a {type:error, content:boom} object", got)
	}
}

func TestRenderJSON_Thinking_Suppressed(t *testing.T) {
	// Reasoning placeholder must not pollute the --json pipeline.
	ev := RenderEvent{Type: EventThinking, Content: "Processing your request..."}
	if got := ev.RenderJSON(); got != "" {
		t.Errorf("thinking JSON: got %q, want empty (suppressed)", got)
	}
}

func TestClassifyChunk_RoutingTelemetry_Dropped(t *testing.T) {
	for _, name := range []string{"intent_classified", "provider_selected"} {
		chunk := client.SSEChunk{Event: name, Data: `{"intent":"chat","provider":"openai"}`}
		ev := ClassifyChunk(chunk)
		if ev.Type != EventEmpty {
			t.Errorf("%s: got type %s, want empty (dropped as noise)", name, ev.Type)
		}
	}
}

func TestExtractError_Variants(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"string error", `{"error":"boom"}`, "boom"},
		{"nested message", `{"error":{"message":"nested boom"}}`, "nested boom"},
		{"bare message", `{"message":"bare boom"}`, "bare boom"},
		{"non-json", "plain boom", "plain boom"},
	}
	for _, tc := range cases {
		if got := extractError(tc.data); got != tc.want {
			t.Errorf("extractError(%q) = %q, want %q", tc.data, got, tc.want)
		}
	}
}

// ─── RenderMarkdown ─────────────────────────────────────────────────────────
//
// RenderMarkdown takes the FULL assembled message (not a stream chunk) and,
// when !plain, runs it through glamour. These tests exercise the exported
// contract only: plain mode is a byte-for-byte passthrough (pipeline
// safety), and styled mode actually parses the markdown structure rather
// than just reformatting whitespace around it.

const testMarkdownDoc = "# Heading\n\nSome text.\n\n```go\nfmt.Println(\"hi\")\n```\n"

func TestRenderMarkdown_Plain_ReturnsRawVerbatim(t *testing.T) {
	got := RenderMarkdown(testMarkdownDoc, true)
	if got != testMarkdownDoc {
		t.Errorf("RenderMarkdown(plain=true) = %q, want raw input unchanged %q", got, testMarkdownDoc)
	}
}

func TestRenderMarkdown_Plain_EmptyInput(t *testing.T) {
	if got := RenderMarkdown("", true); got != "" {
		t.Errorf("RenderMarkdown(\"\", true) = %q, want empty", got)
	}
}

func TestRenderMarkdown_Styled_DiffersFromRawAndPreservesContent(t *testing.T) {
	got := RenderMarkdown(testMarkdownDoc, false)

	if got == "" {
		t.Fatal("RenderMarkdown(plain=false) returned empty output for a non-empty document")
	}
	if got == testMarkdownDoc {
		t.Fatal("RenderMarkdown(plain=false) returned the raw markdown unchanged — styling did not run")
	}

	// The heading text and fenced code-block content must survive styling.
	if !strings.Contains(got, "Heading") {
		t.Errorf("styled output missing heading text %q:\n%s", "Heading", got)
	}
	if !strings.Contains(got, `fmt.Println("hi")`) {
		t.Errorf("styled output missing code-block content %q:\n%s", `fmt.Println("hi")`, got)
	}

	// A real markdown render (not just a whitespace/ANSI wrap of the raw
	// bytes) parses the fence into a code block and drops the ``` markers —
	// their absence is positive evidence that glamour actually ran.
	if strings.Contains(got, "```") {
		t.Errorf("styled output still contains raw fence markers, glamour did not parse the code block:\n%s", got)
	}
}

func TestRenderMarkdown_Styled_HeadingOnly(t *testing.T) {
	const headingOnly = "# Just A Heading\n"
	got := RenderMarkdown(headingOnly, false)
	if got == "" || got == headingOnly {
		t.Errorf("RenderMarkdown(plain=false) on heading-only input = %q, want non-empty and different from raw %q", got, headingOnly)
	}
	if !strings.Contains(got, "Just A Heading") {
		t.Errorf("styled output missing heading text:\n%s", got)
	}
}
