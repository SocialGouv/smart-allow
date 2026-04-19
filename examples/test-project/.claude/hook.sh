#!/usr/bin/env bash
# Hook wrapper for the smart-allow test-project.
# Resolves the smart-allow repo root from this file's location, confines cache
# and log to the project's own .claude/ directory, then exec's the classifier.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"               # <repo>/examples/test-project/.claude
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"                           # <repo>
CLASSIFIER="$REPO_ROOT/home/.claude/hooks/classify-command.py"

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$HERE/.." && pwd)}"

export CLAUDE_CLASSIFIER_CACHE_DIR="$PROJECT_DIR/.claude/cache"
export CLAUDE_CLASSIFIER_LOG="$PROJECT_DIR/.claude/classifier.log"

exec python3 "$CLASSIFIER"
