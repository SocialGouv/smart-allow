#!/usr/bin/env bash
# Runs once when the devcontainer is first created.
#
# Prerequisite: devbox (pre-installed in jetpackio/devbox:latest).
# devbox.json pins Go + go-task + Node.js; Taskfile.yml holds the build
# recipes.
set -euo pipefail

echo ">> install Claude Code CLI"
npm install -g @anthropic-ai/claude-code

echo ">> build and install classify-command via task (devbox-provided Go)"
devbox run -- task install

echo ">> link claude project history if host path differs from container path"
HOST_KEY="$(echo "${HOST_WORKSPACE:-}" | tr '/' '-')"
CONTAINER_KEY="$(echo "$PWD" | tr '/' '-')"
if [ -n "$HOST_KEY" ] && [ "$HOST_KEY" != "$CONTAINER_KEY" ] \
   && [ -d "$HOME/.claude/projects/$HOST_KEY" ]; then
    ln -sfn "$HOME/.claude/projects/$HOST_KEY" "$HOME/.claude/projects/$CONTAINER_KEY"
    echo "   linked $HOST_KEY -> $CONTAINER_KEY"
fi

cat <<'EOF'

>> smart-allow project-scoped hook is wired in .claude/settings.json
   (the global ~/.claude/settings.json is the host's and stays untouched)

>> done. Start Claude Code with:  claude

>> Useful commands (all via devbox + task):
     devbox run -- task build          # rebuild the Go classifier
     devbox run -- task install        # rebuild and copy to ~/.claude/bin/
     devbox run -- task test           # go test ./...
     devbox run -- task check          # lint + test
     devbox run -- task smoke:project  # isolated end-to-end against Ollama
     devbox run -- task --list-all     # discover all tasks
EOF
