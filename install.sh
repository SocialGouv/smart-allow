#!/usr/bin/env bash
# smart-allow — install the Ollama-backed Claude Code classifier into ~/.claude/
# Usage: ./install.sh [--symlink] [--dry-run] [--no-path-update] [--force]
set -euo pipefail

MODE="copy"
DRY_RUN=0
NO_PATH_UPDATE=0
FORCE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --symlink)        MODE="symlink" ;;
        --dry-run)        DRY_RUN=1 ;;
        --no-path-update) NO_PATH_UPDATE=1 ;;
        --force)          FORCE=1 ;;
        -h|--help)
            sed -n '2,4p' "$0"
            exit 0
            ;;
        *)
            echo "Unknown flag: $1" >&2
            exit 1
            ;;
    esac
    shift
done

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$REPO_DIR/home/.claude"
DEST_DIR="$HOME/.claude"
STAMP="$(date +%Y%m%d-%H%M%S)"

say()  { printf '\033[1;34m[install]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[install]\033[0m %s\n' "$*" >&2; }
run()  { if [ "$DRY_RUN" = 1 ]; then echo "  would: $*"; else eval "$@"; fi; }

if ! command -v python3 >/dev/null 2>&1; then
    warn "python3 not found — required for the classifier and for merging settings.json."
    exit 1
fi

say "source: $SRC_DIR"
say "target: $DEST_DIR"
if [ "$DRY_RUN" = 1 ]; then
    say "mode:   $MODE (dry-run)"
else
    say "mode:   $MODE"
fi

mkdir -p "$DEST_DIR/hooks" "$DEST_DIR/policies" "$DEST_DIR/bin"

install_file() {
    local src="$1" dest="$2" kind="$3"  # kind = hook|policy|bin
    if [ -e "$dest" ] || [ -L "$dest" ]; then
        if [ "$kind" = "policy" ] && [ "$FORCE" = 0 ]; then
            say "skip policy (already exists, use --force to overwrite): $dest"
            return 0
        fi
        if [ "$FORCE" = 0 ]; then
            run "cp -a '$dest' '$dest.bak-$STAMP'"
        fi
        run "rm -f '$dest'"
    fi
    if [ "$MODE" = "symlink" ]; then
        run "ln -s '$src' '$dest'"
    else
        run "cp '$src' '$dest'"
    fi
}

for f in "$SRC_DIR"/hooks/*; do
    install_file "$f" "$DEST_DIR/hooks/$(basename "$f")" hook
done

for f in "$SRC_DIR"/policies/*.md; do
    install_file "$f" "$DEST_DIR/policies/$(basename "$f")" policy
done

for f in "$SRC_DIR"/bin/*; do
    install_file "$f" "$DEST_DIR/bin/$(basename "$f")" bin
done

run "chmod +x '$DEST_DIR/hooks/classify-command.py' '$DEST_DIR/bin/claude-policy'"

say "merging hook into $DEST_DIR/settings.json"
SNIPPET="$SRC_DIR/settings.json.snippet"
SETTINGS="$DEST_DIR/settings.json"

if [ "$DRY_RUN" = 1 ]; then
    echo "  would merge snippet $SNIPPET into $SETTINGS"
else
    if [ -f "$SETTINGS" ]; then
        cp -a "$SETTINGS" "$SETTINGS.bak-$STAMP"
    fi
    python3 - "$SNIPPET" "$SETTINGS" <<'PY'
import json, sys, os
snippet_path, settings_path = sys.argv[1], sys.argv[2]
with open(snippet_path) as f:
    snippet = json.load(f)
if os.path.exists(settings_path):
    with open(settings_path) as f:
        settings = json.load(f)
else:
    settings = {}

settings.setdefault("hooks", {}).setdefault("PreToolUse", [])
sentinel = "classify-command.py"
already = False
for matcher_entry in settings["hooks"]["PreToolUse"]:
    for h in matcher_entry.get("hooks", []):
        if sentinel in h.get("command", ""):
            already = True
            break
if not already:
    settings["hooks"]["PreToolUse"].extend(snippet["hooks"]["PreToolUse"])
    with open(settings_path, "w") as f:
        json.dump(settings, f, indent=2)
    print(f"  hook added to {settings_path}")
else:
    print(f"  hook already registered in {settings_path} (no change)")
PY
fi

BIN_EXPORT='export PATH="$HOME/.claude/bin:$PATH"'
if ! printf '%s' "${PATH:-}" | tr ':' '\n' | grep -qx "$HOME/.claude/bin"; then
    if [ "$NO_PATH_UPDATE" = 1 ]; then
        say "add this to your shell rc to use claude-policy: $BIN_EXPORT"
    else
        rc=""
        case "${SHELL:-}" in
            */zsh)  rc="$HOME/.zshrc" ;;
            */bash) rc="$HOME/.bashrc" ;;
            *)      rc="" ;;
        esac
        if [ -n "$rc" ] && ! grep -qsF "$BIN_EXPORT" "$rc" 2>/dev/null; then
            printf '\n# added by smart-allow install.sh\n%s\n' "$BIN_EXPORT" >> "$rc"
            say "added PATH export to $rc (restart your shell or 'source $rc')"
        fi
    fi
fi

say "running fast-path smoke test"
if [ -x "$REPO_DIR/tests/smoke.sh" ]; then
    if "$REPO_DIR/tests/smoke.sh" --fast; then
        say "fast-path smoke test: OK"
    else
        warn "fast-path smoke test failed"
    fi
else
    warn "smoke test not executable, skipping"
fi

cat <<EOF

Done. Next steps:
  - Pick a policy:    claude-policy normal    (also: strict | permissive)
  - Per-project:      create <project>/.claude/session-policy.md (see examples/)
  - Full smoke test:  bash $REPO_DIR/tests/smoke.sh
  - Env vars:         OLLAMA_HOST, CLAUDE_CLASSIFIER_MODEL, CLAUDE_CLASSIFIER_TIMEOUT,
                      CLAUDE_CLASSIFIER_CACHE_TTL, CLAUDE_HOOK_DEBUG=1
EOF
