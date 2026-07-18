package commands

// cmd_bloom.go — /bloom command: fullscreen animated bonsai garden TUI.

import (
	"context"
	"fmt"
	"time"

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
			st, err := state.Load()
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}
			belt := spirit.CurrentBelt(st.Spirit.XP)

			// Headless: the animation has no terminal to paint into, but /bloom
			// visualizes real spirit state — so emit that (belt, XP, streak,
			// sessions, next belt, a koan) instead of refusing. No command is a
			// dead end headless.
			if r.out.JSON() || r.headless {
				sp := st.Spirit
				next, xpToNext := spirit.NextBelt(sp.XP)
				koan := spirit.RandomKoan(belt.Rank, time.Now())
				if r.out.JSON() {
					data := map[string]any{
						"belt": belt.Name, "belt_title": belt.Title, "rank": belt.Rank,
						"xp": sp.XP, "streak_days": sp.StreakDays, "sessions": sp.TotalSessions,
						"progress_pct": spirit.ProgressPercent(sp.XP), "koan": koan,
					}
					if next != nil {
						data["next_belt"] = next.Name
						data["xp_to_next"] = xpToNext
					}
					r.out.Data(data)
					return nil
				}
				fmt.Println()
				fmt.Printf("  %s %s\n\n", belt.Name, belt.Title)
				printKV("xp", fmt.Sprintf("%d", sp.XP))
				if next != nil {
					printKV("next belt", fmt.Sprintf("%s (%d XP to go)", next.Name, xpToNext))
				}
				printKV("streak", fmt.Sprintf("%d days", sp.StreakDays))
				printKV("sessions", fmt.Sprintf("%d", sp.TotalSessions))
				fmt.Println()
				fmt.Println("  " + koan)
				fmt.Println()
				return nil
			}

			model := tui.NewBloomModel(st.Spirit, belt)

			p := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("bloom: %w", err)
			}
			return nil
		},
	}
}
