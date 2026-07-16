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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DojoGenesis/cli/internal/art"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/commands"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guide"
	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/plugins"
	"github.com/DojoGenesis/cli/internal/protocol"
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

	// protocol injects the workspace genius protocol into the first chat turn of
	// the session (and sets ChatRequest.SystemPrompt). Once-per-session, request
	// context only — never rendered into output. nil-safe: Apply is inert when
	// the protocol is disabled.
	protocol *protocol.Injector

	// nudger tracks which JIT protocol gates have already been surfaced this
	// session so the tell-triggered nudge (maybeProtocolNudge) fires at most once
	// per distinct gate. Zero value is ready to use; driven only from the single
	// read-loop goroutine, so it needs no lock.
	nudger protocol.Nudger

	mu         sync.Mutex         // guards turnCancel
	turnCancel context.CancelFunc // cancels the in-flight streaming turn; nil when idle
}

// beginTurn registers the cancel func for the streaming turn about to start so a
// SIGINT can cancel just that turn. The returned func clears the registration.
func (r *REPL) beginTurn(cancel context.CancelFunc) func() {
	r.mu.Lock()
	r.turnCancel = cancel
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.turnCancel = nil
		r.mu.Unlock()
	}
}

// cancelActiveTurn cancels the in-flight streaming turn, if any. Returns true
// when a turn was actually cancelled. Called from the SIGINT watcher.
func (r *REPL) cancelActiveTurn() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.turnCancel != nil {
		r.turnCancel()
		return true
	}
	return false
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
		cfg:     cfg,
		gw:      gw,
		turns:   0,
		resumed: false,
		plain:   plain,
		// Resolve the protocol context once at session start (project ./DOJO.md
		// > ~/.dojo/DOJO.md > embedded default; empty when disabled).
		protocol: protocol.NewInjector(cfg),
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
		// Show last session hint (cosmetic only) when not resuming. Gated
		// behind !plain like the welcome banner in printWelcome — piped/CI
		// consumers under --plain get no decorative lines here either.
		if !plain {
			if st, loadErr := state.Load(); loadErr == nil && st.LastSessionID != "" {
				fmt.Printf("\n  %s %s\n",
					gcolor.HEX("#94a3b8").Sprint("Last session:"),
					gcolor.HEX("#e8b04a").Sprint(st.LastSessionID),
				)
			}
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

// fireSessionStart fires the SessionStart hooks. Called once from Run(),
// after session/config setup completes and before the read loop begins, so
// a plugin's startup hook (e.g. kata-harness's roll-status-injector, whose
// entire job is injecting status at session start) sees a fully-initialized
// session. Hook errors are logged, not fatal — same idiom as PreCommand/
// PostCommand in handle().
func (r *REPL) fireSessionStart(ctx context.Context) {
	payload := map[string]any{"session": r.session, "resumed": r.resumed}
	if err := r.runner.Fire(ctx, hooks.EventSessionStart, payload); err != nil {
		log.Printf("[hooks] SessionStart error: %v", err)
	}
}

// fireSessionEnd fires the SessionEnd hooks. Called via defer from Run() so
// it runs on every exit path — normal exit, SIGTERM, a readline EOF/error,
// or falling through to runPlain. Deliberately uses context.Background()
// rather than Run()'s ctx: by the time Run() returns because ctx itself was
// cancelled (SIGTERM), firing a "command" hook with that same cancelled ctx
// would race exec.CommandContext's kill-watcher goroutine (see runCommand in
// internal/hooks/runner.go) and could truncate or skip the hook entirely —
// SessionEnd should still get a chance to complete.
func (r *REPL) fireSessionEnd() {
	payload := map[string]any{"session": r.session, "resumed": r.resumed}
	if err := r.runner.Fire(context.Background(), hooks.EventSessionEnd, payload); err != nil {
		log.Printf("[hooks] SessionEnd error: %v", err)
	}
}

// Run starts the interactive loop. Returns when the user exits.
func (r *REPL) Run(ctx context.Context) error {
	printWelcome(r.cfg, r.session, r.resumed, r.plain)

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

		// Spirit visuals (streak, belt, promotions, achievements) are decorative —
		// suppress them in plain/pipeline mode so stdout stays clean. XP state above
		// is still recorded either way.
		if !r.plain {
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

	// Session/config setup is complete as of the state save above — fire
	// SessionStart now, before the read loop starts. SessionEnd fires via
	// defer so it covers every return path out of Run() below.
	r.fireSessionStart(ctx)
	defer r.fireSessionEnd()

	// Two-tier interrupt: a SIGINT that arrives while a response is streaming
	// cancels ONLY that turn (the base ctx from main is not SIGINT-bound, so the
	// session survives and the loop returns to the prompt). At an idle prompt the
	// terminal is in raw mode and Ctrl+C is delivered to readline as a byte —
	// handled below as ErrInterrupt — so this watcher fires only mid-stream.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				r.cancelActiveTurn()
			}
		}
	}()

	rl, err := newReadline(r.turns)
	if err != nil {
		// Fallback to plain stdin if readline init fails (e.g. in pipes)
		return r.runPlain(ctx)
	}
	defer func() { _ = rl.Close() }()

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
				// Ctrl+C at the prompt clears a partial line; on an empty line
				// (idle, or a second Ctrl+C right after cancelling a response)
				// it quits cleanly.
				if strings.TrimSpace(line) == "" {
					fmt.Println("goodbye")
					return nil
				}
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

			// Done-means-verified: after a successful /agent dispatch or /run,
			// run the opt-in build+test gate (Verify.AfterAgent, OFF by default).
			// Best-effort — a red gate reports but never turns this into a failed
			// command. REPL-only by construction: the one-shot path in
			// cmd/dojo/main.go never calls handle(), so this can't fire there.
			r.maybeVerifyAfterAgent(ctx, strings.ToLower(line))
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
	// Carry the genius protocol on the FIRST turn only (once-per-session guard
	// lives in the Injector). This mutates req.Message (immediate effect) and
	// sets req.SystemPrompt (forward-compat) before the request goes out; it is
	// request context, never echoed into the rendered response.
	r.protocol.Apply(&req)

	// Derive a per-turn context so a SIGINT cancels THIS response only (the
	// watcher in Run calls cancelActiveTurn); the session's base ctx is untouched.
	turnCtx, cancel := context.WithCancel(ctx)
	clearTurn := r.beginTurn(cancel)
	defer func() {
		cancel()
		clearTurn()
	}()

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  dojo  "))

	turnStart := time.Now()
	var fullText strings.Builder
	var sawError bool
	var lastErrText string
	var tokensIn, tokensOut int
	var sawUsage bool
	err := r.gw.ChatStream(turnCtx, req, func(chunk client.SSEChunk) {
		ev := ClassifyChunk(chunk)
		if ev.Type == EventError {
			sawError = true
			lastErrText = ev.Content
		}
		rendered := ev.Render(r.plain)
		if rendered != "" {
			fmt.Print(rendered)
			fullText.WriteString(ev.Content)
		}
		// Per-turn cost/token readout: opportunistically check every chunk for
		// a usage payload (see extractUsage) rather than gating on a specific
		// event name — in practice the gateway, if it sends usage at all, only
		// attaches it to the terminal "done" event, but checking every chunk
		// costs one cheap failed json.Unmarshal when absent and does not affect
		// what gets rendered or how the stream proceeds.
		if in, out, ok := extractUsage(chunk.Data); ok {
			tokensIn, tokensOut = in, out
			sawUsage = true
		}
	})

	fmt.Println()
	fmt.Println()

	if err != nil {
		switch {
		case ctx.Err() != nil:
			// Base context ended (SIGTERM / shutdown) — let the loop exit.
			return nil
		case turnCtx.Err() != nil:
			// Ctrl+C cancelled just this response; return to the prompt.
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [cancelled]"))
			return nil
		default:
			// Real failure (conn refused, DNS, auth, 5xx, stall) — show WHY,
			// not a canned "stream interrupted". Combined with the error-event
			// surfacing, the user now sees the actual cause.
			gcolor.Red.Printf("  error: %s\n", r.gw.FriendlyError(err))
			// JIT protocol nudge — pass the RAW error text, not FriendlyError's
			// rewrite: FriendlyError collapses a "dial tcp … connection refused"
			// into a URL-naming message that no longer carries the wiring tell.
			r.maybeProtocolNudge(err.Error())
			return nil
		}
	}

	if sawError {
		// Stream ended cleanly but carried a gateway error event; it was already
		// rendered inline. Don't count it as a successful turn or award XP.
		r.maybeProtocolNudge(lastErrText)
		return nil
	}

	if fullText.Len() == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  [no response — the gateway may have encountered an internal error]"))
	}

	// Per-turn cost/token readout — dim, interactive-only footer. Suppressed
	// under --plain, which also covers --json: --json is a one-shot-only flag
	// (cmd/dojo/main.go) that never reaches the REPL's chat() path at all, so
	// gating on r.plain alone is sufficient here.
	//
	// As of this writing the gateway's /v1/chat stream never actually carries
	// usage data: renderer.go's ClassifyChunk discards the "done" event's
	// payload entirely, and both TestClassifyChunk_EventDone* fixtures in
	// renderer_test.go model "done" data as either empty or a plain human
	// string, never JSON. So sawUsage never flips true today and this block is
	// a graceful no-op. It activates automatically — no further REPL changes
	// needed — the moment the gateway attaches usage to any chunk, since
	// extractUsage already recognizes every usage shape used elsewhere against
	// this same gateway/providers (see extractUsage's doc comment).
	if sawUsage && !r.plain {
		total := tokensIn + tokensOut
		fmt.Printf("  %s\n",
			gcolor.HEX("#64748b").Sprintf("%s tokens · %.1fs", formatTokenCount(total), time.Since(turnStart).Seconds()),
		)
	}

	// Spirit: award chat XP
	if spiritSt, spiritErr := state.Load(); spiritErr == nil {
		spirit.AwardXP(&spiritSt.Spirit, spirit.XPForAction("chat_message"))
		_ = spiritSt.Save()
	}

	r.turns++
	return nil
}

// ─── JIT tell-triggered protocol nudge ────────────────────────────────────────

// maybeProtocolNudge surfaces the single most relevant protocol gate as a dim
// one-line "[protocol] …" nudge when a failed chat turn's error text matches a
// wiring/boundary tell (protocol.TellFor). It is JIT by design — situated to the
// moment of failure rather than recited up front — and deliberately quiet:
//   - skipped when the protocol is disabled (Protocol.Enabled false, which
//     already reflects DOJO_PROTOCOL_DISABLED via config.Load),
//   - skipped under --plain/--no-color (r.plain) so piped/CI stdout stays clean
//     (--json is one-shot-only and never reaches this REPL chat path),
//   - fired at most once per distinct gate per session (r.nudger) so a recurring
//     error never nags.
func (r *REPL) maybeProtocolNudge(errText string) {
	if r.plain || r.cfg == nil || !r.cfg.Protocol.Enabled {
		return
	}
	if gate, ok := r.nudger.NudgeFor(errText); ok {
		fmt.Println(gcolor.HEX("#64748b").Sprintf("  [protocol] %s", gate))
	}
}

// ─── Verify loop (done-means-verified) ────────────────────────────────────────

// maybeVerifyAfterAgent runs the opt-in verify gate after a successful /agent
// dispatch or /run, printing a single clearly-labeled "[verify] PASS" or
// "[verify] FAIL: …" line. Gated on Verify.AfterAgent (OFF by default;
// DOJO_VERIFY_AFTER_AGENT flips it on) and on the command being a verify
// trigger. Deliberately best-effort and non-blocking: a FAIL is reported, never
// returned, so a red gate never aborts the session.
func (r *REPL) maybeVerifyAfterAgent(ctx context.Context, lowerLine string) {
	if r.cfg == nil || !r.cfg.Verify.AfterAgent || !isVerifyTrigger(lowerLine) {
		return
	}
	line, passed := verifyGate(ctx)
	if r.plain {
		fmt.Printf("  %s\n", line)
		return
	}
	hex := "#22c55e" // green for PASS
	if !passed {
		hex = "#ef4444" // red for FAIL
	}
	fmt.Println(gcolor.HEX(hex).Sprintf("  %s", line))
}

// isVerifyTrigger reports whether a successfully-dispatched slash-command line
// is one whose work may have changed code — a /agent dispatch or a /run — and
// so should be followed by the verify gate. It matches the canonical forms plus
// the /agents alias, on the already-lowercased, field-split line, so extra
// spacing never defeats it. Bare "/agent" (no dispatch) does not trigger.
func isVerifyTrigger(lowerLine string) bool {
	f := strings.Fields(lowerLine)
	if len(f) == 0 {
		return false
	}
	if f[0] == "/run" {
		return true
	}
	return (f[0] == "/agent" || f[0] == "/agents") && len(f) >= 2 && f[1] == "dispatch"
}

// verifyGate runs the build+test gate — go build ./... then go test ./... at the
// module root — and returns a single PASS/FAIL summary line plus whether it
// passed. It is best-effort: a missing go.mod, an absent go toolchain, or a
// failing step all resolve to a "[verify] FAIL: …" line rather than a returned
// error. Steps run under ctx so a session shutdown cancels them. This mirrors
// the /code gate's exec pattern (internal/commands/cmd_code.go); that package's
// runGoCmd/findGoModRoot are unexported and unreachable from here, so the small
// amount of exec logic is reproduced locally.
func verifyGate(ctx context.Context) (line string, passed bool) {
	root, err := goModRoot()
	if err != nil {
		return "[verify] FAIL: " + err.Error(), false
	}
	for _, args := range [][]string{
		{"build", "./..."},
		{"test", "./..."},
	} {
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = root
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			return fmt.Sprintf("[verify] FAIL: go %s — %s", strings.Join(args, " "), verifyFailDetail(out, cmdErr)), false
		}
	}
	return "[verify] PASS", true
}

// goModRoot walks up from the current working directory to the nearest
// directory containing a go.mod — the module root the verify gate builds and
// tests from. Mirrors internal/commands' findGoModRoot, which is unexported and
// so not reachable from this package.
func goModRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found from %s upward", dir)
		}
		dir = parent
	}
}

// verifyFailDetail condenses a failed gate's combined output into one concise
// line for the FAIL summary: the first non-empty output line (where the go
// toolchain prints the actual error), falling back to the process error when
// there is no captured output. Truncated so a wall of build errors never floods
// the prompt.
func verifyFailDetail(out []byte, err error) string {
	for _, ln := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			return truncateSSE(s, 200)
		}
	}
	return err.Error()
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

// extractUsage looks for token-usage counters in a raw SSE chunk payload
// (client.SSEChunk.Data). client.SSEChunk carries no typed usage field —
// Data is opaque JSON (or, per renderer_test.go's "done" fixtures, sometimes
// not JSON at all) — so this is a best-effort, forward-compatible parse
// rather than a decode against a known schema.
//
// It recognizes every usage shape already in use elsewhere against this same
// gateway/providers, in this priority order:
//   - {"usage": {"tokens_in": N, "tokens_out": N}} — the Dojo gateway's own
//     naming, confirmed live on the /events "complete" entries consumed by
//     internal/tui/pilot.go and the /api/telemetry/* rows in
//     internal/commands/cmd_telemetry.go.
//   - {"usage": {"input_tokens": N, "output_tokens": N}} — Anthropic's
//     naming, used by the direct-API path in internal/providers/providers.go.
//   - {"usage": {"prompt_tokens": N, "completion_tokens": N}} — OpenAI's
//     naming, also used in internal/providers/providers.go.
//
// The same three key-pairs are also checked at the top level (no "usage"
// wrapper), in case a future /v1/chat event flattens them. Returns
// ok == false — a no-op for the caller — when data is empty, non-JSON, or
// JSON that matches none of the above; that is the case for every "done"
// payload seen in this codebase's tests today.
func extractUsage(data string) (tokensIn, tokensOut int, ok bool) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return 0, 0, false
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return 0, 0, false
	}

	usage := m
	if nested, isMap := m["usage"].(map[string]any); isMap {
		usage = nested
	}

	pairs := [][2]string{
		{"tokens_in", "tokens_out"},
		{"input_tokens", "output_tokens"},
		{"prompt_tokens", "completion_tokens"},
	}
	for _, p := range pairs {
		in, inOK := numField(usage, p[0])
		out, outOK := numField(usage, p[1])
		if inOK || outOK {
			return in, out, true
		}
	}
	return 0, 0, false
}

// numField reads a numeric field out of a decoded-JSON object. encoding/json
// decodes every JSON number into float64 when the target is map[string]any,
// so this centralizes the float64→int conversion instead of repeating the
// type assertion at each call site in extractUsage.
func numField(m map[string]any, key string) (int, bool) {
	v, ok := m[key].(float64)
	if !ok {
		return 0, false
	}
	return int(v), true
}

// formatTokenCount renders a token count as a compact, human-scannable
// string for the per-turn footer. Under 1000 it prints the exact count
// ("342"); at or above 1000 it prints a tilde-prefixed one-decimal "k"
// figure ("~1.2k") — the tilde signals "approximate" without repeating the
// word. A trailing ".0" is dropped so an even thousand reads "~1k", not
// "~1.0k".
func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	k := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000.0), ".0")
	return "~" + k + "k"
}

// runPlain is the fallback when readline is unavailable (piped input, CI).
func (r *REPL) runPlain(ctx context.Context) error {
	// Note: printWelcome is already called by Run() before fallback here.
	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
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
	// /guide start's children are generated from guide.All (internal/guide)
	// instead of hand-maintained: guide.All has grown to 17 guides over time
	// but this completer only ever offered the original 4 (welcome, spirit,
	// agents, memory), so a static list would just drift stale again the next
	// time a guide is added. Reading the catalogue here keeps it correct by
	// construction — internal/guide itself is untouched.
	guideStartItems := make([]readline.PrefixCompleterInterface, 0, len(guide.All))
	for _, g := range guide.All {
		guideStartItems = append(guideStartItems, readline.PcItem(g.ID))
	}

	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/health"),
		readline.PcItem("/home",
			readline.PcItem("plain"),
		),
		readline.PcItem("/model",
			readline.PcItem("ls"),
			readline.PcItem("set"),
			readline.PcItem("direct"),
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
			readline.PcItem("search"),
			readline.PcItem("get"),
			readline.PcItem("inspect"),
			readline.PcItem("tags"),
			readline.PcItem("package-all"),
		),
		readline.PcItem("/doc"),
		readline.PcItem("/session",
			readline.PcItem("new"),
			readline.PcItem("ls"),
			readline.PcItem("resume"),
		),
		readline.PcItem("/run"),
		readline.PcItem("/garden",
			readline.PcItem("ls"),
			readline.PcItem("stats"),
			readline.PcItem("plant"),
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
			readline.PcItem("effective"),
			readline.PcItem("providers"),
			readline.PcItem("set"),
			readline.PcItem("profile",
				readline.PcItem("ls"),
				readline.PcItem("set"),
				readline.PcItem("show"),
				readline.PcItem("create"),
			),
		),
		readline.PcItem("/init",
			readline.PcItem("--force"),
			readline.PcItem("--gateway"),
			readline.PcItem("--plugins-source"),
			readline.PcItem("--skip-seeds"),
		),
		readline.PcItem("/practice"),
		readline.PcItem("/projects"),
		readline.PcItem("/plugin",
			readline.PcItem("ls"),
			readline.PcItem("install"),
			readline.PcItem("rm"),
		),
		readline.PcItem("/protocol",
			readline.PcItem("status"),
			readline.PcItem("harnesses"),
			readline.PcItem("install"),
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
			readline.PcItem("start", guideStartItems...),
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
			readline.PcItem("undo"),
		),
		readline.PcItem("/craft",
			readline.PcItem("adr"),
			readline.PcItem("scout"),
			readline.PcItem("claude-md",
				readline.PcItem("--fix"),
			),
			readline.PcItem("memory",
				readline.PcItem("ls"),
				readline.PcItem("add"),
				readline.PcItem("rm"),
				readline.PcItem("prune"),
				readline.PcItem("search"),
			),
			readline.PcItem("seed",
				readline.PcItem("ls"),
				readline.PcItem("plant"),
				readline.PcItem("harvest"),
				readline.PcItem("search"),
				readline.PcItem("elevate"),
			),
			readline.PcItem("view"),
			readline.PcItem("scaffold",
				readline.PcItem("go-service"),
				readline.PcItem("fullstack"),
				readline.PcItem("orchestration"),
				readline.PcItem("plugin"),
				readline.PcItem("minimal"),
			),
			readline.PcItem("converge"),
		),
		readline.PcItem("/doctor"),
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

func printWelcome(cfg *config.Config, session string, resumed bool, plain bool) {
	fmt.Println()

	// Decorative banner (bonsai + gradient wordmark) is interactive-only —
	// piped/CI consumers get the plain session/gateway lines below, nothing more.
	if !plain {
		// Bonsai sigil — zen visual anchor
		fmt.Print(art.SmallBonsaiString())

		// Sunset gradient wordmark
		fmt.Println(sunsetWordmark("  Dojo CLI"))
	}

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

	// JetBrains Mono one-time tip — decorative, interactive-only. Skipping it in
	// plain mode also leaves the marker uncreated, so it still shows on the next
	// interactive run.
	if !plain {
		home, _ := os.UserHomeDir()
		hintFile := home + "/.dojo/.mono-hint"
		if _, err := os.Stat(hintFile); os.IsNotExist(err) {
			gcolor.HEX("#94a3b8").Println("  tip: set terminal font to JetBrains Mono for best rendering")
			// Create the marker file so the tip never shows again
			_ = os.MkdirAll(home+"/.dojo", 0o755)
			f, ferr := os.Create(hintFile)
			if ferr == nil {
				_ = f.Close()
			}
		}
	}

	fmt.Println()
}
