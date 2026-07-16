package commands

// cmd_garden.go — /garden command and all garden subcommand functions.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/client"
	gcolor "github.com/gookit/color"
)

// ─── /garden ────────────────────────────────────────────────────────────────

func (r *Registry) gardenCmd() Command {
	return Command{
		Name:    "garden",
		Aliases: []string{"seeds", "memory"},
		Usage:   "/garden [ls|stats|plant <text>|search <query>|rm <id>]",
		Short:   "Memory garden — list seeds, show stats, or plant new seeds",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "stats":
				stats, err := r.gw.GardenStats(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch garden stats: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Garden Stats"))
				fmt.Println()
				fmt.Println()
				for k, v := range stats {
					printKV(k, fmt.Sprintf("%v", v))
				}
				fmt.Println()

			case "plant":
				// /garden plant <text...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /garden plant <text>")
				}
				text := strings.Join(args[1:], " ")
				title := "Planted " + time.Now().Format("2006-01-02")
				req := client.CreateSeedRequest{
					Name:    title,
					Content: text,
				}
				seed, err := r.gw.CreateSeed(ctx, req)
				if err != nil {
					return fmt.Errorf("could not plant seed: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Seed planted"))
				fmt.Println()
				if seed != nil {
					printKV("id", seed.ID)
					printKV("name", seed.Name)
				}
				fmt.Println()

			case "search":
				// /garden search <query...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /garden search <query>")
				}
				query := strings.Join(args[1:], " ")
				results, err := r.gw.SearchMemories(ctx, query)
				if err != nil {
					return fmt.Errorf("could not search memories: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Search results (%d)\n\n", len(results)))
				if len(results) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No results found."))
					fmt.Println()
					return nil
				}
				for _, m := range results {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-24s", m.ID),
						gcolor.White.Sprint(truncate(m.Content, 80)),
					)
				}
				fmt.Println()

			case "rm":
				// /garden rm <id>
				if len(args) < 2 {
					return fmt.Errorf("usage: /garden rm <id>")
				}
				id := args[1]
				if err := r.gw.DeleteSeed(ctx, id); err != nil {
					return fmt.Errorf("could not delete seed: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Seed deleted"))
				fmt.Println()

			default: // ls — also catches unrecognized subcommands (e.g. the
				// former "harvest" verb, which was a silent no-op alias for
				// this same branch; dropped rather than documented as if it
				// were a distinct, intentional action).
				seeds, err := r.gw.Seeds(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch seeds: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Seeds (%d)\n\n", len(seeds)))
				if len(seeds) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Garden is empty. Use /garden plant <text> to add a seed."))
					fmt.Println()
					return nil
				}
				for _, s := range seeds {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-44s", s.Name),
						gcolor.HEX("#94a3b8").Sprint(truncate(s.Content, 50)),
					)
				}
				fmt.Println()
			}
			return nil
		},
	}
}
