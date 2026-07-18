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
		Usage: "/session [new|ls|resume [id]|<id>]",
		Short: "Show, list, or change the active session ID",
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
			case "ls":
				return sessionLs(*r.session)
			case "resume":
				// Bare `/session resume` restores whatever was last active — unchanged
				// from before session history existed. `/session resume <id>` targets
				// one specific session by ID (Claude-Code-style resume-a-past-session).
				if len(args) >= 2 {
					return sessionResumeByID(r.session, args[1])
				}
				return sessionResumeLast(r.session)
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

// sessionResumeLast restores the most recently active session — the original
// bare `/session resume` behavior, unchanged now that history exists
// alongside LastSessionID.
func sessionResumeLast(session *string) error {
	st, err := state.Load()
	if err != nil || st.LastSessionID == "" {
		return fmt.Errorf("no prior session to resume")
	}
	*session = st.LastSessionID
	fmt.Println()
	fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Session resumed"))
	printKV("session", *session)
	fmt.Println()
	// LastSessionID isn't changing, but re-saving still bumps this session
	// back to the front of history / refreshes its timestamp — see
	// State.Save's RecordSession hook in internal/state/state.go.
	state.SaveSession(*session)
	return nil
}

// sessionResumeByID implements `/session resume <id>` — switches directly to
// one specific session, unlike bare resume's "whatever was last active".
// There is no gateway round-trip to confirm the id is real (same limitation
// as the bare `/session <id>` switch above), so this can only warn — never
// block — when the id isn't one dojo has seen before locally.
func sessionResumeByID(session *string, id string) error {
	st, loadErr := state.Load()
	var hist []state.SessionEntry
	if loadErr == nil {
		hist = st.History()
	}

	fmt.Println()
	if warning := sessionResumeWarning(id, hist); warning != "" {
		fmt.Println(gcolor.HEX("#e8b04a").Sprintf("  warning: %s — switching anyway (not verified against gateway)", warning))
	}

	*session = id
	fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Session resumed"))
	printKV("session", *session)
	fmt.Println()
	state.SaveSession(*session)
	return nil
}

// sessionResumeWarning returns a human-readable warning for a resume-by-id
// request, or "" when id looks fine and is present in hist. Pure and
// side-effect-free (no disk I/O) so it's directly unit-testable. Checks the
// same malformed-ID heuristic as the bare `/session <id>` switch first, then
// falls back to a "not found locally" check against history.
func sessionResumeWarning(id string, hist []state.SessionEntry) string {
	if w := sessionIDMalformedWarning(id); w != "" {
		return w
	}
	for _, e := range hist {
		if e.ID == id {
			return ""
		}
	}
	return fmt.Sprintf("%q not found in local session history", id)
}

// SessionListEntry is one row of the JSON-mode `/session ls` payload: the
// stored history entry plus whether it's the REPL's currently active
// session, which isn't itself a field on state.SessionEntry.
type SessionListEntry struct {
	state.SessionEntry
	Active bool `json:"active"`
}

// sessionLs lists recent sessions from local history, most-recent-first,
// marking whichever one is currently active in this REPL. Claude-Code-style
// `/session ls`.
func sessionLs(active string) error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("could not load session history: %w", err)
	}
	hist := st.History()

	if curEmitter.JSON() {
		entries := make([]SessionListEntry, len(hist))
		for i, e := range hist {
			entries[i] = SessionListEntry{SessionEntry: e, Active: e.ID == active}
		}
		curEmitter.Data(entries)
		return nil
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Sessions (%d)\n\n", len(hist)))
	if len(hist) == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  no session history yet — /session new or /session resume <id> to start one"))
		fmt.Println()
		return nil
	}
	for _, entry := range hist {
		marker := "  "
		idColor := "#f4a261"
		activeTag := ""
		if entry.ID == active {
			marker = gcolor.HEX("#7fb88c").Sprint("* ")
			idColor = "#7fb88c"
			activeTag = gcolor.HEX("#94a3b8").Sprint("  (active)")
		}
		idField := gcolor.HEX(idColor).Sprintf("%-40s", entry.ID)
		agoField := gcolor.HEX("#94a3b8").Sprint(fmtAgo(entry.SavedAt))
		fmt.Printf("  %s%s %s%s\n", marker, idField, agoField, activeTag)
	}
	fmt.Println()
	return nil
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
