#!/usr/bin/env bash
# smart-allow — remove the classifier hook from ~/.claude/.
# Usage: ./uninstall.sh [--purge-policies] [--purge-cache] [--purge-log]
set -euo pipefail

PURGE_POLICIES=0
PURGE_CACHE=0
PURGE_LOG=0

while [ $# -gt 0 ]; do
    case "$1" in
        --purge-policies) PURGE_POLICIES=1 ;;
        --purge-cache)    PURGE_CACHE=1 ;;
        --purge-log)      PURGE_LOG=1 ;;
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

DEST_DIR="$HOME/.claude"
STAMP="$(date +%Y%m%d-%H%M%S)"

say()  { printf '\033[1;34m[uninstall]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[uninstall]\033[0m %s\n' "$*" >&2; }

for f in "$DEST_DIR/hooks/classify-command.py" "$DEST_DIR/bin/claude-policy" "$DEST_DIR/active-policy.md"; do
    if [ -e "$f" ] || [ -L "$f" ]; then
        rm -f "$f"
        say "removed $f"
    fi
done

SETTINGS="$DEST_DIR/settings.json"
if [ -f "$SETTINGS" ]; then
    cp -a "$SETTINGS" "$SETTINGS.bak-$STAMP"
    python3 - "$SETTINGS" <<'PY'
import json, sys
p = sys.argv[1]
with open(p) as f:
    s = json.load(f)
pre = s.get("hooks", {}).get("PreToolUse", [])
kept = []
for entry in pre:
    hooks = [h for h in entry.get("hooks", []) if "classify-command.py" not in h.get("command", "")]
    if hooks:
        entry["hooks"] = hooks
        kept.append(entry)
if pre:
    s["hooks"]["PreToolUse"] = kept
    if not kept:
        del s["hooks"]["PreToolUse"]
    if not s.get("hooks"):
        s.pop("hooks", None)
with open(p, "w") as f:
    json.dump(s, f, indent=2)
print(f"  classify-command.py entry removed from {p}")
PY
    say "backup: $SETTINGS.bak-$STAMP"
fi

if [ "$PURGE_POLICIES" = 1 ]; then
    rm -rf "$DEST_DIR/policies"
    say "removed $DEST_DIR/policies"
else
    say "kept $DEST_DIR/policies (use --purge-policies to remove)"
fi

if [ "$PURGE_CACHE" = 1 ]; then
    rm -rf "$DEST_DIR/classifier-cache"
    say "removed $DEST_DIR/classifier-cache"
fi

if [ "$PURGE_LOG" = 1 ]; then
    rm -f "$DEST_DIR/classifier.log"
    say "removed $DEST_DIR/classifier.log"
fi

say "done."
