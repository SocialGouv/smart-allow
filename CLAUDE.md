# smart-allow — dev guide for Claude Code

## Development prerequisite: **devbox**

This repo uses [devbox](https://www.jetify.com/devbox) to pin the Go toolchain
and keep the build reproducible. Don't install Go system-wide — always use
devbox.

The devcontainer image (`jetpackio/devbox:latest`) already has the `devbox` CLI
available. On a bare host, install it once:

```bash
curl -fsSL https://get.jetify.com/devbox | bash
```

## How to run commands

Every build/test/run command goes through `devbox run`. The recipes live in
[devbox.json](devbox.json):

| Command                        | What it does                                       |
|--------------------------------|----------------------------------------------------|
| `devbox run build`             | Compile `./cmd/classify-command` → `bin/classify-command` |
| `devbox run install-local`     | Copy the built binary to `$HOME/.claude/bin/classify-command` |
| `devbox run test`              | `go test ./...` (unit tests only)                  |
| `devbox run smoke`             | Shell smoke test via `tests/smoke.sh` (needs Ollama) |
| `devbox run smoke-project`     | Isolated smoke via `examples/test-project/run-test.sh` |
| `devbox run clean`             | Remove `bin/`                                      |

Interactive shell with the toolchain active: `devbox shell`.

## Project layout

- `cmd/classify-command/` — the Go binary that Claude Code calls via
  `PreToolUse` hook. `main.go` orchestrates; `fastpath.go`, `cache.go`,
  `ollama.go`, `policy.go` are the pieces.
- `policies/` — French-language Markdown policies (`strict.md`, `normal.md`,
  `permissive.md`) fed to Ollama via the system prompt.
- `scripts/claude-policy` — bash util to switch the active policy symlink.
- `.claude/settings.json` — project-scoped hook: activates smart-allow whenever
  `claude` is run from this repo. Points at the installed binary in
  `$HOME/.claude/bin/` (or `$SMART_ALLOW_BIN` if you override it).
- `examples/test-project/` — self-contained sandbox that exercises the binary
  without touching `~/.claude/`.
- `.devcontainer/` — devbox-based devcontainer with Claude Code CLI preinstalled.
- `.goreleaser.yaml` + `.github/workflows/release.yaml` — cross-platform release
  pipeline. Tagging `vX.Y.Z` publishes per-platform binaries.
- `install.sh` — end-user install script (downloads binary, wires global hook).
- `install-host-ollama.sh` — one-shot host setup for Ollama.

## Pipeline inside the classifier

```
stdin (PreToolUse JSON)
    │
    ▼
1. Fast-path (deterministic)
    │  allowlist prefix → "allow"
    │  hard-deny substring → "deny"
    │  dangerous regex → fall through
    ▼
2. Cache lookup (SHA256(cmd+policy+model), TTL=1h)
    │
    ▼
3. Ollama HTTP POST /api/generate (format=json, temperature=0)
    │
    ▼
4. Emit hookSpecificOutput.permissionDecision (allow|ask|deny)
    │
    ▼
Append-only JSON log line
```

Fail-safe: any error at step 3 produces `ask`, never `allow`.

## Recurring session context

- **Claude Code 2.1+** expects the nested `hookSpecificOutput` envelope, not the
  legacy `{"decision": ...}`. Internally we still use `approve/ask/deny` labels;
  `emit()` translates `approve → allow`.
- **`rm -rf /` in any substring** triggers fast-path deny — including
  `echo "rm -rf /"` — because the check is a naive `strings.Contains`. This is
  conservative on purpose. Avoid it in fixtures (`printf` with concatenation, or
  run tests via `go test`).
- **Ollama host resolution**: default `http://127.0.0.1:11434` (works on host).
  The devcontainer sets `OLLAMA_HOST=http://host.docker.internal:11434` via
  `containerEnv`. The binary reads the env variable only — no smart detection.
- **Paths**: `CLAUDE_CLASSIFIER_CACHE_DIR` and `CLAUDE_CLASSIFIER_LOG` default to
  `$HOME/.claude/...`. Project-scoped hooks (`.claude/settings.json` here)
  override them to stay inside the repo.

## End-to-end validation

After any change to `cmd/classify-command/`:

```bash
devbox run test             # unit tests
devbox run build            # compile
devbox run install-local    # update ~/.claude/bin/classify-command
devbox run smoke-project    # isolated end-to-end against Ollama
```

Then trigger a Bash command **in this Claude Code session** — the hook fires
automatically via `.claude/settings.json`. A command with the substring
`rm -rf /` should be blocked with `fast-path: hard-deny pattern`.
