#!/usr/bin/env bash
# smart-allow — smoke tests, drive the Go binary directly.
# Usage: ./tests/smoke.sh [--fast]
#   --fast : skip Ollama round-trip (only fast-path + fail-safe checks)
set -euo pipefail

FAST=0
if [ "${1:-}" = "--fast" ]; then FAST=1; fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"

# Resolve the binary: prefer locally-built one at repo root, fall back to installed one.
if [ -x "$REPO/classify-command" ]; then
    BIN="$REPO/classify-command"
elif [ -x "$HOME/.claude/bin/classify-command" ]; then
    BIN="$HOME/.claude/bin/classify-command"
else
    echo "FAIL: classify-command binary not found." >&2
    echo "      Build it with: devbox run -- task build" >&2
    exit 1
fi

# Isolate cache/log so we don't touch the user's global install.
TMP="$(mktemp -d)"
trap "rm -r '$TMP' 2>/dev/null" EXIT
export CLAUDE_CLASSIFIER_CACHE_DIR="$TMP/cache"
export CLAUDE_CLASSIFIER_LOG="$TMP/classifier.log"

pass() { printf '\033[1;32m  PASS\033[0m  %s\n' "$*"; }
fail() { printf '\033[1;31m  FAIL\033[0m  %s\n' "$*" >&2; exit 1; }

run_bin() {
    local cmd="$1"
    printf '{"tool_input":{"command":%s},"cwd":"/tmp"}' \
        "$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$cmd")" \
        | "$BIN"
}

echo "Using: $BIN"
echo ""

echo "== fast-path approve (ls) =="
out="$(run_bin 'ls -la')"
echo "  $out"
[[ "$out" == *'"permissionDecision":"allow"'* ]] && pass "allow via fast-path" || fail "expected allow, got: $out"

echo "== fast-path deny (rm -rf /) =="
out="$(run_bin 'rm -rf /')"
echo "  $out"
[[ "$out" == *'"permissionDecision":"deny"'* ]] && pass "deny via fast-path" || fail "expected deny, got: $out"

echo "== empty command =="
out="$(run_bin '')"
echo "  $out"
[[ "$out" == *'"permissionDecision":"allow"'* ]] && pass "empty command allowed" || fail "unexpected: $out"

if [ "$FAST" = 1 ]; then
    echo ""
    echo "(--fast: skipping Ollama round-trip)"
    exit 0
fi

OLLAMA="${OLLAMA_HOST:-http://127.0.0.1:11434}"
echo "== ollama reachable ($OLLAMA) =="
if curl -fsS -m 3 "$OLLAMA/api/tags" >/dev/null 2>&1; then
    pass "ollama endpoint OK"
else
    fail "ollama not reachable at $OLLAMA — start it or set OLLAMA_HOST"
fi

echo "== LLM path (kubectl apply -> ask) =="
out="$(OLLAMA_HOST="$OLLAMA" run_bin 'kubectl apply -f deploy.yaml')"
echo "  $out"
[[ "$out" == *'"permissionDecision":"ask"'* || "$out" == *'"permissionDecision":"deny"'* ]] \
    && pass "non-allow decision via LLM" \
    || fail "expected ask/deny, got: $out"

echo "== cache hit (same command again) =="
out2="$(OLLAMA_HOST="$OLLAMA" run_bin 'kubectl apply -f deploy.yaml')"
echo "  $out2"
[[ "$out2" == "$out" ]] && pass "cache returned identical decision" || fail "cache mismatch"

echo "== fail-safe (bad OLLAMA_HOST -> ask) =="
out="$(OLLAMA_HOST='http://127.0.0.1:1' run_bin 'npm install foo-unknown-package-xyz')"
echo "  $out"
[[ "$out" == *'"permissionDecision":"ask"'* ]] && pass "fail-safe fell back to ask" || fail "expected ask, got: $out"

echo ""
echo "All smoke checks passed."
