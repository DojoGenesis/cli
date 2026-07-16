package mdrender

import (
	"strings"
	"testing"

	gcolor "github.com/gookit/color"
)

// ─── RenderMarkdown ─────────────────────────────────────────────────────────
//
// RenderMarkdown takes the FULL assembled document (not a stream chunk) and,
// when gcolor.Enable is true, runs it through glamour. These tests exercise
// the exported contract only: with color disabled it's a byte-for-byte
// passthrough (pipeline safety), and with color enabled it actually parses
// the markdown structure rather than just reformatting whitespace around it.

const testMarkdownDoc = "# Heading\n\nSome text.\n\n```go\nfmt.Println(\"hi\")\n```\n"

// withColorEnabled sets gcolor.Enable for the duration of a test and restores
// the previous value afterward — Enable is gookit/color package-level global
// state, not a per-call parameter, so tests that flip it must clean up after
// themselves to avoid bleeding into other tests.
func withColorEnabled(t *testing.T, enabled bool) {
	t.Helper()
	prev := gcolor.Enable
	gcolor.Enable = enabled
	t.Cleanup(func() { gcolor.Enable = prev })
}

func TestRenderMarkdown_ColorDisabled_ReturnsRawVerbatim(t *testing.T) {
	withColorEnabled(t, false)
	got := RenderMarkdown(testMarkdownDoc)
	if got != testMarkdownDoc {
		t.Errorf("RenderMarkdown(gcolor.Enable=false) = %q, want raw input unchanged %q", got, testMarkdownDoc)
	}
}

func TestRenderMarkdown_ColorDisabled_EmptyInput(t *testing.T) {
	withColorEnabled(t, false)
	if got := RenderMarkdown(""); got != "" {
		t.Errorf("RenderMarkdown(\"\") with gcolor.Enable=false = %q, want empty", got)
	}
}

func TestRenderMarkdown_ColorEnabled_DiffersFromRawAndPreservesContent(t *testing.T) {
	withColorEnabled(t, true)
	got := RenderMarkdown(testMarkdownDoc)

	if got == "" {
		t.Fatal("RenderMarkdown(gcolor.Enable=true) returned empty output for a non-empty document")
	}
	if got == testMarkdownDoc {
		t.Fatal("RenderMarkdown(gcolor.Enable=true) returned the raw markdown unchanged — styling did not run")
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

func TestRenderMarkdown_ColorEnabled_HeadingOnly(t *testing.T) {
	withColorEnabled(t, true)
	const headingOnly = "# Just A Heading\n"
	got := RenderMarkdown(headingOnly)
	if got == "" || got == headingOnly {
		t.Errorf("RenderMarkdown(gcolor.Enable=true) on heading-only input = %q, want non-empty and different from raw %q", got, headingOnly)
	}
	if !strings.Contains(got, "Just A Heading") {
		t.Errorf("styled output missing heading text:\n%s", got)
	}
}
