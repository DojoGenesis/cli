package commands

// cmd_agent.go — /agent command and all agent-related helper functions.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DojoGenesis/dojo-cli/internal/client"
	"github.com/DojoGenesis/dojo-cli/internal/state"
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

				// Append explicit completion criteria so agents don't self-terminate prematurely.
				message = message + "\n\nCompletion requirements: (1) Do not stop after reading files — you must create or modify files to complete the task. (2) After making changes, run `make test` or the relevant test command. (3) Your final response must include the list of files you created or modified. If you cannot complete the task, say why explicitly."

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

				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

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
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))
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

			default: // ls
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
			}
		},
	}
}

// streamAgentChat sends a message to an agent and streams the SSE response.
// Thinking and tool-call events are rendered in dim colors; text is printed inline.
func (r *Registry) streamAgentChat(ctx context.Context, agentID, message string) error {
	req := client.AgentChatRequest{
		Message: message,
		UserID:  r.cfg.Auth.UserID,
		Stream:  true,
	}

	err := r.gw.AgentChatStream(ctx, agentID, req, func(chunk client.SSEChunk) {
		switch chunk.Event {
		case "thinking":
			// Gateway sends: event: thinking / data: {"type":"thinking","data":{"message":"..."},...}
			msg := agentNestedField(chunk.Data, "message")
			if msg == "" {
				msg = truncate(chunk.Data, 80)
			}
			fmt.Print(gcolor.HEX("#94a3b8").Sprint("\n  [Thinking] " + msg))
		case "tool_call", "tool_invoked":
			name := agentNestedField(chunk.Data, "tool")
			if name == "" {
				name = truncate(chunk.Data, 60)
			}
			fmt.Print(gcolor.HEX("#457b9d").Sprintf("\n  [Tool: %s]", name))
		case "tool_result", "tool_completed":
			// absorbed into the response
		case "response_chunk":
			// Gateway sends: event: response_chunk / data: {"type":"response_chunk","data":{"content":"..."},...}
			if text := agentNestedField(chunk.Data, "content"); text != "" {
				fmt.Print(text)
			}
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
