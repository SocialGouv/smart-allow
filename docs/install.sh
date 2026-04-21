#!/bin/sh
# smart-allow bootstrap installer.
#
#   curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh
#   curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --global --yes
#   curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --status
#
# This script only downloads the classify-command binary from a GitHub release
# and hands off to the binary itself. All install/uninstall/policy logic lives
# in the binary (subcommands), so this stays short and shell-agnostic.
#
# Env overrides:
#   VERSION=v0.1.2              Pin a specific release (default: latest)
#   INSTALL_DIR=/usr/local/bin   Binary destination (default: $HOME/.claude/bin)
set -eu

REPO="SocialGouv/smart-allow"
BINARY_NAME="classify-command"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.claude/bin}"

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
    if [ "$OS" = "windows" ]; then
        EXT=".exe"
    fi
}

fetch() {
    # fetch <url> <output>
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

verify_checksum() {
    # verify_checksum <binary> <checksum-url>
    sha_file="${1}.sha256"
    fetch "$2" "$sha_file" 2>/dev/null || return 0
    [ -s "$sha_file" ] || return 0
    expected="$(awk '{print $1}' "$sha_file")"
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$1" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$1" | awk '{print $1}')"
    else
        return 0
    fi
    if [ "$actual" != "$expected" ]; then
        error "checksum mismatch (expected $expected, got $actual)"
    fi
    info "checksum ok"
}

main() {
    detect_platform

    if [ -z "${VERSION:-}" ]; then
        get_latest_version
    fi

    FILENAME="${BINARY_NAME}-${OS}-${ARCH}${EXT}"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

    info "downloading ${BINARY_NAME} ${VERSION} (${OS}/${ARCH})..."
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' EXIT
    fetch "$URL" "${tmpdir}/${FILENAME}" || {
        cat <<MSG >&2

The release '${VERSION}' has no asset for ${OS}-${ARCH}. This usually means
the release workflow never ran for that tag. Trigger it manually:

    gh workflow run "📦 Release" --ref ${VERSION}

or pick another tag:

    VERSION=vX.Y.Z curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh

MSG
        exit 1
    }
    verify_checksum "${tmpdir}/${FILENAME}" "${URL}.sha256"

    dest="${INSTALL_DIR}/${BINARY_NAME}${EXT}"
    mkdir -p "$INSTALL_DIR"
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmpdir}/${FILENAME}" "$dest"
        chmod +x "$dest"
    else
        info "installing to ${INSTALL_DIR} (requires sudo)..."
        sudo mv "${tmpdir}/${FILENAME}" "$dest"
        sudo chmod +x "$dest"
    fi
    info "binary installed: $dest"

    # Hand off to the binary for everything else (policies, settings.json merge,
    # interactive wizard). Pass through any args the user piped along.
    #
    # When we were invoked via `curl | sh`, stdin is the pipe from curl, which
    # is already EOF by the time we exec — the wizard would see no input and
    # silently default to "Quit". Reattach stdin to the controlling terminal
    # so the user can actually answer prompts. Falls back to the current
    # stdin if /dev/tty is unavailable (CI, non-interactive shells).
    info ""
    if [ -r /dev/tty ]; then
        exec "$dest" install "$@" < /dev/tty
    else
        exec "$dest" install "$@"
    fi
}

main "$@"
