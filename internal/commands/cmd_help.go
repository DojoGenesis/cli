package commands

// cmd_help.go — /help command and shared formatting helpers.

import (
	"context"
	"fmt"
	"strings"

	gcolor "github.com/gookit/color"
)

// helpCmd returns the /help command.
func (r *Registry) helpCmd() Command {
	return Command{
		Name:  "help",
		Usage: "/help",
		Short: "List available slash commands",
		Run: func(ctx context.Context, args []string) error {
			type helpSection struct {
				name string
				cmds []struct{ cmd, desc string }
			}

			sections := []helpSection{
				{"Chat", []struct{ cmd, desc string }{
					{"/help", "show this message"},
					{"/model [ls]", "list available models and providers"},
					{"/model set <name>", "switch to a different model"},
					{"/model direct <provider> <model> <msg>", "direct API call (bypass gateway)"},
					{"/session", "show active session ID"},
					{"/session new", "start a fresh session"},
					{"/session resume", "resume the most recent session"},
				{"/session <id>", "resume a prior session by ID"},
				{"/disposition", "show current disposition preset"},
				{"/disposition ls", "list all disposition presets"},
				{"/disposition set <name>", "switch to a named preset"},
				{"/disposition create <name> ...", "create custom preset"},
				}},
				{"Agents", []struct{ cmd, desc string }{
					{"/agent ls", "list agents registered in the gateway"},
					{"/agent dispatch <mode> <msg>", "create agent and stream response"},
					{"/agent chat <id> <msg>", "chat with existing agent by ID"},
					{"/agent info <id>", "show full agent detail"},
					{"/agent channels <id>", "list channels bound to an agent"},
					{"/agent bind <id> <channel>", "bind an agent to a channel"},
					{"/agent unbind <id> <channel>", "unbind an agent from a channel"},
				}},
				{"Memory", []struct{ cmd, desc string }{
					{"/garden ls", "list memory seeds"},
					{"/garden stats", "memory garden statistics"},
					{"/garden plant <text>", "plant a new seed"},
					{"/garden search <query>", "search memory seeds"},
					{"/garden rm <id>", "delete a seed"},
					{"/trail", "show memory timeline"},
					{"/trail add <text>", "store a memory entry"},
					{"/trail search <query>", "search memories"},
					{"/snapshot", "list/save/restore/export memory snapshots"},
				}},
				{"Workspace", []struct{ cmd, desc string }{
					{"/home", "workspace state overview"},
					{"/projects ls", "local workspace view"},
					{"/project init <name>", "create a new project"},
					{"/project status", "show active project phase"},
					{"/project switch <name>", "change active project"},
					{"/project list", "list all projects"},
					{"/project archive <name>", "archive a project"},
					{"/project phase <phase>", "set phase manually"},
					{"/project track add <name>", "add a parallel track"},
					{"/project decision <text>", "record a project decision"},
				}},
				{"Orchestration", []struct{ cmd, desc string }{
					{"/run <task>", "submit multi-step orchestration plan"},
					{"/workflow <name> [json]", "execute a workflow"},
					{"/warroom [topic]", "split-panel debate: Scout vs Challenger"},
					{"/pilot", "live SSE event stream (Ctrl+C to stop)"},
					{"/apps", "list running MCP apps"},
					{"/apps launch <name>", "launch an MCP app"},
					{"/apps close <name>", "stop an MCP app"},
					{"/apps status", "MCP app connection status"},
					{"/apps call <app> <tool> <json>", "invoke a tool on an MCP app"},
					{"/skill ls [filter]", "list skills"},
					{"/skill get <name>", "fetch skill content"},
					{"/skill inspect <hash>", "fetch raw CAS content"},
					{"/skill tags", "list CAS skill tags"},
					{"/doc <id>", "fetch and display a document"},
					{"/plugin ls", "list installed plugins"},
					{"/plugin install <url>", "install a plugin from git URL"},
					{"/plugin rm <name>", "remove an installed plugin"},
				}},
				{"System", []struct{ cmd, desc string }{
					{"/health", "gateway health + stats"},
					{"/settings", "show config and active settings"},
					{"/settings providers", "show provider configuration"},
					{"/hooks ls", "list loaded hook rules"},
					{"/hooks fire <event>", "manually fire a hook event"},
					{"/init", "set up workspace with plugins, dispositions, seeds"},
					{"/trace <id>", "inspect execution trace"},
					{"/activity [n]", "show recent activity log entries"},
					{"/practice", "daily reflection prompts"},
				}},
				{"Telemetry", []struct{ cmd, desc string }{
					{"/telemetry sessions", "recent sessions with cost/token/error data"},
					{"/telemetry costs", "cost breakdown by provider + 7-day trend"},
					{"/telemetry tools", "tool call stats: count, latency, success rate"},
					{"/telemetry summary", "combined overview of all telemetry data"},
				}},
				{"Spirit", []struct{ cmd, desc string }{
					{"/card", "show your dojo profile card"},
					{"/sensei", "receive wisdom from the sensei"},
				}},
			}

			fmt.Println()
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("Dojo CLI — slash commands"))
			fmt.Println()

			for _, sec := range sections {
				fmt.Println()
				gcolor.Bold.Print("  " + gcolor.HEX("#e8b04a").Sprint(sec.name))
				fmt.Println()
				for _, c := range sec.cmds {
					fmt.Printf("    %-36s", gcolor.HEX("#f4a261").Sprint(c.cmd))
					fmt.Println(gcolor.HEX("#94a3b8").Sprint(c.desc))
				}
			}

			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Type a message without / to chat with the gateway."))
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Ctrl+C or type exit to quit."))
			fmt.Println()
			return nil
		},
	}
}

// ─── Shared formatting helpers ──────────────────────────────────────────────

// printKV prints a key-value pair: key in cloud-gray, value in white.
func printKV(key, value string) {
	fmt.Printf("%s%s\n",
		gcolor.HEX("#94a3b8").Sprintf("  %-24s", key),
		gcolor.White.Sprint(value),
	)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "\u2026"
}

func colorStatus(s string) string {
	switch strings.ToLower(s) {
	case "ok", "healthy", "active", "running", "ready", "completed":
		return gcolor.HEX("#7fb88c").Sprint(s) // soft-sage
	case "loading", "starting", "submitted", "pending":
		return gcolor.HEX("#e8b04a").Sprint(s) // warm-amber
	case "", "unknown":
		return gcolor.HEX("#94a3b8").Sprint("unknown") // cloud-gray
	default:
		return gcolor.HEX("#e63946").Sprint(s) // danger-red
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
