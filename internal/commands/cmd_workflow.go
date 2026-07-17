package commands

// cmd_workflow.go — /workflow, /run, /doc, /pilot, /practice, /tools commands.
// (/skill moved to cmd_skill.go.)

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DojoGenesis/cli/internal/art"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/mdrender"
	"github.com/DojoGenesis/cli/internal/orchestration"
	"github.com/DojoGenesis/cli/internal/tui"
	gcolor "github.com/gookit/color"
)

// ─── /workflow ───────────────────────────────────────────────────────────────

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

			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

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

// ─── /run ────────────────────────────────────────────────────────────────────

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

			// Guard: reject bare command names that look like misrouted slash commands.
			// If the task is a single token that matches a registered command (or
			// alias), the user almost certainly meant "/run" + a command name by
			// mistake (e.g. "/run pilot" instead of "/pilot"). Sending it to
			// /v1/chat would produce confusing results and expose command names to
			// the intent classifier.
			taskLower := strings.ToLower(strings.TrimSpace(task))
			if !strings.ContainsRune(taskLower, ' ') {
				if _, isCmd := r.cmds[taskLower]; isCmd {
					return fmt.Errorf("/%s is a dojo command, not a task — did you mean /%s?", taskLower, taskLower)
				}
				// Also check aliases.
				for _, cmd := range r.cmds {
					for _, alias := range cmd.Aliases {
						if alias == taskLower {
							return fmt.Errorf("/%s is a dojo command, not a task — did you mean /%s?", taskLower, cmd.Name)
						}
					}
				}
			}

			// Check for --dag flag: strip it from args and force NL-based DAG mode.
			forceDAG := false
			{
				var filtered []string
				for _, a := range args {
					if a == "--dag" {
						forceDAG = true
					} else {
						filtered = append(filtered, a)
					}
				}
				if forceDAG {
					args = filtered
					task = strings.Join(args, " ")
				}
			}

			if forceDAG {
				plan := orchestration.ParseTaskToDAG(task)

				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  NL-DAG plan: %s", plan.Name))
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Nodes (%d):", len(plan.DAG)))
				for _, node := range plan.DAG {
					deps := ""
					if len(node.DependsOn) > 0 {
						deps = gcolor.HEX("#64748b").Sprintf("  ← %s", strings.Join(node.DependsOn, ", "))
					}
					fmt.Printf("    %s  %s%s\n",
						gcolor.HEX("#f4a261").Sprintf("%-10s", node.ID),
						gcolor.White.Sprint(node.ToolName),
						deps,
					)
				}
				fmt.Println()

				userID := r.cfg.Auth.UserID
				status, err := r.gw.Orchestrate(ctx, client.OrchestrateRequest{
					Plan:   plan,
					UserID: userID,
				})
				if err == nil {
					printKV("execution_id", status.ExecutionID)
					printKV("status", colorStatus(status.Status))
					fmt.Println()

					return r.pollDAGUntilTerminal(ctx, status.ExecutionID)
				}
				// Orchestration failed — fall through to ChatStream MVP.
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  orchestration unavailable (%v), falling back to chat", err))
				fmt.Println()
			}

			// Try client-side DAG template matching first.
			if tmpl := orchestration.MatchTemplate(task); tmpl != nil {
				plan := tmpl.Build(task)

				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  DAG template: %s", tmpl.Name))
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Plan: %s (%d nodes)", plan.Name, len(plan.DAG)))
				fmt.Println()

				userID := r.cfg.Auth.UserID
				status, err := r.gw.Orchestrate(ctx, client.OrchestrateRequest{
					Plan:   plan,
					UserID: userID,
				})
				if err == nil {
					printKV("execution_id", status.ExecutionID)
					printKV("status", colorStatus(status.Status))
					fmt.Println()

					return r.pollDAGUntilTerminal(ctx, status.ExecutionID)
				}
				// Orchestration failed — fall through to ChatStream MVP.
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  orchestration unavailable (%v), falling back to chat", err))
				fmt.Println()
			}

			// Fallback: ChatStream MVP.
			workspaceRoot, _ := os.Getwd()
			req := client.ChatRequest{
				Message:       task,
				Model:         r.cfg.Defaults.Model,
				Provider:      r.cfg.Defaults.Provider,
				SessionID:     *r.session,
				Stream:        true,
				WorkspaceRoot: workspaceRoot,
			}

			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Running: %s", truncate(task, 60)))
			fmt.Println()

			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

			var fullText strings.Builder
			var streamErrMsg string
			err := r.gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
				// Capture gateway error events (e.g. rate limit, agent failure)
				if chunk.Event == "error" {
					var m map[string]any
					if json.Unmarshal([]byte(chunk.Data), &m) == nil {
						if e, ok := m["error"].(string); ok && e != "" {
							streamErrMsg = e
						}
					}
					return
				}
				if text := agentExtractText(chunk.Data); text != "" {
					fmt.Print(text)
					fullText.WriteString(text)
				}
			})

			fmt.Println()
			fmt.Println()

			if streamErrMsg != "" {
				fmt.Println(gcolor.HEX("#ef4444").Sprintf("  [agent error: %s]", truncate(streamErrMsg, 120)))
				fmt.Println()
			} else if fullText.Len() == 0 && err == nil {
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [no response — the agent may have hit a rate limit or internal error]"))
				fmt.Println()
			}

			return err
		},
	}
}

// pollDAGUntilTerminal polls the orchestration DAG for executionID, printing
// node status after every poll, until the DAG reaches a terminal state
// ("completed" or "failed") or a poll fails. It is shared by both /run DAG
// paths (--dag NL-parse and client-side template match), which previously
// duplicated this loop verbatim.
//
// On a poll error this prints a message and returns nil — mirroring the
// pre-extraction behavior of both call sites, which treated "stop polling"
// as "fall through to success" rather than propagating the error. A poll
// error caused by context cancellation (ctx.Err() != nil — e.g. the user hit
// Ctrl+C) prints a clean "[cancelled]" instead of the raw
// "poll error: context canceled" text.
func (r *Registry) pollDAGUntilTerminal(ctx context.Context, executionID string) error {
	for {
		dag, pollErr := r.gw.OrchestrationDAG(ctx, executionID)
		if pollErr != nil {
			if ctx.Err() != nil {
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [cancelled]"))
			} else {
				fmt.Println(gcolor.HEX("#ef4444").Sprintf("  poll error: %v", pollErr))
			}
			return nil
		}
		r.printDAGNodes(dag.Nodes)
		if dag.Status == "completed" || dag.Status == "failed" {
			fmt.Println()
			printKV("result", colorStatus(dag.Status))
			fmt.Println()
			return nil
		}
		time.Sleep(800 * time.Millisecond)
	}
}

// printDAGNodes renders DAG node status with icons.
func (r *Registry) printDAGNodes(nodes []map[string]any) {
	for _, n := range nodes {
		id, _ := n["id"].(string)
		st, _ := n["status"].(string)
		tool, _ := n["tool_name"].(string)

		var icon string
		switch st {
		case "completed":
			icon = gcolor.HEX("#22c55e").Sprint("\u2713")
		case "running":
			icon = gcolor.HEX("#3b82f6").Sprint("\u2192")
		case "failed":
			icon = gcolor.HEX("#ef4444").Sprint("\u2717")
		default:
			icon = gcolor.HEX("#94a3b8").Sprint("\u25cb")
		}
		fmt.Printf("  %s %-10s %-20s %s\n",
			icon,
			gcolor.HEX("#f4a261").Sprint(id),
			gcolor.White.Sprint(tool),
			gcolor.HEX("#94a3b8").Sprint(st),
		)
	}
}

// ─── /doc ────────────────────────────────────────────────────────────────────

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
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Document: %s\n\n", id))
			for k, v := range doc {
				// "content" is the conventional document-body field (same name
				// CASGetContent uses for a skill body above) — render it as
				// markdown instead of a raw dump so headings, lists, and code
				// fences display properly. Every other field is metadata and
				// keeps the plain key-value / JSON rendering below.
				if k == "content" {
					if s, ok := v.(string); ok {
						fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  %-24s", k))
						fmt.Println()
						fmt.Println(mdrender.RenderMarkdown(s))
						fmt.Println()
						continue
					}
				}
				switch val := v.(type) {
				case map[string]any, []any:
					b, jsonErr := json.MarshalIndent(val, "    ", "  ")
					if jsonErr != nil {
						printKV(k, fmt.Sprintf("%v", val))
					} else {
						fmt.Printf("%s\n    %s\n",
							gcolor.HEX("#94a3b8").Sprintf("  %-24s", k),
							gcolor.White.Sprint(string(b)),
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

// ─── /pilot ─────────────────────────────────────────────────────────────────

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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Pilot — live event stream  (Ctrl+C to stop)"))
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
						gcolor.White.Sprint(truncate(chunk.Data, 100)),
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

// ─── /practice ──────────────────────────────────────────────────────────────

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
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Practice — " + now.Format("2006-01-02")))
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

// ─── /tools ─────────────────────────────────────────────────────────────────

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
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Tools (%d)\n\n", len(tools)))

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
					gcolor.HEX("#64748b").Sprint("\u2500\u2500\u2500\u2500"),
					gcolor.HEX("#e8b04a").Sprint("["+n+"]"),
					gcolor.HEX("#64748b").Sprint("\u2500\u2500\u2500\u2500"),
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
