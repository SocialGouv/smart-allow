# smart-allow

A local-LLM gatekeeper for **Claude Code**'s Bash tool. A `PreToolUse` hook ships
each shell command to a small model served by **Ollama** on your host. The model
reads a Markdown policy you can switch per session and returns `allow` / `ask` /
`deny`. Deterministic fast-paths skip the LLM for obvious cases, a cache avoids
re-classifying repeated commands, and if the LLM is unreachable the hook **falls
back to `ask`** — never to silent approval.

The classifier is a **single Go binary** (no Python / runtime deps). Works on
Linux, macOS, and Windows (WSL). Works natively with plain Docker, **Dev
Containers**, **DevPod** + VSCodium, and bare host.

```
Claude Code (host or devcontainer)
        │
        │ PreToolUse Bash
        ▼
  classify-command (Go binary) ──► Ollama on host (http://host.docker.internal:11434)
        │                                    │
        │                                    └─► qwen2.5-coder:7b (configurable)
        ▼
  {"hookSpecificOutput": {"permissionDecision": "allow" | "ask" | "deny", ...}}
```

## Prerequisites

- [Claude Code](https://docs.claude.com/en/docs/agents-and-tools/claude-code/overview) ≥ 2.1 installed and authenticated.
- [Ollama](https://ollama.com/) running on your **host** (Linux, macOS, or Windows).
- For devcontainer / DevPod scenarios: Docker Desktop, Rancher Desktop, or a DevPod provider.
- **For contributing to this repo**: [devbox](https://www.jetify.com/devbox) — it pins the Go toolchain. Install with `curl -fsSL https://get.jetify.com/devbox | bash`. End users of the binary do **not** need devbox.

## Quickstart

### A. Local host (simplest)

```bash
git clone https://github.com/SocialGouv/smart-allow && cd smart-allow
./install-host-ollama.sh          # configures Ollama, pulls the model (y/N prompts)
./install.sh                      # downloads the binary from GitHub releases,
                                  # installs policies, wires ~/.claude/settings.json
bash tests/smoke.sh               # 7 checks, including an Ollama round-trip
```

Start `claude` from any project. `ls` is auto-allowed, `kubectl apply` prompts
you, `rm -rf /` is blocked.

If you are on Linux/macOS and the binary download fails (no release yet, or
GitHub unreachable), add `--from-source` — it falls back to `go build`.

### B. Dev Containers (VS Code / DevPod / VSCodium)

The repo ships a functional devcontainer that bind-mounts your host's
`~/.claude/` (sharing auth), installs Claude Code CLI, and builds the
classifier via devbox.

```bash
# Host setup, once:
./install-host-ollama.sh

# VS Code
# → palette: "Dev Containers: Reopen in Container"

# DevPod + VSCodium
devpod up /path/to/smart-allow --ide vscodium
```

Inside the container, verify:

```bash
whoami && pwd                     # devbox /workspaces/smart-allow
bash tests/smoke.sh               # all checks green
```

### C. Wiring it into your own project

Two common layouts:

**1. As a sibling clone.** In your project's
`.devcontainer/devcontainer.json`, merge the snippet from
[examples/devcontainer.json](examples/devcontainer.json) (adds
`host.docker.internal`, sets `OLLAMA_HOST`, mounts smart-allow as a sibling,
runs `install.sh`).

**2. As a submodule.**

```bash
cd your-project
git submodule add https://github.com/SocialGouv/smart-allow vendor/smart-allow
```

Then in your devcontainer:

```jsonc
"postCreateCommand": "bash ${containerWorkspaceFolder}/vendor/smart-allow/install.sh --no-path-update"
```

## Switching policies

Three policies ship in [policies/](policies/) and are installed to
`~/.claude/policies/`:

| Policy       | When to use                                                                   |
|--------------|-------------------------------------------------------------------------------|
| `strict`     | Touching prod, sensitive clusters, live infra. Ask for almost everything.     |
| `normal`     | Everyday dev. Reads auto-allowed, writes outside project ask.                 |
| `permissive` | Throwaway containers. Allow broadly, still block `rm -rf /` and secret edits. |

Switch with the included helper:

```bash
claude-policy strict        # activate strict
claude-policy show          # print the currently active policy path
claude-policy list          # list available policies
```

### Per-project override

Drop a `.claude/session-policy.md` inside any project to override the global
policy for that project only. It takes priority over the global
`~/.claude/active-policy.md`. See
[examples/session-policy.md](examples/session-policy.md) and the concrete demo
in [examples/test-project/](examples/test-project/).

## Customization

Environment variables the hook honours:

| Variable                         | Default                             | Purpose                                   |
|----------------------------------|-------------------------------------|-------------------------------------------|
| `OLLAMA_HOST`                    | `http://127.0.0.1:11434`            | Ollama endpoint                           |
| `CLAUDE_CLASSIFIER_MODEL`        | `qwen2.5-coder:7b`                  | Model served by Ollama                    |
| `CLAUDE_CLASSIFIER_TIMEOUT`      | `12`                                | Seconds before the LLM call gives up      |
| `CLAUDE_CLASSIFIER_CACHE_TTL`    | `3600`                              | Seconds a cached decision stays valid     |
| `CLAUDE_CLASSIFIER_CACHE_DIR`    | `$HOME/.claude/classifier-cache`    | Where decision cache is written           |
| `CLAUDE_CLASSIFIER_LOG`          | `$HOME/.claude/classifier.log`      | Where audit log is appended               |
| `CLAUDE_HOOK_DEBUG`              | (unset)                             | Set to `1` for stderr debug lines         |
| `SMART_ALLOW_BIN`                | `$HOME/.claude/bin/classify-command` | Alternate binary path (for local dev)     |

Devcontainer sets `OLLAMA_HOST=http://host.docker.internal:11434` automatically
via `containerEnv`.

Model options:

| Model                     | Size     | Latency (rough) | Notes                         |
|---------------------------|----------|-----------------|-------------------------------|
| `qwen2.5-coder:7b`        | ~4.5 GB  | 300–600 ms      | Recommended default           |
| `qwen2.5:3b`              | ~2 GB    | 150–300 ms      | Lighter laptops               |
| `llama3.1:8b`             | ~4.7 GB  | 400–700 ms      | Alternative                   |
| `mistral-small:22b`       | ~13 GB   | 1–2 s           | Workstation w/ dedicated GPU  |

## Audit

The hook appends one JSON line per decision to `~/.claude/classifier.log`.

```bash
# Last 20 decisions
tail -20 ~/.claude/classifier.log | jq -c '{cmd, decision, via}'

# Aggregate counts
jq -s 'group_by(.decision) | map({decision: .[0].decision, count: length})' ~/.claude/classifier.log

# All commands that ended up in "ask" (candidates for the policy / fast-path)
jq -c 'select(.decision == "ask") | .cmd' ~/.claude/classifier.log | sort -u
```

## Security note

`install-host-ollama.sh` binds Ollama to `0.0.0.0:11434` so a container can
reach it. Anything that can route to your host on that port will then hit your
Ollama. On a trusted LAN that is usually fine. On untrusted networks, restrict
with a firewall — example for `ufw` on Linux:

```bash
sudo ufw allow from 172.16.0.0/12 to any port 11434   # Docker bridge range
sudo ufw deny 11434                                    # everyone else
```

## Troubleshooting

Common cases:

- `curl: (7) Failed to connect to host.docker.internal:11434` from inside the container → the `--add-host=host.docker.internal:host-gateway` line is missing from your `runArgs` (only needed on Linux hosts).
- Hard-deny command still executes → you're on Claude Code < 2.1, upgrade. The hook emits the `hookSpecificOutput` envelope, which older versions ignore.
- `Connection refused` from the container but OK from the host → Ollama is still on 127.0.0.1. Re-run `./install-host-ollama.sh` on the host.
- `403 Forbidden` from Ollama → `OLLAMA_ORIGINS` not set to `*`.
- Very slow first call → the model isn't in VRAM. Prime it with `ollama run qwen2.5-coder:7b "hi"` once.

See also [plan-ollama-classifier.md — Annexe A](plan-ollama-classifier.md) for the original debugging table.

## Install flags

```
./install.sh [--from-source] [--version vX.Y.Z] [--no-global-hook] [--force] [--no-path-update] [--dry-run]
```

- `--from-source` — build the binary locally via `go build` (needs Go installed) instead of downloading a release.
- `--version vX.Y.Z` — pin a specific release tag. Default: latest.
- `--no-global-hook` — install the binary and policies but do **not** merge the PreToolUse hook into `~/.claude/settings.json`. Use this when you'll register the hook project-by-project only.
- `--force` — overwrite existing policy files.
- `--no-path-update` — skip shell rc (`~/.bashrc`, `~/.zshrc`) PATH update.
- `--dry-run` — print the plan, do nothing.

## Developer workflow (this repo)

All build/test commands go through `devbox` to pin the Go toolchain. See
[CLAUDE.md](CLAUDE.md) for the full guide.

```bash
devbox run build            # compile ./cmd/classify-command
devbox run test             # go test ./...
devbox run install-local    # copy binary to ~/.claude/bin/classify-command
devbox run smoke-project    # end-to-end test against Ollama, project-scoped
```

## Uninstall

```bash
./uninstall.sh                      # removes the hook, keeps your policies + cache + log
./uninstall.sh --purge-policies     # also removes ~/.claude/policies/
./uninstall.sh --purge-cache        # also removes ~/.claude/classifier-cache/
./uninstall.sh --purge-log          # also removes ~/.claude/classifier.log
```

A timestamped backup of `~/.claude/settings.json` is written before the hook entry is removed.

## Design

Full design, fast-path catalog, prompt engineering choices, and devcontainer
debugging table: [plan-ollama-classifier.md](plan-ollama-classifier.md).

Comparison with Anthropic's Auto Mode:
[comparison-auto-mode.md](comparison-auto-mode.md).

Prior art that inspired this project:

- [oryband/claude-code-auto-approve](https://github.com/oryband/claude-code-auto-approve)
- [Evaneos/agent-callable](https://github.com/Evaneos/agent-callable) ([blog](https://tech.evaneos.com/agent-callable-skip-the-boring-approvals-in-claude-code-2ddb21dc2afb))
- [Evaneos/kubectl-readonly](https://github.com/Evaneos/kubectl-readonly) ([blog](https://tech.evaneos.com/introducing-kubectl-readonly-7ef1987c945b))

## License

MIT — see [LICENSE](LICENSE).
