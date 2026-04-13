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
  }
}
```

**Environment variable overrides:**

| Variable               | Overrides           |
|------------------------|---------------------|
| `DOJO_GATEWAY_URL`     | `gateway.url`       |
| `DOJO_GATEWAY_TOKEN`   | `gateway.token`     |
| `DOJO_PLUGINS_PATH`    | `plugins.path`      |
| `DOJO_PROVIDER`        | `defaults.provider` |
| `DOJO_TELEMETRY_URL`   | telemetry API base  |
| `DOJO_SKILLS_PATH`     | default skill dir for `/skill package-all` |

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
| `/agent dispatch [mode] <msg>`               | Create agent and stream response                          |
| `/agent chat <id> <msg>`                     | Chat with an existing agent by ID                         |
| `/agent info <id>`                           | Show agent detail: disposition, channels, config          |
| `/agent channels <id>`                       | List bound channels for an agent                          |
| `/agent bind <id> <channel>`                 | Bind a channel to an agent                                |
| `/agent unbind <id> <channel>`               | Unbind a channel from an agent                            |
| `/run <task>`                                | Submit multi-step task; uses DAG templates or chat stream |
| `/workflow <name> [input-json]`              | Execute a named workflow and stream progress              |
| `/pilot`                                     | Live SSE event dashboard (Ctrl+C to stop)                 |
| `/pilot plain`                               | Live event stream in plain text                           |

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
| `/session <id>`         | Resume a prior session by ID             |

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
| `/plugin install <url>`   | Clone a plugin from a git URL into `~/.dojo/plugins/`  |
| `/plugin rm <name>`       | Remove an installed plugin                              |
| `/hooks ls`               | List loaded hook rules from all plugins                 |
| `/hooks fire <event>`     | Manually fire a hook event (for testing)                |

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

## Plugin System

Plugins extend the CLI with hook rules and skills. Place plugin directories under `~/.dojo/plugins/` (or the path configured in `plugins.path`), or use `/plugin install` to clone from a git URL.

Each plugin directory must contain a `plugin.json` manifest:

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "hooks": [
    {
      "event": "session.start",
      "type": "command",
      "command": "/usr/local/bin/my-hook"
    }
  ]
}
```

Plugin management commands:

```
/plugin ls                              # list installed plugins
/plugin install https://github.com/...  # clone a plugin from git
/plugin rm my-plugin                    # remove an installed plugin
/hooks ls                               # list all hook rules
/hooks fire session.start               # manually fire a hook event
```

Plugins are rescanned live after install and remove operations — no restart needed.

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

Sessions scope conversation history on the gateway. Each `dojo` invocation generates a session ID automatically. You can rotate or resume sessions mid-session.

```
/session                     # show current session ID
/session new                 # rotate to a fresh session
/session dojo-cli-20260409   # resume a specific session
```

Use `--resume` at startup to continue the most recent session automatically.

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
