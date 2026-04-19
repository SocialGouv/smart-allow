#!/usr/bin/env bash
# Runs once when the devcontainer is first created.
#
# Prerequisite: devbox (pre-installed in jetpackio/devbox:latest).
# devbox.json pins the Go toolchain used to build the classifier.
set -euo pipefail

echo ">> install Claude Code CLI"
npm install -g @anthropic-ai/claude-code

echo ">> build classify-command via devbox (go toolchain)"
devbox run build
devbox run install-local

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

>> Useful devbox scripts:
     devbox run build            # rebuild the Go classifier
     devbox run install-local    # copy binary to ~/.claude/bin/classify-command
     devbox run test             # run Go tests
     devbox run smoke-project    # run the isolated smoke test
EOF
