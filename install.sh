#!/usr/bin/env bash
# smart-allow — install the Ollama-backed Claude Code classifier.
# Downloads the Go binary from GitHub releases (or builds it from source) and
# drops it into ~/.claude/bin, then installs policies and (optionally) wires a
# global PreToolUse Bash hook in ~/.claude/settings.json.
#
# Usage: ./install.sh [flags]
#   --from-source      Build locally via `go build` instead of downloading
#   --version v0.1.2   Download a specific tag (default: latest)
#   --no-global-hook   Skip merging into ~/.claude/settings.json
#   --force            Overwrite existing policies
#   --no-path-update   Don't touch ~/.bashrc / ~/.zshrc
#   --dry-run          Show what would happen
set -euo pipefail

REPO_OWNER="SocialGouv"
REPO_NAME="smart-allow"

FROM_SOURCE=0
VERSION=""
NO_GLOBAL_HOOK=0
FORCE=0
NO_PATH_UPDATE=0
DRY_RUN=0

while [ $# -gt 0 ]; do
    case "$1" in
        --from-source)     FROM_SOURCE=1 ;;
        --version)         VERSION="$2"; shift ;;
        --no-global-hook)  NO_GLOBAL_HOOK=1 ;;
        --force)           FORCE=1 ;;
        --no-path-update)  NO_PATH_UPDATE=1 ;;
        --dry-run)         DRY_RUN=1 ;;
        -h|--help)
            sed -n '2,14p' "$0"
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
DEST_DIR="$HOME/.claude"
BIN_DEST="$DEST_DIR/bin/classify-command"
POLICIES_DEST="$DEST_DIR/policies"
STAMP="$(date +%Y%m%d-%H%M%S)"

say()  { printf '\033[1;34m[install]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[install]\033[0m %s\n' "$*" >&2; }
run()  { if [ "$DRY_RUN" = 1 ]; then echo "  would: $*"; else eval "$@"; fi; }

# ---------- OS / arch detection ----------
detect_platform() {
    local os arch
    case "$(uname -s)" in
        Linux*)  os=linux ;;
        Darwin*) os=darwin ;;
        MINGW*|MSYS*|CYGWIN*) os=windows ;;
        *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64)   arch=amd64 ;;
        aarch64|arm64)  arch=arm64 ;;
        *) echo "Unsupported arch: $(uname -m)" >&2; exit 1 ;;
    esac
    echo "$os-$arch"
}

# ---------- binary install ----------
install_binary() {
    local tmp plat url ext=""
    plat="$(detect_platform)"
    case "$plat" in
        windows-*) ext=".exe"; BIN_DEST="${BIN_DEST}.exe" ;;
    esac

    mkdir -p "$DEST_DIR/bin"

    if [ "$FROM_SOURCE" = 1 ]; then
        if ! command -v go >/dev/null 2>&1; then
            warn "Go not found, but --from-source requested. Install Go (or run without --from-source to download a release)."
            exit 2
        fi
        say "building from source in $REPO_DIR"
        run "cd '$REPO_DIR' && go build -trimpath -ldflags='-s -w' -o '$BIN_DEST' ./cmd/classify-command"
    else
        if [ -z "$VERSION" ]; then
            url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/latest/download/classify-command-${plat}${ext}"
        else
            url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/${VERSION}/classify-command-${plat}${ext}"
        fi
        say "downloading $url"
        tmp="$(mktemp)"
        if [ "$DRY_RUN" = 1 ]; then
            echo "  would: curl -fsSL '$url' -o '$BIN_DEST' && chmod +x '$BIN_DEST'"
            return 0
        fi
        if ! curl -fsSL "$url" -o "$tmp"; then
            warn "download failed."
            warn "  - Is there a release at the URL above?"
            warn "  - Try --from-source if you have the Go toolchain."
            rm -f "$tmp"
            exit 3
        fi
        install -m755 "$tmp" "$BIN_DEST"
        rm -f "$tmp"
    fi
    say "binary installed: $BIN_DEST"
}

# ---------- policies ----------
install_policies() {
    mkdir -p "$POLICIES_DEST"
    for f in "$REPO_DIR"/policies/*.md; do
        local dest="$POLICIES_DEST/$(basename "$f")"
        if [ -e "$dest" ] && [ "$FORCE" = 0 ]; then
            say "skip policy (already exists, use --force to overwrite): $dest"
            continue
        fi
        run "cp '$f' '$dest'"
    done
    if [ ! -L "$DEST_DIR/active-policy.md" ] && [ ! -e "$DEST_DIR/active-policy.md" ]; then
        run "ln -sfn '$POLICIES_DEST/normal.md' '$DEST_DIR/active-policy.md'"
        say "activated policy: normal"
    fi
}

# ---------- claude-policy util ----------
install_util() {
    if [ -f "$REPO_DIR/scripts/claude-policy" ]; then
        run "install -m755 '$REPO_DIR/scripts/claude-policy' '$DEST_DIR/bin/claude-policy'"
    fi
}

# ---------- settings.json merge (global hook) ----------
merge_global_hook() {
    local settings="$DEST_DIR/settings.json"
    if [ "$DRY_RUN" = 1 ]; then
        echo "  would merge hook into $settings"
        return 0
    fi
    if [ -f "$settings" ]; then
        cp -a "$settings" "$settings.bak-$STAMP"
    fi
    python3 - "$settings" "$BIN_DEST" <<'PY'
import json, os, sys
settings_path, bin_path = sys.argv[1], sys.argv[2]
cmd = (
    'CLAUDE_CLASSIFIER_CACHE_DIR="$CLAUDE_PROJECT_DIR/.claude/cache" '
    'CLAUDE_CLASSIFIER_LOG="$CLAUDE_PROJECT_DIR/.claude/classifier.log" '
    f'"{bin_path}"'
)
try:
    with open(settings_path) as f:
        settings = json.load(f)
except FileNotFoundError:
    settings = {}

settings.setdefault("hooks", {}).setdefault("PreToolUse", [])
sentinel = "classify-command"
already = any(
    sentinel in h.get("command", "")
    for entry in settings["hooks"]["PreToolUse"]
    for h in entry.get("hooks", [])
)
if already:
    print(f"  hook already registered in {settings_path}")
else:
    settings["hooks"]["PreToolUse"].append({
        "matcher": "Bash",
        "hooks": [{"type": "command", "command": cmd, "timeout": 15000}],
    })
    with open(settings_path, "w") as f:
        json.dump(settings, f, indent=2)
    print(f"  hook added to {settings_path}")
PY
}

# ---------- PATH update ----------
maybe_update_path() {
    local export_line='export PATH="$HOME/.claude/bin:$PATH"'
    if printf '%s' "${PATH:-}" | tr ':' '\n' | grep -qx "$HOME/.claude/bin"; then
        return 0
    fi
    if [ "$NO_PATH_UPDATE" = 1 ]; then
        say "PATH hint (add to your shell rc): $export_line"
        return 0
    fi
    local rc=""
    case "${SHELL:-}" in
        */zsh)  rc="$HOME/.zshrc" ;;
        */bash) rc="$HOME/.bashrc" ;;
    esac
    if [ -n "$rc" ] && ! grep -qsF "$export_line" "$rc" 2>/dev/null; then
        run "printf '\\n# added by smart-allow install.sh\\n%s\\n' '$export_line' >> '$rc'"
        say "added PATH export to $rc (restart your shell or 'source $rc')"
    fi
}

# ---------- main ----------
if ! command -v python3 >/dev/null 2>&1; then
    warn "python3 is required for the settings.json merge step."
    warn "Install it and re-run, or pass --no-global-hook to skip that step."
    [ "$NO_GLOBAL_HOOK" = 0 ] && exit 1
fi

say "target: $DEST_DIR"
install_binary
install_policies
install_util

if [ "$NO_GLOBAL_HOOK" = 0 ]; then
    say "merging global hook into $DEST_DIR/settings.json"
    merge_global_hook
else
    say "skipped global hook (--no-global-hook). Use project-scoped .claude/settings.json instead."
fi

maybe_update_path

say "done."
cat <<EOF

Next steps:
  - Pick a policy:    claude-policy normal | strict | permissive
  - Per-project:      drop <project>/.claude/session-policy.md (override)
  - Full smoke test:  bash $REPO_DIR/tests/smoke.sh
  - Env vars you can tune:
      OLLAMA_HOST, CLAUDE_CLASSIFIER_MODEL, CLAUDE_CLASSIFIER_TIMEOUT,
      CLAUDE_CLASSIFIER_CACHE_TTL, CLAUDE_HOOK_DEBUG=1
EOF
