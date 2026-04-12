#!/usr/bin/env bash
# dojo-cli/docs/testing-loop/run-tests.sh
#
# Automated smoke test runner for dojo-cli agent tool calls.
# Runs 5 cases via --one-shot --json --plain, checks that the agent emits
# tool_call events (confirming tool_choice:"required" is working and path
# resolution is correct), then runs a diagnostic sweep on any failures.
#
# Usage:
#   ./run-tests.sh
#   DOJO_BIN=/path/to/dojo ./run-tests.sh
#   DOJO_GATEWAY=http://localhost:7340 ./run-tests.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
GATEWAY="${DOJO_GATEWAY:-http://localhost:7340}"

# ─── Locate dojo binary ───────────────────────────────────────────────────────

locate_dojo() {
  if [[ -n "${DOJO_BIN:-}" ]]; then
    echo "$DOJO_BIN"
    return
  fi
  if command -v dojo &>/dev/null; then
    command -v dojo
    return
  fi
  local local_bin="$WORKSPACE_ROOT/cli/dojo"
  if [[ -x "$local_bin" ]]; then
    echo "$local_bin"
    return
  fi
  echo "  [build] dojo not found — building from $WORKSPACE_ROOT/cli ..." >&2
  (cd "$WORKSPACE_ROOT/cli" && go build -o /tmp/dojo-test-runner ./cmd/dojo/) >&2
  echo "/tmp/dojo-test-runner"
}

DOJO_BIN="$(locate_dojo)"

# ─── Timeout helper (macOS compatibility) ─────────────────────────────────────
# macOS does not ship GNU `timeout`. Use gtimeout (brew install coreutils) if
# available, otherwise implement a shell-native background-process timeout.

if command -v timeout &>/dev/null; then
  run_with_timeout() { timeout "$@"; }
elif command -v gtimeout &>/dev/null; then
  run_with_timeout() { gtimeout "$@"; }
else
  # Usage: run_with_timeout SECONDS CMD [ARGS...]
  run_with_timeout() {
    local secs=$1; shift
    "$@" &
    local pid=$!
    ( sleep "$secs" && kill "$pid" 2>/dev/null ) &
    local watcher=$!
    wait "$pid" 2>/dev/null
    local rc=$?
    kill "$watcher" 2>/dev/null
    wait "$watcher" 2>/dev/null
    return $rc
  }
fi

# ─── Gateway health check ─────────────────────────────────────────────────────

if ! curl -s --connect-timeout 3 --max-time 5 -o /dev/null "$GATEWAY/" 2>/dev/null; then
  echo ""
  echo "  ERROR: Gateway not reachable at $GATEWAY"
  echo "  Start it first:"
  echo "    cd $WORKSPACE_ROOT/AgenticGatewayByDojoGenesis && go run . &"
  echo ""
  exit 1
fi

# ─── Test definitions ─────────────────────────────────────────────────────────
# Each entry in MESSAGES is sent via --one-shot (no /run prefix needed).
# The agent receives tool_choice:"required" from the gateway on first iteration,
# so it must call a tool rather than respond with text.
#
# Workspace root (os.Getwd()) is automatically sent with every request.
# Tests are run from WORKSPACE_ROOT so relative paths resolve correctly.

declare -a NAMES=(
  "directory listing"
  "file read + field extraction"
  "web search"
  "write + read round-trip"
  "recursive file search"
)

declare -a MESSAGES=(
  "list the files in cli/internal/commands/"
  "read cli/internal/client/client.go and tell me what ChatRequest fields exist"
  'search the web for "Go context value injection pattern" and give me one key insight'
  'write /tmp/smoke-test.txt with the content "smoke test passed" then read it back and confirm the content'
  "find all files matching *.go in AgenticGatewayByDojoGenesis/tools/ and list their names"
)

declare -a EXPECTED_TOOLS=(
  "list_directory"
  "read_file"
  "web_search"
  "write_file"
  "search_files"
)

# ─── Run tests ────────────────────────────────────────────────────────────────

PASS=0
FAIL=0
declare -a FAIL_DETAILS=()
N=${#NAMES[@]}

# Run from workspace root so relative paths in messages resolve correctly.
cd "$WORKSPACE_ROOT"

echo ""
echo "  dojo agent tool smoke tests"
echo "  workspace : $WORKSPACE_ROOT"
echo "  binary    : $DOJO_BIN"
echo "  gateway   : $GATEWAY"
echo ""
printf "  %-32s  %-18s  %s\n" "Test case" "Expected tool" "Result"
printf "  %s\n" "$(printf '─%.0s' $(seq 1 68))"

for i in $(seq 0 $((N - 1))); do
  name="${NAMES[$i]}"
  msg="${MESSAGES[$i]}"
  expected="${EXPECTED_TOOLS[$i]}"

  tmpout="$(mktemp /tmp/dojo-test-XXXXXX.jsonl)"

  set +e
  run_with_timeout 120 "$DOJO_BIN" \
    --gateway "$GATEWAY" \
    --one-shot "$msg" \
    --json --plain \
    > "$tmpout" 2>&1
  rc=$?
  set -e

  # Count tool_call events in JSONL output.
  # ClassifyChunk maps SSE event:"tool_call" -> {"type":"tool_call",...}
  tool_calls="$(grep -c '"type":"tool_call"' "$tmpout" 2>/dev/null || echo 0)"

  if [[ $rc -ne 0 ]]; then
    result="FAIL (exit $rc)"
    FAIL=$((FAIL + 1))
    first_line="$(head -1 "$tmpout" 2>/dev/null || echo "(no output)")"
    FAIL_DETAILS+=("$name: process exited $rc — $first_line")
  elif [[ "$tool_calls" -eq 0 ]]; then
    result="FAIL (no tool calls)"
    FAIL=$((FAIL + 1))
    # Capture first text chunk for the diagnostic
    first_text="$(grep -o '"content":"[^"]*"' "$tmpout" 2>/dev/null | head -1 || echo "(no content)")"
    FAIL_DETAILS+=("$name: no tool_call events. Expected: $expected. Agent output started: $first_text")
  else
    result="PASS ($tool_calls call(s))"
    PASS=$((PASS + 1))
  fi

  rm -f "$tmpout"

  printf "  %-32s  %-18s  %s\n" "${name:0:32}" "$expected" "$result"
done

# ─── Summary ──────────────────────────────────────────────────────────────────

printf "  %s\n" "$(printf '─%.0s' $(seq 1 68))"
echo ""
echo "  $PASS/$N passed — $FAIL failed"
echo ""

[[ $FAIL -eq 0 ]] && exit 0

# ─── Diagnostic sweep ─────────────────────────────────────────────────────────
# For each failure, ask the agent to diagnose root cause and suggest a fix.
# Uses --plain (no --json) so output is human-readable.

echo "  Failure details:"
for d in "${FAIL_DETAILS[@]}"; do
  echo "    - $d"
done
echo ""
echo "  Running diagnostic sweep..."
echo ""

failure_block=""
for d in "${FAIL_DETAILS[@]}"; do
  failure_block+="  - $d
"
done

sweep_msg="These dojo-cli smoke tests failed. Each test sends a message via --one-shot and checks that the agent emitted at least one tool_call event in the JSONL output. A failure means the agent either crashed, returned a non-zero exit code, or responded with plain text instead of calling a tool.

Failures:
${failure_block}
Gateway: $GATEWAY
Workspace root: $WORKSPACE_ROOT

For each failure, diagnose the most likely root cause (e.g. gateway not loading tool_choice:required, path resolution broken, missing API key, provider routing issue) and suggest a specific fix."

tmpsweep="$(mktemp /tmp/dojo-sweep-XXXXXX.txt)"
set +e
run_with_timeout 120 "$DOJO_BIN" \
  --gateway "$GATEWAY" \
  --one-shot "$sweep_msg" \
  --plain \
  > "$tmpsweep" 2>&1
set -e

sed 's/^/    /' "$tmpsweep"
echo ""
rm -f "$tmpsweep"

exit $FAIL
