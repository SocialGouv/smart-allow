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

### A. One-liner bootstrap (recommended)

```bash
# Download the binary and open the interactive installer.
curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh
```

The bootstrap downloads the latest release binary into `$HOME/.claude/bin/`
then hands off to its `install` subcommand. The installer opens a numbered
menu: **install globally** (all Claude Code sessions), **install for this
project only** (current git root), **switch**, **uninstall**, **quit**. It
writes the 3 Markdown policies and the active-policy symlink the first time.

Non-interactive variants (pass flags after `--` in the pipe):

```bash
curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --global --yes
curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --project --yes
curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --status
```

Env overrides for the bootstrap: `VERSION=v0.2.0`, `INSTALL_DIR=/usr/local/bin`.

Once installed, use `classify-command install` any time to re-open the wizard
or `classify-command install --status` to see what's wired where.

### B. Ollama on the host (once)

```bash
git clone https://github.com/SocialGouv/smart-allow && cd smart-allow
./install-host-ollama.sh          # configures Ollama, pulls the model (y/N prompts)
```

Skip if you already have Ollama listening on `0.0.0.0:11434` with a coder
model pulled (default: `qwen2.5-coder:7b`).

### C. From a checkout (contributor flow)

```bash
git clone https://github.com/SocialGouv/smart-allow && cd smart-allow
devbox run -- task install:project    # build + wire hook for this repo only
bash tests/smoke.sh                   # 7 checks, including an Ollama round-trip
```

Start `claude` from the repo. `ls` is auto-allowed, `kubectl apply` prompts
you, `rm -rf /` is blocked. Switch scopes any time with
`classify-command install` (wizard) or the `devbox run -- task install:global`
target.

### D. Dev Containers (VS Code / DevPod / VSCodium)

The repo ships a functional devcontainer that bind-mounts your host's
`~/.claude/` (sharing auth), installs Claude Code CLI, builds the classifier
via devbox, and wires the hook at **project scope** (so it only fires when
Claude Code runs from this workspace).

```bash
# Host setup, once:
./install-host-ollama.sh

# VS Code
# → palette: "Dev Containers: Reopen in Container"

# DevPod + VSCodium
devpod up /path/to/smart-allow --ide vscodium
```

### E. Wiring it into your own project

```bash
cd your-project
curl -fsSL https://socialgouv.github.io/smart-allow/install.sh | sh -s -- --project --yes
```

The `--project` flag walks up from CWD to the nearest `.git/` and writes the
hook into `<repo-root>/.claude/settings.json`. Override the target with
`--here` (current dir, no walk-up) or `--path <dir>` (arbitrary).

Alternatively, if your project uses a devcontainer, merge the snippet from
[examples/devcontainer.json](examples/devcontainer.json) to get the
`host.docker.internal` wiring and run the curl-pipe bootstrap from
`postCreateCommand`.

## Managing the install

```bash
classify-command install --status     # show where the hook is wired
classify-command install              # open the interactive wizard
classify-command install --global     # force global scope
classify-command install --project    # force project scope (git-root default)
classify-command install --here       # force project scope = CWD
classify-command install --path DIR   # force project scope = DIR

classify-command uninstall            # interactive removal
classify-command uninstall --all      # remove both global and project hooks
```

## Switching policies

Three policies ship in [policies/](policies/) (embedded in the binary) and get
deployed to `~/.claude/policies/` on first install:

| Policy       | When to use                                                                   |
|--------------|-------------------------------------------------------------------------------|
| `strict`     | Touching prod, sensitive clusters, live infra. Ask for almost everything.     |
| `normal`     | Everyday dev. Reads auto-allowed, writes outside project ask.                 |
| `permissive` | Throwaway containers. Allow broadly, still block `rm -rf /` and secret edits. |

```bash
classify-command policy list          # the three shipped policies
classify-command policy show          # current active-policy target
classify-command policy set strict    # repoint ~/.claude/active-policy.md
classify-command policy edit          # $EDITOR on the active policy
```

### Per-project override

Drop a `.claude/session-policy.md` inside any project to override the global
active policy for that project only. It takes priority over
`~/.claude/active-policy.md`. See
[examples/session-policy.md](examples/session-policy.md) and the concrete
demo in [examples/test-project/](examples/test-project/).

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

See also [docs/plan-ollama-classifier.md — Annexe A](docs/plan-ollama-classifier.md) for the original debugging table.

## Developer workflow (this repo)

Everything goes through [devbox](https://www.jetify.com/devbox) (pins Go,
`go-task`, Node.js) and [go-task](https://taskfile.dev) (`task`). See
[CLAUDE.md](CLAUDE.md) for the full guide.

```bash
devbox run -- task build            # compile → ./classify-command (ldflags inject version)
devbox run -- task check            # go fmt + go vet + go test
devbox run -- task install          # copy binary to ~/.claude/bin/ (no hook wiring)
devbox run -- task install:project  # + wire hook at project scope (this repo)
devbox run -- task install:global   # + wire hook globally
devbox run -- task install:status   # report where hooks are wired
devbox run -- task uninstall        # interactive hook removal
devbox run -- task smoke:project    # end-to-end against Ollama, project-scoped
devbox run -- task --list-all       # discover all targets
```

### Releasing

1. Merge conventional commits (`feat:`, `fix:`, …) to `main`.
2. `version.yml` runs `release-it --ci`, bumps `package.json`, tags `vX.Y.Z`,
   creates the GitHub release.
3. The tag push triggers `release.yml`, which matrix-builds Linux / macOS /
   Windows × amd64 / arm64 binaries (each with a `.sha256` companion) and
   attaches them to the release via `softprops/action-gh-release`.

No `goreleaser` — plain `go build -trimpath -ldflags` with
`-X internal/appinfo.Version=…` and `-X internal/appinfo.Commit=…`.

### GitHub Pages (one-liner hosting)

The curl-pipe installer at
`https://socialgouv.github.io/smart-allow/install.sh` is served by GitHub
Pages from [docs/install.sh](docs/install.sh). To enable:

1. On GitHub: **Settings → Pages → Build and deployment**.
2. Source: **Deploy from a branch** — branch `main`, folder `/docs`.
3. Save. Pages takes ~1 min to build; after that the install URL is live.

The two design docs ([docs/plan-ollama-classifier.md](docs/plan-ollama-classifier.md),
[docs/comparison-auto-mode.md](docs/comparison-auto-mode.md)) are served
alongside — Jekyll renders them at `socialgouv.github.io/smart-allow/<file>.html`.

## Uninstall

```bash
classify-command uninstall              # interactive
classify-command uninstall --global     # remove hook from ~/.claude/settings.json
classify-command uninstall --project    # remove hook from <git-root>/.claude/settings.json
classify-command uninstall --all --yes  # remove both, no prompts
```

A timestamped backup (`settings.json.bak-<YYYYMMDD-HHMMSS>`) is written before
any change. The binary itself and the Markdown policies at
`~/.claude/policies/` are left in place; delete those manually if you want a
clean slate.

## Design

Full design, fast-path catalog, prompt engineering choices, and devcontainer
debugging table: [docs/plan-ollama-classifier.md](docs/plan-ollama-classifier.md).

Comparison with Anthropic's Auto Mode:
[docs/comparison-auto-mode.md](docs/comparison-auto-mode.md).

### On Claude Code Auto Mode

Anthropic shipped its own classifier-based PreToolUse gating in April 2026.
smart-allow and Auto Mode are complementary (see the dedicated comparison above):

- [Auto Mode — Anthropic blog](https://claude.com/blog/auto-mode)
- [Claude Code Auto Mode : permissions et autonomie — SFEIR Institute](https://institute.sfeir.com/fr/articles/claude-code-auto-mode-permissions-autonomie/)

### Prior art

Projects that inspired this one:

- [oryband/claude-code-auto-approve](https://github.com/oryband/claude-code-auto-approve)
- [Evaneos/agent-callable](https://github.com/Evaneos/agent-callable)
  ([blog post](https://tech.evaneos.com/agent-callable-skip-the-boring-approvals-in-claude-code-2ddb21dc2afb))
- [Evaneos/kubectl-readonly](https://github.com/Evaneos/kubectl-readonly)
  ([blog post](https://tech.evaneos.com/introducing-kubectl-readonly-7ef1987c945b))

## License

MIT — see [LICENSE](LICENSE).
