package commands

// cmd_skill.go — the /skill command: list, fetch, and inspect skills from the
// gateway CAS, plus read-only discovery of external skills (foreign agent
// ecosystems' SKILL.md dirs — Claude Code's .claude/skills, Cursor's
// .cursor/skills, ~/.agents/skills) via cfg.Skills.ExternalDirs.
//
// The gateway-backed subcommands moved here verbatim from cmd_workflow.go
// (pure extraction; no behavior change to them). External skills are strictly
// supplementary and READ-ONLY: they are listed after the gateway listing,
// resolvable via the explicit `ext:` prefix (or as a fallback when the
// gateway tag lookup misses), and are never offered for packaging or pushing
// — promoted knowledge is reference material, not configuration.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/mdrender"
	"github.com/DojoGenesis/cli/internal/skills"
	gcolor "github.com/gookit/color"
)

// ─── /skill ─────────────────────────────────────────────────────────────────

func (r *Registry) skillCmd() Command {
	return Command{
		Name:    "skill",
		Aliases: []string{"skills"},
		Usage:   "/skill [ls [filter]|search <query>|get <name>[@ver]|inspect <hash>|tags|package-all <dir> [ver]]",
		Short:   "List, fetch, or inspect skills from CAS",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "search":
				// /skill search <query>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill search <query>")
				}
				query := strings.Join(args[1:], " ")
				skills, err := r.gw.SearchSkills(ctx, query)
				if err != nil {
					return fmt.Errorf("search failed: %w", err)
				}

				if r.out.JSON() {
					r.out.Data(skills)
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skills matching %q (%d)\n\n", query, len(skills)))

				if len(skills) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No matching skills found."))
					fmt.Println()
					return nil
				}

				for _, s := range skills {
					name := gcolor.HEX("#f4a261").Sprintf("%-40s", s.Name)
					cat := ""
					if s.Category != "" {
						cat = gcolor.HEX("#94a3b8").Sprintf("[%s]", s.Category)
					}
					plugin := ""
					if s.Plugin != "" {
						plugin = gcolor.HEX("#64748b").Sprintf(" (%s)", s.Plugin)
					}
					fmt.Printf("    %s %s%s\n", name, cat, plugin)
					if s.Description != "" {
						desc := s.Description
						if len(desc) > 100 {
							desc = desc[:97] + "..."
						}
						fmt.Printf("    %s\n", gcolor.HEX("#94a3b8").Sprint("  "+desc))
					}
				}
				fmt.Println()
				return nil

			case "get":
				// /skill get <name>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill get <name>")
				}
				name := args[1]
				// Explicit external namespace: /skill get ext:<name> never
				// touches the gateway — read-only local resolution only.
				if extName, isExt := strings.CutPrefix(name, "ext:"); isExt {
					ext := skills.FindExternal(r.externalSkillDirs(), extName)
					if ext == nil {
						return fmt.Errorf("external skill %q not found", extName)
					}
					return printExternalSkill(ext)
				}
				// Optional @<version> suffix — e.g. "my-skill@1.2.0". Absent
				// defaults to "latest", matching the prior hardcoded behavior.
				baseName, ver, hasVer := strings.Cut(name, "@")
				if !hasVer {
					ver = "latest"
				}
				tag, err := r.gw.CASResolveTag(ctx, baseName, ver)
				if err != nil {
					// Gateway lookup missed: fall back to external skills
					// (read-only). A miss on both surfaces the existing
					// gateway error unchanged. External skills have no
					// version concept, so the fallback looks up baseName
					// (the name with any @version suffix stripped).
					if ext := skills.FindExternal(r.externalSkillDirs(), baseName); ext != nil {
						return printExternalSkill(ext)
					}
					return fmt.Errorf("could not resolve tag %q: %w", name, err)
				}
				content, err := r.gw.CASGetContent(ctx, tag.Ref)
				if err != nil {
					return fmt.Errorf("could not fetch content for ref %q: %w", tag.Ref, err)
				}

				if r.out.JSON() {
					r.out.Data(map[string]any{
						"name":    tag.Name,
						"version": tag.Version,
						"ref":     tag.Ref,
						"content": string(content),
					})
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Skill: %s @ %s\n\n", tag.Name, tag.Version))
				printKV("ref", tag.Ref)
				fmt.Println()
				fmt.Println(mdrender.RenderMarkdown(string(content)))
				fmt.Println()
				return nil

			case "inspect":
				// /skill inspect <hash>
				if len(args) < 2 {
					return fmt.Errorf("usage: /skill inspect <hash>")
				}
				ref := args[1]
				content, err := r.gw.CASGetContent(ctx, ref)
				if err != nil {
					return fmt.Errorf("could not fetch content for ref %q: %w", ref, err)
				}

				if r.out.JSON() {
					r.out.Data(map[string]any{"ref": ref, "content": string(content)})
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  CAS ref: %s\n\n", ref))
				fmt.Println(gcolor.White.Sprint(string(content)))
				fmt.Println()
				return nil

			case "tags":
				// /skill tags
				tags, err := r.gw.CASListTags(ctx)
				if err != nil {
					return fmt.Errorf("could not list CAS tags: %w", err)
				}

				if r.out.JSON() {
					r.out.Data(tags)
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  CAS Tags (%d)\n\n", len(tags)))
				if len(tags) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No tags found."))
					fmt.Println()
					return nil
				}
				// Table header
				fmt.Printf("  %s  %s  %s\n",
					gcolor.HEX("#94a3b8").Sprintf("%-32s", "Name"),
					gcolor.HEX("#94a3b8").Sprintf("%-12s", "Version"),
					gcolor.HEX("#94a3b8").Sprint("Ref"),
				)
				fmt.Printf("  %s\n", gcolor.HEX("#64748b").Sprint(strings.Repeat("─", 72)))
				for _, t := range tags {
					fmt.Printf("  %s  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-32s", truncate(t.Name, 32)),
						gcolor.White.Sprintf("%-12s", truncate(t.Version, 12)),
						gcolor.HEX("#94a3b8").Sprint(truncate(t.Ref, 20)),
					)
				}
				fmt.Println()
				return nil

			case "package-all":
				// /skill package-all [dir] [ver]
				// Walk a directory for SKILL.md files, put each into CAS, and create tags.
				// Default source: $DOJO_SKILLS_PATH or current directory.
				// NOTE: this deliberately walks ONLY its explicit arg or
				// $DOJO_SKILLS_PATH — never cfg.Skills.ExternalDirs. External
				// skills are read-only and are never packaged or pushed.
				dir := os.Getenv("DOJO_SKILLS_PATH")
				if len(args) >= 2 {
					dir = args[1]
				}
				if dir == "" {
					return fmt.Errorf("usage: /skill package-all <dir> [ver]\n  or set DOJO_SKILLS_PATH")
				}
				// Optional trailing version arg — every packaged skill is
				// tagged name@ver instead of the hardcoded name@latest.
				ver := "latest"
				if len(args) >= 3 {
					ver = args[2]
				}

				// Walk for SKILL.md files.
				var skills []string
				err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return nil // skip inaccessible
					}
					if !info.IsDir() && info.Name() == "SKILL.md" {
						skills = append(skills, path)
					}
					return nil
				})
				if err != nil {
					return fmt.Errorf("walking %s: %w", dir, err)
				}

				if len(skills) == 0 {
					if r.out.JSON() {
						r.out.Data(map[string]any{"succeeded": 0, "failed": 0, "total": 0, "version": ver})
						return nil
					}
					fmt.Println()
					fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  No SKILL.md files found in %s", dir))
					fmt.Println()
					return nil
				}

				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Packaging %d skills from %s\n\n", len(skills), dir))

				var succeeded, failed int
				for _, path := range skills {
					// Derive skill name from parent directory.
					skillName := filepath.Base(filepath.Dir(path))

					content, err := os.ReadFile(path)
					if err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: %s\n", skillName, err)
						r.out.Emit(map[string]any{"skill": skillName, "status": "fail", "error": err.Error()})
						failed++
						continue
					}

					// Put content into CAS.
					// Sleep before each API call: gateway allows 300 req/min (5/sec) sustained,
					// burst 50. Each skill makes 2 calls, so 250ms per call = 4 req/sec total.
					ref, err := r.gw.CASPutContent(ctx, content)
					time.Sleep(250 * time.Millisecond)
					if err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: CAS put: %s\n", skillName, err)
						r.out.Emit(map[string]any{"skill": skillName, "status": "fail", "error": err.Error()})
						failed++
						continue
					}

					// Create tag: name@ver -> ref (ver defaults to "latest").
					if err := r.gw.CASCreateTag(ctx, skillName, ver, ref); err != nil {
						gcolor.HEX("#ef4444").Printf("  [FAIL] %s: tag create: %s\n", skillName, err)
						r.out.Emit(map[string]any{"skill": skillName, "status": "fail", "error": err.Error()})
						failed++
						time.Sleep(250 * time.Millisecond)
						continue
					}
					time.Sleep(250 * time.Millisecond)

					gcolor.HEX("#22c55e").Printf("  [OK]   %s → %s\n", skillName, shortRef(ref))
					r.out.Emit(map[string]any{"skill": skillName, "status": "ok", "ref": shortRef(ref), "version": ver})
					succeeded++
				}

				r.out.Data(map[string]any{
					"succeeded": succeeded,
					"failed":    failed,
					"total":     len(skills),
					"version":   ver,
				})

				fmt.Println()
				summary := fmt.Sprintf("  Done: %d succeeded, %d failed", succeeded, failed)
				if failed > 0 {
					gcolor.HEX("#eab308").Println(summary)
				} else {
					gcolor.HEX("#22c55e").Println(summary)
				}
				fmt.Println()
				return nil

			default: // ls / all / filter — main skill browser
				filter, showAll, page := parseSkillLsArgs(args)

				rawSkills, err := r.gw.Skills(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch skills: %w", err)
				}
				// Fill in missing categories via semantic clustering so skills
				// group correctly regardless of gateway metadata completeness.
				skillList := skills.EnrichCategories(rawSkills)

				// Filter by name, category, or plugin — computed once, ahead
				// of both the JSON short-circuit below and the human display
				// branches further down, so both paths see the same result.
				displaySkills := skillList
				if filter != "" {
					fl := strings.ToLower(filter)
					var matched []client.Skill
					for _, s := range skillList {
						if strings.Contains(strings.ToLower(s.Name), fl) ||
							strings.Contains(strings.ToLower(s.Category), fl) ||
							strings.Contains(strings.ToLower(s.Plugin), fl) {
							matched = append(matched, s)
						}
					}
					displaySkills = matched
				}

				if r.out.JSON() {
					r.out.Data(displaySkills)
					return nil
				}

				if len(skillList) == 0 {
					fmt.Println()
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No skills found."))
					fmt.Println()
					r.printExternalSkillsSection()
					return nil
				}

				// Category summary: no filter, no explicit "all", page 1.
				if filter == "" && !showAll && page == 1 {
					if err := printSkillCategorySummary(skillList); err != nil {
						return err
					}
					r.printExternalSkillsSection()
					return nil
				}

				if err := printSkillsPage(displaySkills, filter, page); err != nil {
					return err
				}
				r.printExternalSkillsSection()
				return nil
			}
		},
	}
}

// ─── /skill helpers ──────────────────────────────────────────────────────────

const skillPageSize = 30

// shortRef returns the first 12 characters of a CAS ref for compact display,
// or ref unchanged if it is shorter than that. ref comes straight from the
// gateway response (CASPutContent) with no length guarantee — an unguarded
// ref[:12] slice panics (and previously crashed the whole CLI mid
// /skill package-all) whenever the gateway returns a short ref.
func shortRef(ref string) string {
	if len(ref) >= 12 {
		return ref[:12]
	}
	return ref
}

// parseSkillLsArgs extracts (filter, showAll, page) from the args slice.
// Recognises: "all", "p<N>" or plain integers as page numbers, everything else
// is joined as the filter term.
func parseSkillLsArgs(args []string) (filter string, showAll bool, page int) {
	page = 1
	var filterParts []string
	for _, a := range args {
		al := strings.ToLower(a)
		if al == "ls" {
			continue
		}
		if al == "all" {
			showAll = true
			continue
		}
		// p<N> — page number
		if strings.HasPrefix(al, "p") {
			if n, err := strconv.Atoi(al[1:]); err == nil && n >= 1 {
				page = n
				continue
			}
		}
		// bare integer — page number
		if n, err := strconv.Atoi(a); err == nil && n >= 1 {
			page = n
			continue
		}
		filterParts = append(filterParts, a)
	}
	filter = strings.Join(filterParts, " ")
	return
}

// skillCategoryOrder groups skills by category. Returns (map, ordered keys sorted by count desc).
func skillCategoryOrder(skills []client.Skill) (map[string][]client.Skill, []string) {
	cats := map[string][]client.Skill{}
	for _, s := range skills {
		cat := s.Category
		if cat == "" {
			cat = "general"
		}
		cats[cat] = append(cats[cat], s)
	}
	keys := make([]string, 0, len(cats))
	for k := range cats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ci, cj := len(cats[keys[i]]), len(cats[keys[j]])
		if ci != cj {
			return ci > cj
		}
		return keys[i] < keys[j]
	})
	return cats, keys
}

// printSkillCategorySummary renders the landing page for /skill ls (no args).
func printSkillCategorySummary(skills []client.Skill) error {
	cats, order := skillCategoryOrder(skills)

	// Count distinct plugins.
	pluginSet := map[string]struct{}{}
	for _, s := range skills {
		if s.Plugin != "" {
			pluginSet[s.Plugin] = struct{}{}
		}
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf(
		"  Skills  %d total · %d categories · %d plugins\n\n",
		len(skills), len(order), len(pluginSet),
	))

	divider := gcolor.HEX("#334155").Sprint(strings.Repeat("─", 66))
	fmt.Println("  " + divider)

	for _, cat := range order {
		count := len(cats[cat])
		bar := buildMiniBar(count, len(skills), 12)
		fmt.Printf("    %s  %s  %s\n",
			gcolor.HEX("#f4a261").Sprintf("%-28s", cat),
			gcolor.HEX("#94a3b8").Sprintf("%3d", count)+" "+gcolor.HEX("#334155").Sprint(bar),
			gcolor.HEX("#64748b").Sprintf("/skill ls %s", cat),
		)
	}

	fmt.Println("  " + divider)
	fmt.Println()
	fmt.Printf("    %s  /skill ls all          %s  /skill ls all p2\n",
		gcolor.HEX("#94a3b8").Sprint("list all:"),
		gcolor.HEX("#94a3b8").Sprint("next page:"),
	)
	fmt.Printf("    %s  /skill search <query>\n",
		gcolor.HEX("#94a3b8").Sprint("search:  "),
	)
	fmt.Println()
	return nil
}

// buildMiniBar returns a fixed-width ASCII progress bar.
func buildMiniBar(count, total, width int) string {
	if total == 0 || width == 0 {
		return strings.Repeat("░", width)
	}
	filled := count * width / total
	if filled == 0 && count > 0 {
		filled = 1
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// printSkillsPage renders a paginated grouped skill list.
// Pages are built by complete category: a new page starts only after a full
// category has been rendered, so categories are never split mid-display.
func printSkillsPage(skills []client.Skill, filter string, page int) error {
	if len(skills) == 0 {
		fmt.Println()
		if filter != "" {
			fmt.Println(gcolor.HEX("#94a3b8").Sprintf("  No skills matching %q.", filter))
		} else {
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No skills found."))
		}
		fmt.Println()
		return nil
	}

	cats, order := skillCategoryOrder(skills)

	// Build pages by grouping complete categories until skillPageSize is reached.
	type pageSlice struct {
		cats  []string
		count int
	}
	var pages []pageSlice
	var cur pageSlice
	for _, cat := range order {
		cur.cats = append(cur.cats, cat)
		cur.count += len(cats[cat])
		if cur.count >= skillPageSize {
			pages = append(pages, cur)
			cur = pageSlice{}
		}
	}
	if len(cur.cats) > 0 {
		pages = append(pages, cur)
	}

	totalPages := len(pages)
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	pg := pages[page-1]

	// Count skills on this page.
	pageCount := 0
	for _, cat := range pg.cats {
		pageCount += len(cats[cat])
	}

	fmt.Println()
	label := "Skills"
	if filter != "" {
		label = fmt.Sprintf("Skills › %s", filter)
	}
	pageLabel := ""
	if totalPages > 1 {
		pageLabel = fmt.Sprintf(" · page %d of %d", page, totalPages)
	}
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  %s  (%d of %d%s)\n\n", label, pageCount, len(skills), pageLabel))

	// Render each category group.
	for _, cat := range pg.cats {
		fmt.Printf("  %s %s %s\n",
			gcolor.HEX("#334155").Sprint("────"),
			gcolor.HEX("#e8b04a").Sprint("["+cat+"]"),
			gcolor.HEX("#334155").Sprint("────────────────────────────────────────────────"),
		)
		for _, s := range cats[cat] {
			plugin := ""
			if s.Plugin != "" {
				plugin = gcolor.HEX("#64748b").Sprintf("(%s)", s.Plugin)
			}
			fmt.Printf("    %s %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-40s", truncate(s.Name, 40)),
				plugin,
			)
		}
	}

	// Footer navigation hints.
	fmt.Println()
	if totalPages > 1 {
		base := "/skill ls"
		if filter != "" {
			base += " " + filter
		} else {
			base += " all"
		}
		if page < totalPages {
			fmt.Printf("  %s  %s p%d\n",
				gcolor.HEX("#94a3b8").Sprint("next:"),
				gcolor.HEX("#64748b").Sprint(base),
				page+1,
			)
		}
		if page > 1 {
			fmt.Printf("  %s  %s p%d\n",
				gcolor.HEX("#94a3b8").Sprint("prev:"),
				gcolor.HEX("#64748b").Sprint(base),
				page-1,
			)
		}
	}
	fmt.Printf("  %s  /skill search <query>\n", gcolor.HEX("#94a3b8").Sprint("search:"))
	fmt.Println()
	return nil
}

// ─── external skills (read-only) ─────────────────────────────────────────────

// externalSkillDirs returns the configured external skill directories
// (cfg.Skills.ExternalDirs), or nil when no config is attached.
func (r *Registry) externalSkillDirs() []string {
	if r.cfg == nil {
		return nil
	}
	return r.cfg.Skills.ExternalDirs
}

// printExternalSkillsSection appends the supplementary "External (read-only)"
// section to /skill ls output. An empty scan prints nothing at all — the
// section is supplementary, so its definitive empty state is silence, not an
// empty header.
func (r *Registry) printExternalSkillsSection() {
	ext := skills.ScanExternal(r.externalSkillDirs())
	if len(ext) == 0 {
		return
	}
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  External (read-only)\n\n"))
	for _, s := range ext {
		desc := ""
		if s.Description != "" {
			desc = " — " + clipRunes(s.Description, 100)
		}
		fmt.Printf("    %s%s %s\n",
			gcolor.HEX("#f4a261").Sprint("ext:"+s.Name),
			gcolor.HEX("#94a3b8").Sprint(desc),
			gcolor.HEX("#64748b").Sprintf("(%s)", s.SourceDir),
		)
	}
	fmt.Println()
}

// printExternalSkill renders one external skill's SKILL.md through the same
// markdown path /skill get uses for CAS skills, prefixed by a read-only
// header naming the source file. External skills are never packaged, pushed,
// or otherwise written — display only.
func printExternalSkill(ext *skills.ExternalSkill) error {
	content, err := os.ReadFile(ext.Path)
	if err != nil {
		return fmt.Errorf("could not read external skill %q: %w", ext.Name, err)
	}

	// This free function has no *Registry receiver (it is shared by the
	// /skill get ext:<name> case and the gateway-miss fallback), so it
	// consults the package-level curEmitter the same way printKV does.
	if curEmitter.JSON() {
		curEmitter.Data(map[string]any{
			"name":      ext.Name,
			"path":      ext.Path,
			"sourceDir": ext.SourceDir,
			"external":  true,
			"content":   string(content),
		})
		return nil
	}

	fmt.Println()
	fmt.Printf("  %s\n\n", gcolor.HEX("#94a3b8").Sprintf("external skill (read-only): %s", ext.Path))
	fmt.Println(mdrender.RenderMarkdown(string(content)))
	fmt.Println()
	return nil
}

// clipRunes cuts s to at most n runes for single-line terminal display,
// appending "..." when clipped (matching the house style used by
// /skill search descriptions), but rune-safe rather than byte-sliced.
func clipRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}
