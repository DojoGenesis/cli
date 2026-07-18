package commands

// activityCmd implements the /activity slash command, which displays and
// manages the NDJSON activity log at ~/.dojo/activity.log.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	gcolor "github.com/gookit/color"
)

// activityCmd returns the /activity command.
//
//	/activity         — show last 10 entries
//	/activity <n>     — show last n entries
//	/activity clear   — clear the activity log
func (r *Registry) activityCmd() Command {
	return Command{
		Name:    "activity",
		Aliases: []string{"log", "act"},
		Usage:   "/activity [n|clear]",
		Short:   "Show or clear the activity log",
		Run: func(ctx context.Context, args []string) error {
			if len(args) > 0 && strings.ToLower(args[0]) == "clear" {
				if err := activity.Clear(); err != nil {
					return fmt.Errorf("activity clear: %w", err)
				}
				fmt.Println()
				if r.out.JSON() {
					r.out.Data(map[string]any{"cleared": true})
					return nil
				}
				gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Activity log cleared"))
				fmt.Println()
				fmt.Println()
				return nil
			}

			n := 10
			if len(args) > 0 {
				if parsed, err := strconv.Atoi(args[0]); err == nil && parsed > 0 {
					n = parsed
				}
			}

			entries, err := activity.Recent(n)
			if err != nil {
				return fmt.Errorf("activity: %w", err)
			}

			fmt.Println()
			if r.out.JSON() {
				r.out.Data(entries)
				return nil
			}
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Activity log (last %d)", n))
			fmt.Println()
			fmt.Println()

			if len(entries) == 0 {
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No activity recorded yet."))
				fmt.Println()
				return nil
			}

			for _, e := range entries {
				ts := e.Timestamp.Format("2006-01-02 15:04")
				fmt.Printf("  %s  %s  %s\n",
					gcolor.HEX("#94a3b8").Sprintf("%-16s", ts),
					gcolor.HEX("#f4a261").Sprintf("%-20s", string(e.Type)),
					gcolor.White.Sprint(e.Summary),
				)
			}
			fmt.Println()
			return nil
		},
	}
}
