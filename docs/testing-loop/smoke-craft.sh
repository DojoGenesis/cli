#!/usr/bin/env bash
# dojo-cli/docs/testing-loop/smoke-craft.sh
#
# Smoke tests for the /craft command group.
# Pipes slash commands via REPL stdin and checks output.
#
# Usage:
#   ./smoke-craft.sh              # run all tests
#   ./smoke-craft.sh --offline    # skip Gateway-dependent tests
#   DOJO_BIN=/path/to/dojo ./smoke-craft.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GATEWAY="${DOJO_GATEWAY:-http://localhost:7340}"
OFFLINE=false

for arg in "$@"; do
  [[ "$arg" == "--offline" ]] && OFFLINE=true
done

# ─── Locate/build dojo binary ────────────────────────────────────────────────

locate_dojo() {
  if [[ -n "${DOJO_BIN:-}" ]]; then echo "$DOJO_BIN"; return; fi
  echo "  [build] building dojo from $CLI_ROOT ..." >&2
  (cd "$CLI_ROOT" && go build -o /tmp/dojo-craft-smoke ./cmd/dojo/) >&2
  echo "/tmp/dojo-craft-smoke"
}

DOJO_BIN="$(locate_dojo)"

TMPDIR="$(mktemp -d /tmp/dojo-craft-smoke-XXXXXX)"
trap 'rm -rf "$TMPDIR"' EXIT

PASS=0
FAIL=0
SKIP=0
declare -a FAIL_DETAILS=()

pass() { PASS=$((PASS + 1)); printf "  %-40s  %s\n" "$1" "PASS"; }
fail() { FAIL=$((FAIL + 1)); FAIL_DETAILS+=("$1: $2"); printf "  %-40s  %s\n" "$1" "FAIL — $2"; }
skip() { SKIP=$((SKIP + 1)); printf "  %-40s  %s\n" "$1" "SKIP (offline)"; }

# Run a slash command in the REPL via stdin, capture output.
run_cmd() {
  local cmd="$1"
  local tmpout="$TMPDIR/cmd-out-$$"
  echo "$cmd" | "$DOJO_BIN" --gateway "$GATEWAY" --plain 2>/dev/null > "$tmpout" || true
  cat "$tmpout"
  rm -f "$tmpout"
}

gateway_up() {
  $OFFLINE && return 1
  curl -s --connect-timeout 2 --max-time 3 -o /dev/null "$GATEWAY/health" 2>/dev/null
}

echo ""
echo "  /craft smoke tests"
echo "  binary  : $DOJO_BIN"
echo "  gateway : $GATEWAY"
echo "  offline : $OFFLINE"
echo ""
printf "  %-40s  %s\n" "Test" "Result"
printf "  %s\n" "$(printf '─%.0s' $(seq 1 55))"

# ─── Test 1: /craft help ─────────────────────────────────────────────────────

out=$(run_cmd "/craft")
if echo "$out" | grep -q "DojoCraft"; then
  pass "/craft help"
else
  fail "/craft help" "missing DojoCraft header"
fi

# ─── Test 2: /craft view ─────────────────────────────────────────────────────

cd "$CLI_ROOT"
out=$(run_cmd "/craft view .")
if echo "$out" | grep -q "Codebase View"; then
  pass "/craft view ."
else
  fail "/craft view ." "missing Codebase View header"
fi

if echo "$out" | grep -qi "go.mod\|Go module"; then
  pass "/craft view: go.mod detected"
else
  fail "/craft view: go.mod detected" "go.mod not in output"
fi

if echo "$out" | grep -qi "main\|Entry"; then
  pass "/craft view: entry points"
else
  fail "/craft view: entry points" "no entry points"
fi

# ─── Test 3: /craft scaffold ─────────────────────────────────────────────────

out=$(run_cmd "/craft scaffold")
if echo "$out" | grep -q "go-service"; then
  pass "/craft scaffold: lists templates"
else
  fail "/craft scaffold: lists templates" "missing templates"
fi

cd "$TMPDIR" && mkdir -p scaffold-test && cd scaffold-test
out=$(run_cmd "/craft scaffold orchestration")
if [[ -d "$TMPDIR/scaffold-test/decisions" ]]; then
  pass "/craft scaffold orchestration"
else
  fail "/craft scaffold orchestration" "no decisions/ dir"
fi

# ─── Test 4: /craft converge ─────────────────────────────────────────────────

cd "$CLI_ROOT"
out=$(run_cmd "/craft converge")
if echo "$out" | grep -qE "RED|YELLOW|GREEN"; then
  pass "/craft converge: signal"
else
  fail "/craft converge: signal" "no signal"
fi

if echo "$out" | grep -q "dirty"; then
  pass "/craft converge: metrics"
else
  fail "/craft converge: metrics" "no dirty count"
fi

# ─── Test 5: error handling ──────────────────────────────────────────────────

out=$(run_cmd "/craft nonexistent")
if echo "$out" | grep -qi "unknown\|try:"; then
  pass "/craft unknown subcommand"
else
  fail "/craft unknown subcommand" "no error"
fi

# ─── Test 6: Gateway-dependent tests ────────────────────────────────────────

if gateway_up; then
  out=$(run_cmd "/craft memory ls")
  if echo "$out" | grep -qiE "Memory|memories"; then
    pass "/craft memory ls"
  else
    fail "/craft memory ls" "unexpected output"
  fi

  out=$(run_cmd "/craft seed ls")
  if echo "$out" | grep -qiE "Seeds|Garden|empty"; then
    pass "/craft seed ls"
  else
    fail "/craft seed ls" "unexpected output"
  fi
else
  skip "/craft memory ls"
  skip "/craft seed ls"
fi

# ─── Summary ─────────────────────────────────────────────────────────────────

TOTAL=$((PASS + FAIL + SKIP))
printf "  %s\n" "$(printf '─%.0s' $(seq 1 55))"
echo ""
echo "  $PASS passed / $FAIL failed / $SKIP skipped  ($TOTAL total)"
echo ""

if [[ $FAIL -gt 0 ]]; then
  echo "  Failures:"
  for d in "${FAIL_DETAILS[@]}"; do
    echo "    - $d"
  done
  echo ""
fi

exit $FAIL
