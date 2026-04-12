// Package repl provides the interactive read-eval-print loop for the dojo CLI.
package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/art"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/commands"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guide"
	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/plugins"
	"github.com/DojoGenesis/cli/internal/providers"
	"github.com/DojoGenesis/cli/internal/spirit"
	"github.com/DojoGenesis/cli/internal/state"
	"github.com/chzyer/readline"
	gcolor "github.com/gookit/color"
)

// REPL is the interactive session.
type REPL struct {
	cfg      *config.Config
	gw       *client.Client
	registry *commands.Registry
	runner   *hooks.Runner
	session  string // active session ID
	turns    int    // number of successful chat turns
	resumed  bool   // true when session was restored via --resume or /session resume
	plain    bool   // true when --plain or --no-color is set; uses unstyled renderer output
}

// New creates a REPL bound to the given config and gateway client.
// It scans cfg.Plugins.Path for CoworkPlugins-format directories on startup.
// If scanning fails a warning is logged and the REPL continues with no hooks.
// When resume is true, the most recent session ID is restored from state
// instead of generating a fresh one.
// When plain is true, chat output uses unstyled text (equivalent to --no-color
// but also strips decorative label prefixes for piped/CI consumers).
func New(cfg *config.Config, gw *client.Client, resume bool, plain bool) *REPL {
	plgs, err := plugins.Scan(cfg.Plugins.Path)
	if err != nil {
		log.Printf("[repl] warning: plugin scan failed (%s): %v — continuing with no plugins", cfg.Plugins.Path, err)
		plgs = nil
	}
	if len(plgs) > 0 {
		log.Printf("[repl] loaded %d plugin(s) from %s", len(plgs), cfg.Plugins.Path)
	}

	r := &REPL{
		cfg:      cfg,
		gw:       gw,
		turns:    0,
		resumed:  false,
		plain:    plain,
	}

	if resume {
		if st, loadErr := state.Load(); loadErr == nil && st.LastSessionID != "" {
			r.session = st.LastSessionID
			r.resumed = true
		} else {
			// --resume requested but no prior session exists; fall back to new
			r.session = fmt.Sprintf("dojo-cli-%s", time.Now().Format("20060102-150405"))
			fmt.Printf("\n  %s\n",
				gcolor.HEX("#94a3b8").Sprint("No prior session found — starting fresh"),
			)
		}
	} else {
		r.session = fmt.Sprintf("dojo-cli-%s", time.Now().Format("20060102-150405"))
		// Show last session hint (cosmetic only) when not resuming
		if st, loadErr := state.Load(); loadErr == nil && st.LastSessionID != "" {
			fmt.Printf("\n  %s %s\n",
				gcolor.HEX("#94a3b8").Sprint("Last session:"),
				gcolor.HEX("#e8b04a").Sprint(st.LastSessionID),
			)
		}
	}

	reg := commands.New(cfg, gw, plgs, &r.session)
	r.registry = reg
	r.runner = reg.Runner()
	return r
}

// vitalityPrompt returns a colored prompt string based on the number of turns.
// 0 turns: neutral-dark dot + cloud-gray "dojo"
// 1–4 turns: warm-amber dot + golden-orange "dojo"
// 5+ turns: golden-orange dot + golden-orange bold "dojo"
func vitalityPrompt(turns int) string {
	sep := gcolor.HEX("#94a3b8").Sprint(" › ")
	switch {
	case turns == 0:
		dot := gcolor.HEX("#64748b").Sprint("●")
		name := gcolor.HEX("#94a3b8").Sprint("dojo")
		return dot + " " + name + sep
	case turns < 5:
		dot := gcolor.HEX("#e8b04a").Sprint("●")
		name := gcolor.HEX("#f4a261").Sprint("dojo")
		return dot + " " + name + sep
	default:
		dot := gcolor.HEX("#f4a261").Sprint("●")
		name := gcolor.Bold.Sprint(gcolor.HEX("#f4a261").Sprint("dojo"))
		return dot + " " + name + sep
	}
}

// sunsetWordmark renders text character-by-character with a linear gradient
// from #ffd166 → #f4a261 → #e76f51.
func sunsetWordmark(text string) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}

	// Three gradient stops
	type rgb struct{ r, g, b uint8 }
	stops := []rgb{
		{0xff, 0xd1, 0x66}, // #ffd166
		{0xf4, 0xa2, 0x61}, // #f4a261
		{0xe7, 0x6f, 0x51}, // #e76f51
	}

	lerp := func(a, b uint8, t float64) uint8 {
		return uint8(float64(a) + t*(float64(b)-float64(a)))
	}

	colorAt := func(i int) rgb {
		if n == 1 {
			return stops[0]
		}
		// Map i in [0, n-1] to t in [0, 1]
		t := float64(i) / float64(n-1)
		// Two segments: [stop0→stop1] for t in [0, 0.5], [stop1→stop2] for t in [0.5, 1]
		if t <= 0.5 {
			seg := t / 0.5
			return rgb{
				lerp(stops[0].r, stops[1].r, seg),
				lerp(stops[0].g, stops[1].g, seg),
				lerp(stops[0].b, stops[1].b, seg),
			}
		}
		seg := (t - 0.5) / 0.5
		return rgb{
			lerp(stops[1].r, stops[2].r, seg),
			lerp(stops[1].g, stops[2].g, seg),
			lerp(stops[1].b, stops[2].b, seg),
		}
	}

	var out strings.Builder
	for i, ch := range runes {
		c := colorAt(i)
		hex := fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
		out.WriteString(gcolor.HEX(hex).Sprint(string(ch)))
	}
	return out.String()
}

// syncProviderKeys pushes locally-available API keys to the gateway so it can
// hot-register cloud providers. Errors are silently ignored — the gateway may
// be offline or the endpoint may not be configured; direct-API mode still works.
func (r *REPL) syncProviderKeys(ctx context.Context) {
	keys := providers.LoadAPIKeys()
	if keys.AnthropicKey != "" {
		if err := r.gw.SetProviderKey(ctx, "anthropic", keys.AnthropicKey); err != nil {
			log.Printf("[repl] syncProviderKeys: anthropic: %v", err)
		}
	}
	if keys.OpenAIKey != "" {
		if err := r.gw.SetProviderKey(ctx, "openai", keys.OpenAIKey); err != nil {
			log.Printf("[repl] syncProviderKeys: openai: %v", err)
		}
	}
	if keys.KimiKey != "" {
		if err := r.gw.SetProviderKey(ctx, "kimi", keys.KimiKey); err != nil {
			log.Printf("[repl] syncProviderKeys: kimi: %v", err)
		}
	}
	if keys.GeminiKey != "" {
		if err := r.gw.SetProviderKey(ctx, "google", keys.GeminiKey); err != nil {
			log.Printf("[repl] syncProviderKeys: google: %v", err)
		}
	}
}

// Run starts the interactive loop. Returns when the user exits.
func (r *REPL) Run(ctx context.Context) error {
	printWelcome(r.cfg, r.session, r.resumed)

	// Spirit: streak + session XP
	if spiritSt, spiritErr := state.Load(); spiritErr == nil {
		// Set member-since on first use
		if spiritSt.Spirit.MemberSince == "" {
			spiritSt.Spirit.MemberSince = time.Now().UTC().Format(time.RFC3339)
		}
		// Record session start time (for marathon achievement)
		spiritSt.Spirit.SessionStart = time.Now().UTC().Format(time.RFC3339)
		spiritSt.Spirit.TotalSessions++

		// Time-based achievement flags
		hour := time.Now().Hour()
		if hour >= 0 && hour < 5 {
			spiritSt.Spirit.NightOwlSeen = true
		}
		if hour >= 5 && hour < 7 {
			spiritSt.Spirit.EarlyBirdSeen = true
		}

		// Update streak
		streakBonus := spirit.UpdateStreak(&spiritSt.Spirit, time.Now())
		if streakBonus > 0 {
			spirit.AwardXP(&spiritSt.Spirit, streakBonus)
		}

		// Award session XP
		beltedUp, newBelt := spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("session_start"))

		// Check achievements
		newAchievements := spirit.CheckAchievements(&spiritSt.Spirit, time.Now())
		for _, a := range newAchievements {
			spirit.AwardXP(&spiritSt.Spirit, a.XPReward)
		}

		_ = spiritSt.Save()

		// Display streak if > 1
		if spiritSt.Spirit.StreakDays > 1 {
			fmt.Printf("  %s%s\n",
				gcolor.HEX("#94a3b8").Sprintf("%-16s", "streak:"),
				gcolor.HEX("#ffd166").Sprintf("%d days", spiritSt.Spirit.StreakDays),
			)
		}

		// Display belt in welcome
		belt := spirit.CurrentBelt(spiritSt.Spirit.XP)
		if spiritSt.Spirit.XP > 0 {
			fmt.Printf("  %s%s\n",
				gcolor.HEX("#94a3b8").Sprintf("%-16s", "belt:"),
				gcolor.HEX(belt.Color).Sprintf("%s %s (%d XP)", belt.Name, belt.Title, spiritSt.Spirit.XP),
			)
		}

		if beltedUp {
			fmt.Println()
			fmt.Printf("  %s\n", gcolor.HEX("#ffd166").Sprint("BELT PROMOTION"))
			fmt.Printf("  You are now: %s\n", gcolor.HEX(newBelt.Color).Sprintf("%s %s", newBelt.Name, newBelt.Title))
			fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprintf("\"%s\"", spirit.BeltQuote(newBelt.Rank)))
			fmt.Println()
		}
		for _, a := range newAchievements {
			fmt.Printf("  %s %s %s\n",
				gcolor.HEX("#ffd166").Sprint("Achievement:"),
				gcolor.HEX("#f4a261").Sprint(a.Icon),
				gcolor.HEX("#e8b04a").Sprint(a.Name),
			)
		}
		fmt.Println()
	}

	// Push local API keys to the gateway so cloud providers get registered.
	r.syncProviderKeys(ctx)

	// Start persistent background SSE connection for push event delivery.
	// Reconnects on error and exits cleanly when the REPL context is cancelled.
	go func() {
		clientID := fmt.Sprintf("dojo-cli-%d", time.Now().UnixMilli())
		for ctx.Err() == nil {
			_ = r.gw.PilotStream(ctx, clientID, func(chunk client.SSEChunk) {
				// Background events — log at debug level for now.
				// Future: dispatch to event bus for agent completions, task updates, etc.
				log.Printf("[repl:sse] event=%q data=%s", chunk.Event, truncateSSE(chunk.Data, 120))
			})
			if ctx.Err() != nil {
				break
			}
			// Brief pause before reconnect to avoid tight loop on persistent errors.
			time.Sleep(2 * time.Second)
		}
	}()

	if st, err := state.Load(); err == nil {
		st.LastSessionID = r.session
		_ = st.Save()
	}

	rl, err := newReadline(r.turns)
	if err != nil {
		// Fallback to plain stdin if readline init fails (e.g. in pipes)
		return r.runPlain(ctx)
	}
	defer rl.Close()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Update prompt to reflect current vitality
		rl.SetPrompt(vitalityPrompt(r.turns))

		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Println()
				continue
			}
			if err == io.EOF {
				fmt.Println("\ngoodbye")
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" || line == "bye" {
			fmt.Println("\ngoodbye")
			return nil
		}

		if err := r.handle(ctx, line); err != nil {
			gcolor.Red.Printf("  error: %s\n", err)
		}
	}
}

// handle routes a line to either a slash command or a chat message.
// For slash commands it fires PreCommand before dispatch and PostCommand after
// a successful dispatch. Hook errors are logged but not fatal.
//
// Safety invariant: only lines that do NOT start with "/" are sent to the
// gateway as chat messages. Slash-prefixed input is always dispatched locally
// and never forwarded to /v1/chat, even when the command is unknown.
func (r *REPL) handle(ctx context.Context, line string) error {
	if strings.HasPrefix(line, "/") {
		payload := map[string]any{"command": line}

		if err := r.runner.Fire(ctx, hooks.EventPreCommand, payload); err != nil {
			log.Printf("[hooks] PreCommand error: %v", err)
		}

		cmdErr := r.registry.Dispatch(ctx, line[1:])

		if cmdErr == nil {
			if err := r.runner.Fire(ctx, hooks.EventPostCommand, payload); err != nil {
				log.Printf("[hooks] PostCommand error: %v", err)
			}

			// Spirit: award command XP + check for specific action bonuses
			if spiritSt, spiritErr := state.Load(); spiritErr == nil {
				spiritSt.Spirit.TotalCommands++
				spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("command_run"))

				// Bonus XP for specific command categories
				lower := strings.ToLower(line)
				switch {
				case strings.HasPrefix(lower, "/agent dispatch"):
					spiritSt.Spirit.TotalAgents++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("agent_dispatched"))
				case strings.HasPrefix(lower, "/practice"):
					spiritSt.Spirit.TotalPractice++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("practice_completed"))
				case strings.HasPrefix(lower, "/garden plant"):
					spiritSt.Spirit.TotalSeeds++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("seed_planted"))
				case strings.HasPrefix(lower, "/plugin install"):
					spiritSt.Spirit.TotalPlugins++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("plugin_installed"))
				case strings.HasPrefix(lower, "/project init"):
					spiritSt.Spirit.TotalProjects++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("project_created"))
				case strings.HasPrefix(lower, "/skill"):
					spiritSt.Spirit.TotalSkills++
					spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("skill_invoked"))
				}

				// Check achievements after all awards
				newAchievements := spirit.CheckAchievements(&spiritSt.Spirit, time.Now())
				for _, a := range newAchievements {
					bUp, nB := spirit.AwardXP(&spiritSt.Spirit, a.XPReward)
					fmt.Printf("  %s %s %s (+%d XP)\n",
						gcolor.HEX("#ffd166").Sprint("Achievement:"),
						gcolor.HEX("#f4a261").Sprint(a.Icon),
						gcolor.HEX("#e8b04a").Sprint(a.Name),
						a.XPReward,
					)
					if bUp {
						// inline belt notification
						fmt.Println()
						fmt.Printf("  %s\n", gcolor.HEX("#ffd166").Sprint("BELT PROMOTION"))
						fmt.Printf("  You are now: %s\n", gcolor.HEX(nB.Color).Sprintf("%s %s", nB.Name, nB.Title))
						fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprintf("\"%s\"", spirit.BeltQuote(nB.Rank)))
						fmt.Println()
					}
				}

				// Guide progress check — advance step if active guide matches this command
				if res, advanced := guide.AdvanceStep(spiritSt, line); advanced {
					spirit.AwardXP(&spiritSt.Spirit, res.XP)
					commands.PrintGuideStepComplete(res)

					if res.GuideComplete {
						spiritSt.Spirit.TotalGuides++
						bonusXP := spirit.XPForAction("guide_completed")
						spirit.AwardXP(&spiritSt.Spirit, bonusXP)
						commands.PrintGuideCompleteBonus(bonusXP)

						// Check achievements unlocked by guide completion
						guideAchievements := spirit.CheckAchievements(&spiritSt.Spirit, time.Now())
						for _, a := range guideAchievements {
							bUp, nB := spirit.AwardXP(&spiritSt.Spirit, a.XPReward)
							fmt.Printf("  %s %s %s (+%d XP)\n",
								gcolor.HEX("#ffd166").Sprint("Achievement:"),
								gcolor.HEX("#f4a261").Sprint(a.Icon),
								gcolor.HEX("#e8b04a").Sprint(a.Name),
								a.XPReward,
							)
							if bUp {
								fmt.Println()
								fmt.Printf("  %s\n", gcolor.HEX("#ffd166").Sprint("BELT PROMOTION"))
								fmt.Printf("  You are now: %s\n", gcolor.HEX(nB.Color).Sprintf("%s %s", nB.Name, nB.Title))
								fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprintf("\"%s\"", spirit.BeltQuote(nB.Rank)))
								fmt.Println()
							}
						}
					}
				}

				_ = spiritSt.Save()
			}
		}
		return cmdErr
	}
	// Safety guard: never forward slash-prefixed input to /v1/chat.
	// This should never be reached because HasPrefix("/") is checked above,
	// but acts as a defence-in-depth barrier against future refactoring.
	if strings.HasPrefix(line, "/") {
		return fmt.Errorf("unknown command %s — type /help for a list", strings.Fields(line)[0])
	}
	return r.chat(ctx, line)
}

// chat sends a freeform message to the gateway and streams the response.
func (r *REPL) chat(ctx context.Context, message string) error {
	workspaceRoot, _ := os.Getwd()
	req := client.ChatRequest{
		Message:       message,
		Model:         r.cfg.Defaults.Model,
		Provider:      r.cfg.Defaults.Provider,
		SessionID:     r.session,
		UserID:        r.cfg.Auth.UserID,
		Stream:        true,
		WorkspaceRoot: workspaceRoot,
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

	var fullText strings.Builder
	err := r.gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
		ev := ClassifyChunk(chunk)
		rendered := ev.Render(r.plain)
		if rendered != "" {
			fmt.Print(rendered)
			fullText.WriteString(ev.Content)
		}
	})

	fmt.Println()
	fmt.Println()

	if err != nil {
		if ctx.Err() != nil {
			// User interrupted with Ctrl+C during streaming — not an error
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [interrupted]"))
			return nil
		}
		// Stream dropped unexpectedly
		fmt.Println(gcolor.HEX("#e8b04a").Sprint("  [stream interrupted — response may be incomplete]"))
		r.turns++ // still count it — partial response was shown
		return nil
	}

	if fullText.Len() == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [no response — the gateway may have encountered an internal error]"))
	}

	// Spirit: award chat XP
	if spiritSt, spiritErr := state.Load(); spiritErr == nil {
		spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("chat_message"))
		_ = spiritSt.Save()
	}

	r.turns++
	return nil
}

// truncateSSE truncates a string to max characters for log output.
func truncateSSE(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// extractText pulls the readable text from an SSE chunk.
// The gateway may send raw text, or a JSON object with a "text"/"content" field.
func extractText(chunk client.SSEChunk) string {
	data := strings.TrimSpace(chunk.Data)
	if data == "" || data == "[DONE]" {
		return ""
	}

	// Try JSON unwrap
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		// OpenAI delta format
		if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]any); ok {
				if delta, ok := choice["delta"].(map[string]any); ok {
					if content, ok := delta["content"].(string); ok {
						return content
					}
				}
				// non-streaming text field
				if text, ok := choice["text"].(string); ok {
					return text
				}
			}
		}
		// Simple {"text": "..."} or {"content": "..."}
		for _, key := range []string{"text", "content", "message", "response"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}

	// Plain text chunk
	return data
}

// runPlain is the fallback when readline is unavailable (piped input, CI).
func (r *REPL) runPlain(ctx context.Context) error {
	// Note: printWelcome is already called by Run() before fallback here.
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}
		if err := r.handle(ctx, line); err != nil {
			gcolor.Red.Printf("error: %s\n", err)
		}
	}
}

// ─── readline setup ──────────────────────────────────────────────────────────

func newReadline(turns int) (*readline.Instance, error) {
	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/health"),
		readline.PcItem("/home"),
		readline.PcItem("/model",
			readline.PcItem("ls"),
			readline.PcItem("set"),
		),
		readline.PcItem("/tools"),
		readline.PcItem("/agent",
			readline.PcItem("ls"),
			readline.PcItem("dispatch",
				readline.PcItem("focused"),
				readline.PcItem("balanced"),
				readline.PcItem("exploratory"),
				readline.PcItem("deliberate"),
			),
			readline.PcItem("chat"),
			readline.PcItem("info"),
			readline.PcItem("channels"),
			readline.PcItem("bind"),
			readline.PcItem("unbind"),
		),
		readline.PcItem("/apps",
			readline.PcItem("launch"),
			readline.PcItem("close"),
			readline.PcItem("status"),
			readline.PcItem("call"),
		),
		readline.PcItem("/workflow"),
		readline.PcItem("/skill",
			readline.PcItem("ls"),
			readline.PcItem("get"),
			readline.PcItem("inspect"),
			readline.PcItem("tags"),
		),
		readline.PcItem("/doc"),
		readline.PcItem("/session",
			readline.PcItem("new"),
			readline.PcItem("resume"),
		),
		readline.PcItem("/run"),
		readline.PcItem("/garden",
			readline.PcItem("ls"),
			readline.PcItem("stats"),
			readline.PcItem("plant"),
			readline.PcItem("harvest"),
			readline.PcItem("search"),
			readline.PcItem("rm"),
		),
		readline.PcItem("/trail",
			readline.PcItem("add"),
			readline.PcItem("rm"),
			readline.PcItem("search"),
		),
		readline.PcItem("/snapshot",
			readline.PcItem("save"),
			readline.PcItem("restore"),
			readline.PcItem("export"),
			readline.PcItem("rm"),
		),
		readline.PcItem("/trace"),
		readline.PcItem("/pilot",
			readline.PcItem("plain"),
		),
		readline.PcItem("/hooks",
			readline.PcItem("ls"),
			readline.PcItem("fire"),
		),
		readline.PcItem("/settings",
			readline.PcItem("providers"),
			readline.PcItem("set"),
		),
		readline.PcItem("/init",
			readline.PcItem("--force"),
			readline.PcItem("--gateway"),
			readline.PcItem("--plugins-source"),
			readline.PcItem("--skip-seeds"),
		),
		readline.PcItem("/practice"),
		readline.PcItem("/projects",
			readline.PcItem("ls"),
		),
		readline.PcItem("/plugin",
			readline.PcItem("ls"),
			readline.PcItem("install"),
			readline.PcItem("rm"),
		),
		readline.PcItem("/disposition",
			readline.PcItem("ls"),
			readline.PcItem("set"),
			readline.PcItem("show"),
			readline.PcItem("create"),
		),
		readline.PcItem("/sensei"),
		readline.PcItem("/card"),
		readline.PcItem("/guide",
			readline.PcItem("ls"),
			readline.PcItem("start",
				readline.PcItem("welcome"),
				readline.PcItem("spirit"),
				readline.PcItem("agents"),
				readline.PcItem("memory"),
			),
			readline.PcItem("status"),
			readline.PcItem("stop"),
		),
		readline.PcItem("/project",
			readline.PcItem("init"),
			readline.PcItem("status"),
			readline.PcItem("switch"),
			readline.PcItem("list"),
			readline.PcItem("archive"),
			readline.PcItem("phase"),
			readline.PcItem("track",
				readline.PcItem("add"),
				readline.PcItem("set"),
			),
			readline.PcItem("decision"),
			readline.PcItem("artifact"),
		),
		readline.PcItem("/activity",
			readline.PcItem("clear"),
		),
		readline.PcItem("/telemetry",
			readline.PcItem("sessions"),
			readline.PcItem("costs"),
			readline.PcItem("tools"),
			readline.PcItem("summary"),
		),
		readline.PcItem("/warroom"),
		readline.PcItem("/bloom"),
		readline.PcItem("/code",
			readline.PcItem("read"),
			readline.PcItem("diff"),
			readline.PcItem("test"),
			readline.PcItem("build"),
			readline.PcItem("vet"),
			readline.PcItem("gate"),
		),
		readline.PcItem("exit"),
	)

	return readline.NewEx(&readline.Config{
		Prompt:          vitalityPrompt(turns),
		HistoryFile:     historyPath(),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
}

func historyPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.dojo/.history"
}

// ─── Welcome banner ──────────────────────────────────────────────────────────

func printWelcome(cfg *config.Config, session string, resumed bool) {
	fmt.Println()

	// Bonsai sigil — zen visual anchor
	fmt.Print(art.SmallBonsaiString())

	// Sunset gradient wordmark
	fmt.Println(sunsetWordmark("  Dojo CLI"))

	// Session line: label in cloud-gray, value in warm-amber, "(resumed)" tag if applicable
	if resumed {
		fmt.Printf("%s%s %s\n",
			gcolor.HEX("#94a3b8").Sprint("  session: "),
			gcolor.HEX("#e8b04a").Sprint(session),
			gcolor.HEX("#7fb88c").Sprint("(resumed)"),
		)
	} else {
		fmt.Printf("%s%s\n",
			gcolor.HEX("#94a3b8").Sprint("  session: "),
			gcolor.HEX("#e8b04a").Sprint(session),
		)
	}

	// Gateway line: label in cloud-gray, value in neutral-dark
	fmt.Printf("%s%s\n",
		gcolor.HEX("#94a3b8").Sprint("  gateway: "),
		gcolor.HEX("#64748b").Sprint(cfg.Gateway.URL),
	)

	// Hint line: cloud-gray
	gcolor.HEX("#94a3b8").Println("  type /help for commands, /health to check the gateway")

	// First-run: suggest /init if workspace is empty
	if _, err := os.Stat(config.SettingsPath()); os.IsNotExist(err) {
		st, _ := state.Load()
		if !st.SetupComplete {
			fmt.Println()
			gcolor.HEX("#e8b04a").Println("  First run detected — workspace is empty.")
			gcolor.HEX("#94a3b8").Println("  Run /init to set up plugins, dispositions, and starter seeds.")
		}
	}

	// JetBrains Mono one-time tip
	home, _ := os.UserHomeDir()
	hintFile := home + "/.dojo/.mono-hint"
	if _, err := os.Stat(hintFile); os.IsNotExist(err) {
		gcolor.HEX("#94a3b8").Println("  tip: set terminal font to JetBrains Mono for best rendering")
		// Create the marker file so the tip never shows again
		_ = os.MkdirAll(home+"/.dojo", 0o755)
		f, ferr := os.Create(hintFile)
		if ferr == nil {
			f.Close()
		}
	}

	fmt.Println()
}
