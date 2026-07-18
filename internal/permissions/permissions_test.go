package permissions

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// captureStderr redirects os.Stderr to a pipe for the duration of f and
// returns everything written to it. Used to observe (or rule out) the YOLO
// warning without depending on go test's own stderr capture behavior.
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stderr = w
	f()
	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		panic(err)
	}
	_ = r.Close()
	return buf.String()
}

// captureStdout is captureStderr's stdout counterpart, used to confirm
// ConfirmInteractive prints nothing when it short-circuits.
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		panic(err)
	}
	_ = r.Close()
	return buf.String()
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		allowed []string
		action  string
		want    Decision
	}{
		// --- default mode ---
		{"default/unmatched, no allowed -> confirm", "default", nil, "code.undo", Confirm},
		{"default/exact match -> allow", "default", []string{"code.undo"}, "code.undo", Allow},
		{"default/exact mismatch -> confirm", "default", []string{"code.undo"}, "code.redo", Confirm},
		{"default/glob prefix match (scaffold) -> allow", "default", []string{"craft.*"}, "craft.scaffold", Allow},
		{"default/glob prefix match (go-service) -> allow", "default", []string{"craft.*"}, "craft.go-service", Allow},
		{"default/glob prefix mismatch -> confirm", "default", []string{"craft.*"}, "plugin.install", Confirm},
		{"default/bare star matches everything -> allow", "default", []string{"*"}, "plugin.install", Allow},

		// --- allowlist mode ---
		{"allowlist/unmatched, no allowed -> deny", "allowlist", nil, "code.undo", Deny},
		{"allowlist/exact match -> allow", "allowlist", []string{"code.undo"}, "code.undo", Allow},
		{"allowlist/exact mismatch -> deny", "allowlist", []string{"code.undo"}, "code.redo", Deny},
		{"allowlist/glob prefix match (scaffold) -> allow", "allowlist", []string{"craft.*"}, "craft.scaffold", Allow},
		{"allowlist/glob prefix match (go-service) -> allow", "allowlist", []string{"craft.*"}, "craft.go-service", Allow},
		{"allowlist/glob prefix mismatch -> deny", "allowlist", []string{"craft.*"}, "plugin.install", Deny},
		{"allowlist/bare star matches everything -> allow", "allowlist", []string{"*"}, "plugin.install", Allow},
		{"allowlist/second pattern in list matches -> allow", "allowlist", []string{"plugin.install", "craft.*"}, "craft.scaffold", Allow},
		{"allowlist/no pattern in list matches -> deny", "allowlist", []string{"plugin.install", "craft.*"}, "code.undo", Deny},

		// --- yolo mode: always Allow, allowed is irrelevant ---
		{"yolo/allow with no allowed list", "yolo", nil, "plugin.install", Allow},
		{"yolo/allow even when action not in allowed", "yolo", []string{"code.undo"}, "plugin.install", Allow},

		// --- unknown / unset mode falls back to default semantics ---
		{"empty mode behaves as default: matched -> allow", "", []string{"code.undo"}, "code.undo", Allow},
		{"empty mode behaves as default: unmatched -> confirm", "", nil, "code.undo", Confirm},
		{"unrecognized mode behaves as default: matched -> allow", "banana", []string{"code.undo"}, "code.undo", Allow},
		{"unrecognized mode behaves as default: unmatched -> confirm", "banana", nil, "code.undo", Confirm},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Route stderr to a discard pipe for yolo cases so the warning
			// (if the once hasn't already fired process-wide) doesn't spam
			// test output; correctness of the warning itself is covered by
			// TestCheck_YoloWarnsOnlyOnce below.
			var got Decision
			captureStderr(func() {
				got = Check(tt.mode, tt.allowed, tt.action)
			})
			if got != tt.want {
				t.Errorf("Check(%q, %v, %q) = %s, want %s", tt.mode, tt.allowed, tt.action, got, tt.want)
			}
		})
	}
}

func TestCheck_YoloWarnsOnlyOnce(t *testing.T) {
	// Reset the package-level guard so this test observes a clean first
	// call, independent of whatever earlier subtests in this process (e.g.
	// TestCheck's yolo cases) already exercised yolo mode.
	yoloWarnOnce = sync.Once{}

	const wantMsg = "permissions: YOLO mode active — skipping all confirmations"

	first := captureStderr(func() {
		Check("yolo", nil, "plugin.install")
	})
	if !strings.Contains(first, wantMsg) {
		t.Fatalf("first yolo Check() stderr = %q, want it to contain %q", first, wantMsg)
	}

	second := captureStderr(func() {
		Check("yolo", []string{"anything"}, "some.other.action")
	})
	if second != "" {
		t.Fatalf("second yolo Check() in the same process wrote %q to stderr, want silence (warn-once)", second)
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		action  string
		want    bool
	}{
		{"code.undo", "code.undo", true},
		{"code.undo", "code.redo", false},
		{"*", "anything.at.all", true},
		{"*", "", true},
		{"craft.*", "craft.scaffold", true},
		{"craft.*", "craft.go-service", true},
		{"craft.*", "craftx.scaffold", false}, // no dot before the differing char: must not match
		{"craft.*", "plugin.install", false},
		{"craft.*", "craft", false}, // missing the separator dot entirely
		{"", "anything", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q~%q", tt.pattern, tt.action), func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.action)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.action, got, tt.want)
			}
		})
	}
}

func TestExplain(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{
			action: "plugin.install",
			want:   `permission denied: "plugin.install" (add to permissions.allowed in ~/.dojo/settings.json, or run with --yolo)`,
		},
		{
			action: "code.undo",
			want:   `permission denied: "code.undo" (add to permissions.allowed in ~/.dojo/settings.json, or run with --yolo)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			if got := Explain(tt.action); got != tt.want {
				t.Errorf("Explain(%q) = %q, want %q", tt.action, got, tt.want)
			}
		})
	}
}

// withNonTTYStdin swaps os.Stdin for an os.Pipe read-end (never a terminal)
// for the duration of f, writing body to the pipe first (or closing it
// immediately if body is empty) so any accidental read has deterministic
// content to observe.
func withNonTTYStdin(t *testing.T, body string, f func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if body != "" {
		if _, err := w.WriteString(body); err != nil {
			t.Fatalf("write to pipe: %v", err)
		}
	}
	_ = w.Close()

	old := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = old
		_ = r.Close()
	}()

	f()
}

func TestConfirmInteractive_NonTTYShortCircuits(t *testing.T) {
	// A "y" answer sits ready in the pipe. If ConfirmInteractive's TTY check
	// were broken (e.g. it read stdin unconditionally), this would read as
	// an affirmative answer and the test would catch the regression as a
	// false positive (true) instead of the required false.
	withNonTTYStdin(t, "y\n", func() {
		got := ConfirmInteractive("plugin.install", "installs a third-party plugin")
		if got {
			t.Errorf("ConfirmInteractive() with non-TTY stdin = true, want false even though the pipe held an affirmative answer")
		}
	})
}

func TestConfirmInteractive_NonTTYPrintsNoPrompt(t *testing.T) {
	withNonTTYStdin(t, "", func() {
		out := captureStdout(func() {
			ConfirmInteractive("plugin.install", "installs a third-party plugin")
		})
		if out != "" {
			t.Errorf("ConfirmInteractive() with non-TTY stdin printed %q, want no prompt at all", out)
		}
	})
}
