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
	"github.com/DojoGenesis/cli/internal/repl"
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
		flagJSON        = flag.Bool("json", false, "Output JSON lines in one-shot mode (for scripted pipelines)")
		flagPlain       = flag.Bool("plain", false, "Plain text output (no ANSI colors, for piped/CI usage)")
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

	// Ensure ~/.dojo exists
	if err := os.MkdirAll(config.DojoDir(), 0700); err != nil {
		fatalf("could not create ~/.dojo: %s", err)
	}

	// Build gateway client
	gw := client.New(cfg.Gateway.URL, cfg.Gateway.Token, cfg.Gateway.Timeout)

	// Cancellable context — catches Ctrl+C
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One-shot mode: send a single message and exit
	if *flagOneShot != "" {
		workspaceRoot, _ := os.Getwd()
		req := client.ChatRequest{
			Message:       *flagOneShot,
			Model:         cfg.Defaults.Model,
			SessionID:     fmt.Sprintf("dojo-oneshot-%d", time.Now().UnixNano()),
			Stream:        true,
			WorkspaceRoot: workspaceRoot,
		}
		err = gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
			ev := repl.ClassifyChunk(chunk)
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
		if err != nil {
			fatalf("one-shot error: %s", err)
		}
		return
	}

	// Run REPL (plugin scan happens inside repl.New)
	r := repl.New(cfg, gw, *flagResume, *flagPlain || *flagNoColor)
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
  )
  _describe 'command' commands
}
compdef _dojo dojo
`)
	case "bash":
		fmt.Print(`_dojo_completions() {
  COMPREPLY=($(compgen -W "/help /health /home /model /tools /agent /skill /session /run /garden /trail /snapshot /trace /pilot /practice /projects /project /hooks /settings /guide /code /bloom /apps /workflow /doc /init /activity /plugin /disposition /telemetry /sensei /card /warroom exit" -- "${COMP_WORDS[COMP_CWORD]}"))
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
