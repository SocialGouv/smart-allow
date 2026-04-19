# smart-allow

A local-LLM gatekeeper for **Claude Code**'s Bash tool. A `PreToolUse` hook sends each
shell command to a small model served by **Ollama** on your host. The model reads a
Markdown policy you can switch per session and returns `approve` / `ask` / `deny`.
Deterministic fast-paths skip the LLM for obvious cases; a cache avoids re-classifying
repeated commands; if the LLM is unreachable the hook **falls back to `ask`** — never to
silent approval.

Works natively with plain Docker, **Dev Containers** (VS Code), **DevPod**, and
**VSCodium**. The host-side Ollama is reused by all of them — no model running inside the
container.

```
Claude Code (host or devcontainer)
        │
        │ PreToolUse Bash
        ▼
  classify-command.py ──► Ollama on host (http://host.docker.internal:11434)
        │                             │
        │                             └─► qwen2.5-coder:7b (configurable)
        ▼
  {"decision": "approve" | "ask" | "deny", "reason": "..."}
```

## Prerequisites

- [Claude Code](https://docs.claude.com/en/docs/agents-and-tools/claude-code/overview) installed and authenticated.
- [Ollama](https://ollama.com/) installed on your **host** machine (Linux or macOS).
- Python **3.10+** available wherever the hook runs (host or devcontainer).
- For devcontainer/DevPod scenarios: Docker Desktop, Rancher Desktop, or a DevPod provider.

## Quickstart

### A. Local host (simplest)

```bash
git clone <this-repo> smart-allow && cd smart-allow
./install-host-ollama.sh          # configures Ollama, pulls the model (y/N prompts)
./install.sh                      # installs the hook into ~/.claude/
bash tests/smoke.sh               # 6 checks, including an Ollama round-trip
```

Start `claude` from any project. `ls` is auto-approved, `kubectl apply` prompts you,
`rm -rf /` is blocked.

### B. Dev Containers (VS Code)

1. On the **host**, once: `./install-host-ollama.sh`.
2. Open this repo in VS Code, run the *"Reopen in Container"* command. The
   `.devcontainer/devcontainer.json` in this repo does the rest (installs `requests`,
   runs `install.sh --symlink`, wires `host.docker.internal`).
3. Inside the container: `bash tests/smoke.sh`.

### C. DevPod + VSCodium

```bash
# Host setup, once:
./install-host-ollama.sh

# Start the workspace:
devpod provider add docker           # if you have not already
devpod up . --ide vscodium

# Inside the container (opened automatically in VSCodium):
bash tests/smoke.sh
```

Quick sanity check that `host.docker.internal` is wired:

```bash
docker inspect $(docker ps -qf "label=devpod.workspace") | grep -A2 ExtraHosts
```

## Using it in your own project

You do **not** have to vendor smart-allow into every repo. Two common layouts:

### 1. As a sibling clone (your project sits next to smart-allow)

In your project's `.devcontainer/devcontainer.json`, merge the keys from
[examples/devcontainer.json](examples/devcontainer.json). The important bits:

```jsonc
{
  "runArgs": ["--add-host=host.docker.internal:host-gateway"],
  "containerEnv": { "OLLAMA_HOST": "http://host.docker.internal:11434" },
  "mounts": [
    "source=${localWorkspaceFolder}/../smart-allow,target=/workspaces/smart-allow,type=bind,consistency=cached"
  ],
  "postCreateCommand": "pip install --user requests && bash /workspaces/smart-allow/install.sh --no-path-update"
}
```

### 2. As a submodule

```bash
cd your-project
git submodule add <smart-allow-repo-url> vendor/smart-allow
```

Then in your devcontainer:

```jsonc
"postCreateCommand": "pip install --user requests && bash ${containerWorkspaceFolder}/vendor/smart-allow/install.sh --no-path-update"
```

## Switching policies

Three policies ship in `home/.claude/policies/` and are installed to `~/.claude/policies/`:

| Policy       | When to use                                                    |
|--------------|----------------------------------------------------------------|
| `strict`     | Touching prod, sensitive clusters, live infra. Ask for almost everything. |
| `normal`     | Everyday dev work. Reads auto-approved, writes outside project ask. |
| `permissive` | Throwaway containers, side projects. Approve broadly, still block `rm -rf /` and ssh/aws/gpg config edits. |

Switch with the included helper:

```bash
claude-policy strict        # activate strict
claude-policy show          # print the currently active policy path
claude-policy list          # list available policies
```

### Per-project override

Drop a `.claude/session-policy.md` inside any project to override the global policy for
that project only. It takes priority over `~/.claude/active-policy.md`. See
[examples/session-policy.md](examples/session-policy.md).

## Customization

Environment variables the hook honours (set them in your devcontainer's `containerEnv`
or your shell rc):

| Variable                         | Default                                 | Purpose                                |
|----------------------------------|-----------------------------------------|----------------------------------------|
| `OLLAMA_HOST`                    | `http://host.docker.internal:11434`     | Ollama endpoint                        |
| `CLAUDE_CLASSIFIER_MODEL`        | `qwen2.5-coder:7b`                      | Model served by Ollama                 |
| `CLAUDE_CLASSIFIER_TIMEOUT`      | `12`                                    | Seconds before the LLM call gives up   |
| `CLAUDE_CLASSIFIER_CACHE_TTL`    | `3600`                                  | Seconds a cached decision stays valid  |
| `CLAUDE_HOOK_DEBUG`              | (unset)                                 | Set to `1` for stderr debug lines      |

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

# All commands that ended up in "ask" (candidates to add to the policy / fast-path)
jq -c 'select(.decision == "ask") | .cmd' ~/.claude/classifier.log | sort -u
```

## Security note

`install-host-ollama.sh` binds Ollama to `0.0.0.0:11434` so a container can reach it.
Anything that can route to your host on that port will then hit your Ollama. On a trusted
LAN that is usually fine. On untrusted networks, restrict with a firewall — example for
`ufw` on Linux:

```bash
sudo ufw allow from 172.16.0.0/12 to any port 11434   # Docker bridge range
sudo ufw deny 11434                                   # everyone else
```

## Troubleshooting

See the original design doc: [plan-ollama-classifier.md — Annexe A](plan-ollama-classifier.md).
The common cases are:

- `curl: (7) Failed to connect to host.docker.internal:11434` from inside the container →
  the `--add-host=host.docker.internal:host-gateway` line is missing from your
  `runArgs` (only needed on Linux hosts).
- `Connection refused` from the container but OK from the host → Ollama is still on
  127.0.0.1. Re-run `./install-host-ollama.sh` on the host.
- `403 Forbidden` from Ollama → `OLLAMA_ORIGINS` not set to `*` (or the container
  origin).
- Very slow first call → the model isn't in VRAM yet. Prime it with
  `ollama run qwen2.5-coder:7b "hi"` once on the host.

## Install flags

```
./install.sh [--symlink] [--dry-run] [--no-path-update] [--force]
```

- `--symlink` — point `~/.claude/**` at files inside this repo (contributor mode).
- `--dry-run` — print what would be copied / merged, do nothing.
- `--no-path-update` — don't write the `~/.claude/bin` export to your shell rc.
- `--force` — overwrite existing policy files (normally preserved).

## Uninstall

```bash
./uninstall.sh                      # removes the hook, keeps your policies + cache + log
./uninstall.sh --purge-policies     # also removes ~/.claude/policies/
./uninstall.sh --purge-cache        # also removes ~/.claude/classifier-cache/
./uninstall.sh --purge-log          # also removes ~/.claude/classifier.log
```

A timestamped backup of `~/.claude/settings.json` is written before the hook entry is
removed.

## Design

Full design, fast-path catalog, prompt engineering choices, and dev-container
debugging table live in [plan-ollama-classifier.md](plan-ollama-classifier.md). Prior
art that inspired this project:

- [oryband/claude-code-auto-approve](https://github.com/oryband/claude-code-auto-approve)
- [Evaneos/agent-callable](https://github.com/Evaneos/agent-callable) ([blog](https://tech.evaneos.com/agent-callable-skip-the-boring-approvals-in-claude-code-2ddb21dc2afb))
- [Evaneos/kubectl-readonly](https://github.com/Evaneos/kubectl-readonly) ([blog](https://tech.evaneos.com/introducing-kubectl-readonly-7ef1987c945b))

## License

MIT — see [LICENSE](LICENSE).
