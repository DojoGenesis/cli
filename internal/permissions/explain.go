package permissions

import "fmt"

// Explain returns the one-line message describing why action was denied —
// by allowlist mode, or because ConfirmInteractive returned false — naming
// both escape hatches available to the user: adding the action (or a
// covering glob) to permissions.allowed in ~/.dojo/settings.json, or
// re-running with --yolo.
func Explain(action string) string {
	return fmt.Sprintf(
		"permission denied: %q (add to permissions.allowed in ~/.dojo/settings.json, or run with --yolo)",
		action,
	)
}
