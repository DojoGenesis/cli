package commands

// cmd_craft.go — /craft command group: DojoCraft practitioner workbench.
// Subcommands: adr, scout, claude-md, memory, seed, view, scaffold, converge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/client"
	gcolor "github.com/gookit/color"
)

// ─── /craft ──────────────────────────────────────────────────────────────────

func (r *Registry) craftCmd() Command {
	return Command{
		Name:    "craft",
		Aliases: []string{"dojoCraft"},
		Usage:   "/craft [adr|scout|claude-md|memory|seed|view|scaffold|converge]",
		Short:   "DojoCraft practitioner workbench — strategic thinking, codebase intelligence, memory curation",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				craftHelp()
				return nil
			}
			sub := strings.ToLower(args[0])
			rest := args[1:]

			switch sub {
			case "adr":
				return r.craftADR(ctx, rest)
			case "scout":
				return r.craftScout(ctx, rest)
			case "claude-md":
				return r.craftClaudeMD(ctx, rest)
			case "memory":
				return r.craftMemory(ctx, rest)
			case "seed":
				return r.craftSeed(ctx, rest)
			case "view":
				return craftView(rest)
			case "scaffold":
				return craftScaffold(rest)
			case "converge":
				return r.craftConverge(ctx)
			default:
				return fmt.Errorf("unknown subcommand %q — try: adr, scout, claude-md, memory, seed, view, scaffold, converge", sub)
			}
		},
	}
}

func craftHelp() {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  /craft — DojoCraft Practitioner Workbench"))
	fmt.Println()
	fmt.Println()
	subcommands := []struct{ name, desc string }{
		{"adr <title>", "write an Architecture Decision Record via Gateway"},
		{"scout <tension>", "tension → routes → synthesis → decision pipeline"},
		{"claude-md [--fix]", "analyse all CLAUDE.md files; --fix applies suggestions"},
		{"memory <ls|add|rm|prune|search>", "manage Gateway memory entries"},
		{"seed <ls|plant|harvest|search|elevate>", "manage memory garden seeds"},
		{"view [path]", "codebase overview — tree, entry points, test coverage"},
		{"scaffold <template>", "create a project from a template directory layout"},
		{"converge", "git + memory health report: RED/YELLOW/GREEN"},
	}
	for _, s := range subcommands {
		fmt.Printf("  %s  %s\n",
			gcolor.HEX("#f4a261").Sprintf("%-40s", s.name),
			gcolor.HEX("#94a3b8").Sprint(s.desc),
		)
	}
	fmt.Println()
}

// ─── /craft adr ──────────────────────────────────────────────────────────────

func (r *Registry) craftADR(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /craft adr <title>")
	}
	title := strings.Join(args, " ")

	// Find next ADR number by scanning decisions/ directory.
	nextNum := craftNextADRNumber()

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  ADR %03d: %s\n\n", nextNum, title))

	systemPrompt := fmt.Sprintf(`You are writing Architecture Decision Records (ADRs) for a software project.
Output a well-structured ADR in Markdown with the following sections:
# ADR-%03d: %s

## Status
Proposed

## Context
[Describe the situation and forces at play]

## Decision
[State the decision clearly in one sentence]

## Consequences
### Positive
- [list]

### Negative / Trade-offs
- [list]

## Alternatives Considered
- [brief alternatives with reason rejected]

Be concise and precise. Focus on the decision, not the technology overview.`, nextNum, title)

	req := client.ChatRequest{
		Message:   fmt.Sprintf("Write an ADR for: %s", title),
		SessionID: fmt.Sprintf("craft-adr-%d", time.Now().UnixNano()),
		Stream:    true,
	}

	return r.craftStream(ctx, systemPrompt, req)
}

// craftNextADRNumber scans decisions/ in CWD for the highest ADR number.
func craftNextADRNumber() int {
	cwd, err := os.Getwd()
	if err != nil {
		return 1
	}
	dir := filepath.Join(cwd, "decisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// No decisions/ dir — start at 1.
		return 1
	}
	highest := 0
	for _, e := range entries {
		name := e.Name()
		// Match patterns like 001-*, 012-*, etc.
		if len(name) >= 3 {
			if n, err := strconv.Atoi(name[:3]); err == nil && n > highest {
				highest = n
			}
		}
	}
	return highest + 1
}

// ─── /craft scout ────────────────────────────────────────────────────────────

func (r *Registry) craftScout(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /craft scout <tension description>")
	}
	tension := strings.Join(args, " ")

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Scout Analysis"))
	fmt.Println()
	gcolor.HEX("#94a3b8").Printf("  tension: %s\n\n", tension)

	systemPrompt := `You are a strategic scout for a solo operator building developer tooling.
Your role is to take a stated tension and produce a structured analysis in 4 sections:

## Tension
Restate the tension precisely.

## Routes
List 3-5 distinct strategic routes (not variations of the same approach). Each route is a noun phrase.

## Synthesis
2-3 sentences on which route resolves the most constraints with the least complexity.

## Decision
A single recommended decision, stated as an imperative sentence. Include the primary trade-off accepted.

Be direct. Prefer action over exploration. Assume the operator has limited attention.`

	req := client.ChatRequest{
		Message:   tension,
		SessionID: fmt.Sprintf("craft-scout-%d", time.Now().UnixNano()),
		Stream:    true,
	}

	return r.craftStream(ctx, systemPrompt, req)
}

// ─── /craft claude-md ────────────────────────────────────────────────────────

func (r *Registry) craftClaudeMD(ctx context.Context, args []string) error {
	fix := false
	for _, a := range args {
		if a == "--fix" {
			fix = true
		}
	}

	// Collect all CLAUDE.md files from CWD downward (max depth 3).
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get cwd: %w", err)
	}

	var files []string
	_ = filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Skip hidden dirs and deep nesting.
			rel, relErr := filepath.Rel(cwd, path)
			if relErr != nil {
				return filepath.SkipDir
			}
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth > 3 || (d.Name() != "." && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "CLAUDE.md" {
			files = append(files, path)
		}
		return nil
	})

	if len(files) == 0 {
		fmt.Println()
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No CLAUDE.md files found in this directory tree."))
		fmt.Println()
		return nil
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  CLAUDE.md Analysis (%d files)\n\n", len(files)))

	var combined strings.Builder
	for _, f := range files {
		rel, _ := filepath.Rel(cwd, f)
		gcolor.HEX("#f4a261").Printf("  %s\n", rel)
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fmt.Fprintf(&combined, "\n## File: %s\n\n", rel)
		combined.Write(content)
		combined.WriteString("\n")
	}
	fmt.Println()

	systemPrompt := `You are auditing CLAUDE.md project instruction files.
Analyse the provided files and produce a structured report:

## Coverage Gaps
Instructions that are missing but would prevent recurring errors.

## Contradictions
Any rules that conflict with each other across files.

## Stale Rules
Rules that reference outdated patterns or approaches.

## Improvement Suggestions
Specific rewrites for the 3 highest-value improvements. Show old → new.

If --fix mode: emit corrected versions of each file after the report, fenced by triple backticks and the file path.
Be specific. Do not suggest generic "add more documentation".`

	message := combined.String()
	if fix {
		message += "\n\n---\nPlease also output fixed versions of each file."
	}

	req := client.ChatRequest{
		Message:   message,
		SessionID: fmt.Sprintf("craft-claudemd-%d", time.Now().UnixNano()),
		Stream:    true,
	}

	return r.craftStream(ctx, systemPrompt, req)
}

// ─── /craft memory ───────────────────────────────────────────────────────────

func (r *Registry) craftMemory(ctx context.Context, args []string) error {
	op := "ls"
	if len(args) > 0 {
		op = strings.ToLower(args[0])
	}

	switch op {
	case "ls", "list":
		memories, err := r.gw.Memories(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch memories: %w", err)
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Memory (%d)\n\n", len(memories)))
		if len(memories) == 0 {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No memories stored. Use /craft memory add <text> to add one."))
			fmt.Println()
			return nil
		}
		for _, m := range memories {
			fmt.Printf("  %s  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-36s", m.ID),
				gcolor.HEX("#64748b").Sprintf("%-12s", truncate(m.Type, 12)),
				gcolor.White.Sprint(truncate(m.Content, 70)),
			)
		}
		fmt.Println()

	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: /craft memory add <text>")
		}
		text := strings.Join(args[1:], " ")
		mem, err := r.gw.StoreMemory(ctx, client.StoreMemoryRequest{Content: text})
		if err != nil {
			return fmt.Errorf("could not add memory: %w", err)
		}
		fmt.Println()
		fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory stored"))
		if mem != nil {
			printKV("id", mem.ID)
		}
		fmt.Println()

	case "rm", "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: /craft memory rm <id>")
		}
		id := args[1]
		if err := r.gw.DeleteMemory(ctx, id); err != nil {
			return fmt.Errorf("could not delete memory: %w", err)
		}
		fmt.Println()
		fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory deleted"))
		fmt.Println()

	case "prune":
		// List first, then report how many exist — pruning logic deferred to Gateway.
		memories, err := r.gw.Memories(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch memories: %w", err)
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Memory Prune Preview"))
		fmt.Println()
		fmt.Println()
		gcolor.HEX("#94a3b8").Printf("  %d memories total\n", len(memories))
		fmt.Println(gcolor.HEX("#457b9d").Sprint("  hint: use /craft memory rm <id> to delete individual entries"))
		fmt.Println()

	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: /craft memory search <query>")
		}
		query := strings.Join(args[1:], " ")
		results, err := r.gw.SearchMemories(ctx, query)
		if err != nil {
			return fmt.Errorf("could not search memories: %w", err)
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Memory Search: %q (%d results)\n\n", query, len(results)))
		if len(results) == 0 {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No results found."))
			fmt.Println()
			return nil
		}
		for _, m := range results {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#94a3b8").Sprintf("%-36s", m.ID),
				gcolor.White.Sprint(truncate(m.Content, 80)),
			)
		}
		fmt.Println()

	default:
		return fmt.Errorf("unknown memory op %q — use: ls, add, rm, prune, search", op)
	}
	return nil
}

// ─── /craft seed ─────────────────────────────────────────────────────────────

func (r *Registry) craftSeed(ctx context.Context, args []string) error {
	op := "ls"
	if len(args) > 0 {
		op = strings.ToLower(args[0])
	}

	switch op {
	case "ls", "list", "harvest":
		seeds, err := r.gw.Seeds(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch seeds: %w", err)
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Seeds (%d)\n\n", len(seeds)))
		if len(seeds) == 0 {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Garden is empty. Use /craft seed plant <text> to add a seed."))
			fmt.Println()
			return nil
		}
		for _, s := range seeds {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-44s", s.Name),
				gcolor.HEX("#94a3b8").Sprint(truncate(s.Content, 50)),
			)
		}
		fmt.Println()

	case "plant":
		if len(args) < 2 {
			return fmt.Errorf("usage: /craft seed plant <text>")
		}
		text := strings.Join(args[1:], " ")
		seedTitle := "Planted " + time.Now().Format("2006-01-02")
		seed, err := r.gw.CreateSeed(ctx, client.CreateSeedRequest{
			Name:    seedTitle,
			Content: text,
		})
		if err != nil {
			return fmt.Errorf("could not plant seed: %w", err)
		}
		fmt.Println()
		fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Seed planted"))
		if seed != nil {
			printKV("id", seed.ID)
			printKV("name", seed.Name)
		}
		fmt.Println()

	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: /craft seed search <query>")
		}
		query := strings.Join(args[1:], " ")
		// Search seeds by listing all and filtering locally.
		// The Gateway does not have a dedicated seed search endpoint.
		allSeeds, err := r.gw.Seeds(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch seeds: %w", err)
		}
		queryLower := strings.ToLower(query)
		var matched []client.Seed
		for _, s := range allSeeds {
			if strings.Contains(strings.ToLower(s.Name), queryLower) ||
				strings.Contains(strings.ToLower(s.Content), queryLower) {
				matched = append(matched, s)
			}
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Seed Search: %q (%d of %d)\n\n", query, len(matched), len(allSeeds)))
		if len(matched) == 0 {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No matching seeds found."))
			fmt.Println()
			return nil
		}
		for _, s := range matched {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-44s", s.Name),
				gcolor.HEX("#94a3b8").Sprint(truncate(s.Content, 50)),
			)
		}
		fmt.Println()

	case "elevate":
		// Elevate seeds to memory: list seeds, display guidance.
		seeds, err := r.gw.Seeds(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch seeds: %w", err)
		}
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Seed Elevation"))
		fmt.Println()
		fmt.Println()
		gcolor.HEX("#94a3b8").Printf("  %d seeds in garden\n\n", len(seeds))
		if len(seeds) == 0 {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No seeds to elevate."))
			fmt.Println()
			return nil
		}
		fmt.Println(gcolor.HEX("#457b9d").Sprint("  To elevate a seed to durable memory:"))
		fmt.Println(gcolor.HEX("#457b9d").Sprint("    /craft memory add <text from seed>"))
		fmt.Println()
		for _, s := range seeds {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-44s", s.Name),
				gcolor.HEX("#94a3b8").Sprint(truncate(s.Content, 50)),
			)
		}
		fmt.Println()

	default:
		return fmt.Errorf("unknown seed op %q — use: ls, plant, harvest, search, elevate", op)
	}
	return nil
}

// ─── /craft view ─────────────────────────────────────────────────────────────

func craftView(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	// Resolve to absolute path.
	if !filepath.IsAbs(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get cwd: %w", err)
		}
		dir = filepath.Join(cwd, dir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("path not found: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Codebase View: %s\n\n", dir))

	// ── Top-level structure ──────────────────────────────────────────────────
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("could not read directory: %w", err)
	}

	gcolor.HEX("#f4a261").Println("  Top-level entries:")
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		icon := "  "
		if e.IsDir() {
			icon = "  "
		}
		fmt.Printf("    %s%s\n", icon, gcolor.White.Sprint(e.Name()))
	}
	fmt.Println()

	// ── Go module info ───────────────────────────────────────────────────────
	goMod := filepath.Join(dir, "go.mod")
	if content, err := os.ReadFile(goMod); err == nil {
		lines := strings.SplitN(string(content), "\n", 5)
		gcolor.HEX("#f4a261").Println("  Go module:")
		for _, l := range lines {
			if l != "" {
				fmt.Printf("    %s\n", gcolor.HEX("#94a3b8").Sprint(l))
			}
		}
		fmt.Println()
	}

	// ── Entry points: scan for main packages ─────────────────────────────────
	var mainDirs []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth > 4 || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			content, readErr := os.ReadFile(path)
			if readErr == nil && strings.Contains(string(content), "package main") {
				relDir, _ := filepath.Rel(dir, filepath.Dir(path))
				mainDirs = append(mainDirs, relDir)
			}
		}
		return nil
	})
	// Deduplicate.
	seen := map[string]bool{}
	var uniqueDirs []string
	for _, d := range mainDirs {
		if !seen[d] {
			seen[d] = true
			uniqueDirs = append(uniqueDirs, d)
		}
	}
	if len(uniqueDirs) > 0 {
		gcolor.HEX("#f4a261").Println("  Entry points (package main):")
		for _, d := range uniqueDirs {
			fmt.Printf("    %s\n", gcolor.White.Sprint(d))
		}
		fmt.Println()
	}

	// ── Test coverage indicator ───────────────────────────────────────────────
	var testFiles, srcFiles int
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			testFiles++
		} else if strings.HasSuffix(d.Name(), ".go") {
			srcFiles++
		}
		return nil
	})

	gcolor.HEX("#f4a261").Println("  Go files:")
	fmt.Printf("    %s  %s\n",
		gcolor.White.Sprintf("%-6d", srcFiles),
		gcolor.HEX("#94a3b8").Sprint("source files"),
	)
	fmt.Printf("    %s  %s\n",
		gcolor.White.Sprintf("%-6d", testFiles),
		gcolor.HEX("#94a3b8").Sprint("test files"),
	)
	fmt.Println()

	// ── Git status summary ────────────────────────────────────────────────────
	gitCmd := exec.Command("git", "-C", dir, "status", "--short")
	gitOut, gitErr := gitCmd.Output()
	if gitErr == nil {
		lines := strings.Split(strings.TrimSpace(string(gitOut)), "\n")
		dirty := 0
		for _, l := range lines {
			if l != "" {
				dirty++
			}
		}
		gcolor.HEX("#f4a261").Println("  Git status:")
		if dirty == 0 {
			fmt.Println("    " + gcolor.HEX("#7fb88c").Sprint("clean"))
		} else {
			fmt.Printf("    %s dirty files\n", gcolor.HEX("#e8b04a").Sprintf("%d", dirty))
		}
		fmt.Println()
	}

	return nil
}

// ─── /craft scaffold ─────────────────────────────────────────────────────────

func craftScaffold(args []string) error {
	if len(args) == 0 {
		fmt.Println()
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Available scaffold templates:"))
		fmt.Println()
		fmt.Println()
		templates := []struct{ name, desc string }{
			{"go-service", "Go HTTP service: cmd/, internal/, go.mod, Makefile"},
			{"fullstack", "Go backend + Svelte frontend: server/, frontend/, Makefile"},
			{"orchestration", "Agentic orchestration: decisions/, docs/, commissions/, CLAUDE.md"},
			{"plugin", "Dojo plugin: plugin.json, .mcp.json, commands/, skills/"},
			{"minimal", "Minimal starter: src/, go.mod"},
		}
		for _, t := range templates {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-16s", t.name),
				gcolor.HEX("#94a3b8").Sprint(t.desc),
			)
		}
		fmt.Println()
		fmt.Println(gcolor.HEX("#457b9d").Sprint("  usage: /craft scaffold <template>"))
		fmt.Println()
		return nil
	}

	template := strings.ToLower(args[0])
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get cwd: %w", err)
	}

	var dirs []string
	var files map[string]string

	switch template {
	case "go-service":
		dirs = []string{"cmd/server", "internal/handlers", "internal/config"}
		files = map[string]string{
			"go.mod":  "module github.com/example/service\n\ngo 1.24.0\n",
			"Makefile": "build:\n\tgo build ./...\n\ntest:\n\tgo test ./...\n\n.PHONY: build test\n",
			"cmd/server/main.go": "package main\n\nfunc main() {\n}\n",
		}
	case "fullstack":
		dirs = []string{"server/cmd", "server/internal", "frontend/src"}
		files = map[string]string{
			"Makefile": "build:\n\tgo build ./server/...\n\n.PHONY: build\n",
			"server/cmd/main.go": "package main\n\nfunc main() {\n}\n",
		}
	case "orchestration":
		dirs = []string{"decisions", "docs", "commissions"}
		files = map[string]string{
			"CLAUDE.md": "# Project\n\n## Overview\n\n## Active Decisions\n\n## Open Items\n",
		}
	case "plugin":
		dirs = []string{"commands", "skills/example"}
		files = map[string]string{
			"plugin.json": "{\n  \"name\": \"my-plugin\",\n  \"version\": \"0.1.0\",\n  \"description\": \"\"\n}\n",
			".mcp.json":   "{\n  \"mcpServers\": {}\n}\n",
			"skills/example/SKILL.md": "---\nname: example\nversion: 0.1.0\n---\n\n# Example Skill\n",
		}
	case "minimal":
		dirs = []string{"src"}
		files = map[string]string{
			"go.mod": "module github.com/example/minimal\n\ngo 1.24.0\n",
		}
	default:
		return fmt.Errorf("unknown template %q — try: go-service, fullstack, orchestration, plugin, minimal", template)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Scaffolding: %s\n\n", template))

	for _, d := range dirs {
		path := filepath.Join(cwd, d)
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("could not create %s: %w", d, err)
		}
		fmt.Printf("  %s  %s\n", gcolor.HEX("#7fb88c").Sprint("mkdir"), gcolor.White.Sprint(d))
	}

	for name, content := range files {
		path := filepath.Join(cwd, name)
		// Don't overwrite existing files.
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  %s  %s\n", gcolor.HEX("#e8b04a").Sprint("skip "), gcolor.HEX("#94a3b8").Sprint(name+" (exists)"))
			continue
		}
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("could not create dir for %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("could not write %s: %w", name, err)
		}
		fmt.Printf("  %s  %s\n", gcolor.HEX("#7fb88c").Sprint("write"), gcolor.White.Sprint(name))
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Scaffold complete."))
	fmt.Println()
	return nil
}

// ─── /craft converge ─────────────────────────────────────────────────────────

func (r *Registry) craftConverge(ctx context.Context) error {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Convergence Report"))
	fmt.Println()
	fmt.Println()

	// ── Git dirty file count ──────────────────────────────────────────────────
	cwd, _ := os.Getwd()
	gitStatus := exec.Command("git", "-C", cwd, "status", "--short")
	gitOut, gitErr := gitStatus.Output()
	dirtyCount := 0
	if gitErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(gitOut)), "\n") {
			if line != "" {
				dirtyCount++
			}
		}
	}

	// ── Recent commit count (last 7 days) ─────────────────────────────────────
	gitLog := exec.Command("git", "-C", cwd, "log", "--oneline", "--since=7 days ago")
	logOut, logErr := gitLog.Output()
	recentCommits := 0
	if logErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
			if line != "" {
				recentCommits++
			}
		}
	}

	// ── Memory health ─────────────────────────────────────────────────────────
	memCount := -1
	memories, memErr := r.gw.Memories(ctx)
	if memErr == nil {
		memCount = len(memories)
	}

	seedCount := -1
	seeds, seedErr := r.gw.Seeds(ctx)
	if seedErr == nil {
		seedCount = len(seeds)
	}

	// ── Determine signal ──────────────────────────────────────────────────────
	// RED: ≥25 dirty files  /  YELLOW: ≥10 dirty files  /  GREEN: clean
	var signal string
	var signalColor string
	switch {
	case dirtyCount >= 25:
		signal = "RED"
		signalColor = "#ef4444"
	case dirtyCount >= 10:
		signal = "YELLOW"
		signalColor = "#e8b04a"
	default:
		signal = "GREEN"
		signalColor = "#22c55e"
	}

	// Print signal banner.
	gcolor.Bold.Print(gcolor.HEX(signalColor).Sprintf("  ● %s\n\n", signal))

	// ── Metrics ──────────────────────────────────────────────────────────────
	printKV("dirty files", fmt.Sprintf("%d", dirtyCount))
	printKV("commits (7d)", fmt.Sprintf("%d", recentCommits))

	if memCount >= 0 {
		printKV("memories", fmt.Sprintf("%d", memCount))
	} else {
		printKV("memories", gcolor.HEX("#94a3b8").Sprint("gateway unreachable"))
	}
	if seedCount >= 0 {
		printKV("seeds", fmt.Sprintf("%d", seedCount))
	} else {
		printKV("seeds", gcolor.HEX("#94a3b8").Sprint("gateway unreachable"))
	}
	fmt.Println()

	// ── Guidance by signal ────────────────────────────────────────────────────
	switch signal {
	case "RED":
		fmt.Println(gcolor.HEX("#ef4444").Sprint("  Action required:"))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  • Too many uncommitted changes — stash, commit, or discard"))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  • Run /code gate before proceeding"))
	case "YELLOW":
		fmt.Println(gcolor.HEX("#e8b04a").Sprint("  Watch signal:"))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  • Commit or checkpoint changes before starting new work"))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  • Consider /snapshot to preserve state"))
	case "GREEN":
		fmt.Println(gcolor.HEX("#22c55e").Sprint("  Workspace is clean — good to start new work."))
	}
	fmt.Println()

	return nil
}

// ─── streaming helper ────────────────────────────────────────────────────────

// craftStream sends a chat request with a system prompt injected as the first user
// message prefix and streams the response to stdout.
// The gateway /v1/chat endpoint does not have a separate system_prompt field,
// so we prepend it inline.
func (r *Registry) craftStream(ctx context.Context, systemPrompt string, req client.ChatRequest) error {
	req.Message = systemPrompt + "\n\n---\n\n" + req.Message
	return r.gw.ChatStream(ctx, req, func(chunk client.SSEChunk) {
		if chunk.Data == "" {
			return
		}
		// Parse delta payload using encoding/json for correct Unicode/escape handling.
		var delta struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(chunk.Data), &delta); err == nil && delta.Delta != "" {
			fmt.Print(delta.Delta)
			return
		}
		// Fallback: print raw data for non-delta events (content, etc.).
		if chunk.Event == "" || chunk.Event == "message" || chunk.Event == "delta" {
			fmt.Print(chunk.Data)
		}
	})
}
