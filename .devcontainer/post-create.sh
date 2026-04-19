#!/usr/bin/env bash
# Runs once when the devcontainer is first created.
# Installs Claude Code CLI + Python deps for the smart-allow classifier,
# then sets up the project-history symlink trick (host/container path diff).
set -euo pipefail

echo ">> install Claude Code CLI"
npm install -g @anthropic-ai/claude-code

echo ">> ensure python3 + pip + requests"
# The devbox image ships python3 without the pip module, so check for pip
# specifically, not just python3.
if ! python3 -m pip --version >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get update
        sudo apt-get install -y --no-install-recommends python3 python3-pip
    else
        echo "!! pip not found and apt-get unavailable — install python3-pip manually" >&2
        exit 1
    fi
fi
# PEP 668: newer distros refuse user installs into system python without --break-system-packages.
python3 -m pip install --user --break-system-packages requests 2>/dev/null \
    || python3 -m pip install --user requests

echo ">> link claude project history if host path differs from container path"
HOST_KEY="$(echo "${HOST_WORKSPACE:-}" | tr '/' '-')"
CONTAINER_KEY="$(echo "$PWD" | tr '/' '-')"
if [ -n "$HOST_KEY" ] && [ "$HOST_KEY" != "$CONTAINER_KEY" ] \
   && [ -d "$HOME/.claude/projects/$HOST_KEY" ]; then
    ln -sfn "$HOME/.claude/projects/$HOST_KEY" "$HOME/.claude/projects/$CONTAINER_KEY"
    echo "   linked $HOST_KEY -> $CONTAINER_KEY"
fi

echo ""
echo ">> smart-allow project-scoped hook: $(pwd)/.claude/settings.json"
echo "   (global ~/.claude/settings.json is mounted from host and left untouched)"
echo ""
echo ">> done. Start Claude Code with:  claude"
