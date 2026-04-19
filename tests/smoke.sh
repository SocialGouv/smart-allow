#!/usr/bin/env bash
# smart-allow — smoke tests.
# Usage: ./tests/smoke.sh [--fast]
#   --fast : skip Ollama round-trip (only fast-path checks)
set -euo pipefail

FAST=0
if [ "${1:-}" = "--fast" ]; then FAST=1; fi

HOOK="$HOME/.claude/hooks/classify-command.py"
if [ ! -x "$HOOK" ]; then
    echo "FAIL: $HOOK not executable (did install.sh run?)" >&2
    exit 1
fi

pass() { printf '\033[1;32m  PASS\033[0m  %s\n' "$*"; }
fail() { printf '\033[1;31m  FAIL\033[0m  %s\n' "$*" >&2; exit 1; }

run_hook() {
    local cmd="$1"
    printf '{"tool_input":{"command":%s},"cwd":"/tmp"}' "$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$cmd")" \
        | python3 "$HOOK"
}

echo "== fast-path approve (ls) =="
out="$(run_hook 'ls -la')"
echo "  $out"
[[ "$out" == *'"approve"'* ]] && pass "approve via fast-path" || fail "expected approve, got: $out"

echo "== fast-path deny (rm -rf /) =="
out="$(run_hook 'rm -rf /')"
echo "  $out"
[[ "$out" == *'"deny"'* ]] && pass "deny via fast-path" || fail "expected deny, got: $out"

echo "== empty command =="
out="$(run_hook '')"
echo "  $out"
[[ "$out" == *'"approve"'* ]] && pass "empty command approved" || fail "unexpected: $out"

if [ "$FAST" = 1 ]; then
    echo "(skipping LLM/cache/fail-safe — --fast)"
    exit 0
fi

OLLAMA="${OLLAMA_HOST:-http://host.docker.internal:11434}"
echo "== ollama reachable ($OLLAMA) =="
if curl -fsS -m 3 "$OLLAMA/api/tags" >/dev/null 2>&1; then
    pass "ollama endpoint OK"
else
    fail "ollama not reachable at $OLLAMA — start it or set OLLAMA_HOST"
fi

echo "== LLM path (kubectl apply -> ask) =="
out="$(run_hook 'kubectl apply -f deploy.yaml')"
echo "  $out"
[[ "$out" == *'"ask"'* || "$out" == *'"deny"'* ]] && pass "non-approve decision via LLM" || fail "expected ask/deny, got: $out"

echo "== cache hit (same command again) =="
out2="$(run_hook 'kubectl apply -f deploy.yaml')"
echo "  $out2"
[[ "$out2" == "$out" ]] && pass "cache returned identical decision" || fail "cache mismatch"

echo "== fail-safe (bad OLLAMA_HOST -> ask) =="
out="$(OLLAMA_HOST='http://127.0.0.1:1' run_hook 'npm install foo-unknown-package-xyz')"
echo "  $out"
[[ "$out" == *'"ask"'* ]] && pass "fail-safe fell back to ask" || fail "expected ask, got: $out"

echo ""
echo "All smoke checks passed."
