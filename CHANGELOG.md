# Changelog

All notable changes to the Dojo CLI are documented here.

This project adheres to [Keep a Changelog v1.1.0](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

## 2026-07-18 — Agent-Navigable & Headless (v1.1)

The whole 35-command surface is now runnable non-interactively with a uniform,
machine-parseable contract — the CLI is a scriptable tool surface, not just a REPL.

### Added

- feat(headless): `dojo --one-shot "/cmd …"` now **runs the slash command** through the same dispatcher the REPL uses, instead of chatting the literal string to a model. All ~35 commands are reachable from a script, an agent, or CI
- feat(output): `--json` emits a stable **`{ok, command, data, error}` envelope** per command; streaming commands (`/run`, `/agent`, `/workflow`, `/pilot`) emit **NDJSON** lines as they go, then a terminal envelope. New `internal/output` seam; `printKV`-based commands yield structured `data` automatically
- feat(discovery): `dojo --commands` prints a machine-readable command catalog (name/aliases/short/usage), generated from the registry — the entry point for an agent discovering the surface (`--json` for the array form)
- feat(cli): exit-code fidelity — `0` ok · `1` runtime/gateway error · `2` usage (unknown command, bad config) · `130` cancel
- feat(tools): `/tools <name>` — inspect a single tool's input schema (unblocks `/apps call` from blind-guessing JSON)
- feat(run): `/run --plan <file>` — submit a hand-authored `ExecutionPlan` (JSON) to the orchestrator (no NL parsing, no chat fallback)
- feat(memory): `/trail edit <id> <text>` — edit a memory entry (previously only add/rm/search); `/snapshot export <id> [path]` — write a snapshot to a file
- feat(skill): CAS version pinning — `/skill get <name>@<ver>` and `package-all <dir> [ver]` (previously always `latest`)
- feat(hooks): native `prompt` and `agent` hook types now execute against the Gateway (advisory-only — they run and log, only `command` hooks can veto); the Gateway client is injected into the hook runner
- feat(plugins): plugin-install integrity — source allowlist (DojoGenesis / TresPies-source by default; `--allow-any-source` to override) + optional `--sha256=<hex>` content pin; never bypassed by `--yes`

### Changed

- Commands that block on interactive confirmation (`/plugin install`, `/protocol install`, `/craft` prune/elevate/scaffold, `/code undo`) now **refuse cleanly** in headless mode instead of hanging on stdin (override with `--yolo`)
- `/warroom` and `/bloom` (pure TUIs) return an "unsupported in headless mode" envelope instead of launching a broken alt-screen; `/home` and `/pilot` fall back to their plain paths
- Unknown subcommands on `/agent`, `/apps`, `/garden`, `/hooks`, `/snapshot` now **error** instead of silently running `ls`

### Fixed

- `/run` (and streaming commands) headlessly seed a session id, and a mid-stream gateway error now propagates to `ok:false` / exit 1 instead of silently reporting success

---

## 2026-07-17 — Hooks, Permissions, Delegation Routing, Guardrails, External Skills

### Added

- feat(hooks): blocking hooks — per-rule `"blocking": true` in `hooks/hooks.json`; only `command`-type hooks can veto (an `http` hook is fire-and-forget by design, and `prompt`/`agent` hook types are unimplemented and now print a once-per-plugin stderr warning instead of a silent no-op)
- feat(hooks): new `UserPromptSubmit` event fires on free-text chat input before it reaches the Gateway, matched against the literal command `"chat"`; blocking takes effect there and at `PreCommand` only — `PostCommand`/`SessionStart`/`SessionEnd` stay log-only regardless of a rule's `blocking` flag. The prompt text rides to command hooks via `DOJO_PROMPT` (truncated to 4096 bytes, delivered through the process environment, never shell-interpolated)
- feat(permissions): new action-permission gate (`internal/permissions/`) — `permissions.mode` (`default` / `allowlist` / `yolo`; env `DOJO_PERMISSIONS_MODE`; `--yolo` flag forces yolo in-memory for the run with a loud stderr warning) and `permissions.allowed` (exact, `craft.*`-style glob, or bare `*` dot-path patterns) gate `code.undo`, `plugin.install`, `plugin.rm`, `craft.adr`, `craft.claude-md` (the `--fix` write path only), and `craft.scaffold`
- feat(agent): per-dispatch model override — `/agent dispatch` and `/agent chat` accept `--model <name>` / `--model=<name>` in any position; precedence is flag > `delegation.model` (env `DOJO_DELEGATION_MODEL`) > Gateway default, printed as `model: <name> (flag|delegation default)` before the stream; `/model ls` shows the delegation default when set
- feat(guardrail): advisory circuit breaker (`internal/guardrail/`) — escalating one-line notices (never blocking) after `guardrails.warn_after` (default 3) / `guardrails.hard_after` (default 5) consecutive identical failures of a slash command or a same-signature chat-stream tool call
- feat(skills): read-only external skill discovery (`internal/skills/external.go`) — `skills.external_dirs` (default `[".claude/skills"]`) scanned for `SKILL.md` files from foreign agent ecosystems; `/skill ls` appends an "External (read-only)" section, `/skill get ext:<name>` forces external resolution, plain `/skill get` falls back to external only on a Gateway CAS miss, and `package-all` never sweeps external dirs
- feat(config): new `permissions` / `delegation` / `guardrails` / `skills` `settings.json` sections, with `DOJO_PERMISSIONS_MODE` / `DOJO_DELEGATION_MODEL` env overrides that are never persisted back to `settings.json`

### Docs

- docs: document the `/craft` command suite (previously undocumented) and the six capabilities above in README; extend the `Configuration` example with the four new settings.json sections; correct a stale `plugin.json`/`hooks.json` example in Plugin System that predated the `hooks/hooks.json` file split

Inspired by Claude Code (hooks/permissions/model-override), Hermes fleet (loop guardrails, delegation lane), and Charm's Crush (permission tiers, Agent Skills interop).

---

## [Unreleased]

### Added

- `/craft` command group — DojoCraft practitioner workbench with 15 offline + online test cases (`1036875`, `ad4ece2`)
- Desktop scaffold (`cli/desktop/`) hibernated pending CLI v1.0.0 ship; Wails v2 + Svelte 5 build is clean (`8f456a2`, `4b5eb7c`)

### Changed

- goreleaser brew token: corrected cross-repo homebrew-tap push (`6995170`)
- Embed anchor committed for `desktop/frontend/dist` to satisfy CI (`af08841`)

### Fixed

- Atomic writes for all config/state JSON files — eliminates partial-write corruption (`e58945f`)
- Redact secret positional args in activity log (`a965ac4`)
- Enforce project-root boundary in `/code` read — prevents path traversal (`abeb071`)
- smoke-craft.sh: use REPL stdin pipe instead of `--one-shot` flag (`956b970`)

### Docs

- README: fill gaps in coverage (`190e24e`)

---

## [1.0.0] — 2026-04-12

### Added

- NL-to-DAG construction: `/run <task> --dag` command (TODO 3.4) (`10a1648`)
- goreleaser release workflow: auto-build on tag push (`723e958`)

### Fixed

- Resolve `WorkspaceRoot` to absolute path instead of hardcoded `"."` (`5c491e0`)
- Shell completions: add 13 missing commands to zsh/bash/fish + REPL (`723e958`)

### Docs

- Mark `TODO.md` fully resolved — all phases complete, CLI shippable (`6114dd5`)

---

## [0.2.0] — 2026-04-11

### Added

- Skills semantic clustering package + telemetry sink/providers tests (`27082ae`)
- Persist disposition profiles to `settings.json`; add `/settings profile` subcommand (`7e97309`)
- `/bloom`, `/code`, `/guide` commands; art animations; War Room updates (`f37f8ef`)
- Specialist awareness + DAG panel + garden context (pilot) (`f37f8ef`)
- Multi-panel layout + telemetry sink (pilot) (`14ec816`)
- Typed event parser + real-time cost tracking (pilot) (`5081c97`)
- War Room TUI mode — split-panel Scout vs Challenger debate (`1a27480`)
- Dojo Spirit — belt ranks, achievements, streaks, koans, profile card (`216ab92`)
- `/telemetry` CLI commands: sessions, costs, tools, summary (`adfd19c`)
- Comprehensive CLI improvements Phases 1–3 (`3270d3d`)
- `/init` command — populate empty workspace with ecosystem assets (`ba393c0`)
- Desktop scaffold and CI workflow (`8f456a2`)

### Changed

- Desktop: hibernate `cli/desktop/` — feature dev gated behind CLI v1.0.0 ship (`4b5eb7c`)
- Stop tracking binaries; stage providers + test runner updates (`2a473d3`)

### Fixed

- Agent dispatch guard, `/run` command routing, persistent SSE connection (`d9718d6`)
- Send `workspace_root` with every chat request for file tool resolution (`009208a`)
- Nightly CI: post-sweep cleanup (`f7f521c`)
- Correct telemetry API default URL (`89dd205`)

### Docs

- Add `llms.txt` for LLM/Context7 discoverability (`d9718d6`)

### Chore

- Add Apache 2.0 LICENSE file (`e3b7044`)
- Commit hygiene: stop tracking binaries (`2a473d3`)
- CI workflow, desktop scaffold, build scripts, gitignore (`8f456a2`)

---

## [0.1.0] — 2026-04-09

### Added

- Phase 7: MCP apps, agent channels, workflows, CAS, documents — initial CLI feature set (`feat: Phase 7`)
