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
			if r.out.JSON() || r.headless {
				return fmt.Errorf("/warroom is interactive-only and unsupported in headless mode")
			}

			sessionID := fmt.Sprintf("warroom-%d", time.Now().UnixMilli())
			if r.session != nil && *r.session != "" {
				sessionID = *r.session + "-warroom"
			}

			topic := strings.Join(args, " ")

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
