package permissions

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ConfirmInteractive prompts `allow <action>? <detail> [y/N] ` on stdout and
// reads a single line from stdin, returning true only when the trimmed
// response is "y" or "yes" (case-insensitive).
//
// If stdin is not attached to a terminal — the CLI is running one-shot,
// piped, or otherwise non-interactively — this does not print the prompt or
// block waiting for input: it returns false immediately, since there is no
// human present to answer. Callers should follow a false return by printing
// Explain(action)'s message, which advises --yolo or permissions.allowed as
// the non-interactive paths forward.
func ConfirmInteractive(action, detail string) bool {
	if !isTerminal(os.Stdin) {
		return false
	}

	fmt.Printf("allow %s? %s [y/N] ", action, detail)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	return strings.EqualFold(line, "y") || strings.EqualFold(line, "yes")
}

// isTerminal reports whether f is attached to a terminal. It is checked at
// call time rather than cached, so tests can swap os.Stdin for a pipe (never
// a terminal) and observe ConfirmInteractive's non-interactive short-circuit.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
