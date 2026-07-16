package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/config"
	gcolor "github.com/gookit/color"
)

func (r *Registry) dispositionCmd() Command {
	return Command{
		Name:    "disposition",
		Aliases: []string{"disp"},
		Usage:   "/disposition [ls|set <name>|show <name>|create <name> <pacing> <depth> <tone> <initiative>]",
		Short:   "Manage ADA disposition presets",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// Show current disposition
				d := orDefault(r.cfg.Defaults.Disposition, "balanced")
				fmt.Println()
				printKV("disposition", d)
				fmt.Println()
				return nil
			}
			switch strings.ToLower(args[0]) {
			case "ls", "list":
				return r.dispositionList()
			case "set":
				if len(args) < 2 {
					return fmt.Errorf("usage: /disposition set <name>")
				}
				return r.dispositionSet(args[1])
			case "show":
				if len(args) < 2 {
					return fmt.Errorf("usage: /disposition show <name>")
				}
				return r.dispositionShow(args[1])
			case "create":
				if len(args) < 6 {
					return fmt.Errorf("usage: /disposition create <name> <pacing> <depth> <tone> <initiative>")
				}
				return r.dispositionCreate(args[1], args[2], args[3], args[4], args[5])
			default:
				return fmt.Errorf("unknown subcommand %q", args[0])
			}
		},
	}
}

func (r *Registry) dispositionList() error {
	filePresets, err := config.LoadDispositionPresets()
	if err != nil {
		return err
	}
	presets := config.MergeConfigProfiles(r.cfg.DispositionProfiles, filePresets)
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Disposition presets (%d)", len(presets)))
	fmt.Println()
	fmt.Println()
	current := orDefault(r.cfg.Defaults.Disposition, "balanced")
	for _, p := range presets {
		marker := "  "
		if p.Name == current {
			marker = gcolor.HEX("#7fb88c").Sprint("-> ")
		}
		fmt.Printf("  %s%-14s %s/%s/%s/%s\n",
			marker,
			gcolor.HEX("#f4a261").Sprint(p.Name),
			gcolor.HEX("#94a3b8").Sprint(p.Pacing),
			gcolor.HEX("#94a3b8").Sprint(p.Depth),
			gcolor.HEX("#94a3b8").Sprint(p.Tone),
			gcolor.HEX("#94a3b8").Sprint(p.Initiative),
		)
	}
	fmt.Println()
	return nil
}

func (r *Registry) dispositionSet(name string) error {
	// Validate against the same merged set /disposition ls shows (builtins +
	// config profiles + file-based presets under ~/.dojo/dispositions/*.json)
	// before saving. Without this, a typo'd or stale name saved fine here and
	// only surfaced on the NEXT startup, when config.Load()'s stricter check
	// (which doesn't know about file-based presets) rejected it and refused
	// to start.
	if !config.IsKnownDisposition(name, r.cfg.DispositionProfiles) {
		return fmt.Errorf("unknown disposition %q — run /disposition ls to see available presets", name)
	}

	r.cfg.Defaults.Disposition = name
	_ = r.cfg.Save()
	activity.Log(activity.CommandRun, "disposition -> "+name)
	fmt.Println()
	gcolor.HEX("#7fb88c").Println("  Disposition updated")
	printKV("disposition", name)
	fmt.Println()
	return nil
}

func (r *Registry) dispositionShow(name string) error {
	filePresets, err := config.LoadDispositionPresets()
	if err != nil {
		return err
	}
	presets := config.MergeConfigProfiles(r.cfg.DispositionProfiles, filePresets)
	for _, p := range presets {
		if p.Name == name {
			fmt.Println()
			printKV("name", p.Name)
			printKV("pacing", p.Pacing)
			printKV("depth", p.Depth)
			printKV("tone", p.Tone)
			printKV("initiative", p.Initiative)
			fmt.Println()
			return nil
		}
	}
	return fmt.Errorf("preset %q not found", name)
}

func (r *Registry) dispositionCreate(name, pacing, depth, tone, initiative string) error {
	p := config.DispositionPreset{
		Name:       name,
		Pacing:     pacing,
		Depth:      depth,
		Tone:       tone,
		Initiative: initiative,
	}
	// Save to file-based presets (backward compat).
	if err := config.SaveDispositionPreset(p); err != nil {
		return err
	}
	// Also persist to settings.json so the profile survives without the file.
	if r.cfg.DispositionProfiles == nil {
		r.cfg.DispositionProfiles = make(map[string]config.DispositionPreset)
	}
	r.cfg.DispositionProfiles[name] = p
	_ = r.cfg.Save()
	activity.Log(activity.CommandRun, "disposition create "+name)
	fmt.Println()
	gcolor.HEX("#7fb88c").Printf("  Preset %q saved\n", name)
	fmt.Println()
	return nil
}
