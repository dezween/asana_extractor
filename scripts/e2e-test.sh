#!/usr/bin/env bash
#
# End-to-end test suite for the Asana extractor binary.
#
# Builds the real binary and runs it as a subprocess for every case —
# no mocks, no unit-test harness. The "success path" case makes real
# requests against the Asana API using credentials from .env (if present);
# every other case exercises a specific failure mode (missing config,
# invalid config, invalid token, invalid workspace, ...).
#
# Usage: ./scripts/e2e-test.sh   (or: make e2e)

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BIN="$ROOT_DIR/bin/asana-extractor-e2e"
WORK_DIR="$(mktemp -d /tmp/asana-e2e.XXXXXX)"
PIDS=()

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

log()  { printf '[e2e] %s\n' "$*"; }
pass() { PASS_COUNT=$((PASS_COUNT + 1)); printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); printf '  \033[31mFAIL\033[0m  %s\n' "$1"; }
skip() { SKIP_COUNT=$((SKIP_COUNT + 1)); printf '  \033[33mSKIP\033[0m  %s\n' "$1"; }

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill -9 "$pid" >/dev/null 2>&1 || true
  done
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT INT TERM

# run_bg LOGFILE ENV_ASSIGNMENTS... -- ARGS...
# Starts $BIN in the background with the given extra env vars and args.
# Sets LAST_PID. ENV_ASSIGNMENTS is a space-separated "KEY=VAL" list
# (word-split deliberately — values here never contain spaces).
run_bg() {
  local logfile=$1; shift
  local -a env_assignments=()
  while [ "$1" != "--" ]; do
    env_assignments+=("$1")
    shift
  done
  shift # drop the --

  env "${env_assignments[@]}" "$BIN" "$@" >"$logfile" 2>&1 &
  LAST_PID=$!
  PIDS+=("$LAST_PID")
}

# wait_for_pattern LOGFILE PATTERN TIMEOUT_SECONDS
wait_for_pattern() {
  local logfile=$1 pattern=$2 timeout=$3 waited=0
  while [ "$waited" -lt "$timeout" ]; do
    grep -qE "$pattern" "$logfile" 2>/dev/null && return 0
    sleep 1
    waited=$((waited + 1))
  done
  return 1
}

# wait_for_exit PID TIMEOUT_SECONDS -> sets LAST_EXIT_CODE
wait_for_exit() {
  local pid=$1 timeout=$2 waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$timeout" ]; then
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
  done
  wait "$pid" 2>/dev/null
  LAST_EXIT_CODE=$?
  return 0
}

kill_and_wait() {
  local pid=$1 signal=${2:-INT} timeout=${3:-10}
  kill -"$signal" "$pid" 2>/dev/null || true
  if ! wait_for_exit "$pid" "$timeout"; then
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null
    LAST_EXIT_CODE=137
  fi
}

# ---------------------------------------------------------------------------
# setup
# ---------------------------------------------------------------------------

log "building binary..."
if ! go build -o "$BIN" ./cmd/backednsvc 2>"$WORK_DIR/build.log"; then
  fail "binary builds"
  cat "$WORK_DIR/build.log"
  echo
  echo "e2e suite aborted: build failed."
  exit 1
fi
pass "binary builds"

REAL_TOKEN=""
REAL_WORKSPACE=""
if [ -f "$ROOT_DIR/.env" ]; then
  # shellcheck disable=SC1091
  set -a; source "$ROOT_DIR/.env"; set +a
  REAL_TOKEN="${ASANA_TOKEN:-}"
  REAL_WORKSPACE="${ASANA_WORKSPACE_GID:-}"
fi

# ---------------------------------------------------------------------------
# Case 1: success path (real Asana API call)
# ---------------------------------------------------------------------------

log "case: success path"
if [ -z "$REAL_TOKEN" ] || [ -z "$REAL_WORKSPACE" ]; then
  skip "success path (no ASANA_TOKEN/ASANA_WORKSPACE_GID in .env to run against the real API)"
else
  out_dir="$WORK_DIR/success-output"
  logfile="$WORK_DIR/success.log"
  run_bg "$logfile" "ASANA_TOKEN=$REAL_TOKEN" "ASANA_WORKSPACE_GID=$REAL_WORKSPACE" "OUTPUT_DIR=$out_dir" -- --interval=30s

  if wait_for_pattern "$logfile" "extraction cycle completed successfully" 20; then
    pass "logs a completed extraction cycle"
  else
    fail "logs a completed extraction cycle (see $logfile)"
  fi

  user_files=$(find "$out_dir/users" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
  project_files=$(find "$out_dir/projects" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
  if [ "${user_files:-0}" -gt 0 ]; then
    pass "writes at least one user JSON file ($user_files found)"
  else
    fail "writes at least one user JSON file"
  fi
  if [ "${project_files:-0}" -gt 0 ]; then
    pass "writes at least one project JSON file ($project_files found)"
  else
    fail "writes at least one project JSON file"
  fi

  sample_file=$(find "$out_dir/users" -name '*.json' 2>/dev/null | head -1)
  if [ -n "$sample_file" ] && grep -q '"gid"' "$sample_file" && grep -q '"name"' "$sample_file"; then
    pass "written file has the expected JSON shape"
  else
    fail "written file has the expected JSON shape ($sample_file)"
  fi

  kill_and_wait "$LAST_PID" INT 10
  if [ "$LAST_EXIT_CODE" -eq 0 ]; then
    pass "exits 0 after SIGINT"
  else
    fail "exits 0 after SIGINT (got $LAST_EXIT_CODE)"
  fi
  if grep -q "shutdown complete" "$logfile"; then
    pass "logs graceful shutdown completion"
  else
    fail "logs graceful shutdown completion"
  fi
fi

# ---------------------------------------------------------------------------
# Case 2: missing ASANA_TOKEN -> fast config error, non-zero exit
# ---------------------------------------------------------------------------

log "case: missing ASANA_TOKEN"
logfile="$WORK_DIR/missing-token.log"
run_bg "$logfile" "ASANA_TOKEN=" "ASANA_WORKSPACE_GID=ws1" "OUTPUT_DIR=$WORK_DIR/out2" --
wait_for_exit "$LAST_PID" 5
if [ "${LAST_EXIT_CODE:-1}" -ne 0 ]; then
  pass "exits non-zero"
else
  fail "exits non-zero (got 0)"
fi
if grep -q "ASANA_TOKEN is required" "$logfile"; then
  pass "reports missing ASANA_TOKEN"
else
  fail "reports missing ASANA_TOKEN (see $logfile)"
fi

# ---------------------------------------------------------------------------
# Case 3: missing ASANA_WORKSPACE_GID -> fast config error, non-zero exit
# ---------------------------------------------------------------------------

log "case: missing ASANA_WORKSPACE_GID"
logfile="$WORK_DIR/missing-workspace.log"
run_bg "$logfile" "ASANA_TOKEN=some-token" "ASANA_WORKSPACE_GID=" "OUTPUT_DIR=$WORK_DIR/out3" --
wait_for_exit "$LAST_PID" 5
if [ "${LAST_EXIT_CODE:-1}" -ne 0 ]; then
  pass "exits non-zero"
else
  fail "exits non-zero (got 0)"
fi
if grep -q "ASANA_WORKSPACE_GID is required" "$logfile"; then
  pass "reports missing ASANA_WORKSPACE_GID"
else
  fail "reports missing ASANA_WORKSPACE_GID (see $logfile)"
fi

# ---------------------------------------------------------------------------
# Case 4: invalid EXTRACT_INTERVAL env value -> fast config error
# ---------------------------------------------------------------------------

log "case: invalid EXTRACT_INTERVAL"
logfile="$WORK_DIR/invalid-interval.log"
run_bg "$logfile" "ASANA_TOKEN=some-token" "ASANA_WORKSPACE_GID=ws1" "EXTRACT_INTERVAL=not-a-duration" "OUTPUT_DIR=$WORK_DIR/out4" --
wait_for_exit "$LAST_PID" 5
if [ "${LAST_EXIT_CODE:-1}" -ne 0 ]; then
  pass "exits non-zero"
else
  fail "exits non-zero (got 0)"
fi
if grep -q "invalid EXTRACT_INTERVAL" "$logfile"; then
  pass "reports invalid EXTRACT_INTERVAL"
else
  fail "reports invalid EXTRACT_INTERVAL (see $logfile)"
fi

# ---------------------------------------------------------------------------
# Case 5: invalid --interval CLI flag -> flag-parsing error, exit code 2
# ---------------------------------------------------------------------------

log "case: invalid --interval flag"
logfile="$WORK_DIR/invalid-flag.log"
run_bg "$logfile" "ASANA_TOKEN=some-token" "ASANA_WORKSPACE_GID=ws1" "OUTPUT_DIR=$WORK_DIR/out5" -- --interval=not-a-duration
wait_for_exit "$LAST_PID" 5
if [ "${LAST_EXIT_CODE:-0}" -eq 2 ]; then
  pass "exits with flag-parsing error code 2"
else
  fail "exits with flag-parsing error code 2 (got ${LAST_EXIT_CODE:-unknown})"
fi

# ---------------------------------------------------------------------------
# Case 6: invalid token against the real API -> 401, cycle fails but process
# keeps running (errors in a cycle must not crash the periodic loop)
# ---------------------------------------------------------------------------

log "case: invalid Asana token (live 401)"
if [ -z "$REAL_WORKSPACE" ]; then
  skip "invalid token case (no ASANA_WORKSPACE_GID in .env to run against the real API)"
else
  logfile="$WORK_DIR/invalid-token.log"
  run_bg "$logfile" "ASANA_TOKEN=invalid-token-deliberately-wrong" "ASANA_WORKSPACE_GID=$REAL_WORKSPACE" "OUTPUT_DIR=$WORK_DIR/out6" -- --interval=30s

  if wait_for_pattern "$logfile" "extraction cycle failed.*status 401" 20; then
    pass "logs a failed cycle with status 401"
  else
    fail "logs a failed cycle with status 401 (see $logfile)"
  fi

  if kill -0 "$LAST_PID" 2>/dev/null; then
    pass "process keeps running after a failed cycle (does not crash)"
  else
    fail "process keeps running after a failed cycle (it exited)"
  fi

  kill_and_wait "$LAST_PID" INT 10
  if [ "$LAST_EXIT_CODE" -eq 0 ]; then
    pass "still shuts down cleanly after a failed cycle"
  else
    fail "still shuts down cleanly after a failed cycle (got $LAST_EXIT_CODE)"
  fi
fi

# ---------------------------------------------------------------------------
# Case 7: nonexistent workspace GID against the real API -> 404, cycle fails
# ---------------------------------------------------------------------------

log "case: nonexistent workspace GID (live 404)"
if [ -z "$REAL_TOKEN" ]; then
  skip "nonexistent workspace case (no ASANA_TOKEN in .env to run against the real API)"
else
  logfile="$WORK_DIR/invalid-workspace.log"
  run_bg "$logfile" "ASANA_TOKEN=$REAL_TOKEN" "ASANA_WORKSPACE_GID=0000000000000000" "OUTPUT_DIR=$WORK_DIR/out7" -- --interval=30s

  if wait_for_pattern "$logfile" "extraction cycle failed" 20; then
    pass "logs a failed cycle for a nonexistent workspace"
  else
    fail "logs a failed cycle for a nonexistent workspace (see $logfile)"
  fi

  kill_and_wait "$LAST_PID" INT 10
fi

# ---------------------------------------------------------------------------
# summary
# ---------------------------------------------------------------------------

echo
log "results: $PASS_COUNT passed, $FAIL_COUNT failed, $SKIP_COUNT skipped"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
