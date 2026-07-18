package commands

// cmd_guide.go — /guide command: interactive tutorials with XP rewards.

import (
	"context"
	"fmt"
	"strings"

	"github.com/DojoGenesis/cli/internal/guide"
	"github.com/DojoGenesis/cli/internal/state"
	gcolor "github.com/gookit/color"
)

func (r *Registry) guideCmd() Command {
	return Command{
		Name:    "guide",
		Aliases: []string{"guides", "tutorial"},
		Usage:   "/guide [ls|start <id>|status|stop]",
		Short:   "Interactive tutorials — learn dojo-cli and earn XP",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}
			switch sub {
			case "ls", "list":
				return guideLs()
			case "start":
				if len(args) < 2 {
					return fmt.Errorf("usage: /guide start <id>  —  run /guide ls to see available guides")
				}
				return guideStart(args[1])
			case "status":
				return guideStatus()
			case "stop", "quit", "exit":
				return guideStop()
			default:
				return fmt.Errorf("unknown subcommand %q — try /guide ls, /guide start <id>, /guide status, /guide stop", sub)
			}
		},
	}
}

// ─── subcommand handlers ─────────────────────────────────────────────────────

// GuideListEntry is one row of the JSON-mode `/guide ls` payload. guide.Guide
// itself carries no JSON tags (it's a REPL-only display type), so this is the
// small tagged projection an agent can parse.
type GuideListEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Short       string `json:"short,omitempty"`
	Steps       int    `json:"steps"`
	Completed   bool   `json:"completed"`
	Active      bool   `json:"active"`
	CurrentStep int    `json:"current_step,omitempty"` // 1-based; only meaningful when Active
}

func guideLs() error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if curEmitter.JSON() {
		entries := make([]GuideListEntry, 0, len(guide.All))
		for _, g := range guide.All {
			e := GuideListEntry{
				ID:        g.ID,
				Title:     g.Title,
				Short:     g.Short,
				Steps:     len(g.Steps),
				Completed: guide.IsCompleted(st, g.ID),
				Active:    st.Guide.Active == g.ID,
			}
			if e.Active {
				e.CurrentStep = st.Guide.Step + 1
			}
			entries = append(entries, e)
		}
		curEmitter.Data(entries)
		return nil
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#e8b04a").Sprint("  Guides"))
	fmt.Println()

	for _, g := range guide.All {
		completed := guide.IsCompleted(st, g.ID)
		active := st.Guide.Active == g.ID

		// Status indicator
		var indicator string
		switch {
		case completed:
			indicator = gcolor.HEX("#7fb88c").Sprint("✓")
		case active:
			indicator = gcolor.HEX("#f4a261").Sprint("►")
		default:
			indicator = gcolor.HEX("#64748b").Sprint("○")
		}

		// Progress suffix
		var progress string
		switch {
		case completed:
			progress = gcolor.HEX("#7fb88c").Sprint("[done]")
		case active:
			step := st.Guide.Step + 1
			total := len(g.Steps)
			progress = gcolor.HEX("#f4a261").Sprintf("[%d/%d steps]", step, total)
		default:
			total := len(g.Steps)
			progress = gcolor.HEX("#64748b").Sprintf("[%d steps]", total)
		}

		idPart := gcolor.HEX("#94a3b8").Sprintf("%-12s", g.ID)
		titlePart := fmt.Sprintf("%-28s", g.Title)
		fmt.Printf("  %s %s  %s  %s\n", indicator, idPart, titlePart, progress)
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#64748b").Sprint("  /guide start <id>   begin a guide"))
	fmt.Println(gcolor.HEX("#64748b").Sprint("  /guide status        see current step"))
	fmt.Println()
	return nil
}

func guideStart(id string) error {
	g := guide.Find(id)
	if g == nil {
		var ids []string
		for _, gg := range guide.All {
			ids = append(ids, gg.ID)
		}
		return fmt.Errorf("unknown guide %q — available: %s", id, strings.Join(ids, ", "))
	}

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	// If already completed, allow restart
	if guide.IsCompleted(st, id) {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  You've already completed \"%s\".", g.Title))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Starting again from step 1."))
		// Remove from completed list
		var filtered []string
		for _, c := range st.Guide.Completed {
			if c != id {
				filtered = append(filtered, c)
			}
		}
		st.Guide.Completed = filtered
	}

	// If switching guides mid-stream, warn and switch
	if st.Guide.Active != "" && st.Guide.Active != id {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Switching from \"%s\" — progress saved.", st.Guide.Active))
	}

	st.Guide.Active = id
	st.Guide.Step = 0
	if err := st.Save(); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	printGuideHeader(g)
	fmt.Println(guide.FormatStepBlock(g, 0))
	fmt.Println(gcolor.HEX("#64748b").Sprintf("  +%d XP on completion. /guide status to see this again.", g.Steps[0].XP))
	fmt.Println()
	return nil
}

func guideStatus() error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	g, stepIdx := guide.Active(st)
	if g == nil {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No guide active. Run /guide ls to see available guides."))
		fmt.Println()
		return nil
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#e8b04a").Sprintf("  Active Guide: %s (%d/%d)",
		g.Title, stepIdx+1, len(g.Steps)))
	fmt.Println()
	fmt.Println(guide.FormatStepBlock(g, stepIdx))

	hint := guide.StepHint(st)
	if hint != "" {
		fmt.Println(gcolor.HEX("#64748b").Sprintf("  Hint: %s", hint))
	}
	fmt.Println()
	return nil
}

func guideStop() error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if st.Guide.Active == "" {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No guide is active."))
		fmt.Println()
		return nil
	}

	id := st.Guide.Active
	st.Guide.Active = ""
	st.Guide.Step = 0
	if err := st.Save(); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Guide stopped. Resume anytime with /guide start %s.", id))
	fmt.Println()
	return nil
}

// ─── display helpers ─────────────────────────────────────────────────────────

func printGuideHeader(g *guide.Guide) {
	w := 40
	fmt.Println()
	fmt.Printf("  \u250c%s\u2510\n", strings.Repeat("\u2500", w))
	fmt.Printf("  \u2502  %-*s\u2502\n", w-2, "GUIDE")
	fmt.Printf("  \u2502  %-*s\u2502\n", w-2, g.Title)
	fmt.Printf("  \u2514%s\u2518\n", strings.Repeat("\u2500", w))
	fmt.Println()
}

// PrintGuideStepComplete prints a step-complete message and the next step.
// Called from repl.go after a successful AdvanceStep.
func PrintGuideStepComplete(res *guide.AdvanceResult) {
	fmt.Println()
	fmt.Printf("  %s  +%d XP\n",
		gcolor.HEX("#7fb88c").Sprint("Step complete!"),
		res.XP,
	)

	if res.GuideComplete {
		printGuideCompleteBox(res.GuideName)
	} else {
		fmt.Println()
		fmt.Printf("  %s\n",
			gcolor.HEX("#e8b04a").Sprintf("Step %d/%d — %s", res.NextStep, res.TotalSteps, res.NextTitle),
		)
		fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprint(res.NextAction))
		fmt.Println()
	}
}

// PrintGuideCompleteBonus prints the guide-completion XP bonus line.
// Called from repl.go after guide completion XP is awarded.
func PrintGuideCompleteBonus(xp int) {
	fmt.Printf("  %s  +%d XP\n",
		gcolor.HEX("#7fb88c").Sprint("Guide bonus!"),
		xp,
	)
	fmt.Println()
}

func printGuideCompleteBox(name string) {
	w := 40
	fmt.Println()
	fmt.Printf("  \u250c%s\u2510\n", strings.Repeat("\u2500", w))
	fmt.Printf("  \u2502  %-*s\u2502\n", w-2, "GUIDE COMPLETE")
	fmt.Printf("  \u2502  %-*s\u2502\n", w-2, name)
	fmt.Printf("  \u2502  %-*s\u2502\n", w-2, "Run /guide ls to see what's next.")
	fmt.Printf("  \u2514%s\u2518\n", strings.Repeat("\u2500", w))
	fmt.Println()
}
