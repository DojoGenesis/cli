// Package mdrender renders complete markdown documents for terminal display.
//
// It exists to break an import cycle: this logic originally lived in package
// repl (Wave 5), but the natural static-document call sites — /skill get and
// /doc — live in internal/commands, and commands cannot import repl (repl
// already imports commands). mdrender imports nothing from repl or commands,
// so both can depend on it without a cycle.
package mdrender

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	gcolor "github.com/gookit/color"
	"golang.org/x/term"
)

// RenderMarkdown renders a FULLY ASSEMBLED markdown document as styled
// terminal output (fenced code blocks, headings, lists, etc.).
//
// Callers must pass the complete document text, not a partial chunk —
// glamour parses markdown structurally and cannot render a partial document.
// This makes RenderMarkdown a fit for static, fully-fetched content (a
// SKILL.md body, a /doc response) rather than per-chunk streaming output.
//
// Styling is gated on gookit/color's package-level gcolor.Enable, which the
// CLI's entrypoint sets to false for --no-color, --plain, or NO_COLOR. When
// Enable is false, full is returned verbatim so piped/CI/scripted consumers
// always see raw markdown text, never ANSI escapes.
func RenderMarkdown(full string) string {
	if !gcolor.Enable {
		return full
	}

	mdRenderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(terminalWidth()),
	)
	if err != nil {
		// Styling failure must never eat the response — fall back to raw text.
		return full
	}

	out, err := mdRenderer.Render(full)
	if err != nil {
		return full
	}

	// glamour's document style brackets output in a blank-line block_prefix/
	// suffix plus a left margin; trim the outer blank lines so the result
	// composes cleanly with the caller's own spacing.
	return strings.TrimSpace(out)
}

// terminalWidth returns the current terminal's column width for glamour's
// word-wrap, falling back to a sane default when stdout isn't a TTY (piped
// output, CI, or `go test`).
func terminalWidth() int {
	const fallbackWidth = 80
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return fallbackWidth
	}
	return w
}
