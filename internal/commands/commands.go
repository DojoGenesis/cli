// Package commands implements all dojo slash commands.
package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/guardrail"
	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/output"
	"github.com/DojoGenesis/cli/internal/plugins"
	gcolor "github.com/gookit/color"
)

// ErrUnknownCommand is the sentinel returned by Dispatch when no command (or
// alias) matches. Headless callers map it to exit code 2 (usage) rather than 1
// (runtime); errors.Is(err, ErrUnknownCommand) is the check.
var ErrUnknownCommand = errors.New("unknown command")

// curEmitter is the output sink for the command currently being dispatched
// headlessly. It is nil in the REPL/Human path (so the free formatting helpers
// print exactly as before). RunHeadless sets it for the duration of one
// dispatch and clears it after. Command dispatch is single-goroutine (the REPL
// read loop or the one-shot path), so an unsynchronized package var is safe;
// the Emitter's own state is likewise touched from that one goroutine only.
var curEmitter *output.Emitter

// Registry maps slash command names to handler functions.
type Registry struct {
	cfg     *config.Config
	gw      *client.Client
	cmds    map[string]Command
	plgs    []plugins.Plugin
	runner  *hooks.Runner
	session *string // pointer to REPL's active session ID

	// guard counts consecutive per-command failures and escalates advisory
	// notices (never blocks — see internal/guardrail). Built lazily on first
	// Dispatch from cfg.Guardrails rather than in New because tests construct
	// Registry literals directly; nil cfg means disabled. Held for the
	// Registry lifetime so counts persist across the whole REPL session.
	guard *guardrail.Tracker

	// advicePrint renders one guardrail advice line. nil means the default
	// stdout printer (same surface the REPL prints command errors to);
	// overridable so tests can capture advice without hijacking os.Stdout.
	advicePrint func(msg string)

	// out is the structured output sink for the current headless dispatch, or
	// nil in the REPL/Human path. Set + cleared by RunHeadless. Command methods
	// that want to emit a rich typed payload call r.out.Data(...)/Field(...);
	// the free helpers (printKV, colorStatus) consult the package curEmitter.
	out *output.Emitter

	// headless is true while a command runs under RunHeadless (no TTY, no
	// interactive stdin). Commands that would block on a y/N confirmation call
	// r.headlessRefuse(...) to fail cleanly instead of hanging a script.
	headless bool

	// assumeYes records explicit --yes consent for this run (set by SetAssumeYes).
	// It lets confirmation-gated commands run headlessly without the blunt --yolo.
	assumeYes bool
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
		runner:  hooks.New(plgs, gw),
		session: session,
	}
	r.register()
	return r
}

// Runner returns the hook runner so the REPL can fire events.
func (r *Registry) Runner() *hooks.Runner {
	return r.runner
}

// secretArgPositions maps (command name, subcommand) → the index within args
// (after the command name) that holds a secret. A subcommand of "" means the
// secret sits at args[0] with no prior subcommand token.
//
// To add a new secret-bearing command: add one entry here — that is the only
// place that needs changing.
var secretArgPositions = map[[2]string]int{
	// /settings set <provider> <api-key>  → args = ["set", provider, api-key]
	// api-key is at index 2 within args.
	{"settings", "set"}: 2,
}

// redactSecretArgs returns a copy of args with any secret positional argument
// replaced by "<redacted>". The name parameter is the command name (without
// the leading "/"). This function never mutates the original slice.
func redactSecretArgs(name string, args []string) []string {
	if len(args) == 0 {
		return args
	}
	sub := strings.ToLower(args[0])
	key := [2]string{strings.ToLower(name), sub}
	secretIdx, ok := secretArgPositions[key]
	if !ok {
		return args
	}
	if secretIdx >= len(args) {
		return args
	}
	// Copy to avoid mutating the original.
	redacted := make([]string, len(args))
	copy(redacted, args)
	redacted[secretIdx] = "<redacted>"
	return redacted
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
		err := cmd.Run(ctx, args)
		if err == nil {
			logArgs := redactSecretArgs(name, args)
			activity.Log(activity.CommandRun, fmt.Sprintf("/%s %s", name, strings.Join(logArgs, " ")))
		}
		r.guardAdvise(cmd.Name, args, err)
		return err
	}
	// Alias scan
	for _, cmd := range r.cmds {
		for _, a := range cmd.Aliases {
			if a == name {
				err := cmd.Run(ctx, args)
				if err == nil {
					logArgs := redactSecretArgs(cmd.Name, args)
					activity.Log(activity.CommandRun, fmt.Sprintf("/%s %s", name, strings.Join(logArgs, " ")))
				}
				// Canonical name, not the typed alias, so every spelling of
				// the same command feeds one failure streak.
				r.guardAdvise(cmd.Name, args, err)
				return err
			}
		}
	}
	return fmt.Errorf("%w /%s — type /help for a list", ErrUnknownCommand, name)
}

// guardKey builds the guardrail key for one dispatched command: "cmd:" plus
// the canonical command name, plus the first subcommand token (lowercased,
// matching how subcommand matching works elsewhere in this package) when one
// is present — e.g. "cmd:code test". Keying on the subcommand keeps
// "/code test" and "/code build" as independent failure streaks.
func guardKey(name string, args []string) string {
	key := "cmd:" + name
	if len(args) > 0 {
		key += " " + strings.ToLower(args[0])
	}
	return key
}

// guardAdvise records one dispatched command's outcome with the guardrail
// tracker and prints any resulting advice on its own line. Advisory only:
// the command's error is returned to the caller untouched by Dispatch, and
// the advice line goes to stdout — the same surface the REPL prints command
// errors to (repl.go's `gcolor.Red.Printf("  error: ...")`) — styled with
// the warning amber used by the renderer's EventWarning.
func (r *Registry) guardAdvise(name string, args []string, runErr error) {
	if r.guard == nil {
		// Lazy build (see Registry.guard doc). Dispatch runs on the single
		// REPL read-loop goroutine, so an unguarded nil check is safe here;
		// the Tracker itself is mutex-guarded regardless.
		if r.cfg == nil {
			r.guard = guardrail.New(0, 0, false) // nil cfg → disabled
		} else {
			g := r.cfg.Guardrails
			r.guard = guardrail.New(g.WarnAfter, g.HardAfter, g.Enabled)
		}
	}
	adv := r.guard.Record(guardKey(name, args), runErr != nil)
	if adv.Level == guardrail.None {
		return
	}
	if r.advicePrint != nil {
		r.advicePrint(adv.Msg)
		return
	}
	fmt.Printf("  %s\n", gcolor.HEX("#f4a261").Sprint(adv.Msg))
}

func (r *Registry) add(cmd Command) {
	r.cmds[cmd.Name] = cmd
}

// ─── Headless / JSON dispatch ───────────────────────────────────────────────

// RunHeadless dispatches input in JSON mode and returns the result Envelope. It
// is the non-interactive counterpart to Dispatch: same routing, guardrail, and
// activity logging, but the command's output is captured into a structured
// {ok, command, data, error} envelope instead of streamed as human text.
//
// input is the command line WITHOUT the leading "/", identical to Dispatch.
//
// Mechanism: os.Stdout (and gcolor's writer) are redirected to a pipe for the
// duration of the handler so that any direct fmt.Printf a not-yet-converted
// command emits does not corrupt the JSON stream — it is captured and, if the
// command produced no structured data, surfaced as a {"text": …} fallback. The
// Emitter keeps the real stdout, so a streaming command's Emit(...) NDJSON lines
// bypass the redirect and appear live before this terminal envelope.
func (r *Registry) RunHeadless(ctx context.Context, input string) output.Envelope {
	real := os.Stdout
	em := output.New(output.JSON, real)

	rp, wp, err := os.Pipe()
	if err != nil {
		// Can't set up capture — fall back to running without the redirect so
		// the command still executes; stray stdout may interleave, but the
		// envelope is still emitted.
		r.out, curEmitter, r.headless = em, em, true
		derr := r.Dispatch(ctx, input)
		r.out, curEmitter, r.headless = nil, nil, false
		return envelopeFor(input, em, "", derr)
	}

	os.Stdout = wp
	gcolor.SetOutput(wp)
	r.out, curEmitter, r.headless = em, em, true

	// Drain the pipe concurrently so a chatty command cannot deadlock on a full
	// pipe buffer while it is still writing.
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, rp); close(done) }()

	derr := r.Dispatch(ctx, input)

	_ = wp.Close()
	<-done
	_ = rp.Close()
	os.Stdout = real
	gcolor.SetOutput(real)
	r.out, curEmitter, r.headless = nil, nil, false

	return envelopeFor(input, em, buf.String(), derr)
}

// envelopeFor assembles the terminal Envelope from a dispatch outcome.
func envelopeFor(input string, em *output.Emitter, captured string, err error) output.Envelope {
	env := output.Envelope{
		Command: commandName(input),
		OK:      err == nil,
		Data:    em.Payload(captured),
	}
	if err != nil {
		env.Data = nil
		env.Error = &output.Err{Code: errCode(err), Message: cleanErr(err)}
	}
	return env
}

// errCode maps an error to a stable, machine-branchable slug. Unknown commands
// are a usage error (exit 2); everything else is a runtime error (exit 1).
func errCode(err error) string {
	if errors.Is(err, ErrUnknownCommand) {
		return "unknown_command"
	}
	return "error"
}

// cleanErr strips the leading "unknown command " sentinel prefix so the
// envelope message reads naturally (the code already carries the class).
func cleanErr(err error) string {
	msg := err.Error()
	return strings.TrimPrefix(msg, "unknown command ")
}

// commandName returns the command token (first field, lowercased) from a
// dispatch input, for the envelope's `command` field. Multi-word commands keep
// only the head — e.g. "skill get foo" → "skill".
func commandName(input string) string {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0])
}

// SetAssumeYes records explicit non-interactive consent for the whole run — the
// `--yes` flag. It lets confirmation-gated commands (delete, undo, install, …)
// run headlessly: headlessRefuse then passes, and because the command is past
// that gate in headless mode, autoConfirmed() reports the prompt pre-answered.
// It is scoped per invocation (never persisted), and narrower than --yolo, which
// also skips the permissions gate.
func (r *Registry) SetAssumeYes(b bool) { r.assumeYes = b }

// autoConfirmed reports whether an interactive y/N prompt should be treated as
// already answered "yes" without reading stdin. True in headless mode (a command
// only reaches its prompt in headless mode after headlessRefuse let it past,
// which requires consent), false in the interactive REPL (where the prompt runs
// for real). The canonical guard is `if !r.autoConfirmed() && !confirm() { … }`.
func (r *Registry) autoConfirmed() bool { return r.headless }

// headlessRefuse returns a non-nil error when a command that needs interactive
// confirmation is invoked headlessly WITHOUT consent. Commands call it before any
// y/N prompt so a scripted caller fails cleanly instead of hanging on stdin.
// Returns nil in the interactive REPL, or headless under explicit consent —
// `--yes` (this command only) or `--yolo` (skip all permission prompts).
func (r *Registry) headlessRefuse(action string) error {
	if !r.headless {
		return nil
	}
	if r.assumeYes {
		return nil
	}
	if r.cfg != nil && r.cfg.Permissions.Mode == "yolo" {
		return nil
	}
	return fmt.Errorf("refused: %q needs confirmation — re-run with --yes (or --yolo), or in the REPL", action)
}

// ─── Discovery ──────────────────────────────────────────────────────────────

// CommandInfo is the machine-readable descriptor of one command, emitted by
// `dojo --commands`. It is the serializable projection of Command (the Run func
// is omitted). Args/subcommands are not structured — they live in the free-text
// Usage string today.
type CommandInfo struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Short   string   `json:"short"`
	Usage   string   `json:"usage"`
}

// Commands returns every registered command as a CommandInfo, sorted by name —
// the single source of truth for the command catalog (help, completions, and
// `--commands` discovery should all derive from this rather than drift as
// hand-maintained lists).
func (r *Registry) Commands() []CommandInfo {
	out := make([]CommandInfo, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, CommandInfo{
			Name:    c.Name,
			Aliases: c.Aliases,
			Short:   c.Short,
			Usage:   c.Usage,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ─── Registration ─────────────────────────────────────────────────────────────

func (r *Registry) register() {
	r.add(r.helpCmd())
	r.add(r.healthCmd())
	r.add(r.doctorCmd())
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
	r.add(r.projectCmd())
	r.add(r.activityCmd())
	r.add(r.pluginCmd())
	r.add(r.protocolCmd())
	r.add(r.dispositionCmd())
	r.add(r.telemetryCmd())
	r.add(r.senseiCmd())
	r.add(r.cardCmd())
	r.add(r.warroomCmd())
	r.add(r.guideCmd())
	r.add(r.bloomCmd())
	r.add(r.codeCmd())
	r.add(r.craftCmd())
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

// fmtUnixAgo formats a unix epoch timestamp (seconds) as a human-readable "X ago" string.
func fmtUnixAgo(ts int64) string {
	if ts == 0 {
		return "unknown"
	}
	t := time.Unix(ts, 0)
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
