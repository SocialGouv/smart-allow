#!/bin/sh
# smart-allow installer
#
# Minimal one-liner:
#   curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh
#
# With options via env:
#   VERSION=v0.1.2             Pin a specific release (default: latest)
#   INSTALL_DIR=/usr/local/bin  Alternate binary location (default: $HOME/.claude/bin)
#   POLICIES_DIR=…              Alternate policies dir (default: $HOME/.claude/policies)
#   NO_HOOK=1                   Skip merging the PreToolUse hook into ~/.claude/settings.json
#   NO_POLICIES=1               Skip downloading and installing the Markdown policies
set -eu

REPO="SocialGouv/smart-allow"
BINARY_NAME="classify-command"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.claude/bin}"
POLICIES_DIR="${POLICIES_DIR:-$HOME/.claude/policies}"
SETTINGS="$HOME/.claude/settings.json"

info() { printf '\033[1;34m%s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m%s\033[0m\n' "$*" >&2; }
error() { printf '\033[1;31merror: %s\033[0m\n' "$*" >&2; exit 1; }

detect_platform() {
    OS="$(uname -s)"
    case "$OS" in
        Linux)  OS="linux" ;;
        Darwin) OS="darwin" ;;
        MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
        *) error "unsupported OS: $OS" ;;
    esac
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        *) error "unsupported architecture: $ARCH" ;;
    esac
    EXT=""
    [ "$OS" = "windows" ] && EXT=".exe"
}

fetch() {
    # fetch <url> <output>
    #
    # curl -f swallows the body on HTTP errors and returns non-zero with no
    # message; combined with `set -e` that would kill the script silently.
    # We surface the HTTP status explicitly.
    if command -v curl >/dev/null 2>&1; then
        code=$(curl -fsSL --write-out '%{http_code}' --output "$2" "$1" 2>/dev/null) \
            || { rm -f "$2"; error "failed to fetch ${1} (HTTP ${code:-?})"; }
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$1" -O "$2" \
            || { rm -f "$2"; error "failed to fetch ${1}"; }
    else
        error "curl or wget is required"
    fi
}

get_latest_version() {
    tmp="$(mktemp)"
    fetch "https://api.github.com/repos/${REPO}/releases/latest" "$tmp"
    VERSION="$(grep '"tag_name"' "$tmp" | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"
    rm -f "$tmp"
    [ -n "$VERSION" ] || error "could not determine latest release tag"
}

install_binary() {
    FILENAME="${BINARY_NAME}-${OS}-${ARCH}${EXT}"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
    CHECKSUM_URL="${URL}.sha256"

    info "downloading ${BINARY_NAME} ${VERSION} (${OS}/${ARCH})..."
    TMPDIR="$(mktemp -d)"
    if ! fetch "$URL" "${TMPDIR}/${FILENAME}"; then
        cat <<MSG >&2

The release '${VERSION}' exists, but has no asset for ${OS}-${ARCH}.
This usually means the release workflow never ran for that tag.
Trigger it manually and retry:

    gh workflow run "📦 Release" --ref ${VERSION}

Or pick another tag with:

    VERSION=vX.Y.Z curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh

MSG
        exit 1
    fi
    # Checksum is best-effort: old releases predate the .sha256 companion.
    fetch "$CHECKSUM_URL" "${TMPDIR}/${FILENAME}.sha256" 2>/dev/null || true

    if [ -s "${TMPDIR}/${FILENAME}.sha256" ]; then
        info "verifying checksum..."
        EXPECTED="$(awk '{print $1}' "${TMPDIR}/${FILENAME}.sha256")"
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL="$(sha256sum "${TMPDIR}/${FILENAME}" | awk '{print $1}')"
        elif command -v shasum >/dev/null 2>&1; then
            ACTUAL="$(shasum -a 256 "${TMPDIR}/${FILENAME}" | awk '{print $1}')"
        else
            ACTUAL=""
        fi
        [ -n "$ACTUAL" ] && [ "$ACTUAL" != "$EXPECTED" ] \
            && error "checksum mismatch (expected $EXPECTED, got $ACTUAL)"
    fi

    DEST="${INSTALL_DIR}/${BINARY_NAME}${EXT}"
    mkdir -p "$INSTALL_DIR"
    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMPDIR}/${FILENAME}" "$DEST"
        chmod +x "$DEST"
    else
        info "installing to ${INSTALL_DIR} (requires sudo)..."
        sudo mv "${TMPDIR}/${FILENAME}" "$DEST"
        sudo chmod +x "$DEST"
    fi
    rm -rf "$TMPDIR"
    info "binary installed: $DEST"
}

install_policies() {
    mkdir -p "$POLICIES_DIR"
    for p in normal strict permissive; do
        URL="https://raw.githubusercontent.com/${REPO}/${VERSION}/policies/${p}.md"
        if [ -f "${POLICIES_DIR}/${p}.md" ]; then
            info "policy ${p}: already present (skip)"
        else
            fetch "$URL" "${POLICIES_DIR}/${p}.md"
            info "policy ${p}: installed"
        fi
    done
    # Activate 'normal' as default if no active-policy symlink yet.
    if [ ! -e "$HOME/.claude/active-policy.md" ] && [ ! -L "$HOME/.claude/active-policy.md" ]; then
        ln -sfn "${POLICIES_DIR}/normal.md" "$HOME/.claude/active-policy.md"
        info "activated policy: normal"
    fi
    # Install claude-policy util (pure bash, 25 lines).
    UTIL_URL="https://raw.githubusercontent.com/${REPO}/${VERSION}/scripts/claude-policy"
    fetch "$UTIL_URL" "${INSTALL_DIR}/claude-policy" 2>/dev/null \
        && chmod +x "${INSTALL_DIR}/claude-policy" \
        && info "util installed: ${INSTALL_DIR}/claude-policy" \
        || warn "claude-policy util download skipped"
}

merge_hook() {
    if ! command -v python3 >/dev/null 2>&1; then
        warn "python3 not found — skipping ~/.claude/settings.json merge."
        warn "Register the hook manually, or set NO_HOOK=1 to silence this."
        return
    fi
    BIN_PATH="${INSTALL_DIR}/${BINARY_NAME}${EXT}"
    STAMP="$(date +%Y%m%d-%H%M%S)"
    [ -f "$SETTINGS" ] && cp "$SETTINGS" "${SETTINGS}.bak-${STAMP}"
    python3 - "$SETTINGS" "$BIN_PATH" <<'PY'
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
    print("  hook already registered (no change)")
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

main() {
    detect_platform
    if [ -n "${1:-}" ]; then
        VERSION="$1"
    elif [ -n "${VERSION:-}" ]; then
        : # honour env-provided VERSION
    else
        get_latest_version
    fi

    install_binary

    if [ -z "${NO_POLICIES:-}" ]; then
        install_policies
    fi

    if [ -z "${NO_HOOK:-}" ]; then
        info "merging PreToolUse hook into ${SETTINGS}"
        merge_hook
    fi

    info ""
    info "done. ${BINARY_NAME} ${VERSION} is at ${INSTALL_DIR}/${BINARY_NAME}${EXT}"
    info ""
    info "verify:"
    info "  ${INSTALL_DIR}/${BINARY_NAME}${EXT} --version"
    info ""
    info "next steps:"
    info "  1. ensure Ollama is running on the host (http://127.0.0.1:11434)"
    info "     and has the qwen2.5-coder:7b model pulled"
    info "  2. optionally switch policy:  claude-policy strict | normal | permissive"
    info "  3. start Claude Code from any project: the hook fires on every Bash tool call"
}

main "$@"
