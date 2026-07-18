package commands

// cmd_warroom.go — /warroom command: split-panel Scout vs Challenger debate TUI.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DojoGenesis/cli/internal/tui"
)

// warroomCmd returns the /warroom command.
func (r *Registry) warroomCmd() Command {
	return Command{
		Name:    "warroom",
		Aliases: []string{"war", "debate"},
		Usage:   "/warroom [topic]",
		Short:   "Split-panel debate: Scout vs Challenger",
		Run: func(ctx context.Context, args []string) error {
			// /warroom has no plain-text fallback — it IS the split-panel
			// debate TUI. A headless JSON dispatch (or any non-interactive
			// run) has no terminal to paint an alt-screen Bubbletea program
			// into, so refuse cleanly instead of hanging or spraying
			// screen-control bytes into a JSON stream.
			sessionID := fmt.Sprintf("warroom-%d", time.Now().UnixMilli())
			if r.session != nil && *r.session != "" {
				sessionID = *r.session + "-warroom"
			}
			topic := strings.Join(args, " ")

			// Headless: run the debate without the alt-screen TUI. Stream both
			// agents' turns as labeled NDJSON (JSON mode) or print a linear
			// transcript (plain), and return both full transcripts in the
			// envelope. A topic is required here — the interactive TUI can prompt
			// for one, a non-interactive caller cannot.
			if r.out.JSON() || r.headless {
				if strings.TrimSpace(topic) == "" {
					return fmt.Errorf("/warroom needs a topic outside the interactive TUI, e.g. /warroom should we ship")
				}
				var onChunk func(agent, text string)
				if r.out.JSON() {
					onChunk = func(agent, text string) {
						r.out.Emit(map[string]any{"stream": "warroom", "agent": agent, "content": text})
					}
				}
				scout, challenger, derr := tui.RunWarRoomHeadless(
					ctx, r.cfg.Gateway.URL, r.cfg.Gateway.Token,
					r.cfg.Defaults.Model, r.cfg.Defaults.Provider,
					sessionID, topic, onChunk,
				)
				if r.out.JSON() {
					r.out.Data(map[string]any{"topic": topic, "scout": scout, "challenger": challenger})
					return derr
				}
				fmt.Println()
				fmt.Println("  SCOUT")
				fmt.Println(scout)
				fmt.Println()
				fmt.Println("  CHALLENGER")
				fmt.Println(challenger)
				fmt.Println()
				return derr
			}

			// Interactive REPL: the split-panel alt-screen TUI (unchanged).
			model := tui.NewWarRoomModel(
				r.cfg.Gateway.URL,
				r.cfg.Gateway.Token,
				r.cfg.Defaults.Model,
				r.cfg.Defaults.Provider,
				sessionID,
				topic,
			)

			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}
