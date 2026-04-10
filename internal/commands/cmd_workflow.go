package commands

// cmd_workflow.go — /workflow, /run, /doc, /pilot, /practice, /skill, /tools commands.

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
	"github.com/DojoGenesis/dojo-cli/internal/client"
	"github.com/DojoGenesis/dojo-cli/internal/orchestration"
	"github.com/DojoGenesis/dojo-cli/internal/tui"
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

					// Poll DAG until terminal state.
					for {
						dag, pollErr := r.gw.OrchestrationDAG(ctx, status.ExecutionID)
						if pollErr != nil {
							fmt.Println(gcolor.HEX("#ef4444").Sprintf("  poll error: %v", pollErr))
							break
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
					return nil
				}
				// Orchestration failed — fall through to ChatStream MVP.
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  orchestration unavailable (%v), falling back to chat", err))
				fmt.Println()
			}

			// Fallback: ChatStream MVP.
			req := client.ChatRequest{
				Message:   task,
				Model:     r.cfg.Defaults.Model,
				Provider:  r.cfg.Defaults.Provider,
				SessionID: *r.session,
				Stream:    true,
			}

			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Running: %s", truncate(task, 60)))
			fmt.Println()

			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

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

// ─── /skill ─────────────────────────────────────────────────────────────────

func (r *Registry) skillCmd() Command {
	return Command{
		Name:    "skill",
		Aliases: []string{"skills"},
		Usage:   "/skill [ls [filter]|search <query>|get <name>|inspect <hash>|tags|package-all <dir>]",
		Short:   "List, fetch, or inspect skills from CAS",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "search":
				// /skill search <query>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill search <query>")
				}
				query := strings.Join(args[1:], " ")
				skills, err := r.gw.SearchSkills(ctx, query)
				if err != nil {
					return fmt.Errorf("search failed: %w", err)
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skills matching %q (%d)\n\n", query, len(skills)))

				if len(skills) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No matching skills found."))
					fmt.Println()
					return nil
				}

				for _, s := range skills {
					name := gcolor.HEX("#f4a261").Sprintf("%-40s", s.Name)
					cat := ""
					if s.Category != "" {
						cat = gcolor.HEX("#94a3b8").Sprintf("[%s]", s.Category)
					}
					plugin := ""
					if s.Plugin != "" {
						plugin = gcolor.HEX("#64748b").Sprintf(" (%s)", s.Plugin)
					}
					fmt.Printf("    %s %s%s\n", name, cat, plugin)
					if s.Description != "" {
						desc := s.Description
						if len(desc) > 100 {
							desc = desc[:97] + "..."
						}
						fmt.Printf("    %s\n", gcolor.HEX("#94a3b8").Sprint("  "+desc))
					}
				}
				fmt.Println()
				return nil

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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skill: %s @ %s\n\n", tag.Name, tag.Version))
				printKV("ref", tag.Ref)
				fmt.Println()
				fmt.Println(gcolor.White.Sprint(string(content)))
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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  CAS ref: %s\n\n", ref))
				fmt.Println(gcolor.White.Sprint(string(content)))
				fmt.Println()
				return nil

			case "tags":
				// /skill tags
				tags, err := r.gw.CASListTags(ctx)
				if err != nil {
					return fmt.Errorf("could not list CAS tags: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  CAS Tags (%d)\n\n", len(tags)))
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
				fmt.Printf("  %s\n", gcolor.HEX("#64748b").Sprint(strings.Repeat("\u2500", 72)))
				for _, t := range tags {
					fmt.Printf("  %s  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-32s", truncate(t.Name, 32)),
						gcolor.White.Sprintf("%-12s", truncate(t.Version, 12)),
						gcolor.HEX("#94a3b8").Sprint(truncate(t.Ref, 20)),
					)
				}
				fmt.Println()
				return nil

			case "package-all":
				// /skill package-all [dir]
				// Walk a directory for SKILL.md files, put each into CAS, and create tags.
				// Default source: $DOJO_SKILLS_PATH or current directory.
				dir := os.Getenv("DOJO_SKILLS_PATH")
				if len(args) >= 2 {
					dir = args[1]
				}
				if dir == "" {
					return fmt.Errorf("usage: /skill package-all <dir>\n  or set DOJO_SKILLS_PATH")
				}

				// Walk for SKILL.md files.
				var skills []string
				err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return nil // skip inaccessible
					}
					if !info.IsDir() && info.Name() == "SKILL.md" {
						skills = append(skills, path)
					}
					return nil
				})
				if err != nil {
					return fmt.Errorf("walking %s: %w", dir, err)
				}

				if len(skills) == 0 {
					fmt.Println()
					fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  No SKILL.md files found in %s", dir))
					fmt.Println()
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Packaging %d skills from %s\n\n", len(skills), dir))

				var succeeded, failed int
				for _, path := range skills {
					// Derive skill name from parent directory.
					skillName := filepath.Base(filepath.Dir(path))

					content, err := os.ReadFile(path)
					if err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: %s\n", skillName, err)
						failed++
						continue
					}

					// Put content into CAS.
					// Sleep before each API call: gateway allows 300 req/min (5/sec) sustained,
					// burst 50. Each skill makes 2 calls, so 250ms per call = 4 req/sec total.
					ref, err := r.gw.CASPutContent(ctx, content)
					time.Sleep(250 * time.Millisecond)
					if err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: CAS put: %s\n", skillName, err)
						failed++
						continue
					}

					// Create tag: name@latest -> ref.
					if err := r.gw.CASCreateTag(ctx, skillName, "latest", ref); err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: tag create: %s\n", skillName, err)
						failed++
						time.Sleep(250 * time.Millisecond)
						continue
					}
					time.Sleep(250 * time.Millisecond)

					gcolor.HEX("#22c55e").Printf("  [OK]   %s → %s\n", skillName, ref[:12])
					succeeded++
				}

				fmt.Println()
				summary := fmt.Sprintf("  Done: %d succeeded, %d failed", succeeded, failed)
				if failed > 0 {
					gcolor.HEX("#eab308").Println(summary)
				} else {
					gcolor.HEX("#22c55e").Println(summary)
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
					gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skills matching %q (%d)\n\n", filter, len(skills)))
				} else {
					gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skills (%d)\n\n", len(skills)))
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
						gcolor.HEX("#64748b").Sprint("\u2500\u2500\u2500\u2500"),
						gcolor.HEX("#e8b04a").Sprint("["+cat+"]"),
						gcolor.HEX("#64748b").Sprint("\u2500\u2500\u2500\u2500"),
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
