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
			type helpEntry struct {
				cmd      string
				desc     string
				maturity string // "", "beta", or "experimental"
			}
			type helpSection struct {
				name string
				cmds []helpEntry
			}

			sections := []helpSection{
				{"Chat", []helpEntry{
					{"/help", "show this message", ""},
					{"/model [ls]", "list available models and providers", ""},
					{"/model set <provider> <model>", "switch to a different model", ""},
					{"/model direct <provider> <model> <msg>", "direct API call (bypass gateway)", ""},
					{"/session", "show active session ID", ""},
					{"/session new", "start a fresh session", ""},
					{"/session resume", "resume the most recent session", ""},
					{"/session <id>", "switch to a session by ID (not verified against gateway)", ""},
					{"/disposition", "show current disposition preset", ""},
					{"/disposition ls", "list all disposition presets", ""},
					{"/disposition set <name>", "switch to a named preset", ""},
					{"/disposition show <name>", "show a preset's pacing/depth/tone/initiative", ""},
					{"/disposition create <name> ...", "create a custom preset", "beta"},
				}},
				{"Agents", []helpEntry{
					{"/agent ls", "list agents registered in the gateway", ""},
					{"/agent dispatch <mode> <msg>", "create agent and stream response", ""},
					{"/agent chat <id> <msg>", "chat with existing agent by ID", ""},
					{"/agent info <id>", "show full agent detail", ""},
					{"/agent channels <id>", "list channels bound to an agent", "beta"},
					{"/agent bind <id> <channel>", "bind an agent to a channel", "beta"},
					{"/agent unbind <id> <channel>", "unbind an agent from a channel", "beta"},
				}},
				{"Memory/Garden", []helpEntry{
					{"/garden ls", "list memory seeds", ""},
					{"/garden stats", "memory garden statistics", ""},
					{"/garden plant <text>", "plant a new seed", ""},
					{"/garden search <query>", "search memory seeds", ""},
					{"/garden rm <id>", "delete a seed", ""},
					{"/trail", "show memory timeline", ""},
					{"/trail add <text>", "store a memory entry", ""},
					{"/trail rm <id>", "delete a memory entry", ""},
					{"/trail search <query>", "search memories", ""},
					{"/snapshot", "list memory snapshots", "beta"},
					{"/snapshot save", "save a snapshot of the active session", "beta"},
					{"/snapshot restore <id>", "restore a snapshot", "beta"},
					{"/snapshot export <id>", "export a snapshot", "beta"},
					{"/snapshot rm <id>", "delete a snapshot", "beta"},
				}},
				{"Workspace", []helpEntry{
					{"/home [plain]", "workspace state overview (add 'plain' for text-only)", ""},
					{"/projects", "local workspace view — cwd, plugins, session (no arguments)", ""},
					{"/project init <name>", "create a new project", ""},
					{"/project status", "show active project phase", ""},
					{"/project switch <name>", "change active project", ""},
					{"/project list", "list all projects", ""},
					{"/project archive <name>", "archive a project", ""},
					{"/project phase <phase>", "set phase manually", "beta"},
					{"/project track add <name>", "add a parallel track", "beta"},
					{"/project track set <id> <status>", "update a track's status", "beta"},
					{"/project decision <text>", "record a project decision", "beta"},
					{"/project artifact <type> <file> <content>", "save a project artifact", ""},
				}},
				{"Orchestration", []helpEntry{
					{"/run <task>", "submit multi-step orchestration plan", ""},
					{"/workflow <name> [json]", "execute a workflow", ""},
					{"/warroom [topic]", "split-panel debate: Scout vs Challenger", "beta"},
					{"/pilot [plain]", "live SSE event stream, Ctrl+C to stop (add 'plain' for text-only)", ""},
					{"/apps", "list running MCP apps", ""},
					{"/apps launch <name>", "launch an MCP app", ""},
					{"/apps close <name>", "stop an MCP app", ""},
					{"/apps status", "MCP app connection status", ""},
					{"/apps call <app> <tool> <json>", "invoke a tool on an MCP app", ""},
				}},
				{"Skills", []helpEntry{
					{"/skill ls [filter]", "list skills, grouped by category", ""},
					{"/skill search <query>", "search skills by keyword", ""},
					{"/skill get <name>", "fetch skill content", ""},
					{"/skill inspect <hash>", "fetch raw CAS content", ""},
					{"/skill tags", "list CAS skill tags", ""},
					{"/skill package-all <dir>", "package SKILL.md files into CAS", ""},
				}},
				{"Plugins", []helpEntry{
					{"/plugin ls", "list installed plugins", ""},
					{"/plugin install <url>", "install a plugin from a git URL", ""},
					{"/plugin rm <name>", "remove an installed plugin", ""},
				}},
				{"Code", []helpEntry{
					{"/code read <file>", "display file contents in the REPL", ""},
					{"/code diff [file]", "show git diff (staged + unstaged)", ""},
					{"/code test [pkg]", "run go test for a package", ""},
					{"/code build", "run go build ./...", ""},
					{"/code vet", "run go vet ./...", ""},
					{"/code gate", "run build + test + vet (full gate)", ""},
				}},
				{"Practice", []helpEntry{
					{"/practice", "daily reflection prompts (rotates by day of week)", ""},
					{"/guide ls", "list available tutorials", ""},
					{"/guide start <id>", "begin a guide", ""},
					{"/guide status", "show current step", ""},
					{"/guide stop", "stop the active guide", ""},
				}},
				{"Spirit", []helpEntry{
					{"/card", "show your dojo profile card", ""},
					{"/sensei", "receive a koan from the sensei", ""},
					{"/bloom", "animated bonsai garden meditation", ""},
				}},
				{"Telemetry", []helpEntry{
					{"/telemetry sessions", "recent sessions with cost/token/error data", ""},
					{"/telemetry costs", "cost breakdown by provider + 7-day trend", ""},
					{"/telemetry tools", "tool call stats: count, latency, success rate", ""},
					{"/telemetry summary", "combined overview of all telemetry data", ""},
				}},
				{"System", []helpEntry{
					{"/health", "gateway health + stats", ""},
					{"/settings", "show config and active settings", ""},
					{"/settings effective", "show merged file + env + flag config", ""},
					{"/settings providers", "show provider configuration", ""},
					{"/settings set <provider> <key>", "store a provider API key", ""},
					{"/settings profile [ls|set|show|create]", "manage disposition profiles (shared with /disposition)", ""},
					{"/hooks ls", "list loaded hook rules", ""},
					{"/hooks fire <event>", "manually fire a hook event", ""},
					{"/init", "set up workspace with plugins, dispositions, seeds", ""},
					{"/trace <id>", "inspect an execution trace by ID", "beta"},
					{"/activity [n]", "show recent activity log entries", ""},
					{"/activity clear", "clear the activity log", ""},
					{"/tools", "list registered MCP tools, grouped by namespace", ""},
					{"/doc <id>", "fetch and display a document", "beta"},
				}},
				{"DojoCraft", []helpEntry{
					{"/craft adr <title>", "write an ADR via the gateway", ""},
					{"/craft scout <tension>", "tension → routes → synthesis → decision", ""},
					{"/craft claude-md [--fix]", "analyze CLAUDE.md files; --fix suggests rewrites", ""},
					{"/craft memory <ls|add|rm|prune|search>", "manage Gateway memory entries", ""},
					{"/craft seed <ls|plant|harvest|search|elevate>", "manage memory garden seeds", ""},
					{"/craft view [path]", "codebase overview — tree, entry points, test coverage", ""},
					{"/craft scaffold <template>", "create a project from a template layout", ""},
					{"/craft converge", "git + memory health report: RED/YELLOW/GREEN", ""},
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
					label := gcolor.HEX("#f4a261").Sprint(c.cmd)
					if c.maturity != "" {
						label += " " + gcolor.HEX("#64748b").Sprintf("[%s]", c.maturity)
					}
					fmt.Printf("    %-44s", label)
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
