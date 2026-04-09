// Package commands implements all dojo slash commands.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DojoGenesis/dojo-cli/internal/art"
	"github.com/DojoGenesis/dojo-cli/internal/bootstrap"
	"github.com/DojoGenesis/dojo-cli/internal/client"
	"github.com/DojoGenesis/dojo-cli/internal/config"
	"github.com/DojoGenesis/dojo-cli/internal/hooks"
	"github.com/DojoGenesis/dojo-cli/internal/plugins"
	"github.com/DojoGenesis/dojo-cli/internal/state"
	"github.com/DojoGenesis/dojo-cli/internal/tui"
	"github.com/fatih/color"
	gcolor "github.com/gookit/color"
)

// Registry maps slash command names to handler functions.
type Registry struct {
	cfg     *config.Config
	gw      *client.Client
	cmds    map[string]Command
	plgs    []plugins.Plugin
	runner  *hooks.Runner
	session *string // pointer to REPL's active session ID
}

// Command is a callable slash command.
type Command struct {
	Name    string
	Aliases []string
	Usage   string
	Short   string
	Run     func(ctx context.Context, args []string) error
}

// New builds the command registry. session is a pointer to the REPL's active session ID
// so that /session new and /session <id> can update it across turns.
func New(cfg *config.Config, gw *client.Client, plgs []plugins.Plugin, session *string) *Registry {
	r := &Registry{
		cfg:     cfg,
		gw:      gw,
		cmds:    make(map[string]Command),
		plgs:    plgs,
		runner:  hooks.New(plgs),
		session: session,
	}
	r.register()
	return r
}

// Runner returns the hook runner so the REPL can fire events.
func (r *Registry) Runner() *hooks.Runner {
	return r.runner
}

// Dispatch finds and executes a slash command. Input should be the full line
// after the leading "/", e.g. "skill ls" or "chat hello world".
func (r *Registry) Dispatch(ctx context.Context, input string) error {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	name := strings.ToLower(parts[0])
	args := parts[1:]

	// Exact match
	if cmd, ok := r.cmds[name]; ok {
		return cmd.Run(ctx, args)
	}
	// Alias scan
	for _, cmd := range r.cmds {
		for _, a := range cmd.Aliases {
			if a == name {
				return cmd.Run(ctx, args)
			}
		}
	}
	return fmt.Errorf("unknown command /%s — type /help for a list", name)
}

func (r *Registry) add(cmd Command) {
	r.cmds[cmd.Name] = cmd
}

// ─── Registration ─────────────────────────────────────────────────────────────

func (r *Registry) register() {
	r.add(r.helpCmd())
	r.add(r.healthCmd())
	r.add(r.homeCmd())
	r.add(r.modelCmd())
	r.add(r.toolsCmd())
	r.add(r.agentCmd())
	r.add(r.skillCmd())
	r.add(r.gardenCmd())
	r.add(r.trailCmd())
	r.add(r.snapshotCmd())
	r.add(r.traceCmd())
	r.add(r.pilotCmd())
	r.add(r.hooksCmd())
	r.add(r.settingsCmd())
	r.add(r.sessionCmd())
	r.add(r.runCmd())
	r.add(r.practiceCmd())
	r.add(r.projectsCmd())
	r.add(r.appsCmd())
	r.add(r.workflowCmd())
	r.add(r.docCmd())
	r.add(r.initCmd())
}

// ─── /help ────────────────────────────────────────────────────────────────────

func (r *Registry) helpCmd() Command {
	return Command{
		Name:  "help",
		Usage: "/help",
		Short: "List available slash commands",
		Run: func(ctx context.Context, args []string) error {
			fmt.Println()
			// Section header: warm-amber + bold
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("Dojo CLI — slash commands"))
			fmt.Println()
			fmt.Println()
			cmds := []struct{ cmd, desc string }{
				{"/help", "show this message"},
				{"/health", "gateway health + stats"},
				{"/home", "workspace state overview"},
				{"/model [ls]", "list available models and providers"},
				{"/model set <name>", "switch to a different model (in-memory)"},
				{"/tools [ls]", "list registered MCP tools"},
				{"/agent ls", "list agents registered in the gateway"},
				{"/agent dispatch <mode> <msg>", "create agent and stream response"},
				{"/agent chat <id> <msg>", "chat with existing agent by ID"},
				{"/agent info <id>", "show full agent detail (status, disposition, channels)"},
				{"/agent channels <id>", "list channels bound to an agent"},
				{"/agent bind <id> <channel>", "bind an agent to a channel"},
				{"/agent unbind <id> <channel>", "unbind an agent from a channel"},
				{"/apps", "list running MCP apps"},
				{"/apps launch <name>", "launch an MCP app"},
				{"/apps close <name>", "stop an MCP app"},
				{"/apps status", "show MCP app connection status"},
				{"/apps call <app> <tool> <json>", "invoke a tool on an MCP app"},
				{"/skill ls [filter]", "list skills (optionally filter by name)"},
				{"/skill get <name>", "fetch skill content from CAS (latest version)"},
				{"/skill inspect <hash>", "fetch raw CAS content by ref/hash"},
				{"/skill tags", "list all CAS skill tags (name, version, ref)"},
				{"/workflow <name> [input-json]", "execute a workflow and stream progress"},
				{"/doc <id>", "fetch and display a document by ID"},
				{"/session", "show active session ID"},
				{"/session new", "start a fresh session"},
				{"/session <id>", "resume a prior session by ID"},
				{"/run <task>", "submit multi-step orchestration plan"},
				{"/garden ls", "list memory seeds"},
				{"/garden stats", "memory garden statistics"},
				{"/garden plant <text>", "plant a new seed into the garden"},
				{"/garden search <query>", "search memory seeds"},
				{"/garden rm <id>", "delete a seed"},
				{"/trail", "show memory timeline"},
				{"/trail add <text>", "store a memory entry"},
				{"/trail search <query>", "search memories"},
				{"/snapshot", "list/save/restore/export memory snapshots"},
				{"/trace <id>", "inspect execution trace"},
				{"/pilot", "live SSE event stream (Ctrl+C to stop)"},
				{"/hooks ls", "list loaded hook rules from plugins"},
				{"/hooks fire <event>", "manually fire a hook event (for testing)"},
				{"/init", "set up workspace with plugins, dispositions, seeds"},
				{"/settings", "show config file path and active settings"},
				{"/settings providers", "show provider configuration"},
				{"/practice", "daily reflection prompts (rotates by day of week)"},
				{"/projects ls", "local workspace view — cwd, plugins, session"},
			}
			for _, c := range cmds {
				// Command names in golden-orange, descriptions in cloud-gray
				fmt.Printf("  %-32s", gcolor.HEX("#f4a261").Sprint(c.cmd))
				fmt.Println(gcolor.HEX("#94a3b8").Sprint(c.desc))
			}
			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Type a message without / to chat with the gateway."))
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Ctrl+C or type exit to quit."))
			fmt.Println()
			return nil
		},
	}
}

// ─── /health ─────────────────────────────────────────────────────────────────

func (r *Registry) healthCmd() Command {
	return Command{
		Name:    "health",
		Aliases: []string{"ping", "status"},
		Usage:   "/health",
		Short:   "Gateway health + stats",
		Run: func(ctx context.Context, args []string) error {
			h, err := r.gw.Health(ctx)
			if err != nil {
				return fmt.Errorf("gateway unreachable: %w", err)
			}
			fmt.Println()
			printKV("status", colorStatus(h.Status))
			printKV("version", h.Version)
			printKV("uptime", fmt.Sprintf("%ds", h.UptimeSeconds))
			printKV("requests", fmt.Sprintf("%d", h.RequestsProcessed))
			for name, st := range h.Providers {
				printKV("provider/"+name, colorStatus(st))
			}
			printKV("gateway", r.cfg.Gateway.URL)
			fmt.Println()
			return nil
		},
	}
}

// ─── /home ───────────────────────────────────────────────────────────────────

func (r *Registry) homeCmd() Command {
	return Command{
		Name:    "home",
		Aliases: []string{"ws", "workspace"},
		Usage:   "/home [plain]",
		Short:   "Workspace state overview",
		Run: func(ctx context.Context, args []string) error {
			// /home plain — text-only fallback
			if len(args) > 0 && args[0] == "plain" {
				return r.homePlain(ctx)
			}

			// Default: Bubbletea TUI panel
			model := tui.NewHomeModel(r.cfg, r.gw, *r.session, len(r.plgs))
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

func (r *Registry) homePlain(ctx context.Context) error {
	h, err := r.gw.Health(ctx)
	if err != nil {
		return fmt.Errorf("gateway unreachable: %w", err)
	}
	agents, agentErr := r.gw.Agents(ctx)
	seeds, seedErr := r.gw.Seeds(ctx)

	fmt.Println()
	color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Dojo Workspace"))
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + r.cfg.Gateway.URL))
	fmt.Println()

	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("gateway"), colorStatus(h.Status))
	if agentErr != nil {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("agents"), gcolor.HEX("#e63946").Sprint("unavailable"))
	} else {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("agents"), color.WhiteString("%d", len(agents)))
	}
	if seedErr != nil {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("seeds"), gcolor.HEX("#e63946").Sprint("unavailable"))
	} else {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("seeds"), color.WhiteString("%d", len(seeds)))
	}
	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("plugins"), color.WhiteString("%d", len(r.plgs)))
	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("session"), gcolor.HEX("#e8b04a").Sprint(*r.session))
	fmt.Println()
	return nil
}

// ─── /model ──────────────────────────────────────────────────────────────────

func (r *Registry) modelCmd() Command {
	return Command{
		Name:    "model",
		Aliases: []string{"models", "providers"},
		Usage:   "/model [ls|set <name>]",
		Short:   "List models and providers, or switch the active model",
		Run: func(ctx context.Context, args []string) error {
			// /model set <name>
			if len(args) >= 2 && strings.ToLower(args[0]) == "set" {
				newModel := args[1]
				oldModel := r.cfg.Defaults.Model
				if oldModel == "" {
					oldModel = "(auto)"
				}
				r.cfg.Defaults.Model = newModel
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#7fb88c").Sprint("  Model updated"))
				fmt.Println()
				printKV("old model", oldModel)
				printKV("new model", newModel)
				fmt.Println()
				return nil
			}

			// /model or /model ls — list behavior
			providers, err := r.gw.Providers(ctx)
			if err != nil {
				// fallback to /v1/models
				models, err2 := r.gw.Models(ctx)
				if err2 != nil {
					return fmt.Errorf("could not fetch models: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Models (%d)\n\n", len(models)))
				for _, m := range models {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-42s", m.ID),
						gcolor.HEX("#94a3b8").Sprint(m.Provider),
					)
				}
				fmt.Println()
				return nil
			}

			fmt.Println()
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Providers (%d)\n\n", len(providers)))
			for _, p := range providers {
				status := colorStatus(p.Status)
				caps := ""
				if p.Info != nil && len(p.Info.Capabilities) > 0 {
					caps = gcolor.HEX("#94a3b8").Sprintf(" [%s]", strings.Join(p.Info.Capabilities, ", "))
				}
				fmt.Printf("  %s  %s%s\n", gcolor.HEX("#f4a261").Sprintf("%-20s", p.Name), status, caps)
			}
			fmt.Println()
			return nil
		},
	}
}

// ─── /tools ──────────────────────────────────────────────────────────────────

func (r *Registry) toolsCmd() Command {
	return Command{
		Name:    "tools",
		Aliases: []string{"tool"},
		Usage:   "/tools [ls]",
		Short:   "List registered MCP tools",
		Run: func(ctx context.Context, args []string) error {
			tools, err := r.gw.Tools(ctx)
			if err != nil {
				return fmt.Errorf("could not fetch tools: %w", err)
			}
			fmt.Println()
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Tools (%d)\n\n", len(tools)))

			// Group by namespace
			ns := map[string][]client.Tool{}
			order := []string{}
			for _, t := range tools {
				n := t.Namespace
				if n == "" {
					n = "builtin"
				}
				if _, seen := ns[n]; !seen {
					order = append(order, n)
				}
				ns[n] = append(ns[n], t)
			}
			for _, n := range order {
				// Glass-effect section divider
				fmt.Printf("  %s %s %s\n",
					gcolor.HEX("#64748b").Sprint("────"),
					gcolor.HEX("#e8b04a").Sprint("["+n+"]"),
					gcolor.HEX("#64748b").Sprint("────"),
				)
				for _, t := range ns[n] {
					fmt.Printf("    %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-34s", t.Name),
						gcolor.HEX("#94a3b8").Sprint(truncate(t.Description, 60)),
					)
				}
			}
			fmt.Println()
			return nil
		},
	}
}

// ─── /agent ──────────────────────────────────────────────────────────────────

func (r *Registry) agentCmd() Command {
	return Command{
		Name:    "agent",
		Aliases: []string{"agents"},
		Usage:   "/agent [ls|dispatch <mode> <msg>|chat <id> <msg>|info <id>|channels <id>|bind <id> <ch>|unbind <id> <ch>]",
		Short:   "List, create, chat with, or manage agent channels",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "dispatch":
				// /agent dispatch [mode] <msg...>
				// mode is optional — defaults to "balanced"
				validModes := map[string]bool{
					"focused": true, "balanced": true,
					"exploratory": true, "deliberate": true,
				}
				mode := "balanced"
				var msgArgs []string
				if len(args) >= 2 && validModes[args[1]] {
					mode = args[1]
					msgArgs = args[2:]
				} else {
					msgArgs = args[1:]
				}
				if len(msgArgs) == 0 {
					return fmt.Errorf("usage: /agent dispatch [focused|balanced|exploratory|deliberate] <message>")
				}
				message := strings.Join(msgArgs, " ")

				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Creating agent (mode: %s)...", mode))

				agentResp, err := r.gw.CreateAgent(ctx, client.CreateAgentRequest{
					WorkspaceRoot: ".",
					ActiveMode:    mode,
				})
				if err != nil {
					return fmt.Errorf("could not create agent: %w", err)
				}

				shortID := agentResp.AgentID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				fmt.Printf("  %s %s",
					gcolor.HEX("#f4a261").Sprint("Agent:"),
					gcolor.HEX("#e8b04a").Sprint(shortID),
				)
				if agentResp.Disposition != nil {
					fmt.Printf("  %s", gcolor.HEX("#94a3b8").Sprintf(
						"pacing=%s depth=%s",
						agentResp.Disposition.Pacing,
						agentResp.Disposition.Depth,
					))
				}
				fmt.Println()
				fmt.Println()

				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

				// Persist agent to local state.
				if st, loadErr := state.Load(); loadErr == nil {
					st.AddAgent(agentResp.AgentID, mode)
					if saveErr := st.Save(); saveErr != nil {
						fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  [warn] could not save state: %v", saveErr))
					}
				}

				return r.streamAgentChat(ctx, agentResp.AgentID, message)

			case "chat":
				// /agent chat <id> <msg...>
				if len(args) < 3 {
					return fmt.Errorf("usage: /agent chat <agent-id> <message>")
				}
				agentID := args[1]
				message := strings.Join(args[2:], " ")
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))
				chatErr := r.streamAgentChat(ctx, agentID, message)

				// Update last_used for this agent.
				if st, loadErr := state.Load(); loadErr == nil {
					st.TouchAgent(agentID)
					if saveErr := st.Save(); saveErr != nil {
						fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  [warn] could not save state: %v", saveErr))
					}
				}

				return chatErr

			case "info":
				// /agent info <id>
				if len(args) < 2 {
					return fmt.Errorf("usage: /agent info <agent-id>")
				}
				agentID := args[1]
				detail, err := r.gw.GetAgent(ctx, agentID)
				if err != nil {
					return fmt.Errorf("could not fetch agent detail: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Agent: %s\n\n", agentID))
				printKV("agent_id", detail.AgentID)
				printKV("status", colorStatus(detail.Status))
				if detail.Disposition != nil {
					d := detail.Disposition
					printKV("disposition", fmt.Sprintf("tone=%s pacing=%s depth=%s", d.Tone, d.Pacing, d.Depth))
				} else {
					printKV("disposition", gcolor.HEX("#94a3b8").Sprint("(default)"))
				}
				printKV("created_at", detail.CreatedAt)
				if len(detail.Channels) > 0 {
					printKV("channels", strings.Join(detail.Channels, ", "))
				} else {
					printKV("channels", gcolor.HEX("#94a3b8").Sprint("(none)"))
				}
				if len(detail.Config) > 0 {
					b, jsonErr := json.MarshalIndent(detail.Config, "    ", "  ")
					if jsonErr == nil {
						fmt.Printf("%s\n    %s\n",
							gcolor.HEX("#94a3b8").Sprintf("  %-24s", "config"),
							color.WhiteString(string(b)),
						)
					}
				}
				fmt.Println()
				return nil

			case "channels":
				// /agent channels <id>
				if len(args) < 2 {
					return fmt.Errorf("usage: /agent channels <agent-id>")
				}
				agentID := args[1]
				channels, err := r.gw.ListAgentChannels(ctx, agentID)
				if err != nil {
					return fmt.Errorf("could not list agent channels: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Channels for %s (%d)\n\n", agentID, len(channels)))
				if len(channels) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No channels bound."))
				}
				for _, ch := range channels {
					fmt.Printf("  %s\n", gcolor.HEX("#f4a261").Sprint(ch))
				}
				fmt.Println()
				return nil

			case "bind":
				// /agent bind <id> <channel>
				if len(args) < 3 {
					return fmt.Errorf("usage: /agent bind <agent-id> <channel>")
				}
				agentID := args[1]
				channel := args[2]
				if err := r.gw.BindAgentChannels(ctx, agentID, []string{channel}); err != nil {
					return fmt.Errorf("could not bind channel: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Channel bound"))
				printKV("agent", agentID)
				printKV("channel", channel)
				fmt.Println()
				return nil

			case "unbind":
				// /agent unbind <id> <channel>
				if len(args) < 3 {
					return fmt.Errorf("usage: /agent unbind <agent-id> <channel>")
				}
				agentID := args[1]
				channel := args[2]
				if err := r.gw.UnbindAgentChannel(ctx, agentID, channel); err != nil {
					return fmt.Errorf("could not unbind channel: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Channel unbound"))
				printKV("agent", agentID)
				printKV("channel", channel)
				fmt.Println()
				return nil

			default: // ls
				agents, err := r.gw.Agents(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch agents: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Agents (%d)\n\n", len(agents)))
				if len(agents) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No agents registered. Start the gateway with agent configs."))
				}
				for _, a := range agents {
					status := colorStatus(a.Status)
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-32s", a.AgentID),
						status,
					)
					if a.Disposition != nil {
						fmt.Println(gcolor.HEX("#94a3b8").Sprintf("    tone=%s pacing=%s", a.Disposition.Tone, a.Disposition.Pacing))
					}
				}

				// Show recently used local agents from state.
				if st, loadErr := state.Load(); loadErr == nil {
					recent := st.RecentAgents(5)
					if len(recent) > 0 {
						fmt.Println()
						fmt.Println(gcolor.HEX("#94a3b8").Sprint("  ──── [recent] ────"))
						for _, a := range recent {
							shortID := a.AgentID
							if len(shortID) > 8 {
								shortID = shortID[:8]
							}
							lastUsedAgo := fmtAgo(a.LastUsed)
							fmt.Printf("  %s  %-12s  %s\n",
								gcolor.HEX("#f4a261").Sprint(shortID),
								gcolor.HEX("#e8b04a").Sprint(a.Mode),
								gcolor.HEX("#94a3b8").Sprintf("last used: %s", lastUsedAgo),
							)
						}
					}
				}
				fmt.Println()
				return nil
			}
		},
	}
}

// streamAgentChat sends a message to an agent and streams the SSE response.
// Thinking and tool-call events are rendered in dim colors; text is printed inline.
func (r *Registry) streamAgentChat(ctx context.Context, agentID, message string) error {
	req := client.AgentChatRequest{
		Message: message,
		Stream:  true,
	}

	err := r.gw.AgentChatStream(ctx, agentID, req, func(chunk client.SSEChunk) {
		switch chunk.Event {
		case "thinking":
			fmt.Print(gcolor.HEX("#94a3b8").Sprint("\n  [Thinking] " + truncate(chunk.Data, 80)))
		case "tool_call":
			fmt.Print(gcolor.HEX("#457b9d").Sprintf("\n  [Tool: %s]", truncate(chunk.Data, 60)))
		case "tool_result":
			// absorbed into the response
		default:
			if text := agentExtractText(chunk.Data); text != "" {
				fmt.Print(text)
			}
		}
	})

	fmt.Println()
	fmt.Println()
	return err
}

// agentExtractText pulls readable text from an agent SSE data field.
func agentExtractText(data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		for _, key := range []string{"text", "content", "message", "delta"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}
	return data
}

// ─── /skill ──────────────────────────────────────────────────────────────────

func (r *Registry) skillCmd() Command {
	return Command{
		Name:    "skill",
		Aliases: []string{"skills"},
		Usage:   "/skill [ls [filter]|get <name>|inspect <hash>|tags]",
		Short:   "List, fetch, or inspect skills from CAS",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "get":
				// /skill get <name>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill get <name>")
				}
				name := args[1]
				tag, err := r.gw.CASResolveTag(ctx, name, "latest")
				if err != nil {
					return fmt.Errorf("could not resolve tag %q: %w", name, err)
				}
				content, err := r.gw.CASGetContent(ctx, tag.Ref)
				if err != nil {
					return fmt.Errorf("could not fetch content for ref %q: %w", tag.Ref, err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Skill: %s @ %s\n\n", tag.Name, tag.Version))
				printKV("ref", tag.Ref)
				fmt.Println()
				fmt.Println(color.WhiteString(string(content)))
				fmt.Println()
				return nil

			case "inspect":
				// /skill inspect <hash>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill inspect <hash>")
				}
				ref := args[1]
				content, err := r.gw.CASGetContent(ctx, ref)
				if err != nil {
					return fmt.Errorf("could not fetch content for ref %q: %w", ref, err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  CAS ref: %s\n\n", ref))
				fmt.Println(color.WhiteString(string(content)))
				fmt.Println()
				return nil

			case "tags":
				// /skill tags
				tags, err := r.gw.CASListTags(ctx)
				if err != nil {
					return fmt.Errorf("could not list CAS tags: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  CAS Tags (%d)\n\n", len(tags)))
				if len(tags) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No tags found."))
					fmt.Println()
					return nil
				}
				// Table header
				fmt.Printf("  %s  %s  %s\n",
					gcolor.HEX("#94a3b8").Sprintf("%-32s", "Name"),
					gcolor.HEX("#94a3b8").Sprintf("%-12s", "Version"),
					gcolor.HEX("#94a3b8").Sprint("Ref"),
				)
				fmt.Printf("  %s\n", gcolor.HEX("#64748b").Sprint(strings.Repeat("─", 72)))
				for _, t := range tags {
					fmt.Printf("  %s  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-32s", truncate(t.Name, 32)),
						color.WhiteString("%-12s", truncate(t.Version, 12)),
						gcolor.HEX("#94a3b8").Sprint(truncate(t.Ref, 20)),
					)
				}
				fmt.Println()
				return nil

			default: // ls (sub may be "ls" or a filter term)
				// args[0] may be "ls" or a filter term
				filter := ""
				for _, a := range args {
					if a != "ls" {
						filter = strings.ToLower(a)
					}
				}

				skills, err := r.gw.Skills(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch skills: %w", err)
				}

				// Filter
				if filter != "" {
					var filtered []client.Skill
					for _, s := range skills {
						if strings.Contains(strings.ToLower(s.Name), filter) ||
							strings.Contains(strings.ToLower(s.Plugin), filter) {
							filtered = append(filtered, s)
						}
					}
					skills = filtered
				}

				fmt.Println()
				if filter != "" {
					color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Skills matching %q (%d)\n\n", filter, len(skills)))
				} else {
					color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Skills (%d)\n\n", len(skills)))
				}

				if len(skills) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No skills found."))
					fmt.Println()
					return nil
				}

				// Group by category
				cats := map[string][]client.Skill{}
				order := []string{}
				for _, s := range skills {
					cat := s.Category
					if cat == "" {
						cat = "general"
					}
					if _, seen := cats[cat]; !seen {
						order = append(order, cat)
					}
					cats[cat] = append(cats[cat], s)
				}
				for _, cat := range order {
					// Glass-effect section divider
					fmt.Printf("  %s %s %s\n",
						gcolor.HEX("#64748b").Sprint("────"),
						gcolor.HEX("#e8b04a").Sprint("["+cat+"]"),
						gcolor.HEX("#64748b").Sprint("────"),
					)
					for _, s := range cats[cat] {
						plugin := ""
						if s.Plugin != "" {
							plugin = gcolor.HEX("#94a3b8").Sprintf("(%s)", s.Plugin)
						}
						fmt.Printf("    %s %s\n",
							gcolor.HEX("#f4a261").Sprintf("%-40s", s.Name),
							plugin,
						)
					}
				}
				fmt.Println()
				return nil
			}
		},
	}
}

// ─── /garden ─────────────────────────────────────────────────────────────────

func (r *Registry) gardenCmd() Command {
	return Command{
		Name:    "garden",
		Aliases: []string{"seeds", "memory"},
		Usage:   "/garden [ls|stats|plant <text>|harvest]",
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
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Garden Stats"))
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
				color.New(color.Bold).Print(gcolor.HEX("#7fb88c").Sprint("  Seed planted"))
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
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Search results (%d)\n\n", len(results)))
				if len(results) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No results found."))
					fmt.Println()
					return nil
				}
				for _, m := range results {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-24s", m.ID),
						color.WhiteString(truncate(m.Content, 80)),
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

			case "harvest": // alias for ls
				fallthrough
			default: // ls
				seeds, err := r.gw.Seeds(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch seeds: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Seeds (%d)\n\n", len(seeds)))
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

// ─── /trail ──────────────────────────────────────────────────────────────────

func (r *Registry) trailCmd() Command {
	return Command{
		Name:  "trail",
		Usage: "/trail [add <text>|rm <id>|search <query>]",
		Short: "Show memory timeline or add/remove/search memories",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// default: list
				memories, err := r.gw.Memories(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch memory trail: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Memory Trail (%d)\n\n", len(memories)))
				if len(memories) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No memory entries yet."))
					fmt.Println()
					return nil
				}
				for _, m := range memories {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-20s", m.CreatedAt),
						color.WhiteString(truncate(m.Content, 80)),
					)
				}
				fmt.Println()
				return nil
			}

			sub := strings.ToLower(args[0])
			switch sub {
			case "add":
				// /trail add <text...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail add <text>")
				}
				text := strings.Join(args[1:], " ")
				mem, err := r.gw.StoreMemory(ctx, client.StoreMemoryRequest{Content: text})
				if err != nil {
					return fmt.Errorf("could not store memory: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory stored"))
				if mem != nil {
					printKV("id", mem.ID)
				}
				fmt.Println()

			case "rm":
				// /trail rm <id>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail rm <id>")
				}
				id := args[1]
				if err := r.gw.DeleteMemory(ctx, id); err != nil {
					return fmt.Errorf("could not delete memory: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory deleted"))
				fmt.Println()

			case "search":
				// /trail search <query...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail search <query>")
				}
				query := strings.Join(args[1:], " ")
				results, err := r.gw.SearchMemories(ctx, query)
				if err != nil {
					return fmt.Errorf("could not search memories: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Search results (%d)\n\n", len(results)))
				if len(results) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No results found."))
					fmt.Println()
					return nil
				}
				for _, m := range results {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-24s", m.ID),
						color.WhiteString(truncate(m.Content, 80)),
					)
				}
				fmt.Println()

			default:
				return fmt.Errorf("unknown trail subcommand %q — use: add, rm, search", sub)
			}
			return nil
		},
	}
}

// ─── /snapshot ───────────────────────────────────────────────────────────────

func (r *Registry) snapshotCmd() Command {
	return Command{
		Name:  "snapshot",
		Usage: "/snapshot [save|restore <id>|export <id>|rm <id>]",
		Short: "List, save, restore, export, or delete memory snapshots",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "save":
				snap, err := r.gw.CreateSnapshot(ctx, *r.session)
				if err != nil {
					return fmt.Errorf("could not create snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot saved"))
				if snap != nil {
					printKV("id", snap.ID)
					printKV("session", snap.SessionID)
					printKV("created", snap.CreatedAt)
				}
				fmt.Println()

			case "restore":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot restore <id>")
				}
				id := args[1]
				if err := r.gw.RestoreSnapshot(ctx, id); err != nil {
					return fmt.Errorf("could not restore snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot restored"))
				printKV("id", id)
				fmt.Println()

			case "export":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot export <id>")
				}
				id := args[1]
				data, err := r.gw.ExportSnapshot(ctx, id)
				if err != nil {
					return fmt.Errorf("could not export snapshot: %w", err)
				}
				fmt.Println(string(data))

			case "rm":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot rm <id>")
				}
				id := args[1]
				if err := r.gw.DeleteSnapshot(ctx, id); err != nil {
					return fmt.Errorf("could not delete snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot deleted"))
				fmt.Println()

			default: // ls
				snaps, err := r.gw.ListSnapshots(ctx, *r.session)
				if err != nil {
					return fmt.Errorf("could not fetch snapshots: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Snapshots (%d)\n\n", len(snaps)))
				if len(snaps) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No snapshots found. Use /snapshot save to create one."))
					fmt.Println()
					return nil
				}
				for _, s := range snaps {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-36s", s.ID),
						gcolor.HEX("#94a3b8").Sprint(s.CreatedAt),
					)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

// ─── /trace ──────────────────────────────────────────────────────────────────

func (r *Registry) traceCmd() Command {
	return Command{
		Name:  "trace",
		Usage: "/trace [<id>]",
		Short: "Inspect an execution trace by ID, or show guidance",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// Guidance mode
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Dojo Trace"))
				fmt.Println()
				fmt.Println()
				fmt.Println(gcolor.HEX("#457b9d").Sprint("  Trace follows the active session's decision and tool-use history."))
				fmt.Println(gcolor.HEX("#457b9d").Sprint("  Connect the gateway with --trace to enable full trace output."))
				fmt.Println()
				printKV("gateway", r.cfg.Gateway.URL)
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  hint: /trace <id>  — provide a trace ID to inspect"))
				fmt.Println()
				return nil
			}

			traceID := args[0]
			data, err := r.gw.GetTrace(ctx, traceID)
			if err != nil {
				return fmt.Errorf("could not fetch trace: %w", err)
			}
			fmt.Println()
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Trace: %s\n\n", traceID))
			for k, v := range data {
				switch val := v.(type) {
				case map[string]any, []any:
					// Format nested structures as indented JSON
					b, jsonErr := json.MarshalIndent(val, "    ", "  ")
					if jsonErr != nil {
						printKV(k, fmt.Sprintf("%v", val))
					} else {
						fmt.Printf("%s\n    %s\n",
							gcolor.HEX("#94a3b8").Sprintf("  %-24s", k),
							color.WhiteString(string(b)),
						)
					}
				default:
					printKV(k, fmt.Sprintf("%v", val))
				}
			}
			fmt.Println()
			return nil
		},
	}
}

// ─── /pilot ──────────────────────────────────────────────────────────────────

func (r *Registry) pilotCmd() Command {
	return Command{
		Name:  "pilot",
		Usage: "/pilot [plain]",
		Short: "Live SSE event dashboard (Ctrl+C to stop)",
		Run: func(ctx context.Context, args []string) error {
			clientID := fmt.Sprintf("dojo-cli-%d", time.Now().UnixMilli())

			// /pilot plain — fallback text mode
			if len(args) > 0 && args[0] == "plain" {
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Pilot — live event stream  (Ctrl+C to stop)"))
				fmt.Println()
				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  client_id: %s", clientID))
				fmt.Println()

				return r.gw.PilotStream(ctx, clientID, func(chunk client.SSEChunk) {
					ev := chunk.Event
					if ev == "" {
						ev = "message"
					}
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#457b9d").Sprintf("%-16s", ev),
						color.WhiteString(truncate(chunk.Data, 100)),
					)
				})
			}

			// Default: Bubbletea TUI dashboard
			model := tui.NewPilotModel(r.gw, clientID)
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

// ─── /hooks ──────────────────────────────────────────────────────────────────

func (r *Registry) hooksCmd() Command {
	return Command{
		Name:  "hooks",
		Usage: "/hooks [ls|fire <event>]",
		Short: "List loaded hook rules or fire an event manually",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "fire":
				if len(args) < 2 {
					return fmt.Errorf("usage: /hooks fire <event>")
				}
				event := args[1]
				fmt.Printf("\n  Firing event %q ...\n\n", event)
				if err := r.runner.Fire(ctx, event, nil); err != nil {
					return fmt.Errorf("hook fire error: %w", err)
				}
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  done"))
				fmt.Println()

			default: // ls
				// Count total rules
				totalRules := 0
				for _, p := range r.plgs {
					totalRules += len(p.HookRules)
				}

				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Hooks (%d rules across %d plugins)\n\n", totalRules, len(r.plgs)))

				if totalRules == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  No hook rules loaded. Place plugins in %s", r.cfg.Plugins.Path))
					fmt.Println()
					return nil
				}

				for _, p := range r.plgs {
					if len(p.HookRules) == 0 {
						continue
					}
					// Glass-effect section divider
					fmt.Printf("  %s %s %s\n",
						gcolor.HEX("#64748b").Sprint("────"),
						gcolor.HEX("#e8b04a").Sprint("["+p.Name+"]"),
						gcolor.HEX("#64748b").Sprint("────"),
					)
					for _, rule := range p.HookRules {
						for _, h := range rule.Hooks {
							asyncLabel := ""
							if h.Async {
								asyncLabel = gcolor.HEX("#94a3b8").Sprint("  (async)")
							}
							cmd := h.Command
							if cmd == "" {
								cmd = h.Prompt
							}
							if cmd == "" {
								cmd = h.URL
							}
							fmt.Printf("    %s  %s  %s%s\n",
								gcolor.HEX("#f4a261").Sprintf("%-20s", rule.Event),
								color.WhiteString("%-10s", h.Type),
								gcolor.HEX("#94a3b8").Sprint(truncate(cmd, 50)),
								asyncLabel,
							)
						}
					}
				}
				fmt.Println()
			}
			return nil
		},
	}
}

// ─── /settings ───────────────────────────────────────────────────────────────

func (r *Registry) settingsCmd() Command {
	return Command{
		Name:    "settings",
		Aliases: []string{"config", "cfg"},
		Usage:   "/settings [providers|set <provider> <key>]",
		Short:   "Show active config and settings, or manage provider keys",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// Default: show config summary
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Active Settings"))
				fmt.Println()
				fmt.Println()
				printKV("config file", config.SettingsPath())
				printKV("gateway.url", r.cfg.Gateway.URL)
				printKV("gateway.timeout", r.cfg.Gateway.Timeout)
				printKV("plugins.path", r.cfg.Plugins.Path)
				printKV("plugins loaded", fmt.Sprintf("%d", len(r.plgs)))
				printKV("defaults.provider", orDefault(r.cfg.Defaults.Provider, "(auto)"))
				printKV("defaults.disposition", r.cfg.Defaults.Disposition)
				printKV("defaults.model", orDefault(r.cfg.Defaults.Model, "(auto)"))
				fmt.Println()
				return nil
			}

			sub := strings.ToLower(args[0])
			switch sub {
			case "providers":
				providerSettings, err := r.gw.GetProviderSettings(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch provider settings: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Provider Configuration"))
				fmt.Println()
				fmt.Println()
				for k, v := range providerSettings {
					printKV(k, colorStatus(fmt.Sprintf("%v", v)))
				}
				fmt.Println()

			case "set":
				// /settings set <provider> <key>
				if len(args) < 3 {
					return fmt.Errorf("usage: /settings set <provider> <api-key>")
				}
				provider := args[1]
				apiKey := args[2]
				if err := r.gw.SetProviderKey(ctx, provider, apiKey); err != nil {
					return fmt.Errorf("could not set provider key: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Provider key updated"))
				printKV("provider", provider)
				fmt.Println()

			default:
				return fmt.Errorf("unknown settings subcommand %q — use: providers, set", sub)
			}
			return nil
		},
	}
}

// ─── /session ────────────────────────────────────────────────────────────────

func (r *Registry) sessionCmd() Command {
	return Command{
		Name:  "session",
		Usage: "/session [new|<id>]",
		Short: "Show or change the active session ID",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				fmt.Println()
				printKV("session", *r.session)
				fmt.Println()
				return nil
			}
			switch args[0] {
			case "new":
				*r.session = fmt.Sprintf("dojo-cli-%s", time.Now().Format("20060102-150405"))
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  New session started"))
				printKV("session", *r.session)
				fmt.Println()
			default:
				*r.session = args[0]
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Session resumed"))
				printKV("session", *r.session)
				fmt.Println()
			}
			return nil
		},
	}
}

// ─── /run ─────────────────────────────────────────────────────────────────────

func (r *Registry) runCmd() Command {
	return Command{
		Name:  "run",
		Usage: "/run <task description>",
		Short: "Send a multi-step task to the gateway and stream the response",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /run <task description>")
			}
			task := strings.Join(args, " ")

			// MVP approach: send the task through ChatStream and let the
			// gateway's intent classifier route complex multi-step tasks to
			// its orchestration path internally. This is simpler and more
			// reliable than constructing a DAG client-side.
			req := client.ChatRequest{
				Message:   task,
				Model:     r.cfg.Defaults.Model,
				SessionID: *r.session,
				Stream:    true,
			}

			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Running: %s", truncate(task, 60)))
			fmt.Println()

			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

			var fullText strings.Builder
			err := r.gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
				if text := agentExtractText(chunk.Data); text != "" {
					fmt.Print(text)
					fullText.WriteString(text)
				}
			})

			fmt.Println()
			fmt.Println()
			return err
		},
	}
}

// ─── /practice ───────────────────────────────────────────────────────────────

func (r *Registry) practiceCmd() Command {
	return Command{
		Name:  "practice",
		Usage: "/practice",
		Short: "Daily reflection prompts (rotates by day of week)",
		Run: func(ctx context.Context, args []string) error {
			now := time.Now()
			dayName := now.Weekday().String()

			var prompts []string
			switch now.Weekday() {
			case time.Monday:
				prompts = []string{
					"What tensions are you noticing?",
					"What surprised you last week?",
					"What would you do differently?",
				}
			case time.Tuesday:
				prompts = []string{
					"What's the riskiest assumption right now?",
					"Where are you over-invested?",
					"What can you let go of?",
				}
			case time.Wednesday:
				prompts = []string{
					"What's working that you should double down on?",
					"Who needs your attention?",
					"What decision are you avoiding?",
				}
			case time.Thursday:
				prompts = []string{
					"What would you ship today if forced to?",
					"Where is complexity hiding?",
					"What's the simplest next step?",
				}
			case time.Friday:
				prompts = []string{
					"What did you learn this week?",
					"What would you celebrate?",
					"What would you change?",
				}
			default: // Saturday, Sunday
				prompts = []string{
					"Rest. Reflect. Return Monday with clarity.",
				}
			}

			// Bonsai sigil — contemplative anchor for practice
			fmt.Print(art.LargeBonsaiString())

			// Header: date in warm-amber, day in golden-orange
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Practice — " + now.Format("2006-01-02")))
			fmt.Print("  ")
			fmt.Println(gcolor.HEX("#f4a261").Sprint(dayName))
			fmt.Println()
			for i, p := range prompts {
				fmt.Printf("  %s %s\n",
					gcolor.HEX("#e8b04a").Sprintf("%d.", i+1),
					gcolor.HEX("#94a3b8").Sprint(p),
				)
			}
			fmt.Println()
			return nil
		},
	}
}

// ─── /projects ───────────────────────────────────────────────────────────────

func (r *Registry) projectsCmd() Command {
	return Command{
		Name:  "projects",
		Usage: "/projects ls",
		Short: "Local workspace view — cwd, plugins, session",
		Run: func(ctx context.Context, args []string) error {
			fmt.Println()
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  Projects — local workspace"))
			fmt.Println()
			fmt.Println()

			// Current working directory name as the project
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "(unknown)"
			}
			project := filepath.Base(cwd)
			printKV("project", project)
			printKV("path", cwd)

			// Check for .ada/disposition.yaml
			adaPath := filepath.Join(cwd, ".ada", "disposition.yaml")
			if data, readErr := os.ReadFile(adaPath); readErr == nil {
				// Extract active_mode from the YAML with a simple scan (no yaml dep needed)
				activeMode := ""
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "active_mode:") {
						activeMode = strings.TrimSpace(strings.TrimPrefix(line, "active_mode:"))
						break
					}
				}
				if activeMode == "" {
					activeMode = "(set)"
				}
				printKV("disposition", activeMode)
			} else {
				printKV("disposition", gcolor.HEX("#94a3b8").Sprint("no .ada/disposition.yaml"))
			}

			printKV("plugins loaded", fmt.Sprintf("%d", len(r.plgs)))
			printKV("session", *r.session)
			fmt.Println()
			return nil
		},
	}
}

// ─── /apps ───────────────────────────────────────────────────────────────────

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
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  App Status\n\n"))
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
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Result: %s/%s\n\n", appName, toolName))
				b, jsonErr := json.MarshalIndent(result, "  ", "  ")
				if jsonErr != nil {
					return fmt.Errorf("could not marshal result: %w", jsonErr)
				}
				fmt.Println(color.WhiteString("  " + string(b)))
				fmt.Println()
				return nil

			default: // ls
				apps, err := r.gw.ListApps(ctx)
				if err != nil {
					return fmt.Errorf("could not list apps: %w", err)
				}
				fmt.Println()
				color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  MCP Apps (%d)\n\n", len(apps)))
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

// ─── /workflow ────────────────────────────────────────────────────────────────

func (r *Registry) workflowCmd() Command {
	return Command{
		Name:  "workflow",
		Usage: "/workflow <name> [input-json]",
		Short: "Execute a workflow and stream progress",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /workflow <name> [input-json]")
			}
			name := args[0]

			// Parse optional JSON input
			var input map[string]any
			if len(args) >= 2 {
				inputJSON := strings.Join(args[1:], " ")
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("invalid input JSON: %w", err)
				}
			}
			if input == nil {
				input = map[string]any{}
			}

			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Executing workflow: %s", name))

			resp, err := r.gw.ExecuteWorkflow(ctx, name, input)
			if err != nil {
				return fmt.Errorf("could not execute workflow: %w", err)
			}

			printKV("run_id", resp.RunID)
			printKV("status", colorStatus(resp.Status))
			fmt.Println()

			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

			// Stream progress
			err = r.gw.WorkflowExecutionStream(ctx, resp.RunID, func(chunk client.SSEChunk) {
				switch chunk.Event {
				case "thinking":
					fmt.Print(gcolor.HEX("#94a3b8").Sprint("\n  [Thinking] " + truncate(chunk.Data, 80)))
				case "tool_call":
					fmt.Print(gcolor.HEX("#457b9d").Sprintf("\n  [Tool: %s]", truncate(chunk.Data, 60)))
				case "tool_result":
					// absorbed into the response
				default:
					if text := agentExtractText(chunk.Data); text != "" {
						fmt.Print(text)
					}
				}
			})

			fmt.Println()
			fmt.Println()
			return err
		},
	}
}

// ─── /doc ─────────────────────────────────────────────────────────────────────

func (r *Registry) docCmd() Command {
	return Command{
		Name:  "doc",
		Usage: "/doc <id>",
		Short: "Fetch and display a document by ID",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /doc <id>")
			}
			id := args[0]
			doc, err := r.gw.GetDocument(ctx, id)
			if err != nil {
				return fmt.Errorf("could not fetch document: %w", err)
			}
			fmt.Println()
			color.New(color.Bold).Print(gcolor.HEX("#e8b04a").Sprintf("  Document: %s\n\n", id))
			for k, v := range doc {
				switch val := v.(type) {
				case map[string]any, []any:
					b, jsonErr := json.MarshalIndent(val, "    ", "  ")
					if jsonErr != nil {
						printKV(k, fmt.Sprintf("%v", val))
					} else {
						fmt.Printf("%s\n    %s\n",
							gcolor.HEX("#94a3b8").Sprintf("  %-24s", k),
							color.WhiteString(string(b)),
						)
					}
				default:
					printKV(k, fmt.Sprintf("%v", val))
				}
			}
			fmt.Println()
			return nil
		},
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// printKV prints a key-value pair: key in cloud-gray, value in white.
func printKV(key, value string) {
	fmt.Printf("%s%s\n",
		gcolor.HEX("#94a3b8").Sprintf("  %-24s", key),
		color.WhiteString(value),
	)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
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

// ─── /init ────────────────────────────────────────────────────────────────────

func (r *Registry) initCmd() Command {
	return Command{
		Name:    "init",
		Aliases: []string{"setup", "bootstrap"},
		Usage:   "/init [--force] [--gateway <url>] [--plugins-source <path>]",
		Short:   "Initialize Dojo workspace with plugins, dispositions, and seeds",
		Run: func(ctx context.Context, args []string) error {
			opts := bootstrap.Options{
				GatewayURL: r.cfg.Gateway.URL,
			}
			for i := 0; i < len(args); i++ {
				switch args[i] {
				case "--force":
					opts.Force = true
				case "--gateway":
					if i+1 < len(args) {
						i++
						opts.GatewayURL = args[i]
					}
				case "--plugins-source":
					if i+1 < len(args) {
						i++
						opts.PluginsSource = args[i]
					}
				case "--skip-seeds":
					opts.SkipSeeds = true
				}
			}

			_, err := bootstrap.Run(ctx, opts, r.gw, os.Stdout)
			return err
		},
	}
}

// fmtAgo formats an RFC3339 timestamp as a human-readable "X ago" string.
func fmtAgo(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil || ts == "" {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
