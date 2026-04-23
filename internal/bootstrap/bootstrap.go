package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
	"github.com/DojoGenesis/cli/internal/ioutilx"
	"github.com/DojoGenesis/cli/internal/state"
	gcolor "github.com/gookit/color"
)

// Options configures the bootstrap run.
type Options struct {
	GatewayURL    string
	PluginsSource string // path to CoworkPluginsByDojoGenesis/plugins/
	Force         bool
	SkipSeeds     bool
}

// Result summarises what was created or skipped.
type Result struct {
	SettingsCreated     bool
	PluginsCopied       int
	PluginsSkipped      int
	DispositionsWritten int
	MCPConfigWritten    bool
	SeedsPlanted        int
	SeedsSkipped        int
	Errors              []string
}

// Run executes the full bootstrap sequence and prints a summary.
func Run(ctx context.Context, opts Options, gw *client.Client, w io.Writer) (*Result, error) {
	dojoDir := config.DojoDir()
	os.MkdirAll(dojoDir, 0700)
	r := &Result{}

	// 1. Settings
	created, err := writeSettings(dojoDir, opts)
	if err != nil {
		r.Errors = append(r.Errors, "settings: "+err.Error())
	}
	r.SettingsCreated = created

	// 2. Plugins
	copied, skipped, errs := copyPlugins(dojoDir, opts)
	r.PluginsCopied = copied
	r.PluginsSkipped = skipped
	r.Errors = append(r.Errors, errs...)

	// 3. Dispositions
	written, err := writeDispositions(dojoDir, opts.Force)
	if err != nil {
		r.Errors = append(r.Errors, "dispositions: "+err.Error())
	}
	r.DispositionsWritten = written

	// 4. MCP Config
	mcpWritten, err := writeMCPConfig(dojoDir, opts.Force)
	if err != nil {
		r.Errors = append(r.Errors, "mcp: "+err.Error())
	}
	r.MCPConfigWritten = mcpWritten

	// 5. Seeds (if gateway available)
	if !opts.SkipSeeds && gw != nil {
		planted, seedSkipped, seedErrs := plantSeeds(ctx, gw)
		r.SeedsPlanted = planted
		r.SeedsSkipped = seedSkipped
		r.Errors = append(r.Errors, seedErrs...)
	} else {
		r.SeedsSkipped = len(starterSeeds)
	}

	// 6. Mark setup complete
	st, _ := state.Load()
	st.SetupComplete = true
	_ = st.Save()

	// 7. Print summary
	printSummary(w, r)
	return r, nil
}

// writeSettings creates settings.json with the gateway URL from opts.
// Skips if the file already exists and Force is false.
func writeSettings(dojoDir string, opts Options) (bool, error) {
	path := filepath.Join(dojoDir, "settings.json")
	if !opts.Force {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		}
	}

	gwURL := opts.GatewayURL
	if gwURL == "" {
		gwURL = config.DefaultGatewayURL
	}

	cfg := map[string]any{
		"gateway": map[string]any{
			"url":     gwURL,
			"timeout": "60s",
		},
		"plugins": map[string]any{
			"path": filepath.Join(dojoDir, "plugins"),
		},
		"defaults": map[string]any{
			"disposition": "balanced",
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return true, ioutilx.AtomicWriteFile(path, data, 0600)
}

// firstPartyPlugins is the canonical list of first-party Dojo plugins.
var firstPartyPlugins = []string{
	"agent-orchestration",
	"continuous-learning",
	"pretext-pdf",
	"skill-forge",
	"specification-driven-development",
	"strategic-thinking",
	"system-health",
	"wisdom-garden",
}

// copyPlugins copies first-party plugin directories into ~/.dojo/plugins/.
func copyPlugins(dojoDir string, opts Options) (copied, skipped int, errs []string) {
	source := opts.PluginsSource
	if source == "" {
		if v := os.Getenv("DOJO_PLUGINS_SOURCE"); v != "" {
			source = v
		} else {
			home, _ := os.UserHomeDir()
			source = filepath.Join(home, "ZenflowProjects", "CoworkPluginsByDojoGenesis", "plugins")
		}
	}

	destDir := filepath.Join(dojoDir, "plugins")
	os.MkdirAll(destDir, 0755)

	for _, name := range firstPartyPlugins {
		src := filepath.Join(source, name)
		dst := filepath.Join(destDir, name)

		if !opts.Force {
			if _, err := os.Stat(dst); err == nil {
				skipped++
				continue
			}
		}

		if _, err := os.Stat(src); os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("plugin source not found: %s", name))
			skipped++
			continue
		}

		// Remove existing if force
		if opts.Force {
			os.RemoveAll(dst)
		}

		if err := copyDir(src, dst); err != nil {
			errs = append(errs, fmt.Sprintf("copy %s: %s", name, err))
			skipped++
			continue
		}
		copied++
	}
	return
}

// copyDir recursively copies a directory, skipping .git, .DS_Store, and __pycache__.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if name == ".git" || name == ".DS_Store" || name == "__pycache__" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// dispositionPresets are the four ADA disposition YAML files.
var dispositionPresets = map[string]string{
	"focused.yaml": `name: focused
description: "Precise, minimal-overhead mode. Direct answers, no tangents."
pacing: fast
depth: surface
tone: concise
initiative: low
`,
	"balanced.yaml": `name: balanced
description: "Default mode. Thoughtful responses with appropriate depth."
pacing: moderate
depth: standard
tone: conversational
initiative: moderate
`,
	"exploratory.yaml": `name: exploratory
description: "Wide-ranging mode. Explores tangents, offers alternatives, asks questions."
pacing: relaxed
depth: deep
tone: curious
initiative: high
`,
	"deliberate.yaml": `name: deliberate
description: "Methodical, step-by-step mode. Maximum reasoning depth, explicit tradeoffs."
pacing: slow
depth: exhaustive
tone: analytical
initiative: moderate
`,
}

// writeDispositions writes the four YAML preset files to ~/.dojo/dispositions/.
func writeDispositions(dojoDir string, force bool) (int, error) {
	dir := filepath.Join(dojoDir, "dispositions")
	os.MkdirAll(dir, 0755)
	written := 0
	for name, content := range dispositionPresets {
		path := filepath.Join(dir, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// mcpConfigJSON is the default MCP server configuration.
var mcpConfigJSON = `{
  "version": "1.0",
  "servers": [
    {"id": "pretext_pdf", "display_name": "Pretext PDF Export", "namespace": "pretext", "transport": "stdio"},
    {"id": "zen_sci_latex", "display_name": "ZenSci LaTeX", "namespace": "zen_sci_latex", "transport": "stdio"},
    {"id": "zen_sci_blog", "display_name": "ZenSci Blog", "namespace": "zen_sci_blog", "transport": "stdio"},
    {"id": "zen_sci_slides", "display_name": "ZenSci Slides", "namespace": "zen_sci_slides", "transport": "stdio"},
    {"id": "zen_sci_newsletter", "display_name": "ZenSci Newsletter", "namespace": "zen_sci_newsletter", "transport": "stdio"},
    {"id": "zen_sci_grant", "display_name": "ZenSci Grant Proposals", "namespace": "zen_sci_grant", "transport": "stdio"},
    {"id": "zen_sci_paper", "display_name": "ZenSci Academic Paper", "namespace": "zen_sci_paper", "transport": "stdio"}
  ]
}
`

// writeMCPConfig writes ~/.dojo/mcp.json. Skips if exists and Force is false.
func writeMCPConfig(dojoDir string, force bool) (bool, error) {
	path := filepath.Join(dojoDir, "mcp.json")
	if !force {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		}
	}
	return true, ioutilx.AtomicWriteFile(path, []byte(mcpConfigJSON), 0644)
}

// starterSeeds are the five default seeds planted on first bootstrap.
var starterSeeds = []client.CreateSeedRequest{
	{
		Name:        "context_iceberg",
		Description: "4-tier context management: hot/warm/cold/pruned",
		Content:     "Manage context in four tiers. Hot context is the current task. Warm context is the session. Cold context is persisted memory. Pruned context is archived. Move information between tiers deliberately — what's hot now becomes warm after the task, then cold after the session. Never let hot context bloat beyond what the current step needs.",
	},
	{
		Name:        "safety_switch",
		Description: "No autopilot — always verify before irreversible actions",
		Content:     "Before any irreversible action (delete, publish, push, deploy, send), pause and confirm. The cost of asking is seconds; the cost of a mistake is hours or worse. This applies to agents too — never let an automated workflow skip the safety check. Build confirmation gates into every pipeline that touches shared state.",
	},
	{
		Name:        "harness_trace",
		Description: "Inspect agent reasoning through structured trace logs",
		Content:     "Every agent action should produce a trace: what was the input, what tools were considered, which was chosen, what was the output, and what was decided next. When debugging agent behavior, read the trace before changing the prompt. Most issues are visible in the trace as wrong tool selection or missing context, not wrong instructions.",
	},
	{
		Name:        "collaborative_calibration",
		Description: "Norms for human-agent partnership",
		Content:     "The human sets direction; the agent executes with judgment. When unsure, the agent asks rather than guesses. When the agent spots a better approach, it suggests rather than substitutes. Both sides maintain shared context through explicit handoffs. Neither autopilots — both stay engaged.",
	},
	{
		Name:        "sanctuary_architecture",
		Description: "Design calm digital spaces that respect attention",
		Content:     "A workspace should feel like a sanctuary — calm, focused, intentional. Minimize notifications. Default to quiet. Surface information when asked, not when available. Use color to guide attention, not to demand it. The sunset palette exists for this reason: warm, grounding, never urgent.",
	},
}

// plantSeeds plants the starter seeds, skipping any that already exist by name.
func plantSeeds(ctx context.Context, gw *client.Client) (planted, skipped int, errs []string) {
	existing, err := gw.Seeds(ctx)
	if err != nil {
		return 0, len(starterSeeds), []string{"gateway unreachable: " + err.Error()}
	}
	existingNames := make(map[string]bool)
	for _, s := range existing {
		existingNames[strings.ToLower(s.Name)] = true
	}

	for _, seed := range starterSeeds {
		if existingNames[strings.ToLower(seed.Name)] {
			skipped++
			continue
		}
		if _, err := gw.CreateSeed(ctx, seed); err != nil {
			errs = append(errs, fmt.Sprintf("seed %s: %s", seed.Name, err))
			skipped++
			continue
		}
		planted++
	}
	return
}

// printSummary writes a formatted bootstrap summary to w.
func printSummary(w io.Writer, r *Result) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, gcolor.HEX("#e8b04a").Sprint("  Dojo workspace initialized"))
	fmt.Fprintln(w)

	check := gcolor.HEX("#7fb88c").Sprint("✓")
	skip := gcolor.HEX("#94a3b8").Sprint("–")

	if r.SettingsCreated {
		fmt.Fprintf(w, "  %s  settings.json created\n", check)
	} else {
		fmt.Fprintf(w, "  %s  settings.json (already exists)\n", skip)
	}

	if r.PluginsCopied > 0 {
		fmt.Fprintf(w, "  %s  %d plugins installed\n", check, r.PluginsCopied)
	}
	if r.PluginsSkipped > 0 {
		fmt.Fprintf(w, "  %s  %d plugins skipped\n", skip, r.PluginsSkipped)
	}

	if r.DispositionsWritten > 0 {
		fmt.Fprintf(w, "  %s  %d disposition presets written\n", check, r.DispositionsWritten)
	} else {
		fmt.Fprintf(w, "  %s  dispositions (already exist)\n", skip)
	}

	if r.MCPConfigWritten {
		fmt.Fprintf(w, "  %s  mcp.json created (7 servers)\n", check)
	} else {
		fmt.Fprintf(w, "  %s  mcp.json (already exists)\n", skip)
	}

	if r.SeedsPlanted > 0 {
		fmt.Fprintf(w, "  %s  %d starter seeds planted\n", check, r.SeedsPlanted)
	}
	if r.SeedsSkipped > 0 {
		fmt.Fprintf(w, "  %s  %d seeds skipped\n", skip, r.SeedsSkipped)
	}

	if len(r.Errors) > 0 {
		fmt.Fprintln(w)
		for _, e := range r.Errors {
			fmt.Fprintf(w, "  %s  %s\n", gcolor.HEX("#e8b04a").Sprint("!"), gcolor.HEX("#94a3b8").Sprint(e))
		}
	}

	fmt.Fprintln(w)
}
