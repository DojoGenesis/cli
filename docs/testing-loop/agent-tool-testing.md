# dojo-cli Agent Tool Testing

Structured test prompts organized by what they exercise, roughly in order of complexity.

Covers: `tool_choice: "required"`, file path resolution (workspace root fix), web search (SerpAPI), multi-step tool iteration, provider routing, error handling, and session continuity.

---

## 1. Baseline — Does the agent respond and call tools?

These confirm `tool_choice: "required"` is working and the agent acts rather than writes a plan.

```
/run list the files in the dojo-cli directory
```
Expected: `list_directory` tool called, returns actual file list.

```
/run what is in the tools/ directory of the gateway?
```
Expected: `list_directory` called on `AgenticGatewayByDojoGenesis/tools/`.

```
/run how many lines is cli/internal/repl/repl.go?
```
Expected: `read_file` called, agent counts lines or returns line count from tool result.

---

## 2. File path resolution (the fix we just shipped)

These directly test that relative paths resolve against the workspace root, not the gateway's CWD.

```
/run read the file cli/internal/commands/cmd_workflow.go and summarize what the /run command does
```
Expected: `read_file` succeeds, returns content of that file.

```
/run search for all .go files in cli/internal/commands/
```
Expected: `search_files` returns the command files.

```
/run read AgenticGatewayByDojoGenesis/tools/file_operations.go and tell me what the resolveFilePath function does
```
Expected: Reads the file, explains the function we just wrote.

```
/run what providers are supported? read AgenticGatewayByDojoGenesis/server/services/providers/ and list them
```
Expected: `list_directory` on providers dir, then reads files, lists anthropic/openai/groq/etc.

---

## 3. Write and verify (write_file round-trip)

```
/run create a file at /tmp/dojo-test.txt with the content "dojo file write test - 2026-04-10"
```
Expected: `write_file` succeeds at `/tmp/dojo-test.txt`.

```
/run write a file cli/scratch/hello.go with a simple Go hello world program, then read it back and confirm it was written correctly
```
Expected: Two tool calls — `write_file` then `read_file`. Tests multi-step + relative path write.

---

## 4. Web search (SerpAPI key configured)

```
/run search for "Go context.Context best practices" and summarize the top results
```
Expected: `web_search` called using SerpAPI (not DDG fallback), returns organic results.

```
/run what is the latest version of the Anthropic Claude API? search the web and tell me
```
Expected: Web search called, returns current info.

```
/run search for "duckduckgo instant answer api" and give me the URL for their documentation
```
Expected: Tests that SerpAPI returns richer results than DDG would have.

---

## 5. Multi-step agentic tasks (iteration across tool calls)

These require the agent to call multiple tools in sequence and synthesize results.

```
/run find all files named *.go in cli/internal/providers/ and tell me what each one does
```
Expected: `search_files` or `list_directory`, then `read_file` for each result.

```
/run check if there is a file called CLAUDE.md in the workspace root. if yes, summarize its Go development section
```
Expected: `read_file` on `CLAUDE.md`, then summarizes the Go section.

```
/run count how many TODO comments are in the dojo-cli codebase
```
Expected: `search_files` + `read_file` on multiple files, aggregates count.

---

## 6. Provider routing and model selection

```
/model set gpt-4o
/run read cli/go.mod and list all direct dependencies
```
Expected: GPT-4o is used, calls `read_file`, parses go.mod.

```
/model set claude-sonnet-4-6
/run what intent categories does the mini delegation agent classify? read the relevant source file
```
Expected: Sonnet used, finds and reads `mini_delegation_agent.go`, lists intents.

---

## 7. Error handling probes

These probe failure paths gracefully.

```
/run read the file this/path/does/not/exist.go
```
Expected: `read_file` returns `success: false`, agent reports the error cleanly (not silently).

```
/run search for *.rs files in the workspace
```
Expected: Returns empty or near-empty results (no Rust files), agent reports that cleanly.

---

## 8. Context and session continuity

```
/run list the files in cli/internal/tui/
```
Then, in the same session:
```
/run now read the home.go file from that directory and explain the View() function
```
Expected: Agent uses the path from prior turn without re-listing.

---

## Quick smoke test sequence (5 prompts, covers the main fixes)

Fast pass across the core fixes in order:

```
1. /run list cli/internal/commands/
2. /run read cli/internal/client/client.go and tell me what ChatRequest fields exist
3. /run search the web for "Go context value injection pattern" and give me one key insight
4. /run write /tmp/smoke-test.txt with "smoke test passed" then read it back
5. /run find all files matching *.go in AgenticGatewayByDojoGenesis/tools/ and list their names
```

Each one exercises a distinct fixed behavior: directory listing, file read with relative path, SerpAPI, write round-trip, and search.
