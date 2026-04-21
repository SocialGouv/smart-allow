#!/usr/bin/env bash
# Runs once when the devcontainer is first created.
#
# Prerequisite: devbox (pre-installed in jetpackio/devbox:latest).
# devbox.json pins Go + go-task + Node.js; Taskfile.yml holds the build
# recipes.
set -euo pipefail

echo ">> install Claude Code CLI"
npm install -g @anthropic-ai/claude-code

echo ">> build smart-allow and wire it into this project's .claude/settings.json"
devbox run -- task install:project

echo ">> link claude project history if host path differs from container path"
HOST_KEY="$(echo "${HOST_WORKSPACE:-}" | tr '/' '-')"
CONTAINER_KEY="$(echo "$PWD" | tr '/' '-')"
if [ -n "$HOST_KEY" ] && [ "$HOST_KEY" != "$CONTAINER_KEY" ] \
   && [ -d "$HOME/.claude/projects/$HOST_KEY" ]; then
    ln -sfn "$HOME/.claude/projects/$HOST_KEY" "$HOME/.claude/projects/$CONTAINER_KEY"
    echo "   linked $HOST_KEY -> $CONTAINER_KEY"
fi

cat <<'EOF'

>> smart-allow hook is wired at project scope (.claude/settings.json in this repo).
   It fires only when Claude Code runs from this workspace — your host's
   global ~/.claude/settings.json is left untouched.

>> done. Start Claude Code with:  claude

>> Useful commands (via devbox + task):
     devbox run -- task install:status  # show where the hook is installed
     devbox run -- task install:global  # also register it for every Claude Code session
     devbox run -- task uninstall       # interactive removal
     devbox run -- task build           # rebuild after Go source changes
     devbox run -- task test            # go test ./...
     devbox run -- task check           # lint + test
     devbox run -- task smoke:project   # isolated end-to-end against Ollama
     devbox run -- task --list-all      # discover all tasks
EOF
