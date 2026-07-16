package commands

// cmd_session.go — /session command.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/state"
	gcolor "github.com/gookit/color"
)

// ─── /session ───────────────────────────────────────────────────────────────

func (r *Registry) sessionCmd() Command {
	return Command{
		Name:  "session",
		Usage: "/session [new|resume|<id>]",
		Short: "Show or change the active session ID",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				fmt.Println()
				printKV("session", *r.session)
				fmt.Println()
				return nil
			}
			switch args[0] {
			case "new":
				*r.session = fmt.Sprintf("dojo-cli-%s", time.Now().Format("20060102-150405"))
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  New session started"))
				printKV("session", *r.session)
				fmt.Println()
				state.SaveSession(*r.session)
			case "resume":
				st, err := state.Load()
				if err != nil || st.LastSessionID == "" {
					return fmt.Errorf("no prior session to resume")
				}
				*r.session = st.LastSessionID
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Session resumed"))
				printKV("session", *r.session)
				fmt.Println()
			default:
				// /session <id> switches the client's local pointer to whatever
				// string was typed — it is never checked against the gateway, so
				// "resumed" would overclaim success. Say what actually happened.
				id := args[0]
				*r.session = id
				fmt.Println()
				if warning := sessionIDMalformedWarning(id); warning != "" {
					fmt.Println(gcolor.HEX("#e8b04a").Sprintf("  warning: %s", warning))
				}
				fmt.Println(gcolor.HEX("#7fb88c").Sprintf("  switched to session %s (not verified against gateway)", id))
				printKV("session", *r.session)
				fmt.Println()
				state.SaveSession(*r.session)
			}
			return nil
		},
	}
}

// sessionIDMalformedWarning does a cheap, best-effort sanity check on a
// user-supplied session ID and returns a human-readable warning if it looks
// obviously wrong (e.g. way too short, or full of characters no session ID
// would plausibly contain — like a pasted shell fragment). Returns "" when
// the ID looks plausible. This is NOT validation — /session <id> never
// confirms the session exists on the gateway; see the caller.
func sessionIDMalformedWarning(id string) string {
	trimmed := strings.TrimSpace(id)
	switch {
	case trimmed == "":
		return "empty session id"
	case len(trimmed) < 3:
		return fmt.Sprintf("%q is unusually short for a session id", id)
	case strings.ContainsAny(trimmed, "\"'<>{}|\\^`"):
		return fmt.Sprintf("%q contains characters that don't look like a session id", id)
	default:
		return ""
	}
}
