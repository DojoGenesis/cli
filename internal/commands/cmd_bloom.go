package commands

// cmd_bloom.go — /bloom command: fullscreen animated bonsai garden TUI.

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DojoGenesis/cli/internal/spirit"
	"github.com/DojoGenesis/cli/internal/state"
	"github.com/DojoGenesis/cli/internal/tui"
)

// bloomCmd returns the /bloom command.
func (r *Registry) bloomCmd() Command {
	return Command{
		Name: "bloom",
		// "garden" is deliberately NOT an alias here: /garden is itself a
		// registered command name, and Registry.Dispatch checks exact command
		// names before scanning aliases — so a "garden" alias on this command
		// would always lose to the real /garden and could never be reached.
		Aliases: []string{"tree", "zen"},
		Usage:   "/bloom",
		Short:   "Watch your bonsai grow — animated zen garden",
		Run: func(ctx context.Context, args []string) error {
			// /bloom has no plain-text fallback — it IS the fullscreen
			// animated TUI. A headless JSON dispatch (or any non-interactive
			// run) has no terminal to paint an alt-screen Bubbletea program
			// into, so refuse cleanly instead of hanging or spraying
			// screen-control bytes into a JSON stream.
			if r.out.JSON() || r.headless {
				return fmt.Errorf("/bloom is interactive-only and unsupported in headless mode")
			}

			st, err := state.Load()
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			belt := spirit.CurrentBelt(st.Spirit.XP)
			model := tui.NewBloomModel(st.Spirit, belt)

			p := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("bloom: %w", err)
			}
			return nil
		},
	}
}
