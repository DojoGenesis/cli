package commands

// cmd_agent.go — /agent command and all agent-related helper functions.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/state"
	gcolor "github.com/gookit/color"
)

// ─── /agent ─────────────────────────────────────────────────────────────────

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
				// /agent dispatch [mode] [--model <name>|--model=<name>] <msg...>
				// mode is optional — defaults to "balanced"; --model is
				// position-independent among the remaining args and is
				// stripped before mode-detection and message-joining.
				validModes := map[string]bool{
					"focused": true, "balanced": true,
					"exploratory": true, "deliberate": true,
				}
				rest, modelFlag, modelFlagSet, flagErr := extractModelFlag(args[1:])
				if flagErr != nil {
					return fmt.Errorf("usage: /agent dispatch [mode] [--model <name>] <message>: %w", flagErr)
				}
				mode := "balanced"
				var msgArgs []string
				if len(rest) >= 1 && validModes[rest[0]] {
					mode = rest[0]
					msgArgs = rest[1:]
				} else {
					msgArgs = rest
				}
				if len(msgArgs) == 0 {
					return fmt.Errorf("usage: /agent dispatch [focused|balanced|exploratory|deliberate] <message>")
				}
				message := strings.Join(msgArgs, " ")

				// Append explicit completion criteria so agents don't self-terminate prematurely.
				message = message + "\n\nCompletion requirements: (1) Do not stop after reading files — you must create or modify files to complete the task. (2) After making changes, run `make test` or the relevant test command. (3) Your final response must include the list of files you created or modified. If you cannot complete the task, say why explicitly."

				model, modelSource := r.resolveModel(modelFlag, modelFlagSet)

				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Creating agent (mode: %s)...", mode))
				printModelLine(model, modelSource)

				wd, wdErr := os.Getwd()
				if wdErr != nil {
					wd = "."
				}
				agentResp, err := r.gw.CreateAgent(ctx, client.CreateAgentRequest{
					WorkspaceRoot: wd,
					ActiveMode:    mode,
					Model:         model,
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

				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

				// Persist agent to local state.
				if st, loadErr := state.Load(); loadErr == nil {
					st.AddAgent(agentResp.AgentID, mode)
					if saveErr := st.Save(); saveErr != nil {
						fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  [warn] could not save state: %v", saveErr))
					}
				}

				return r.streamAgentChat(ctx, agentResp.AgentID, message, model)

			case "chat":
				// /agent chat [--model <name>|--model=<name>] <id> <msg...>
				rest, modelFlag, modelFlagSet, flagErr := extractModelFlag(args[1:])
				if flagErr != nil {
					return fmt.Errorf("usage: /agent chat [--model <name>] <agent-id> <message>: %w", flagErr)
				}
				if len(rest) < 2 {
					return fmt.Errorf("usage: /agent chat <agent-id> <message>")
				}
				agentID := rest[0]
				message := strings.Join(rest[1:], " ")

				model, modelSource := r.resolveModel(modelFlag, modelFlagSet)

				fmt.Println()
				printModelLine(model, modelSource)
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))
				chatErr := r.streamAgentChat(ctx, agentID, message, model)

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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Agent: %s\n\n", agentID))
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
							gcolor.White.Sprint(string(b)),
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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Channels for %s (%d)\n\n", agentID, len(channels)))
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

			case "ls":
				agents, err := r.gw.Agents(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch agents: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Agents (%d)\n\n", len(agents)))
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

			default:
				// D1 — an unrecognized subcommand used to fall silently
				// into "ls" (matching Go's zero-value switch semantics for
				// any unmatched string). That hid typos from the caller —
				// especially costly for a headless/agent caller with no
				// human watching the output to notice the mismatch. Bare
				// "/agent" still lists (sub defaults to "ls" above, which
				// the "ls" case handles), so only a real unrecognized
				// token reaches here.
				return fmt.Errorf("unknown subcommand %q — see /help", args[0])
			}
		},
	}
}

// resolveModel implements the /agent dispatch and /agent chat model
// precedence: an explicit --model flag wins, then cfg.Delegation.Model (the
// workspace's dispatch-time default), then "" — meaning the Gateway picks
// its own default and Model is omitted from the JSON body via `omitempty`.
// source is a human-readable label for printModelLine ("flag" or
// "delegation default"); it is "" whenever model is "" too, so callers can
// pass both straight through without an extra check.
func (r *Registry) resolveModel(flagValue string, flagSet bool) (model, source string) {
	if flagSet {
		return flagValue, "flag"
	}
	if r.cfg.Delegation.Model != "" {
		return r.cfg.Delegation.Model, "delegation default"
	}
	return "", ""
}

// printModelLine prints the one-line dim/info banner /agent dispatch and
// /agent chat show before streaming begins, naming which model the
// precedence resolved to. No-op when model is "" — the Gateway default was
// left alone, so there is nothing to announce.
func printModelLine(model, source string) {
	if model == "" {
		return
	}
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  model: %s (%s)", model, source))
}

// extractModelFlag scans args for a "--model <value>" or "--model=<value>"
// flag anywhere in the slice — position-independent among the mode token
// and message words — and returns args with every occurrence of the flag
// (and, for the two-token form, its paired value) removed. present reports
// whether the flag was seen at all; when given more than once, the last
// occurrence's value wins, matching ordinary flag-parsing convention. err is
// non-nil only when an occurrence carries an empty value — a bare trailing
// "--model" with nothing after it, or "--model=" with nothing after the "="
// — in which case the caller must surface err as a usage error and not
// dispatch.
func extractModelFlag(args []string) (rest []string, value string, present bool, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--model":
			if i+1 >= len(args) {
				return nil, "", true, fmt.Errorf("--model requires a value")
			}
			present = true
			value = args[i+1]
			i++
		case strings.HasPrefix(a, "--model="):
			v := strings.TrimPrefix(a, "--model=")
			if v == "" {
				return nil, "", true, fmt.Errorf("--model= requires a value")
			}
			present = true
			value = v
		default:
			rest = append(rest, a)
		}
	}
	return rest, value, present, nil
}

// streamEvent is the NDJSON line shape emitted by chat-like streaming
// commands (/agent chat, /agent dispatch via streamAgentChat; /workflow and
// /run's chat fallback in cmd_workflow.go) in headless JSON mode. It
// deliberately mirrors internal/repl.RenderEvent's RenderJSON() output
// ({type, content, meta}) so a scripted caller parses one event shape
// whether it drove the REPL's --one-shot chat path or one of these
// command-specific streams. Content/Meta are omitempty because most event
// types carry only one or the other (e.g. tool_call has no content, text
// has no meta).
type streamEvent struct {
	Type    string            `json:"type"`
	Content string            `json:"content,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// streamAgentChat sends a message to an agent and streams the SSE response.
// Human mode: thinking and tool-call events are rendered in dim colors, text
// is printed inline. Headless JSON mode: each event becomes one streamEvent
// NDJSON line via r.out.Emit instead — the two are mutually exclusive per
// event (matching how printKV/colorStatus branch), not layered, so a JSON
// dispatch's captured-stdout fallback never fills up with duplicate ANSI text.
func (r *Registry) streamAgentChat(ctx context.Context, agentID, message, model string) error {
	req := client.AgentChatRequest{
		Message: message,
		UserID:  r.cfg.Auth.UserID,
		Stream:  true,
		Model:   model,
	}

	err := r.gw.AgentChatStream(ctx, agentID, req, func(chunk client.SSEChunk) {
		switch chunk.Event {
		case "thinking":
			// Gateway sends: event: thinking / data: {"type":"thinking","data":{"message":"..."},...}
			msg := agentNestedField(chunk.Data, "message")
			if msg == "" {
				msg = truncate(chunk.Data, 80)
			}
			if r.out.JSON() {
				r.out.Emit(streamEvent{Type: "thinking", Content: msg})
				return
			}
			fmt.Print(gcolor.HEX("#94a3b8").Sprint("\n  [Thinking] " + msg))
		case "tool_call", "tool_invoked":
			name := agentNestedField(chunk.Data, "tool")
			if name == "" {
				name = truncate(chunk.Data, 60)
			}
			if r.out.JSON() {
				r.out.Emit(streamEvent{Type: "tool_call", Meta: map[string]string{"tool": name}})
				return
			}
			fmt.Print(gcolor.HEX("#457b9d").Sprintf("\n  [Tool: %s]", name))
		case "tool_result", "tool_completed":
			// absorbed into the response — nothing rendered in either mode
		case "response_chunk":
			// Gateway sends: event: response_chunk / data: {"type":"response_chunk","data":{"content":"..."},...}
			if text := agentNestedField(chunk.Data, "content"); text != "" {
				if r.out.JSON() {
					r.out.Emit(streamEvent{Type: "text", Content: text})
					return
				}
				fmt.Print(text)
			}
		default:
			if text := agentExtractText(chunk.Data); text != "" {
				if r.out.JSON() {
					r.out.Emit(streamEvent{Type: "text", Content: text})
					return
				}
				fmt.Print(text)
			}
		}
	})

	if !r.out.JSON() {
		fmt.Println()
		fmt.Println()
	}
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

// agentNestedField extracts a string value from the "data" sub-object of a
// gateway StreamEvent envelope: {"type":"...","data":{"field":"..."},...}.
func agentNestedField(raw, field string) string {
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil || env.Data == nil {
		return ""
	}
	v, _ := env.Data[field].(string)
	return v
}
