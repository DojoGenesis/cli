# Dojo CLI

> Self-hosted agentic AI in your terminal. A remix of Claude Code's UX pattern powered by the [Dojo Genesis](https://github.com/DojoGenesis) gateway.

## Quick Start

```bash
# 1. Install
git clone https://github.com/DojoGenesis/dojo-cli && cd dojo-cli && make install

# 2. Point at your gateway
echo '{"gateway":{"url":"http://localhost:7340"}}' > ~/.dojo/settings.json

# 3. Run
dojo
```

## Installation

### From source

```bash
git clone https://github.com/DojoGenesis/dojo-cli
cd dojo-cli
make install
```

Requires Go 1.24+. The binary is installed to `$GOPATH/bin/dojo`.

### Pre-built binaries

```bash
curl -sSL https://raw.githubusercontent.com/DojoGenesis/dojo-cli/main/scripts/install.sh | bash
```

### Homebrew (coming soon)

```bash
brew install DojoGenesis/tap/dojo
```

## Configuration

Settings are loaded from `~/.dojo/settings.json`. Missing file is not an error — all fields have defaults.

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

| Variable              | Overrides                  |
|-----------------------|----------------------------|
| `DOJO_GATEWAY_URL`    | `gateway.url`              |
| `DOJO_GATEWAY_TOKEN`  | `gateway.token`            |
| `DOJO_PLUGINS_PATH`   | `plugins.path`             |
| `DOJO_PROVIDER`       | `defaults.provider`        |

## Commands

Type a message without `/` to chat with the gateway. Use slash commands for structured operations.

| Command                               | Description                                          |
|---------------------------------------|------------------------------------------------------|
| `/help`                               | Show available commands                              |
| `/health`                             | Gateway health and uptime stats                      |
| `/home`                               | Workspace state overview (TUI panel)                 |
| `/home plain`                         | Workspace state in plain text                        |
| `/model [ls]`                         | List available models and providers                  |
| `/model set <name>`                   | Switch active model (in-memory, current session)     |
| `/tools [ls]`                         | List registered MCP tools grouped by namespace       |
| `/agent ls`                           | List agents registered in the gateway                |
| `/agent dispatch <mode> <msg>`        | Create agent and stream response                     |
| `/agent chat <id> <msg>`              | Chat with an existing agent by ID                   |
| `/skill ls [filter]`                  | List skills, optionally filtered by name             |
| `/session`                            | Show active session ID                               |
| `/session new`                        | Start a fresh session                                |
| `/session <id>`                       | Resume a prior session by ID                         |
| `/run <task>`                         | Submit multi-step task to the gateway orchestrator   |
| `/garden ls`                          | List memory seeds                                    |
| `/garden stats`                       | Memory garden statistics                             |
| `/garden plant <text>`                | Plant a new seed into the garden                     |
| `/trail`                              | Show memory timeline                                 |
| `/trace`                              | Show trace context and gateway trace guidance        |
| `/pilot`                              | Live SSE event dashboard (Ctrl+C to stop)            |
| `/pilot plain`                        | Live event stream in plain text                      |
| `/hooks ls`                           | List loaded hook rules from plugins                  |
| `/hooks fire <event>`                 | Manually fire a hook event (for testing)             |
| `/settings`                           | Show config file path and all active settings        |
| `/practice`                           | Daily reflection prompts (rotates by day of week)    |
| `/projects ls`                        | Local workspace view — cwd, plugins, session         |

## Surfaces

Dojo CLI maps to the eight Dojo Genesis surfaces. Each surface has a corresponding command or flow:

| Surface     | Command / Flow              | Description                                      |
|-------------|-----------------------------|--------------------------------------------------|
| home        | `/home`                     | Workspace health, agent count, seed count        |
| partnership | chat (direct message)       | Primary conversational interface with the gateway|
| piloting    | `/pilot`                    | Live SSE event stream from the gateway           |
| garden      | `/garden`                   | Long-term memory seeds — list, plant, stats      |
| practice    | `/practice`                 | Day-rotating reflection prompts                  |
| projects    | `/projects`                 | Local working directory and plugin state         |
| pipelines   | `/run`                      | Multi-step orchestration tasks                   |
| trail       | `/trail`                    | Chronological memory timeline                    |

## Agent Dispatch

Dispatch an agent with an optional ADA disposition mode. If mode is omitted, `balanced` is used.

```
/agent dispatch focused summarize the last 5 decisions
```

Modes: `focused` | `balanced` | `exploratory` | `deliberate`

Example output:

```
  Creating agent (mode: focused)...
  Agent: a3f2b1c8  pacing=fast depth=shallow

  dojo  The last five decisions were...
```

Chat with an existing agent by ID:

```
/agent chat a3f2b1c8 what was your last tool call?
```

## Session Management

Sessions scope conversation history on the gateway side. Each `dojo` invocation generates a new session ID automatically.

```
/session                     # show current session ID
/session new                 # rotate to a fresh session
/session dojo-cli-20260409   # resume a specific session
```

Session IDs are formatted as `dojo-cli-YYYYMMDD-HHmmss` when created via `/session new`.

## CLI Flags

| Flag              | Description                                                        |
|-------------------|--------------------------------------------------------------------|
| `--gateway <url>` | Gateway URL (overrides `gateway.url` in settings)                  |
| `--token <tok>`   | Bearer token for gateway auth (overrides `gateway.token`)          |
| `--disposition`   | ADA disposition preset: `focused`, `balanced`, `exploratory`, `deliberate` |
| `--one-shot <msg>`| Execute a single message and exit (non-interactive)                |
| `--no-color`      | Disable color output                                               |
| `--completion`    | Generate shell completions: `bash`, `zsh`, or `fish`               |
| `--version`       | Print version and exit                                             |

**One-shot example:**

```bash
dojo --one-shot "what models are available?" --gateway http://localhost:7340
```

**Shell completions:**

```bash
# zsh
dojo --completion zsh >> ~/.zshrc

# bash
dojo --completion bash >> ~/.bashrc

# fish
dojo --completion fish > ~/.config/fish/completions/dojo.fish
```

## Plugin System

Plugins extend the CLI with hook rules. Place plugin directories under `~/.dojo/plugins/` (or the path set in `plugins.path`).

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

Inspect loaded plugins and their hook rules:

```
/hooks ls
/hooks fire session.start
```

Plugins loaded at startup are reported in `/settings` and `/projects`.

## Design

The CLI renders in truecolor using the Dojo Genesis sunset palette: warm-amber (`#e8b04a`) for headers, golden-orange (`#f4a261`) for command names, cloud-gray (`#94a3b8`) for descriptions, soft-sage (`#7fb88c`) for success states, and info-steel (`#457b9d`) for tool and trace annotations. Interactive panels (`/home`, `/pilot`) use Bubble Tea with alternate-screen mode. Plain-text fallbacks (`/home plain`, `/pilot plain`) are available for non-interactive or `--no-color` contexts. Truecolor rendering is provided by `lipgloss` and `gookit/color`; both degrade gracefully on terminals that report limited color support.

## Development

```bash
make test    # run tests
make vet     # go vet
make build   # build binary to ./bin/dojo
make all     # vet + test + build
```

Module path: `github.com/DojoGenesis/dojo-cli`

Key dependencies: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `fatih/color`, `gookit/color`, `chzyer/readline`.

## License

MIT
