#!/usr/bin/env bash
# smart-allow — configure Ollama on the host so a devcontainer/DevPod can reach it.
# Usage: ./install-host-ollama.sh [--model NAME] [--no-pull] [--yes]
set -euo pipefail

MODEL="qwen2.5-coder:7b"
PULL=1
ASSUME_YES=0

while [ $# -gt 0 ]; do
    case "$1" in
        --model)   MODEL="$2"; shift ;;
        --no-pull) PULL=0 ;;
        --yes|-y)  ASSUME_YES=1 ;;
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

say()  { printf '\033[1;34m[host]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[host]\033[0m %s\n' "$*" >&2; }

confirm() {
    if [ "$ASSUME_YES" = 1 ]; then return 0; fi
    local prompt="$1"
    read -r -p "$prompt [y/N] " reply
    [[ "$reply" =~ ^[yY]$ ]]
}

if ! command -v ollama >/dev/null 2>&1; then
    warn "'ollama' command not found."
    warn "Install it from https://ollama.com/download (Linux one-liner: curl -fsSL https://ollama.com/install.sh | sh)"
    exit 2
fi

say "ollama version: $(ollama --version 2>/dev/null | head -n1 || echo unknown)"

OS="$(uname -s)"

case "$OS" in
Linux)
    say "Linux detected — will configure systemd override to bind 0.0.0.0:11434."
    OVERRIDE_DIR="/etc/systemd/system/ollama.service.d"
    OVERRIDE_FILE="$OVERRIDE_DIR/override.conf"
    read -r -d '' OVERRIDE_CONTENT <<'EOF' || true
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
Environment="OLLAMA_ORIGINS=*"
EOF
    cat <<EOF

The following will be written to $OVERRIDE_FILE (via sudo):
--- 8< ---
$OVERRIDE_CONTENT
--- 8< ---
Then: sudo systemctl daemon-reload && sudo systemctl restart ollama

EOF
    if confirm "Apply the systemd override now?"; then
        sudo mkdir -p "$OVERRIDE_DIR"
        echo "$OVERRIDE_CONTENT" | sudo tee "$OVERRIDE_FILE" >/dev/null
        sudo systemctl daemon-reload
        sudo systemctl restart ollama
        say "systemd override applied and ollama restarted."
    else
        warn "Skipped systemd override. Ollama may remain on 127.0.0.1."
    fi
    ;;
Darwin)
    say "macOS detected — will use launchctl setenv for OLLAMA_HOST/ORIGINS."
    cat <<'EOF'

The following will be executed:
  launchctl setenv OLLAMA_HOST "0.0.0.0:11434"
  launchctl setenv OLLAMA_ORIGINS "*"
Then quit the Ollama menu-bar app and relaunch it for the change to take effect.

EOF
    if confirm "Apply launchctl setenv now?"; then
        launchctl setenv OLLAMA_HOST "0.0.0.0:11434"
        launchctl setenv OLLAMA_ORIGINS "*"
        say "launchctl set. Quit and relaunch the Ollama app from the menu bar to apply."
    else
        warn "Skipped launchctl setenv."
    fi
    ;;
*)
    warn "Unsupported OS: $OS. Run manually:  OLLAMA_HOST=0.0.0.0:11434 ollama serve"
    ;;
esac

if [ "$PULL" = 1 ]; then
    if confirm "Pull model '$MODEL' now? (a few GB download)"; then
        ollama pull "$MODEL"
    else
        warn "Skipped model pull. You can do it later with:  ollama pull $MODEL"
    fi
fi

say "verifying http://127.0.0.1:11434/api/tags"
if curl -fsS -m 3 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    say "Ollama HTTP endpoint OK on 127.0.0.1:11434"
else
    warn "Endpoint not reachable. Check: systemctl status ollama (Linux) / Ollama app (macOS)."
fi

if [ "$OS" = "Linux" ]; then
    cat <<'EOF'

Security note:
  Binding Ollama to 0.0.0.0 exposes it to anything that can reach port 11434.
  If you are on an untrusted network, restrict it to the Docker bridge, e.g.:

    sudo ufw allow from 172.16.0.0/12 to any port 11434
    sudo ufw deny 11434

EOF
fi

say "done."
