#!/usr/bin/env bash
# Project-scoped hook wrapper: activates smart-allow whenever `claude` is run
# from this repo. Does NOT require install.sh to have been run — the user's
# global ~/.claude/settings.json stays untouched.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
CLASSIFIER="$REPO_ROOT/home/.claude/hooks/classify-command.py"

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$REPO_ROOT}"

# Container sets OLLAMA_HOST=http://host.docker.internal:11434 via containerEnv.
# On the host or anywhere it's unset, default to localhost.
: "${OLLAMA_HOST:=http://127.0.0.1:11434}"
export OLLAMA_HOST
export CLAUDE_CLASSIFIER_CACHE_DIR="$PROJECT_DIR/.claude/cache"
export CLAUDE_CLASSIFIER_LOG="$PROJECT_DIR/.claude/classifier.log"

exec python3 "$CLASSIFIER"
