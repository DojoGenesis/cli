# cli -- CLAUDE.md (nexus)

## Orientation

DojoGenesis CLI · Go 1.24 · module `github.com/DojoGenesis/cli` · remote `github.com/DojoGenesis/cli`
Terminal surface of the DojoGenesis platform: connects to the Agentic Gateway, streams chat + agent dispatch, exposes skill/CAS bridge, orchestration, memory garden, and plugin system -- all from a TUI REPL.
State: ALL TODO phases COMPLETE as of 2026-05-27. Shippable; `v1.0.0` tag is the next operator action.
Place in map: one of four active DojoGenesis products; `desktop/` subdir is HIBERNATED (revival conditions in `desktop/HIBERNATED.md`).

## Hot facts / FAQ

**Build / install**
- `make build` -- builds binary to `./dojo` (entrypoint: `./cmd/dojo/main.go`)
- `make install` -- installs to `$GOPATH/bin/dojo` (use this for local dev)
- `make all` -- runs vet + test + build in that order (pre-ship gate)
- `go build ./...` -- module-wide compile check
- `go test ./... -count=1 -race` -- full test suite with race detector (matches `make test`)

**Run**
- `dojo` -- interactive REPL (Bubble Tea TUI)
- `dojo --one-shot "message"` -- single message, exit 0/1; `--json` for pipeline output
- `dojo --resume` -- resume most recent session
- `dojo --gateway http://localhost:7340` -- override gateway URL per run

**Gateway dependency (as of 2026-06-11)**
- Default: `http://localhost:7340` -- must be running before `dojo` connects
- Config file: `~/.dojo/settings.json` (key: `gateway.url`; env override: `DOJO_GATEWAY_URL`)
- Server side: `C:\Users\cruzr\dojo-AgenticGateway\` (remote: `github.com/DojoGenesis/gateway`)
- Missing gateway = immediate REPL error; always confirm Gateway is up first

**Config gotcha**
- `~/.dojo/settings.json` missing is NOT an error -- all fields default safely
- Token: `~/.dojo/settings.json` `gateway.token` or `DOJO_GATEWAY_TOKEN`
- Skills path for `/skill package-all`: `DOJO_SKILLS_PATH` or pass dir arg inline

**Key slash commands (complete list in README.md)**
- `/skill package-all [dir]` -- walk dir for SKILL.md files, push all to CAS
- `/skill search <query>` -- semantic search across skills
- `/code gate` -- build + test + vet in one shot (use before any PR)
- `/code build` / `/code test [pkg]` / `/code vet` -- individual gates
- `/code diff [--full] [file]` -- git diff; `--full` for patch view
- `/agent dispatch [mode] <msg>` -- create agent, stream response
- `/run <task>` -- multi-step orchestration (gateway handles DAG internally)
- `/pilot` -- live SSE event dashboard (Ctrl+C to stop)
- `/workflow <name> [input-json]` -- execute named workflow
- `/disposition set <name>` -- ADA presets: focused / balanced / exploratory / deliberate

**Desktop subdir**
- `cli/desktop/` is HIBERNATED since 2026-04-11 -- do not build or modify
- Revival requires: CLI v1.0.0 tagged + Gateway API stable + user demand signal
- Shares parent `go.mod`; Wails v2.12.0 dep stays in `cli/go.mod`

## Router

| Task | Go to |
|------|-------|
| Go conventions, linting, module layout | `C:\Users\cruzr\zenflow-projects\CLAUDE.md` |
| Agent dispatch rules, Opus/Sonnet split | `C:\Users\cruzr\zenflow-projects\CLAUDE.md` |
| Port 7340 architecture, Gateway API surface | `C:\Users\cruzr\dojo-AgenticGateway\` |
| Skill/CAS design, CoworkPlugins format | `C:\Users\cruzr\TresPies-AI-Orchestration\MEMORY.md` (pointer: project_dojo_cli.md) |
| Desktop Wails v2 reference app | `C:\Users\cruzr\HTMLCraftStudio\` |
| Agentic stack ADRs (28 total) | `C:\Users\cruzr\AgenticStackOrchestration\decisions\` |
| Release + goreleaser config | `C:\Users\cruzr\cli\.goreleaser.yaml` |

## Rules that bite here

- Gateway must be running before any `dojo` command that touches the network -- config errors and gateway errors both exit 1 via `fatalf`; check Gateway first, not code (config-before-code rule)
- `make all` (vet + test + build) is the gate; never skip before any commit that touches `internal/`
- `desktop/` is HIBERNATED -- no writes, no builds; Wails dep stays in `go.mod` intentionally
- On Windows: `GOPATH/bin` may not be on PATH by default; verify `dojo` is reachable after `make install`

## Map

```
cli/
  cmd/dojo/main.go       -- entrypoint; flags + one-shot + REPL launch
  internal/
    client/              -- Gateway HTTP + SSE client
    repl/                -- interactive REPL and TUI
    commands/            -- slash command handlers
    skills/              -- /skill commands + CAS bridge
    plugins/             -- plugin scanner + installer
    hooks/               -- PreCommand/PostCommand hook runner
    orchestration/       -- /run + DAG (nlparse.go)
    project/             -- /project lifecycle
    spirit/              -- belt, XP, achievements, koans
    providers/           -- model provider routing (last modified 2026-06-08)
    artifacts/           -- artifact store (last modified 2026-06-09)
    config/              -- ~/.dojo/settings.json loader + DojoDir()
    state/               -- ~/.dojo/state.json (sessions, agents, XP)
  desktop/               -- HIBERNATED Wails v2 + Svelte 5 scaffold
  scripts/               -- install.sh + release helpers
  Makefile               -- build / test / vet / install targets
  .goreleaser.yaml       -- release config (Homebrew tap: DojoGenesis/tap/dojo)
```

## Bookend

Session harvest: any work on CLI command surface or Gateway protocol that reaches a decision should deposit a distillate to `C:\Users\cruzr\TresPies-AI-Orchestration\MEMORY.md` open items. Transcripts never; decisions always.
Convergence cadence applies (YELLOW >= 10 dirty sessions, RED >= 25).
