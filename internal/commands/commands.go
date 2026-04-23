// Package commands implements all dojo slash commands.
package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/hooks"
	"github.com/DojoGenesis/cli/internal/plugins"
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
				return err
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
	r.add(r.projectCmd())
	r.add(r.activityCmd())
	r.add(r.pluginCmd())
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
