package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/protocol"
	"github.com/DojoGenesis/cli/internal/repl"
	"github.com/DojoGenesis/cli/internal/state"
	gcolor "github.com/gookit/color"
)

var version = "0.1.0"

func main() {
	var (
		flagGateway     = flag.String("gateway", "", "Gateway URL (overrides config, e.g. http://localhost:7340)")
		flagToken       = flag.String("token", "", "Bearer token for gateway auth")
		flagVersion     = flag.Bool("version", false, "Print version and exit")
		flagNoColor     = flag.Bool("no-color", false, "Disable color output")
		flagDisposition = flag.String("disposition", "", "ADA disposition preset (focused|balanced|exploratory|deliberate)")
		flagOneShot     = flag.String("one-shot", "", "Execute a single message and exit (non-interactive)")
		flagCompletion  = flag.String("completion", "", "Generate shell completions (bash|zsh|fish)")
		flagResume      = flag.Bool("resume", false, "Resume the most recent session instead of starting fresh")
		flagSession     = flag.String("session", "", "Resume a specific session ID instead of the most recent one (implies --resume; see /session ls)")
		flagJSON        = flag.Bool("json", false, "Output JSON lines in one-shot mode (for scripted pipelines)")
		flagPlain       = flag.Bool("plain", false, "Plain text output (no ANSI colors, for piped/CI usage)")
		flagYolo        = flag.Bool("yolo", false, "Skip all permission prompts for this run (dangerous; never persisted to settings.json)")
	)
	flag.Parse()

	if *flagNoColor || *flagPlain {
		gcolor.Enable = false
	}

	if *flagVersion {
		fmt.Printf("dojo %s\n", version)
		os.Exit(0)
	}

	// Shell completion generation — no config or gateway needed
	if *flagCompletion != "" {
		printCompletion(*flagCompletion)
		os.Exit(0)
	}

	// Bare positional subcommands: `dojo version` / `dojo help` behave like the
	// --version / -h flags. Neither needs config or a gateway, so handle them
	// before anything else rather than launching the REPL.
	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("dojo %s\n", version)
			os.Exit(0)
		case "help":
			flag.Usage()
			os.Exit(0)
		}
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		fatalf("config error: %s", err)
	}

	// Flag overrides
	if *flagGateway != "" {
		cfg.Gateway.URL = *flagGateway
	}
	if *flagToken != "" {
		cfg.Gateway.Token = *flagToken
	}
	if *flagDisposition != "" {
		cfg.Defaults.Disposition = *flagDisposition
	}
	if *flagYolo {
		// In-memory only for this run — cfg.Save() is never called on this
		// path, and nothing here mutates settings.json, so the override can
		// never leak to disk the way an env-sourced field could (see
		// envOverride and Config.Save() in internal/config/config.go).
		cfg.Permissions.Mode = "yolo"
		fmt.Fprintln(os.Stderr, "dojo: YOLO mode: all permission prompts skipped")
	}

	// Ensure ~/.dojo exists
	if err := os.MkdirAll(config.DojoDir(), 0700); err != nil {
		fatalf("could not create ~/.dojo: %s", err)
	}

	// Build gateway client
	gw := client.New(cfg.Gateway.URL, cfg.Gateway.Token, cfg.Gateway.Timeout)

	// One-shot mode: send a single message and exit. Ctrl+C cancels the single
	// turn and exits.
	//
	// NOTE (SessionStart/SessionEnd, W4-LIFECYCLE): deliberately NOT fired
	// here. It would need its own plugin scan + hooks.Runner (this path never
	// builds a repl.REPL, so there's nothing to reuse), but the actual
	// blocker is that "prompt"/"agent" type hooks write straight to stdout
	// via fmt.Printf regardless of --json (see runHook in
	// internal/hooks/runner.go) — that would corrupt the JSON-lines contract
	// --json promises to scripted/CI consumers of --one-shot. Revisit if/when
	// hook stdout output is made --json-aware.
	if *flagOneShot != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		workspaceRoot, _ := os.Getwd()
		req := client.ChatRequest{
			Message:       *flagOneShot,
			Model:         cfg.Defaults.Model,
			SessionID:     fmt.Sprintf("dojo-oneshot-%d", time.Now().UnixNano()),
			Stream:        true,
			WorkspaceRoot: workspaceRoot,
		}
		// Carry the genius protocol on this single turn (prepends req.Message and
		// sets req.SystemPrompt when enabled; inert under DOJO_PROTOCOL_DISABLED).
		protocol.NewInjector(cfg).Apply(&req)

		// Record the first gateway error event. Without this a failed stream
		// (429 quota, 404 dead model, …) renders blank and the process exits 0,
		// so every failure looks like a silent success. See repl.EventError.
		var (
			errSeen bool
			errMsg  string
		)
		streamErr := gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
			ev := repl.ClassifyChunk(chunk)
			if ev.Type == repl.EventError && !errSeen {
				errSeen = true
				errMsg = ev.Content
			}
			if *flagJSON {
				if out := ev.RenderJSON(); out != "" {
					fmt.Println(out)
				}
			} else {
				if out := ev.Render(*flagPlain || *flagNoColor); out != "" {
					fmt.Print(out)
				}
			}
		})
		if !*flagJSON {
			fmt.Println()
		}

		// Transport-level failure (couldn't connect, stalled, non-200 status).
		if streamErr != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "dojo: cancelled")
				os.Exit(130)
			}
			fmt.Fprintf(os.Stderr, "dojo: %s\n", gw.FriendlyError(streamErr))
			os.Exit(1)
		}
		// Transport succeeded but the stream carried a gateway error event —
		// exit non-zero so scripts and CI see the failure.
		if errSeen {
			fmt.Fprintf(os.Stderr, "dojo: gateway error: %s\n", errMsg)
			os.Exit(1)
		}
		return
	}

	// --json is a one-shot-only pipeline flag; in interactive mode it is a silent
	// no-op today, so say so on stderr rather than ignoring it quietly.
	if *flagJSON {
		fmt.Fprintln(os.Stderr, "dojo: --json only applies to --one-shot mode; ignoring")
	}

	// Interactive REPL. The base context is deliberately NOT bound to SIGINT: a
	// single Ctrl+C during a streaming turn must cancel only that turn (handled
	// inside the REPL), not end the whole session. SIGTERM still shuts down.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	// --session <id> resumes one SPECIFIC session (Claude-Code-style
	// resume-by-id), vs. --resume's "whatever was last active". repl.New's
	// signature is unchanged (owned by a sibling change — see
	// internal/repl/repl.go): its resume bool contract is "load
	// state.LastSessionID and restore it verbatim". --session piggybacks on
	// that exact contract instead of widening it: persist the requested id
	// as the last session BEFORE constructing the REPL, then force
	// resume=true so repl.New picks it up. Bare --resume (no --session) is
	// untouched — same bool passthrough as before.
	resume := *flagResume
	if *flagSession != "" {
		state.SaveSession(*flagSession)
		resume = true
	}

	// Run REPL (plugin scan happens inside repl.New)
	r := repl.New(cfg, gw, resume, *flagPlain || *flagNoColor)
	if err := r.Run(ctx); err != nil {
		fatalf("repl error: %s", err)
	}
}

// printCompletion prints shell completion scripts for the given shell.
func printCompletion(shell string) {
	switch strings.ToLower(shell) {
	case "zsh":
		fmt.Print(`#compdef dojo
_dojo() {
  local -a commands
  commands=(
    '/help:show available commands'
    '/health:gateway health'
    '/home:workspace state'
    '/model:list models'
    '/tools:list tools'
    '/agent:agent operations'
    '/skill:skill operations'
    '/session:session management'
    '/run:orchestration'
    '/garden:memory garden'
    '/trail:memory timeline'
    '/snapshot:memory snapshots'
    '/trace:trace info'
    '/pilot:live event stream'
    '/practice:daily reflections'
    '/projects:project info'
    '/project:project lifecycle management'
    '/hooks:hook management'
    '/settings:show settings'
    '/guide:interactive tutorials'
    '/code:file ops and build tooling'
    '/bloom:bonsai garden meditation'
    '/apps:app launch and management'
    '/workflow:workflow operations'
    '/doc:documentation'
    '/init:workspace initialization'
    '/activity:activity log'
    '/plugin:plugin management'
    '/disposition:ADA disposition presets'
    '/telemetry:observability telemetry'
    '/sensei:koan from the sensei'
    '/card:dojo profile card'
    '/warroom:scout vs challenger debate'
    '/craft:DojoCraft practitioner workbench'
    '/doctor:read-only diagnostic'
    '/protocol:KE harness discovery + install'
  )
  _describe 'command' commands
}
compdef _dojo dojo
`)
	case "bash":
		fmt.Print(`_dojo_completions() {
  COMPREPLY=($(compgen -W "/help /health /home /model /tools /agent /skill /session /run /garden /trail /snapshot /trace /pilot /practice /projects /project /hooks /settings /guide /code /bloom /apps /workflow /doc /init /activity /plugin /disposition /telemetry /sensei /card /warroom /craft /doctor /protocol exit" -- "${COMP_WORDS[COMP_CWORD]}"))
}
complete -F _dojo_completions dojo
`)
	case "fish":
		fmt.Print(`complete -c dojo -f -a "/help" -d "show available commands"
complete -c dojo -f -a "/health" -d "gateway health"
complete -c dojo -f -a "/home" -d "workspace state"
complete -c dojo -f -a "/model" -d "list models"
complete -c dojo -f -a "/tools" -d "list tools"
complete -c dojo -f -a "/agent" -d "agent operations"
complete -c dojo -f -a "/skill" -d "skill operations"
complete -c dojo -f -a "/session" -d "session management"
complete -c dojo -f -a "/run" -d "orchestration"
complete -c dojo -f -a "/garden" -d "memory garden"
complete -c dojo -f -a "/trail" -d "memory timeline"
complete -c dojo -f -a "/snapshot" -d "memory snapshots"
complete -c dojo -f -a "/trace" -d "trace info"
complete -c dojo -f -a "/pilot" -d "live event stream"
complete -c dojo -f -a "/practice" -d "daily reflections"
complete -c dojo -f -a "/projects" -d "project info"
complete -c dojo -f -a "/project" -d "project lifecycle management"
complete -c dojo -f -a "/hooks" -d "hook management"
complete -c dojo -f -a "/settings" -d "show settings"
complete -c dojo -f -a "/guide" -d "interactive tutorials"
complete -c dojo -f -a "/code" -d "file ops and build tooling"
complete -c dojo -f -a "/bloom" -d "bonsai garden meditation"
complete -c dojo -f -a "/apps" -d "app launch and management"
complete -c dojo -f -a "/workflow" -d "workflow operations"
complete -c dojo -f -a "/doc" -d "documentation"
complete -c dojo -f -a "/init" -d "workspace initialization"
complete -c dojo -f -a "/activity" -d "activity log"
complete -c dojo -f -a "/plugin" -d "plugin management"
complete -c dojo -f -a "/disposition" -d "ADA disposition presets"
complete -c dojo -f -a "/telemetry" -d "observability telemetry"
complete -c dojo -f -a "/sensei" -d "koan from the sensei"
complete -c dojo -f -a "/card" -d "dojo profile card"
complete -c dojo -f -a "/warroom" -d "scout vs challenger debate"
complete -c dojo -f -a "/craft" -d "DojoCraft practitioner workbench"
complete -c dojo -f -a "/doctor" -d "read-only diagnostic"
complete -c dojo -f -a "/protocol" -d "KE harness discovery + install"
`)
	default:
		fmt.Fprintf(os.Stderr, "dojo: unknown shell %q (supported: bash, zsh, fish)\n", shell)
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dojo: "+format+"\n", args...)
	os.Exit(1)
}
