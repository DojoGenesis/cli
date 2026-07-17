// Package permissions implements the dojo CLI's action-permission gate — the
// logic that decides whether a given action may proceed silently, must be
// confirmed interactively, or is refused outright.
//
// Actions are named as dot-paths following the convention
// <command>.<subcommand-or-action>, e.g. "code.undo", "plugin.install",
// "craft.scaffold". Patterns in permissions.allowed (see
// internal/config.PermissionsConfig) match a dot-path either exactly, or —
// if the pattern ends in "*" — as a prefix: "craft.*" matches
// "craft.scaffold" and "craft.go-service"; a bare "*" (empty prefix) matches
// every action.
//
// Check takes mode and allowed as plain arguments rather than importing
// internal/config directly, so this package stays dependency-light and
// trivially testable in isolation. Callers thread cfg.Permissions.Mode and
// cfg.Permissions.Allowed through at the call site.
package permissions

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Decision is the outcome of Check: whether an action may proceed, must be
// confirmed interactively, or is refused outright.
type Decision int

const (
	// Allow means the action proceeds silently — no prompt, no output.
	Allow Decision = iota
	// Confirm means the caller must obtain interactive confirmation (see
	// ConfirmInteractive) before the action proceeds.
	Confirm
	// Deny means the action is refused; callers should surface Explain's
	// message and stop.
	Deny
)

// String renders d for logs and test failure messages.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "Allow"
	case Confirm:
		return "Confirm"
	case Deny:
		return "Deny"
	default:
		return fmt.Sprintf("Decision(%d)", int(d))
	}
}

// yoloWarnOnce guards the single per-process YOLO warning emitted by Check
// — see warnYolo.
var yoloWarnOnce sync.Once

// Check evaluates action (a dot-path like "code.undo", "plugin.install",
// "craft.scaffold") against the permission configuration described by mode
// and allowed, and reports whether it may proceed.
//
// mode is validated defensively: any value other than "allowlist" or "yolo"
// — including "", "default", or an unrecognized string — is treated as
// "default". This mirrors internal/config.Config, where Permissions.Mode
// defaults to "default" and Validate() rejects unknown modes before they
// would reach here — but Check does not assume its caller validated first.
//
//   - yolo:      always Allow, for every action, regardless of allowed.
//     Emits one YOLO warning to stderr per process (see warnYolo).
//   - allowlist: Allow if action matches any pattern in allowed (see the
//     package doc comment for match rules); otherwise Deny.
//   - default:   Allow if action matches any pattern in allowed; otherwise
//     Confirm — the caller should then call ConfirmInteractive.
func Check(mode string, allowed []string, action string) Decision {
	switch mode {
	case "yolo":
		warnYolo()
		return Allow
	case "allowlist":
		if matchAny(allowed, action) {
			return Allow
		}
		return Deny
	default: // "default", "", or any unrecognized value
		if matchAny(allowed, action) {
			return Allow
		}
		return Confirm
	}
}

// warnYolo prints the YOLO warning to stderr the first time it is called in
// this process, and does nothing on every subsequent call.
func warnYolo() {
	yoloWarnOnce.Do(func() {
		fmt.Fprintln(os.Stderr, "permissions: YOLO mode active — skipping all confirmations")
	})
}

// matchAny reports whether action matches any pattern in allowed.
func matchAny(allowed []string, action string) bool {
	for _, pattern := range allowed {
		if matchPattern(pattern, action) {
			return true
		}
	}
	return false
}

// matchPattern reports whether action matches pattern. A pattern matches
// either exactly, or — if it ends in "*" — as a prefix, where everything
// before the trailing "*" must prefix action. A bare "*" has an empty
// prefix and therefore matches every action; an empty pattern matches
// nothing.
func matchPattern(pattern, action string) bool {
	if pattern == "" {
		return false
	}
	if pattern == action {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(action, prefix)
	}
	return false
}
