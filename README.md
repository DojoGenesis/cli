# Dojo CLI

Self-hosted agentic AI in your terminal. Own your infrastructure. Control your data. The CLI surface of Dojo Genesis — same gateway, same ADA dispositions, same memory — without the browser.

## What This Is

Dojo Genesis is an open-source AI development platform built on a 100% Go-native architecture. The web shell organizes work into eight named surfaces — Garden, Practice, Trail, Partnership, Projects, Pipelines, Piloting, and Home — each one a different lens on your workspace. This CLI maps all eight to commands you can drive from a terminal, connect to CI scripts, or pipe into other tools.

The gateway does the heavy work: multi-provider model routing, semantic memory, MCP tool execution, durable agent sessions. The CLI connects to it, streams responses, and keeps your hands on the keyboard.

## Quick Start

```bash
# Install via Homebrew (recommended)
brew install DojoGenesis/tap/dojo

# Point at your gateway
echo '{"gateway":{"url":"http://localhost:7340"}}' > ~/.dojo/settings.json

# Run
dojo
```

### From Source

```bash
git clone https://github.com/DojoGenesis/cli
cd cli
make install
```

Requires Go 1.24+. The binary is installed to `$GOPATH/bin/dojo`.

### Install Script

```bash
curl -sSL https://raw.githubusercontent.com/DojoGenesis/cli/main/scripts/install.sh | bash
```

## Configuration

Settings are loaded from `~/.dojo/settings.json`. A missing file is not an error — all fields have defaults.

```json
{
  "gateway": {
    "url": "http://localhost:7340",
    "timeout": "60s",
    "token": ""
  },
  "plugins": {
    "path": "~/.dojo/plugins"
  },
  "defaults": {
    "provider": "",
    "disposition": "balanced",
    "model": ""
  },
  "permissions": {
    "mode": "default",
    "allowed": ["craft.*", "plugin.install"]
  },
  "delegation": {
    "model": ""
  },
  "guardrails": {
    "enabled": true,
    "warn_after": 3,
    "hard_after": 5
  },
  "skills": {
    "external_dirs": [".claude/skills"]
  }
}
```

`permissions`, `delegation`, `guardrails`, and `skills` are covered in detail in [Permissions](#permissions), [Guardrails](#guardrails), [Agents & Orchestration](#agents--orchestration), and [Skills & CAS](#skills--cas) below.

**Environment variable overrides:**

| Variable               | Overrides           |
|------------------------|---------------------|
| `DOJO_GATEWAY_URL`     | `gateway.url`       |
| `DOJO_GATEWAY_TOKEN`   | `gateway.token`     |
| `DOJO_PLUGINS_PATH`    | `plugins.path`      |
| `DOJO_PROVIDER`        | `defaults.provider` |
| `DOJO_TELEMETRY_URL`   | telemetry API base  |
| `DOJO_SKILLS_PATH`     | default skill dir for `/skill package-all` |
| `DOJO_PROTOCOL_DISABLED` | any non-empty value disables the genius protocol (`protocol.enabled`) |
| `DOJO_PROTOCOL_PATH`   | `protocol.path` — explicit override doc, takes precedence over `./DOJO.md` |
| `DOJO_PERMISSIONS_MODE` | `permissions.mode` — `default`, `allowlist`, or `yolo` |
| `DOJO_DELEGATION_MODEL` | `delegation.model` — default model for `/agent dispatch` and `/agent chat` |

`guardrails` and `skills.external_dirs` have no environment override (not requested for this release).

## Requirements

- Go 1.24+ (for building from source)
- [AgenticGateway](https://github.com/DojoGenesis/gateway) running at `localhost:7340` (default)

## CLI Flags

| Flag                  | Description                                                                 |
|-----------------------|-----------------------------------------------------------------------------|
| `--gateway <url>`     | Gateway URL (overrides `gateway.url` in settings)                           |
| `--token <tok>`       | Bearer token for gateway auth                                               |
| `--disposition <d>`   | ADA disposition preset: `focused`, `balanced`, `exploratory`, `deliberate`  |
| `--one-shot <msg>`    | Execute a single message and exit (non-interactive)                         |
| `--resume`            | Resume the most recent session instead of starting fresh                    |
| `--session <id>`      | Resume a specific session ID instead of the most recent one (implies `--resume`) |
| `--no-color`          | Disable color output                                                        |
| `--plain`             | Plain text output — no ANSI colors (for piped or CI use)                    |
| `--json`              | JSON lines output in one-shot mode (for scripted pipelines)                 |
| `--completion <sh>`   | Generate shell completions: `bash`, `zsh`, or `fish`                        |
| `--version`           | Print version and exit                                                      |

**One-shot mode** is useful for scripting:

```bash
dojo --one-shot "what models are available?" --gateway http://localhost:7340

# JSON output for pipelines
dojo --one-shot "summarize the last run" --json | jq '.text'
```

**Resume most recent session:**

```bash
dojo --resume
```

**Resume a specific session:**

```bash
dojo --session dojo-cli-20260409-142301
```

**Shell completions:**

```bash
dojo --completion zsh >> ~/.zshrc
dojo --completion bash >> ~/.bashrc
dojo --completion fish > ~/.config/fish/completions/dojo.fish
```

## Commands

Type a message without `/` to chat with the gateway. Use slash commands for structured operations.

### Gateway & Models

| Command                           | Description                                            |
|-----------------------------------|--------------------------------------------------------|
| `/help`                           | Show available commands                                |
| `/health`                         | Gateway health and uptime stats                        |
| `/doctor`                         | Full diagnostic: gateway, providers, config, protocol, harnesses |
| `/home`                           | Workspace state overview (TUI panel)                   |
| `/home plain`                     | Workspace state in plain text                          |
| `/model [ls]`                     | List available models and providers                    |
| `/model set <name>`               | Switch active model for the current session            |
| `/tools [ls]`                     | List registered MCP tools grouped by namespace         |
| `/settings`                       | Show config file path and all active settings          |

### Agents & Orchestration

| Command                                      | Description                                               |
|----------------------------------------------|-----------------------------------------------------------|
| `/agent ls`                                  | List agents from gateway + recently used local agents     |
| `/agent dispatch [mode] [--model <name>] <msg>` | Create agent and stream response                        |
| `/agent chat [--model <name>] <id> <msg>`    | Chat with an existing agent by ID                          |
| `/agent info <id>`                           | Show agent detail: disposition, channels, config          |
| `/agent channels <id>`                       | List bound channels for an agent                          |
| `/agent bind <id> <channel>`                 | Bind a channel to an agent                                |
| `/agent unbind <id> <channel>`               | Unbind a channel from an agent                            |
| `/run <task>`                                | Submit multi-step task; uses DAG templates or chat stream |
| `/workflow <name> [input-json]`              | Execute a named workflow and stream progress              |
| `/pilot`                                     | Live SSE event dashboard (Ctrl+C to stop)                 |
| `/pilot plain`                               | Live event stream in plain text                           |

**Per-dispatch model override:** `--model <name>` (or `--model=<name>`, either form works anywhere in the argument list) beats `delegation.model` in `~/.dojo/settings.json` (env `DOJO_DELEGATION_MODEL`), which beats the Gateway's own default. When a model is in effect, a `model: <name> (flag|delegation default)` line prints just before the response streams. `/model ls` shows the delegation default when one is set. The Gateway may not honor the field yet — the CLI sends it regardless, as forward compatibility.

### Memory & Seeds

| Command                  | Description                                               |
|--------------------------|-----------------------------------------------------------|
| `/garden ls`             | List memory seeds                                         |
| `/garden stats`          | Memory garden statistics                                  |
| `/garden plant <text>`   | Plant a new seed into the garden                          |
| `/trail`                 | Show memory timeline                                      |
| `/trace`                 | Show trace context and gateway trace guidance             |
| `/snapshot`              | Workspace state snapshot                                  |

### Session Management

| Command                 | Description                              |
|-------------------------|------------------------------------------|
| `/session`              | Show active session ID                   |
| `/session new`          | Start a fresh session                    |
| `/session ls`           | List recent sessions from local history, most-recent-first |
| `/session resume`       | Resume the most recently active session  |
| `/session resume <id>`  | Resume one specific session by ID (warns if not found in local history) |
| `/session <id>`         | Switch directly to a session ID (not verified against gateway) |

Session IDs follow the format `dojo-cli-YYYYMMDD-HHmmss` when created via `/session new`.

### Skills & CAS

| Command                              | Description                                              |
|--------------------------------------|----------------------------------------------------------|
| `/skill ls [filter]`                 | List skills from gateway, grouped by category            |
| `/skill search <query>`              | Semantic search across skills                            |
| `/skill get <name>`                  | Fetch and display a skill by name from CAS               |
| `/skill inspect <hash>`              | Display CAS content by ref hash                          |
| `/skill tags`                        | List all CAS tags (name, version, ref)                   |
| `/skill package-all [dir]`           | Walk a directory for SKILL.md files and push all to CAS  |

`/skill ls` appends a supplementary **External (read-only)** section (silent when empty) discovered from `skills.external_dirs` (default `[".claude/skills"]`) — `SKILL.md` files from foreign agent ecosystems (Claude Code, Cursor, etc.), recognized at `<dir>/SKILL.md` and `<dir>/<child>/SKILL.md`, with `~` expansion. `/skill get ext:<name>` forces external resolution; a plain `/skill get <name>` falls back to external only when the gateway CAS lookup misses. External skills are read-only reference material — `package-all` never walks `skills.external_dirs`.

### Projects

| Command                                     | Description                                              |
|---------------------------------------------|----------------------------------------------------------|
| `/project` or `/project status`             | Show active project: phase, tracks, recent activity      |
| `/project init <name> [--desc "..."]`       | Create a new project and set it as active                |
| `/project list [--all]`                     | List all projects with phase indicators                  |
| `/project switch <name-or-id>`              | Change the active project                                |
| `/project archive <name-or-id>`             | Archive a completed project                              |
| `/project phase <phase>`                    | Set the active project phase manually                    |
| `/project track add <name> [--dep N]`       | Add a parallel track to the active project               |
| `/project track set <id> <status>`          | Update a track's status                                  |
| `/project decision <text>`                  | Record a decision in the active project log              |
| `/project artifact <type> <file> <content>` | Save an artifact for the active project                  |
| `/projects ls`                              | Local workspace view: cwd, plugins, session              |

Project phases: `initialized`, `scouting`, `specifying`, `decomposing`, `commissioning`, `implementing`, `retrospective`, `archived`.

Track statuses: `pending`, `in-progress`, `completed`, `blocked`.

### MCP Apps

| Command                             | Description                                           |
|-------------------------------------|-------------------------------------------------------|
| `/apps` or `/apps ls`               | List running MCP apps with tool count and status      |
| `/apps launch <name> [config-json]` | Launch an MCP app by name                             |
| `/apps close <name>`                | Stop a running MCP app                                |
| `/apps status`                      | Show aggregated app status                            |
| `/apps call <app> <tool> <json>`    | Proxy a tool call directly to a running MCP app       |

### Plugins & Hooks

| Command                   | Description                                             |
|---------------------------|---------------------------------------------------------|
| `/plugin ls`              | List installed plugins with skill count and hook rules  |
| `/plugin install <url>`   | Clone a plugin from a git URL into `~/.dojo/plugins/` (gated: `plugin.install`) |
| `/plugin rm <name>`       | Remove an installed plugin (gated: `plugin.rm`)         |
| `/hooks ls`               | List loaded hook rules from all plugins                 |
| `/hooks fire <event>`     | Manually fire a hook event (for testing), e.g. `/hooks fire SessionStart` |

See [Plugin System](#plugin-system) below for the `hooks.json` format, blocking hooks, and the `UserPromptSubmit` event; see [Permissions](#permissions) for what the gates above mean.

### Protocol & Harnesses

| Command                             | Description                                               |
|--------------------------------------|-------------------------------------------------------------|
| `/protocol` or `/protocol status`   | Genius-protocol enabled/source + kata-harness install state |
| `/protocol harnesses`               | List the KE harness catalog: status, ratified, installed    |
| `/protocol install <name> [--yes]`  | Install a ratified, locally-available harness into `plugins.path` |

See [Genius Protocol](#genius-protocol) below for how the protocol doc is resolved and overridden.

### Dispositions

| Command                                                     | Description                           |
|-------------------------------------------------------------|---------------------------------------|
| `/disposition`                                              | Show current disposition              |
| `/disposition ls`                                           | List all disposition presets          |
| `/disposition set <name>`                                   | Set active disposition                |
| `/disposition show <name>`                                  | Show details of a preset              |
| `/disposition create <name> <pacing> <depth> <tone> <initiative>` | Create a custom preset          |

### Observability

| Command                  | Description                                                   |
|--------------------------|---------------------------------------------------------------|
| `/telemetry sessions`    | Recent sessions: cost, tokens, tool calls, errors             |
| `/telemetry costs`       | 7-day cost breakdown by provider + ASCII bar chart            |
| `/telemetry tools`       | Tool call stats: count, avg latency, success rate             |
| `/telemetry summary`     | Combined overview of all telemetry data                       |
| `/activity [n]`          | Show last N entries from the local activity log (default: 10) |
| `/activity clear`        | Clear the activity log                                        |
| `/doc <id>`              | Fetch and display a document by ID from the gateway           |

### Dojo Spirit

| Command       | Description                                                    |
|---------------|----------------------------------------------------------------|
| `/card`       | Your dojo profile card: belt, XP, progress bar, achievements  |
| `/sensei`     | Receive a koan from the sensei (unlocks by belt rank)          |
| `/practice`   | Daily reflection prompts (rotates by day of week)              |
| `/guide ls`   | List interactive tutorials with progress and XP rewards        |
| `/guide start <id>` | Begin a tutorial guide                                   |
| `/guide status`     | Show the current step in the active guide                |
| `/guide stop`       | Stop the active guide (progress is saved)                |

### TUI Experiences

| Command              | Description                                               |
|----------------------|-----------------------------------------------------------|
| `/bloom`             | Fullscreen animated bonsai garden — zen mode              |
| `/warroom [topic]`   | Split-panel Scout vs Challenger debate TUI                |

### Self-Build Tools

| Command                  | Description                                               |
|--------------------------|-----------------------------------------------------------|
| `/code read <file> [start:end]` | Display a file with line numbers, optional range  |
| `/code diff [--full] [file]`    | Show git diff (stat by default, full with flag)   |
| `/code test [pkg]`              | Run `go test` for a package or `./...`            |
| `/code build`                   | Run `go build ./...`                              |
| `/code vet`                     | Run `go vet ./...`                                |
| `/code gate`                    | Run the full build gate: build + test + vet       |
| `/code undo`                    | Preview unstaged changes to tracked files, then revert (gated: `code.undo`) |

### Craft Workbench

`/craft` is the DojoCraft practitioner workbench — strategic thinking, codebase intelligence, and memory curation in one command group.

| Command                              | Description                                                        |
|---------------------------------------|----------------------------------------------------------------------|
| `/craft`                              | Show the `/craft` subcommand list                                   |
| `/craft adr <title>`                  | Stream an ADR from the gateway, write it to `decisions/NNN-slug.md` (gated: `craft.adr`) |
| `/craft scout <tension>`              | Tension → routes → synthesis → decision, streamed from the gateway  |
| `/craft claude-md`                    | Analyse every `CLAUDE.md` under cwd (depth ≤ 3) for gaps, contradictions, and stale rules |
| `/craft claude-md --fix`              | Same analysis, then rewrite the flagged files in place (gated: `craft.claude-md`) |
| `/craft memory ls`                    | List Gateway memory entries                                         |
| `/craft memory add <text>`            | Store a new memory entry                                            |
| `/craft memory rm <id>`               | Delete a memory entry by ID                                         |
| `/craft memory prune [type]`          | Preview and confirm-delete memories, optionally filtered by type    |
| `/craft memory search <query>`        | Search memory entries                                               |
| `/craft seed ls`                      | List memory garden seeds                                            |
| `/craft seed harvest`                 | Alias for `seed ls` — no distinct curation logic yet                |
| `/craft seed plant <text>`            | Plant a new seed                                                    |
| `/craft seed search <query>`          | Search seeds locally by name/content                                |
| `/craft seed elevate [target]`        | Promote a seed (or freeform text) to durable memory, with confirm   |
| `/craft view [path]`                  | Codebase overview: top-level tree, go.mod, entry points, source/test file counts, git status |
| `/craft scaffold`                     | List available scaffold templates                                   |
| `/craft scaffold <template>`          | Create a project layout from a template (gated: `craft.scaffold`)    |
| `/craft converge`                     | Git + memory health report: RED/YELLOW/GREEN signal                 |

Templates for `/craft scaffold`: `go-service`, `fullstack`, `orchestration`, `plugin`, `minimal`.

`craft.adr`, `craft.claude-md` (the `--fix` write path only — plain analysis is ungated), and `craft.scaffold` go through the permissions gate — see [Permissions](#permissions). `/craft memory prune` and `/craft seed elevate` keep their own y/N confirm instead: they mutate Gateway-side memory state, not local files, and sit outside this wave's gates.

## Directory Structure

```
cli/
├── cmd/           # Cobra command tree
├── internal/
│   ├── repl/      # Interactive REPL and TUI panels
│   ├── client/    # Gateway HTTP + SSE client
│   ├── plugin/    # Plugin loader and hook runner
│   ├── skill/     # Skill and CAS commands
│   └── spirit/    # Belt, XP, achievements, koans
├── desktop/       # Desktop app (Wails v2 + Svelte 5)
└── scripts/       # Install and release scripts
```

## Desktop App

A native desktop variant lives at `cli/desktop/` — built with Wails v2 (Go backend) and Svelte 5 (frontend). It connects to the same gateway and exposes the same chat and piloting surfaces in a windowed app. `go build` is clean; smoke testing against the gateway is in progress.

## Surfaces

Dojo Genesis organizes work into eight named surfaces. The CLI maps each one to a command or interaction mode, so the mental model carries over from the web shell to the terminal.

| Surface     | Command / Flow    | What it does                                                          |
|-------------|-------------------|-----------------------------------------------------------------------|
| home        | `/home`           | Workspace health snapshot: agent count, seed count, recent activity   |
| garden      | `/garden`         | Long-term memory seeds. Plant, list, search semantically              |
| practice    | `/practice`       | Daily reflection prompts. Intentions, observations, retrospectives    |
| trail       | `/trail`          | Chronological timeline of all workspace events and milestones         |
| partnership | direct chat       | Primary conversational interface with the gateway. Just type          |
| projects    | `/project`        | Project lifecycle: phases, tracks, decisions, artifacts               |
| pipelines   | `/run`            | Submit multi-step orchestration tasks to the gateway                  |
| piloting    | `/pilot`          | Live SSE event stream: DAG state, model routing, tool execution       |

## ADA Disposition System

ADA (Adaptive Disposition Architecture) controls how the gateway approaches a task. Every agent dispatch and direct chat session can carry a disposition. Four built-in presets are provided:

| Disposition   | Character                                             |
|---------------|-------------------------------------------------------|
| `focused`     | Fast pacing, shallow depth. High-signal, low-noise    |
| `balanced`    | Default. Steady pacing, moderate depth                |
| `exploratory` | Wider search, longer reasoning chains                 |
| `deliberate`  | Slow and careful. Best for high-stakes decisions      |

Set a session default in `~/.dojo/settings.json` under `defaults.disposition`, or pass it per-session:

```bash
dojo --disposition deliberate
```

Per-dispatch:

```bash
/agent dispatch focused summarize the last 5 decisions
```

Create and save custom presets with `/disposition create`:

```
/disposition create sprint fast shallow assertive high
```

## Genius Protocol

A workspace "genius protocol" — a compact operating doctrine — is injected into every session **by default**. The CLI prepends it to the first turn of a chat or `--one-shot` run (and sets it as the system prompt for gateways that support one); later turns rely on the gateway's own session context instead of re-sending it.

Resolution order (first match wins):

1. an explicit `protocol.path` in `~/.dojo/settings.json`
2. `./DOJO.md` in the current project directory
3. `~/.dojo/DOJO.md`
4. the embedded default doc

**Override or disable it:**

```bash
# Disable for one run
DOJO_PROTOCOL_DISABLED=1 dojo
```

```json
// ~/.dojo/settings.json — disable persistently
{ "protocol": { "enabled": false } }
```

Or drop a `DOJO.md` file in the project root (or `~/.dojo/DOJO.md` for a machine-wide override) to replace the content instead of turning it off — either is picked up automatically ahead of the embedded default.

`/init` writes an editable `~/.dojo/DOJO.md` starter copy (never overwriting one that already exists) and installs the ratified **kata-harness** plugin alongside the rest of the first-party plugin set. Check current state anytime with `/doctor` (PROTOCOL + HARNESSES sections) or `/protocol status`; browse and install other KE harnesses with `/protocol harnesses` and `/protocol install <name>` — see [Protocol & Harnesses](#protocol--harnesses) above.

## Plugin System

Plugins extend the CLI with hook rules and skills. Place plugin directories under `~/.dojo/plugins/` (or the path configured in `plugins.path`), or use `/plugin install` to clone from a git URL.

Each plugin directory must contain a `plugin.json` manifest, checked at `.claude-plugin/plugin.json` first, then the plugin root:

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "description": ""
}
```

Hook rules live in a separate `hooks/hooks.json` file inside the plugin directory, keyed by event name:

```json
{
  "PreCommand": [
    {
      "matcher": "craft*",
      "if": "",
      "blocking": true,
      "hooks": [
        { "type": "command", "command": "/usr/local/bin/my-hook" }
      ]
    }
  ]
}
```

A Claude-Code-style wrapped file (`{"hooks": {"<Event>": [...]}}`) is also accepted; its event names are translated where a dojo equivalent exists (`PreToolUse`→`PreCommand`, `PostToolUse`→`PostCommand`, `SubagentStop`→`PostAgent`; `SessionStart`/`SessionEnd` pass through unchanged) and skipped with a log line otherwise (`Notification`, `UserPromptSubmit`, `Stop`, and `PreCompact` have no dojo equivalent when they arrive via the wrapped schema).

Event names dojo-cli itself fires: `PreCommand`, `PostCommand`, `PostSkill`, `PostAgent`, `SessionStart`, `SessionEnd`, `UserPromptSubmit`.

`matcher` is a glob matched against the command name (leading `/` stripped, so `"garden*"` matches both `/garden ls` and `garden ls`); empty or `"*"` matches everything. `if` is `""`/`"true"` (always fire), `"false"` (never), or an environment variable name (fires when it's set and non-empty).

**Blocking hooks.** Set `"blocking": true` on a rule to let a failing hook veto the action it guards. Only a `command`-type hook can actually block — a non-zero exit or error aborts the caller; `http` hooks are fire-and-forget by design, and `prompt`/`agent` hook types are not implemented and print a one-time-per-plugin stderr warning instead of silently no-op'ing. Blocking has effect on exactly two events:

- **`PreCommand`** — a blocked slash command never dispatches.
- **`UserPromptSubmit`** — fires on free-text chat input before anything is sent to the Gateway; a block returns you to the prompt with nothing sent. This event has no command name to match against, so its rules match the literal string `"chat"`. The chat text itself rides in the `DOJO_PROMPT` environment variable for command hooks (truncated to 4096 bytes) — delivered via the process environment, never shell-interpolated, so metacharacters in your message are inert.

`PostCommand`, `SessionStart`, and `SessionEnd` hooks always run log-only, regardless of `blocking`.

A blocked action prints: `[hooks] blocked by <plugin>/<event>: <reason>`.

Plugin management commands:

```
/plugin ls                              # list installed plugins
/plugin install https://github.com/...  # clone a plugin from git (gated: plugin.install)
/plugin rm my-plugin                    # remove an installed plugin (gated: plugin.rm)
/hooks ls                               # list all hook rules
/hooks fire SessionStart                # manually fire a hook event
```

Plugins are rescanned live after install and remove operations — no restart needed.

## Permissions

Risky actions — file writes, plugin install/remove, working-tree reverts — go through a permission gate before they run. Configure the strategy under `permissions` in `~/.dojo/settings.json`:

| Mode        | Behavior                                                                    |
|-------------|--------------------------------------------------------------------------------|
| `default`   | Prompt per action (`allow <action>? <detail> [y/N]`) unless it matches `permissions.allowed`. A non-interactive context (no TTY) refuses instead of hanging. |
| `allowlist` | Silently allow actions matching `permissions.allowed`; deny everything else, with a pointer to settings. |
| `yolo`      | Allow everything, no prompts. Prints one warning to stderr per process.         |

`permissions.allowed` is a list of dot-path patterns matched against an action name: exact (`"plugin.install"`), trailing-star glob (`"craft.*"`), or a bare `"*"` for everything.

Gated actions this release:

| Action            | Command                                             |
|--------------------|------------------------------------------------------|
| `code.undo`        | `/code undo`                                         |
| `plugin.install`   | `/plugin install`                                     |
| `plugin.rm`        | `/plugin rm`                                          |
| `craft.adr`        | `/craft adr` (the file write)                        |
| `craft.claude-md`  | `/craft claude-md --fix` (the write path only — plain analysis is ungated) |
| `craft.scaffold`   | `/craft scaffold <template>`                          |

`/craft memory prune` and `/craft seed elevate` keep their own y/N confirm — they mutate Gateway-side memory state, not local files, and sit outside this gate for now.

Override the mode for one run with `DOJO_PERMISSIONS_MODE`, or use the `--yolo` flag (sets `yolo` in memory only for that run — never persisted to `settings.json` — and prints a loud stderr warning). A denied or declined action prints a one-line message naming both escape hatches: add the action (or a covering glob) to `permissions.allowed`, or re-run with `--yolo`.

## Guardrails

An advisory circuit breaker watches for repeated, identical failures and escalates a one-line notice — it never blocks or refuses an action, only gets louder. Inspired by Hermes' `tool_loop_guardrails`.

Two independent trackers share the same `guardrails` settings:

- **Slash commands** — consecutive failures of the same command, keyed by command name plus its first subcommand token (`/code test` and `/code build` count as separate streaks).
- **Chat-stream tool calls** — consecutive failures of the same tool with the same error signature during a direct (non-`/agent`) chat turn.

A success resets that streak to zero. Configure in `~/.dojo/settings.json`:

| Key                     | Default | Meaning                                                              |
|--------------------------|---------|------------------------------------------------------------------------|
| `guardrails.enabled`     | `true`  | Master on/off switch                                                    |
| `guardrails.warn_after`  | `3`     | Consecutive failures before the first notice                          |
| `guardrails.hard_after`  | `5`     | Consecutive failures before the escalated notice (repeats every time the streak is at or past this count) |

No environment override is provided for this section.

## Dojo Spirit

Dojo Spirit is the engagement system built into the CLI. It tracks XP, belt ranks, daily streaks, achievements, and unlockable koans — all stored locally in `~/.dojo/state.json`.

**Belt ladder:**

| Belt   | Title        | XP Required |
|--------|--------------|-------------|
| White  | Novice       | 0           |
| Yellow | Apprentice   | 1,000       |
| Orange | Initiate     | 3,000       |
| Green  | Practitioner | 6,000       |
| Blue   | Adept        | 10,000      |
| Purple | Sage         | 15,000      |
| Brown  | Master       | 25,000      |
| Black  | Grandmaster  | 50,000      |

XP is earned through guided tutorials (`/guide`), daily practice sessions, and regular CLI use. Belt promotions display inline in the REPL. `/card` shows your current rank, XP progress bar, session count, streak, and unlocked achievements. `/sensei` delivers a koan matched to your belt rank.

## Session Management

Sessions scope conversation history on the gateway. Each `dojo` invocation generates a session ID automatically. You can rotate, list, or resume sessions mid-session.

```
/session                     # show current session ID
/session new                 # rotate to a fresh session
/session ls                  # list recent sessions from local history
/session resume              # resume the most recently active session
/session resume dojo-cli-20260409-142301  # resume one specific session by ID
/session dojo-cli-20260409-142301         # switch directly (not verified against gateway)
```

Use `--resume` at startup to continue the most recent session automatically, or `--session <id>` to resume one specific session by ID (implies `--resume`).

## Design

The CLI renders in truecolor using the Dojo Genesis sunset palette: warm-amber (`#e8b04a`) for headers, golden-orange (`#f4a261`) for command names, cloud-gray (`#94a3b8`) for descriptions, soft-sage (`#7fb88c`) for success states, and info-steel (`#457b9d`) for tool and trace annotations. The sunset gradient (`#ffd166` → `#f4a261` → `#e76f51`) anchors the palette — the same gradient that runs across the web shell's dock brand mark and hover indicators.

Interactive panels (`/home`, `/pilot`, `/bloom`, `/warroom`) use Bubble Tea with alternate-screen mode. Plain-text fallbacks (`/home plain`, `/pilot plain`) are available for non-interactive or `--no-color` contexts. Truecolor rendering is provided by `lipgloss` and `gookit/color`; both degrade gracefully on terminals that report limited color support.

## Development

```bash
make test    # run tests
make vet     # go vet
make build   # build binary to ./bin/dojo
make all     # vet + test + build
```

Module path: `github.com/DojoGenesis/cli`

Key dependencies: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `fatih/color`, `gookit/color`, `chzyer/readline`.

## License

MIT
