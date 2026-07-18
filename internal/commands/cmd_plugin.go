package commands

// cmd_plugin.go — /plugin command for managing installed plugins.

import (
	"context"
	"fmt"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/plugins"
	gcolor "github.com/gookit/color"
)

// pluginCmd returns the /plugin command with subcommands:
//
//	/plugin ls            — list installed plugins
//	/plugin install <url> [--yes] [--sha256=<hex>] [--allow-any-source]
//	                       — clone a plugin from a git URL
//	/plugin rm <name>     — remove an installed plugin
//
// install's trailing flags (any order, after the URL):
//
//	--yes / -y          — skip the y/N confirmation (never the integrity gate below)
//	--sha256=<hex>      — pin the installed tree to a known-good digest (plugins.HashPluginTree)
//	--allow-any-source  — disable the source allowlist (Rider C2); a separate, deliberate
//	                      opt-out — never implied by --yes
func (r *Registry) pluginCmd() Command {
	return Command{
		Name:    "plugin",
		Aliases: []string{"plugins"},
		Usage:   "/plugin [ls|install <url> [--yes] [--sha256=<hex>] [--allow-any-source]|rm <name>]",
		Short:   "Manage installed plugins",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 || args[0] == "ls" {
				return r.pluginList(ctx)
			}
			switch args[0] {
			case "install":
				if len(args) < 2 {
					return fmt.Errorf("usage: /plugin install <git-url>")
				}
				var noConfirm, allowAnySource bool
				var sha256Pin string
				for _, a := range args[2:] {
					switch {
					case a == "--yes" || a == "-y":
						noConfirm = true
					case a == "--allow-any-source":
						allowAnySource = true
					case strings.HasPrefix(a, "--sha256="):
						sha256Pin = strings.TrimPrefix(a, "--sha256=")
					}
				}
				return r.pluginInstall(ctx, args[1], noConfirm, allowAnySource, sha256Pin)
			case "rm", "remove", "uninstall":
				if len(args) < 2 {
					return fmt.Errorf("usage: /plugin rm <name>")
				}
				return r.pluginRemove(ctx, args[1])
			default:
				return fmt.Errorf("unknown subcommand %q — use ls, install, or rm", args[0])
			}
		},
	}
}

// pluginList prints all currently loaded plugins.
func (r *Registry) pluginList(ctx context.Context) error {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Installed plugins"))
	fmt.Println()
	fmt.Println()

	if len(r.plgs) == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No plugins installed."))
		fmt.Println()
		return nil
	}

	for _, p := range r.plgs {
		name := gcolor.HEX("#f4a261").Sprint(p.Name)
		ver := gcolor.HEX("#94a3b8").Sprint(orDefault(p.Version, "?"))
		fmt.Printf("  %s %s\n", name, ver)

		printKV("    path", p.Path)
		printKV("    hooks", fmt.Sprintf("%d rules", len(p.HookRules)))
		printKV("    skills", fmt.Sprintf("%d", p.SkillCount))
		fmt.Println()
	}

	return nil
}

// pluginInstall clones a plugin from a git URL and rescans.
// A single URL may yield multiple plugins (monorepo case).
//
// The "plugin.install" permission gate is the single confirmation for this
// command: it replaced the ad-hoc y/N prompt inside plugins.InstallConfirmed
// (which is now always called with noConfirm=true — one prompt, never two).
// noConfirm here (--yes / -y) pre-answers the gate's Confirm case, matching
// the flag's historical "skip the interactive prompt" contract; it does not
// override an allowlist-mode Deny. InstallConfirmed itself is retained (it
// lives in internal/plugins and still prints the security warning banner),
// but its prompt path is dead from this call site.
//
// allowAnySource and sha256Pin surface plugins.InstallPolicy's two integrity
// knobs (Rider C2): allowAnySource disables the source allowlist entirely —
// a separate, explicitly-named flag, never implied by noConfirm/--yes — and
// sha256Pin, when non-empty, pins the installed tree to a known-good digest
// (plugins.HashPluginTree), rolling back the install on a mismatch. Neither
// knob is needed for the default path: InstallConfirmed (and therefore this
// call, when both are zero-valued) already applies the house allowlist via
// plugins.DefaultInstallPolicy.
func (r *Registry) pluginInstall(ctx context.Context, gitURL string, noConfirm, allowAnySource bool, sha256Pin string) error {
	if err := r.headlessRefuse("install plugin"); err != nil {
		return err
	}
	if !r.permissionGate("plugin.install",
		fmt.Sprintf("clone %s into %s and load its hooks/skills", gitURL, r.cfg.Plugins.Path),
		noConfirm) {
		return nil
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  Cloning %s ...", gitURL))

	policy := plugins.DefaultInstallPolicy()
	policy.AllowAnySource = allowAnySource
	policy.ExpectedSHA256 = sha256Pin

	results, err := plugins.InstallConfirmedWithPolicy(gitURL, r.cfg.Plugins.Path, true, policy)
	if err != nil {
		return fmt.Errorf("plugin install: %w", err)
	}

	for _, res := range results {
		activity.Log(activity.CommandRun, fmt.Sprintf("plugin installed from %s → %s", gitURL, res.Path))
	}

	// Rescan plugins to pick up the new ones.
	plgs, scanErr := plugins.Scan(r.cfg.Plugins.Path)
	if scanErr == nil {
		r.plgs = plgs
	}

	fmt.Println()
	if len(results) == 1 {
		gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprintf("  Plugin installed at %s", results[0].Path))
	} else {
		gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprintf("  %d plugins installed:", len(results)))
		fmt.Println()
		for _, res := range results {
			fmt.Printf("    %s  %s\n",
				gcolor.HEX("#f4a261").Sprint(res.Name),
				gcolor.HEX("#94a3b8").Sprint(res.Path),
			)
		}
	}
	fmt.Println()
	fmt.Println()
	return nil
}

// pluginRemove removes an installed plugin by name and rescans. Gated as
// "plugin.rm" — removal deletes the plugin directory from disk, and this
// command historically had no confirmation at all.
func (r *Registry) pluginRemove(ctx context.Context, name string) error {
	if !r.permissionGate("plugin.rm",
		fmt.Sprintf("delete plugin %q from %s", name, r.cfg.Plugins.Path),
		false) {
		return nil
	}

	if err := plugins.Uninstall(name, r.cfg.Plugins.Path); err != nil {
		return fmt.Errorf("plugin remove: %w", err)
	}

	activity.Log(activity.CommandRun, fmt.Sprintf("plugin removed: %s", name))

	// Rescan plugins.
	plgs, scanErr := plugins.Scan(r.cfg.Plugins.Path)
	if scanErr == nil {
		r.plgs = plgs
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprintf("  Plugin %q removed", name))
	fmt.Println()
	fmt.Println()
	return nil
}
