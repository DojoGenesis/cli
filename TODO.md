# Dojo CLI -- Integration Todo

## Status: ALL PHASES COMPLETE. Shippable.

---

## Resolved Items (verified against source)

The following items have been verified as implemented and working:

- **Session ID bug (was CRITICAL BUG)** -- RESOLVED. `ChatRequest.SessionID` field in `client.go` line 303, generated at startup in `repl.New()` (`dojo-cli-YYYYMMDD-HHMMSS` format), passed in every `ChatStream` call, printed in welcome banner, `/session` command with `new` and `<id>` subcommands.
- **Integration Spec 1 (Session Continuity)** -- DONE. All acceptance criteria met.
- **Integration Spec 2 (Agent Dispatch)** -- client layer AND command layer DONE. `/agent dispatch`, `/agent chat`, `/agent ls`, `/agent info`, `/agent channels`, `/agent bind`, `/agent unbind` all fully wired in `cmd_agent.go`. Tool call display (`[Tool: tool_name]`) implemented in `streamAgentChat()`.
- **Integration Spec 3 (Orchestration)** -- client layer DONE, `/run` command DONE (MVP approach: routes through ChatStream, gateway handles orchestration internally).
- **Phase 2 work** -- ALL items complete (plugins scanner, hooks runner, `/hooks`, `/garden`, `/trail`, `/pilot`, `/trace`, `/practice`).
- **One-shot mode** -- `dojo --one-shot "task"` implemented in `cmd/dojo/main.go` lines 79-108.
- **Shell completions** -- `dojo completion zsh|bash|fish` implemented in `cmd/dojo/main.go` lines 117-176.
- **Version as `var`** -- `main.go` line 20.
- **Agent persistence** -- `state.AddAgent()`, `state.TouchAgent()`, `state.RecentAgents()` all implemented in `internal/state/state.go` with tests. Agent IDs and modes are stored in `~/.dojo/state.json` across REPL sessions.
- **Exit codes** -- Exit 0 (success), Exit 1 (gateway/config error) via `fatalf`. Correct and standard.

---

## Integration Spec 1 -- Session Continuity (DONE)

All acceptance criteria met:

- [x] Generate session ID at startup (`repl.go` line 56)
- [x] Print session ID in welcome banner (`repl.go` lines 467-470)
- [x] Persist session across turns -- `SessionID: r.session` in every `ChatStream` call
- [x] `/session` command: show, `new`, and resume by ID
- [x] `UserID` field on `ChatRequest` (line 304)
- [x] `Auth.UserID` in config -- `config.go:26`, env override `DOJO_USER_ID`, tested

---

## Integration Spec 2 -- Agent Dispatch (DONE)

Client layer complete:

- [x] `CreateAgent()` -- `client.go` line 466
- [x] `AgentChatStream()` -- `client.go` line 490
- [x] `CreateAgentRequest` / `CreateAgentResponse` / `AgentChatRequest` types

Command layer complete:

- [x] `/agent ls` -- lists agents from gateway + recent local agents from state
- [x] `/agent dispatch <mode> <msg>` -- creates agent, persists to state, streams response
- [x] `/agent chat <id> <msg>` -- chats with existing agent, updates `last_used` in state
- [x] `/agent info <id>` -- shows agent detail (disposition, channels, config)
- [x] `/agent channels <id>` -- lists bound channels
- [x] `/agent bind <id> <ch>` / `/agent unbind <id> <ch>` -- channel management
- [x] Tool call display (`[Tool: tool_name]` + `[Thinking]` events) in `streamAgentChat()`

---

## Integration Spec 3 -- Orchestration Dispatch (DONE -- MVP)

Client layer complete:

- [x] `Orchestrate()` -- `client.go` line 554
- [x] `OrchestrationDAG()` -- `client.go` line 573
- [x] `OrchestrateRequest`, `ExecutionPlan`, `ToolInvocation`, `OrchestrationStatus`, `DAGStatus` types

Command layer (MVP):

- [x] `/run <task>` -- routes through ChatStream; gateway handles orchestration internally (`cmd_workflow.go` lines 85-127)
- [x] DAG status polling with live node display -- shipped as `pollDAGUntilTerminal` (`cmd_workflow.go` line 263); the "not needed for MVP" note here was stale

---

## Phase 2 work (DONE)

All items completed:

- [x] `internal/plugins/scanner.go` -- CoworkPlugins format scanner
- [x] `internal/hooks/runner.go` -- PreCommand/PostCommand hook runner
- [x] `/hooks ls` and `/hooks fire` commands
- [x] `/garden plant <text>` (POST /v1/seeds)
- [x] `/trail` (GET /v1/memory timeline)
- [x] `/pilot` (live SSE event dashboard -- plain text mode + Bubbletea TUI)
- [x] `/trace` (trace inspection)
- [x] `/practice` (daily reflections, rotates by day of week)
- [x] `/workflow <name>` (workflow execution with SSE streaming)

---

## Phase 3 (remaining)

| # | Item | Priority | Effort | Acceptance Criteria |
|---|------|----------|--------|---------------------|
| 3.4 | DAG construction from natural language | P3 | 4-6h | DONE — `nlparse.go` with 12 verb-to-tool mappings, `--dag` flag on `/run`, 42 test cases passing |

**Done:**
- `dojo --one-shot "task"` flag -- implemented in `cmd/dojo/main.go` lines 79-108
- Shell completions (`dojo completion zsh|bash|fish`) -- implemented in `cmd/dojo/main.go` lines 117-176
- Version as `var` not `const` -- `main.go` line 20
- `/agent dispatch` and `/agent chat` subcommand wiring -- fully implemented in `cmd_agent.go`
- `/run` command handler -- implemented as MVP in `cmd_workflow.go`
- Agent persistence across REPL sessions -- `state.AddAgent()` / `state.TouchAgent()` / `state.RecentAgents()` in `internal/state/state.go`
- `~/.dojo/plugins/` auto-install from git URL -- `installer.go` + `InstallConfirmed()` in `cmd_plugin.go`
- `Auth.UserID` in config -- `config.go:26`, env override `DOJO_USER_ID`, tested
- Disposition profiles in `settings.json` -- `DispositionProfiles` map in Config, `/settings profile set|ls|show|create`, `MergeConfigProfiles()`, round-trip tested

---

## One-shot exit codes (VERIFIED CORRECT)

The one-shot path in `cmd/dojo/main.go` already has correct exit code behavior:

- **Exit 0:** success (line 107 -- normal `return`)
- **Exit 1:** gateway/streaming error (line 105 -- `fatalf("one-shot error: %s", err)`)
- **Exit 1:** config error (line 52 -- `fatalf("config error: %s", err)`)

Config errors and gateway errors both exit 1 via `fatalf`. Differentiating exit 2 for config errors would require replacing the shared `fatalf` function with a code-aware variant. The current behavior is standard (all errors = exit 1) and correct for CLI tools. No code change needed.
