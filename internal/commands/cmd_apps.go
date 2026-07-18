package commands

// cmd_apps.go — /apps command and all app-related functions.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gcolor "github.com/gookit/color"
)

// ─── /apps ──────────────────────────────────────────────────────────────────

func (r *Registry) appsCmd() Command {
	return Command{
		Name:    "apps",
		Aliases: []string{"app"},
		Usage:   "/apps [launch <name>|close <name>|status|call <app> <tool> <json>]",
		Short:   "List and manage running MCP apps",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "launch":
				// /apps launch <name> [config-json]
				if len(args) < 2 {
					return fmt.Errorf("usage: /apps launch <name> [config-json]")
				}
				name := args[1]
				var cfg map[string]any
				if len(args) >= 3 {
					if err := json.Unmarshal([]byte(args[2]), &cfg); err != nil {
						return fmt.Errorf("invalid config JSON: %w", err)
					}
				}
				if err := r.gw.LaunchApp(ctx, name, cfg); err != nil {
					return fmt.Errorf("could not launch app %q: %w", name, err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  App launched"))
				printKV("name", name)
				fmt.Println()
				return nil

			case "close":
				// /apps close <name>
				if len(args) < 2 {
					return fmt.Errorf("usage: /apps close <name>")
				}
				name := args[1]
				if err := r.gw.CloseApp(ctx, name); err != nil {
					return fmt.Errorf("could not close app %q: %w", name, err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  App closed"))
				printKV("name", name)
				fmt.Println()
				return nil

			case "status":
				// /apps status
				status, err := r.gw.AppStatus(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch app status: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  App Status\n\n"))
				for k, v := range status {
					printKV(k, fmt.Sprintf("%v", v))
				}
				fmt.Println()
				return nil

			case "call":
				// /apps call <app> <tool> <json>
				if len(args) < 4 {
					return fmt.Errorf("usage: /apps call <app> <tool> <json-input>")
				}
				appName := args[1]
				toolName := args[2]
				inputJSON := strings.Join(args[3:], " ")
				var input map[string]any
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("invalid input JSON: %w", err)
				}
				result, err := r.gw.ProxyToolCall(ctx, appName, toolName, input)
				if err != nil {
					return fmt.Errorf("tool call failed: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Result: %s/%s\n\n", appName, toolName))
				b, jsonErr := json.MarshalIndent(result, "  ", "  ")
				if jsonErr != nil {
					return fmt.Errorf("could not marshal result: %w", jsonErr)
				}
				fmt.Println(gcolor.White.Sprint("  " + string(b)))
				fmt.Println()
				return nil

			default: // ls
				if len(args) > 0 && sub != "ls" {
					return fmt.Errorf("unknown subcommand %q — see /help", args[0])
				}
				apps, err := r.gw.ListApps(ctx)
				if err != nil {
					return fmt.Errorf("could not list apps: %w", err)
				}
				if r.out.JSON() {
					r.out.Data(apps)
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  MCP Apps (%d)\n\n", len(apps)))
				if len(apps) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No apps running. Use /apps launch <name> to start one."))
					fmt.Println()
					return nil
				}
				for _, a := range apps {
					toolCount := fmt.Sprintf("%d tools", a.Tools)
					fmt.Printf("  %s  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-28s", a.Name),
						colorStatus(a.Status),
						gcolor.HEX("#94a3b8").Sprint(toolCount),
					)
				}
				fmt.Println()
				return nil
			}
		},
	}
}
