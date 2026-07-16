package commands

// cmd_doctor.go — /doctor: a read-only, one-screen diagnostic over gateway
// reachability, per-provider status, active config, and loaded plugins/hooks.
// No mutations — this command never writes config, never calls a gateway
// write endpoint, and never changes state.
//
// Self-contained by design: it only depends on symbols that already exist
// elsewhere in this package/tree as of this writing (Registry.cfg/gw/plgs,
// client.Client.Health/Providers/FriendlyError, config.SettingsPath/
// IsKnownDisposition/DefaultDisposition, and the package's existing
// orDefault helper). It deliberately does NOT reference any Protocol config
// fields — those are landing in config.go on a concurrent track.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/DojoGenesis/cli/internal/config"
	gcolor "github.com/gookit/color"
)

// ─── /doctor ────────────────────────────────────────────────────────────────

func (r *Registry) doctorCmd() Command {
	return Command{
		Name:  "doctor",
		Usage: "/doctor",
		Short: "Read-only diagnostic: gateway, providers, config, plugins/hooks",
		Run: func(ctx context.Context, args []string) error {
			fmt.Println()
			gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Dojo Doctor"))
			fmt.Println()
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + r.cfg.Gateway.URL))
			fmt.Println()

			warnCount := 0
			check := func(ok bool, detail string) {
				if !ok {
					warnCount++
				}
				fmt.Printf("    %s  %s\n", doctorTag(ok), detail)
			}
			section := func(name string) {
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#f4a261").Sprint("  " + name))
				fmt.Println()
			}

			section("GATEWAY")
			r.doctorGateway(ctx, check)

			section("PROVIDERS")
			r.doctorProviders(ctx, check)

			section("CONFIG")
			r.doctorConfig(check)

			section("PLUGINS/HOOKS")
			r.doctorPlugins(check)

			fmt.Println()
			if warnCount == 0 {
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  All checks OK"))
			} else {
				plural := "s"
				if warnCount == 1 {
					plural = ""
				}
				fmt.Println(gcolor.HEX("#e63946").Sprintf("  %d warning%s — see above", warnCount, plural))
			}
			fmt.Println()
			return nil
		},
	}
}

// doctorCheck emits one OK/WARN line for a /doctor section and tallies the
// running warning count in its caller's closure.
type doctorCheck func(ok bool, detail string)

// doctorTag renders the OK/WARN prefix. Both branches are exactly 4 visible
// runes ("OK  " / "WARN"), so callers can print it directly ahead of the
// detail text without needing ANSI-aware column padding.
func doctorTag(ok bool) string {
	if ok {
		return gcolor.HEX("#7fb88c").Sprint("OK  ")
	}
	return gcolor.HEX("#e63946").Sprint("WARN")
}

// doctorHealthy reports whether a gateway/provider status string counts as
// good. Mirrors colorStatus's "good" set (cmd_help.go) so /doctor's OK/WARN
// calls agree with how /health colors the same values. Duplicated here as a
// small literal set — rather than calling colorStatus, which only colors a
// string and doesn't expose a boolean classification — to keep this file
// self-contained.
func doctorHealthy(s string) bool {
	switch strings.ToLower(s) {
	case "ok", "healthy", "active", "running", "ready", "completed":
		return true
	default:
		return false
	}
}

// ─── GATEWAY ────────────────────────────────────────────────────────────────

// doctorGateway checks gateway reachability via gw.Health, the same call
// healthCmd (cmd_system.go) uses. Unreachable gets WARN with the client's
// built-in friendly hint (the same one main.go / repl.go print on a failed
// call) instead of a raw error.
func (r *Registry) doctorGateway(ctx context.Context, check doctorCheck) {
	h, err := r.gw.Health(ctx)
	if err != nil {
		check(false, r.gw.FriendlyError(err))
		return
	}
	detail := fmt.Sprintf("reachable — status=%s version=%s uptime=%ds",
		h.Status, orDefault(h.Version, "unknown"), h.UptimeSeconds)
	check(doctorHealthy(h.Status), detail)
}

// ─── PROVIDERS ──────────────────────────────────────────────────────────────

// doctorProviders lists each provider's own status via GET /v1/providers.
// This is the whole point of the section: the gateway's overall /health can
// report "healthy" while an individual provider is failing underneath it
// (observed: openai 429, anthropic 404) — that failure is invisible unless
// each provider is checked on its own.
func (r *Registry) doctorProviders(ctx context.Context, check doctorCheck) {
	providers, err := r.gw.Providers(ctx)
	if err != nil {
		check(false, "could not fetch provider list — "+r.gw.FriendlyError(err))
		return
	}
	if len(providers) == 0 {
		check(false, "gateway reports no providers configured")
		return
	}
	for _, p := range providers {
		detail := fmt.Sprintf("%s: %s", p.Name, orDefault(p.Status, "unknown"))
		if p.Error != "" {
			detail += " — " + p.Error
		}
		check(doctorHealthy(p.Status), detail)
	}
}

// ─── CONFIG ─────────────────────────────────────────────────────────────────

// doctorConfig surfaces gateway.url, the active disposition, and the config
// file path. Deliberately does not touch any Protocol config fields — see
// the file-level comment.
func (r *Registry) doctorConfig(check doctorCheck) {
	url := r.cfg.Gateway.URL
	if url == "" {
		check(false, "gateway.url is not set")
	} else {
		check(true, "gateway.url = "+url)
	}

	disp := orDefault(r.cfg.Defaults.Disposition, config.DefaultDisposition)
	if config.IsKnownDisposition(disp, r.cfg.DispositionProfiles) {
		check(true, "disposition = "+disp)
	} else {
		check(false, fmt.Sprintf("disposition %q is not a recognized preset", disp))
	}

	path := config.SettingsPath()
	if _, statErr := os.Stat(path); statErr != nil {
		check(false, path+" not found — running on defaults/env/flags (this is not fatal)")
	} else {
		check(true, "config file = "+path)
	}
}

// ─── PLUGINS/HOOKS ──────────────────────────────────────────────────────────

// doctorPlugins reports how many plugins are loaded and how many hook rules
// they registered, reading the same Registry.plgs field hooksCmd (cmd_system.go)
// sums over. Always in-process data — no network call, so always OK.
func (r *Registry) doctorPlugins(check doctorCheck) {
	hookRules := 0
	for _, p := range r.plgs {
		hookRules += len(p.HookRules)
	}
	check(true, fmt.Sprintf("%d plugin(s) loaded, %d hook rule(s) registered", len(r.plgs), hookRules))
}
