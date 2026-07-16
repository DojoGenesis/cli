package commands

// cmd_doctor.go — /doctor: a read-only, one-screen diagnostic over gateway
// reachability, per-provider status, active config, protocol state, and
// loaded plugins/hooks/harnesses.
// No mutations — this command never writes config, never calls a gateway
// write endpoint, and never changes state.
//
// Self-contained by design: it only depends on symbols that already exist
// elsewhere in this package/tree as of this writing (Registry.cfg/gw/plgs,
// client.Client.Health/Providers/FriendlyError, config.SettingsPath/
// IsKnownDisposition/DefaultDisposition, and the package's existing
// orDefault helper). Protocol config (cfg.Protocol.Enabled/Path, landed on a
// concurrent track) is now referenced by the PROTOCOL section below — the
// "overrides must be visible" rule means a silenced protocol must show up
// as a WARN here, not go invisible.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DojoGenesis/cli/internal/config"
	gcolor "github.com/gookit/color"
)

// ─── /doctor ────────────────────────────────────────────────────────────────

func (r *Registry) doctorCmd() Command {
	return Command{
		Name:  "doctor",
		Usage: "/doctor",
		Short: "Read-only diagnostic: gateway, providers, config, plugins/hooks, protocol, harnesses",
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

			section("PROTOCOL")
			r.doctorProtocol(check)

			section("HARNESSES")
			r.doctorHarnesses(check)

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
// file path. Deliberately does not touch any Protocol config fields — those
// get their own visibility in the PROTOCOL section below (doctorProtocol),
// so a disabled protocol WARNs on its own line instead of hiding inside a
// generic config dump.
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

// ─── PROTOCOL ───────────────────────────────────────────────────────────────

// doctorProtocol surfaces whether the workspace "genius protocol" (see
// internal/protocol) is live for this session, and where its doc resolves
// from. Enabled defaults to true and can be silenced two ways — a
// settings.json "protocol": {"enabled": false}, or DOJO_PROTOCOL_DISABLED at
// runtime — so per the "overrides must be visible" rule, a silenced
// protocol must WARN here rather than disappear into a quiet default.
//
// The source check is a deliberately cheap approximation of
// protocol.LoadOverlay's real precedence (project ./DOJO.md >
// ~/.dojo/DOJO.md > embedded default): an explicit cfg.Protocol.Path always
// wins outright (mirrors protocol.BuildSystemContext), so the cwd-override
// note only applies — and is only checked — when Path is unset. It is a
// bare os.Stat, not a content read, so an empty/whitespace-only DOJO.md
// (which protocol.LoadOverlay treats as absent) is still reported here as
// an override; that's an accepted gap for the cost of staying read-only and
// dependency-free.
func (r *Registry) doctorProtocol(check doctorCheck) {
	if r.cfg.Protocol.Enabled {
		check(true, "protocol.enabled = true")
	} else {
		check(false, "protocol disabled — set protocol.enabled or unset DOJO_PROTOCOL_DISABLED")
	}

	source := r.cfg.Protocol.Path
	if source == "" {
		source = "embedded default"
		if cwd, err := os.Getwd(); err == nil {
			if _, statErr := os.Stat(filepath.Join(cwd, "DOJO.md")); statErr == nil {
				source = "embedded default — overridden by project ./DOJO.md"
			}
		}
	}
	check(true, "source = "+source)
}

// ─── HARNESSES ──────────────────────────────────────────────────────────────

// harnessSuffix is the naming convention KE-fleet harness plugins share
// (kata-harness today; build-dag-harness, convergence-harness, and
// memory-garden-harness once their ADRs ratify and they ship as plugins —
// see the Harness Rails table in the workspace CLAUDE.md). Recognizing any
// loaded plugin by this suffix avoids hardcoding a name list that goes
// stale the moment the next harness ratifies.
const harnessSuffix = "-harness"

// doctorHarnesses checks whether kata-harness — the only ratified harness as
// of this writing (see firstPartyPlugins in internal/bootstrap/bootstrap.go)
// — is installed under the plugins path, and reports how many of the
// currently loaded plugins (r.plgs) are recognized as harnesses by name.
// Absence isn't a hard requirement (kata-harness ships via /init, nothing
// forces it into an existing ~/.dojo), but it must WARN rather than pass
// silently — the same "genius protocol by default" visibility this doctor
// enhancement exists to surface.
func (r *Registry) doctorHarnesses(check doctorCheck) {
	kataPath := filepath.Join(r.cfg.Plugins.Path, "kata-harness")
	if info, err := os.Stat(kataPath); err == nil && info.IsDir() {
		check(true, "kata-harness installed at "+kataPath)
	} else {
		check(false, "kata-harness not installed — run /init")
	}

	recognized := 0
	for _, p := range r.plgs {
		if strings.HasSuffix(p.Name, harnessSuffix) {
			recognized++
		}
	}
	check(true, fmt.Sprintf("%d of %d loaded plugin(s) recognized as harnesses", recognized, len(r.plgs)))
}
