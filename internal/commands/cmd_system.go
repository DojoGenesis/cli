package commands

// cmd_system.go — /health, /settings, /hooks, /trace, /init, /home commands.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DojoGenesis/cli/internal/bootstrap"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/tui"
	gcolor "github.com/gookit/color"
)

// gatewayHTTPStatus extracts the HTTP status code from a gateway client
// error's text, if present. The client formats non-2xx responses as
// "gateway <path> returned <code>: <body>" (see internal/client/client.go's
// get/post helpers). Returns 0 when no status code is present at all — e.g. a
// connection-level failure (refused, timeout, DNS) that never got an HTTP
// response, which is the one case that is genuinely "unreachable".
func gatewayHTTPStatus(err error) int {
	const marker = "returned "
	msg := err.Error()
	idx := strings.Index(msg, marker)
	if idx == -1 {
		return 0
	}
	rest := msg[idx+len(marker):]
	end := strings.IndexByte(rest, ':')
	if end == -1 {
		end = len(rest)
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if convErr != nil {
		return 0
	}
	return code
}

// wrapGatewayErr labels a gateway call failure by what actually happened
// instead of always saying "unreachable": a 401/403 means the gateway is up
// but rejected our credentials, a 5xx means it's up but erroring internally,
// and anything else (including no status code at all — refused/timeout/DNS)
// keeps the original "unreachable" label since we never got a real response.
func wrapGatewayErr(err error) error {
	switch code := gatewayHTTPStatus(err); {
	case code == 401 || code == 403:
		return fmt.Errorf("gateway auth failed (401/403): %w", err)
	case code >= 500 && code <= 599:
		return fmt.Errorf("gateway error (5xx): %w", err)
	default:
		return fmt.Errorf("gateway unreachable: %w", err)
	}
}

// ─── /health ────────────────────────────────────────────────────────────────

func (r *Registry) healthCmd() Command {
	return Command{
		Name:    "health",
		Aliases: []string{"ping", "status"},
		Usage:   "/health",
		Short:   "Gateway health + stats",
		Run: func(ctx context.Context, args []string) error {
			h, err := r.gw.Health(ctx)
			if err != nil {
				return wrapGatewayErr(err)
			}
			fmt.Println()
			printKV("status", colorStatus(h.Status))
			printKV("version", h.Version)
			printKV("uptime", fmt.Sprintf("%ds", h.UptimeSeconds))
			printKV("requests", fmt.Sprintf("%d", h.RequestsProcessed))
			for name, st := range h.Providers {
				printKV("provider/"+name, colorStatus(st))
			}
			printKV("gateway", r.cfg.Gateway.URL)
			fmt.Println()
			return nil
		},
	}
}

// ─── /home ──────────────────────────────────────────────────────────────────

func (r *Registry) homeCmd() Command {
	return Command{
		Name:    "home",
		Aliases: []string{"ws", "workspace"},
		Usage:   "/home [plain]",
		Short:   "Workspace state overview",
		Run: func(ctx context.Context, args []string) error {
			// /home plain — text-only fallback
			if len(args) > 0 && args[0] == "plain" {
				return r.homePlain(ctx)
			}

			// Headless (JSON dispatch, or any non-interactive run) has no
			// terminal to paint an alt-screen TUI into -- route to the same
			// plain overview "/home plain" uses instead of launching
			// Bubbletea, which would hang or corrupt the dispatch.
			if r.out.JSON() || r.headless {
				return r.homePlain(ctx)
			}

			// Default: Bubbletea TUI panel
			model := tui.NewHomeModel(r.cfg, r.gw, *r.session, len(r.plgs))
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

// homeSnapshot is /home's structured JSON payload -- one typed object
// instead of scraping the aligned key/value table homePlain prints below.
// Agent/seed counts and their fetch errors are mutually exclusive per field
// (an errored fetch has no count), mirroring the human view's "unavailable"
// vs. count display.
type homeSnapshot struct {
	Gateway       string `json:"gateway"`
	GatewayStatus string `json:"gateway_status"`
	AgentCount    int    `json:"agent_count,omitempty"`
	AgentsError   string `json:"agents_error,omitempty"`
	SeedCount     int    `json:"seed_count,omitempty"`
	SeedsError    string `json:"seeds_error,omitempty"`
	Plugins       int    `json:"plugins"`
	Session       string `json:"session"`
}

func (r *Registry) homePlain(ctx context.Context) error {
	h, err := r.gw.Health(ctx)
	if err != nil {
		return wrapGatewayErr(err)
	}
	agents, agentErr := r.gw.Agents(ctx)
	seeds, seedErr := r.gw.Seeds(ctx)

	if r.out.JSON() {
		snap := homeSnapshot{
			Gateway:       r.cfg.Gateway.URL,
			GatewayStatus: h.Status,
			Plugins:       len(r.plgs),
			Session:       *r.session,
		}
		if agentErr != nil {
			snap.AgentsError = agentErr.Error()
		} else {
			snap.AgentCount = len(agents)
		}
		if seedErr != nil {
			snap.SeedsError = seedErr.Error()
		} else {
			snap.SeedCount = len(seeds)
		}
		r.out.Data(snap)
		return nil
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Dojo Workspace"))
	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + r.cfg.Gateway.URL))
	fmt.Println()

	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("gateway"), colorStatus(h.Status))
	if agentErr != nil {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("agents"), gcolor.HEX("#e63946").Sprint("unavailable"))
	} else {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("agents"), gcolor.White.Sprintf("%d", len(agents)))
	}
	if seedErr != nil {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("seeds"), gcolor.HEX("#e63946").Sprint("unavailable"))
	} else {
		fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("seeds"), gcolor.White.Sprintf("%d", len(seeds)))
	}
	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("plugins"), gcolor.White.Sprintf("%d", len(r.plgs)))
	fmt.Printf("  %-18s %s\n", gcolor.HEX("#f4a261").Sprint("session"), gcolor.HEX("#e8b04a").Sprint(*r.session))
	fmt.Println()
	return nil
}

// ─── /settings ──────────────────────────────────────────────────────────────

func (r *Registry) settingsCmd() Command {
	return Command{
		Name:    "settings",
		Aliases: []string{"config", "cfg"},
		Usage:   "/settings [effective|providers|set <provider> <key>|profile [ls|set|show|create]]",
		Short:   "Show active config and settings, or manage provider keys and disposition profiles",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				if r.out.JSON() {
					data := effectiveSettingsData(r.cfg.EffectiveString())
					data["config_file"] = config.SettingsPath()
					data["plugins_loaded"] = fmt.Sprintf("%d", len(r.plgs))
					r.out.Data(data)
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Active Settings"))
				fmt.Println()
				fmt.Println()
				printKV("config file", config.SettingsPath())
				printKV("plugins loaded", fmt.Sprintf("%d", len(r.plgs)))
				fmt.Print(r.cfg.EffectiveString())
				fmt.Println()
				return nil
			}

			sub := strings.ToLower(args[0])
			switch sub {
			case "effective":
				if r.out.JSON() {
					r.out.Data(effectiveSettingsData(r.cfg.EffectiveString()))
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Effective Configuration"))
				fmt.Println()
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  (file + env + flags, in priority order)"))
				fmt.Println()
				fmt.Print(r.cfg.EffectiveString())
				fmt.Println()

			case "providers":
				providerSettings, err := r.gw.GetProviderSettings(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch provider settings: %w", err)
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Provider Configuration"))
				fmt.Println()
				fmt.Println()
				for k, v := range providerSettings {
					printKV(k, colorStatus(fmt.Sprintf("%v", v)))
				}
				fmt.Println()

			case "set":
				// /settings set <provider> <key>
				if len(args) < 3 {
					return fmt.Errorf("usage: /settings set <provider> <api-key>")
				}
				provider := args[1]
				apiKey := args[2]
				if err := r.gw.SetProviderKey(ctx, provider, apiKey); err != nil {
					return fmt.Errorf("could not set provider key: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Provider key updated"))
				printKV("provider", provider)
				fmt.Println()

			case "profile", "profiles":
				return r.settingsProfile(args[1:])

			default:
				return fmt.Errorf("unknown settings subcommand %q — use: effective, providers, set, profile", sub)
			}
			return nil
		},
	}
}

// effectiveSettingsData parses cfg.EffectiveString()'s "key = value" lines
// into a map for JSON mode. It reuses the exact same rendering the human
// path already prints — including its gateway-token masking (EffectiveString
// never puts the raw token in the string) — rather than re-deriving values
// from cfg directly, so a headless caller can never see a field the human
// view redacts.
func effectiveSettingsData(effective string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(effective, "\n"), "\n") {
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

// settingsProfile handles /settings profile [ls|set <name>|show <name>|create <name> ...]
// Delegates to the disposition handlers so both /disposition and /settings profile
// operate on the same state.
func (r *Registry) settingsProfile(args []string) error {
	if len(args) == 0 {
		return r.dispositionList()
	}
	switch strings.ToLower(args[0]) {
	case "ls", "list":
		return r.dispositionList()
	case "set":
		if len(args) < 2 {
			return fmt.Errorf("usage: /settings profile set <name>")
		}
		return r.dispositionSet(args[1])
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: /settings profile show <name>")
		}
		return r.dispositionShow(args[1])
	case "create":
		if len(args) < 6 {
			return fmt.Errorf("usage: /settings profile create <name> <pacing> <depth> <tone> <initiative>")
		}
		return r.dispositionCreate(args[1], args[2], args[3], args[4], args[5])
	default:
		return fmt.Errorf("unknown profile subcommand %q — use: ls, set, show, create", args[0])
	}
}

// ─── /hooks ─────────────────────────────────────────────────────────────────

// hookRuleInfo is one row of /hooks ls's structured JSON payload — one hook
// callback within one plugin's rule, flattened out of the nested
// plugin -> rule -> hook loop the human table below renders.
type hookRuleInfo struct {
	Plugin string `json:"plugin"`
	Event  string `json:"event"`
	Type   string `json:"type"`
	Target string `json:"target"` // whichever of command/prompt/URL the hook carries
	Async  bool   `json:"async"`
}

func (r *Registry) hooksCmd() Command {
	return Command{
		Name:  "hooks",
		Usage: "/hooks [ls|fire <event>]",
		Short: "List loaded hook rules or fire an event manually",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "fire":
				if len(args) < 2 {
					return fmt.Errorf("usage: /hooks fire <event>")
				}
				event := args[1]
				fmt.Printf("\n  Firing event %q ...\n\n", event)
				if err := r.runner.Fire(ctx, event, nil); err != nil {
					return fmt.Errorf("hook fire error: %w", err)
				}
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  done"))
				fmt.Println()

			case "ls":
				if r.out.JSON() {
					// Initialized (not `var rows []hookRuleInfo`) so a
					// no-hooks workspace serializes as `[]`, not `null` — a
					// JSON list consumer should never have to special-case
					// null vs. empty for "nothing here".
					rows := []hookRuleInfo{}
					for _, p := range r.plgs {
						for _, rule := range p.HookRules {
							for _, h := range rule.Hooks {
								target := h.Command
								if target == "" {
									target = h.Prompt
								}
								if target == "" {
									target = h.URL
								}
								rows = append(rows, hookRuleInfo{
									Plugin: p.Name,
									Event:  rule.Event,
									Type:   h.Type,
									Target: target,
									Async:  h.Async,
								})
							}
						}
					}
					r.out.Data(rows)
					return nil
				}

				// Count total rules
				totalRules := 0
				for _, p := range r.plgs {
					totalRules += len(p.HookRules)
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Hooks (%d rules across %d plugins)\n\n", totalRules, len(r.plgs)))

				if totalRules == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  No hook rules loaded. Place plugins in %s", r.cfg.Plugins.Path))
					fmt.Println()
					return nil
				}

				for _, p := range r.plgs {
					if len(p.HookRules) == 0 {
						continue
					}
					// Glass-effect section divider
					fmt.Printf("  %s %s %s\n",
						gcolor.HEX("#64748b").Sprint("────"),
						gcolor.HEX("#e8b04a").Sprint("["+p.Name+"]"),
						gcolor.HEX("#64748b").Sprint("────"),
					)
					for _, rule := range p.HookRules {
						for _, h := range rule.Hooks {
							asyncLabel := ""
							if h.Async {
								asyncLabel = gcolor.HEX("#94a3b8").Sprint("  (async)")
							}
							cmd := h.Command
							if cmd == "" {
								cmd = h.Prompt
							}
							if cmd == "" {
								cmd = h.URL
							}
							fmt.Printf("    %s  %s  %s%s\n",
								gcolor.HEX("#f4a261").Sprintf("%-20s", rule.Event),
								gcolor.White.Sprintf("%-10s", h.Type),
								gcolor.HEX("#94a3b8").Sprint(truncate(cmd, 50)),
								asyncLabel,
							)
						}
					}
				}
				fmt.Println()

			default:
				// D1 — an unrecognized subcommand used to fall silently into
				// "ls" (same failure mode fixed in /agent — see cmd_agent.go).
				// Bare "/hooks" still lists (sub defaults to "ls" above,
				// which the "ls" case handles), so only a real unrecognized
				// token reaches here.
				return fmt.Errorf("unknown subcommand %q — see /help", args[0])
			}
			return nil
		},
	}
}

// ─── /trace ─────────────────────────────────────────────────────────────────

func (r *Registry) traceCmd() Command {
	return Command{
		Name:  "trace",
		Usage: "/trace [<id>]",
		Short: "Inspect an execution trace by ID, or show guidance",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// Guidance mode
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Dojo Trace"))
				fmt.Println()
				fmt.Println()
				fmt.Println(gcolor.HEX("#457b9d").Sprint("  Trace follows the active session's decision and tool-use history."))
				fmt.Println(gcolor.HEX("#457b9d").Sprint("  There is no --trace startup flag. Run /trace <id> with a real trace ID to fetch it from the gateway."))
				fmt.Println()
				printKV("gateway", r.cfg.Gateway.URL)
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  hint: /trace <id>  — provide a trace ID to inspect"))
				fmt.Println()
				return nil
			}

			traceID := args[0]
			data, err := r.gw.GetTrace(ctx, traceID)
			if err != nil {
				return fmt.Errorf("could not fetch trace: %w", err)
			}
			fmt.Println()
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Trace: %s\n\n", traceID))
			for k, v := range data {
				switch val := v.(type) {
				case map[string]any, []any:
					// Format nested structures as indented JSON
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

// ─── /init ──────────────────────────────────────────────────────────────────

func (r *Registry) initCmd() Command {
	return Command{
		Name:    "init",
		Aliases: []string{"setup", "bootstrap"},
		Usage:   "/init [--force] [--gateway <url>] [--plugins-source <path>]",
		Short:   "Initialize Dojo workspace with plugins, dispositions, and seeds",
		Run: func(ctx context.Context, args []string) error {
			opts := bootstrap.Options{
				GatewayURL: r.cfg.Gateway.URL,
			}
			for i := 0; i < len(args); i++ {
				switch args[i] {
				case "--force":
					opts.Force = true
				case "--gateway":
					if i+1 < len(args) {
						i++
						opts.GatewayURL = args[i]
					}
				case "--plugins-source":
					if i+1 < len(args) {
						i++
						opts.PluginsSource = args[i]
					}
				case "--skip-seeds":
					opts.SkipSeeds = true
				}
			}

			_, err := bootstrap.Run(ctx, opts, r.gw, os.Stdout)
			return err
		},
	}
}
