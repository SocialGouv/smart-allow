# smart-allow — dev guide for Claude Code

## Development prerequisite: **devbox + go-task**

This repo uses [devbox](https://www.jetify.com/devbox) to pin Go, `go-task`, and
Node.js, and [go-task](https://taskfile.dev) (`task`) as the command runner.
Don't install Go or Node system-wide — always go through devbox.

The devcontainer image (`jetpackio/devbox:latest`) already has the `devbox` CLI.
On a bare host, install it once:

```bash
curl -fsSL https://get.jetify.com/devbox | bash
```

Pinned versions live in [devbox.json](devbox.json): `go@1.23`,
`go-task@latest`, `nodejs_22@latest`.

## How to run commands

Everything goes through `devbox run -- task <target>`. The recipes live in
[Taskfile.yml](Taskfile.yml):

| Command                                 | What it does                                                          |
|-----------------------------------------|-----------------------------------------------------------------------|
| `devbox run -- task build`              | Compile `./cmd/smart-allow` → `./smart-allow` (ldflags inject version + commit) |
| `devbox run -- task install`            | Build + copy binary to `$HOME/.claude/bin/smart-allow` (no hook wiring) |
| `devbox run -- task install:project`    | Build + copy + register hook in `<this-repo>/.claude/settings.json`   |
| `devbox run -- task install:global`     | Build + copy + register hook in `~/.claude/settings.json` (all sessions) |
| `devbox run -- task install:status`     | Report where hooks are currently wired                                |
| `devbox run -- task uninstall`          | Interactive hook removal                                              |
| `devbox run -- task test`               | `go test ./...` (unit tests)                                          |
| `devbox run -- task test:race`          | Tests with race detector                                              |
| `devbox run -- task fmt`                | `go fmt ./...`                                                        |
| `devbox run -- task vet`                | `go vet ./...`                                                        |
| `devbox run -- task lint`               | fmt + vet                                                             |
| `devbox run -- task check`              | lint + test                                                           |
| `devbox run -- task smoke`              | Shell smoke test via `tests/smoke.sh` (needs Ollama)                  |
| `devbox run -- task smoke:project`      | Isolated end-to-end via `examples/test-project/run-test.sh`           |
| `devbox run -- task version`            | Print the injected version (from `package.json` + git short SHA)      |
| `devbox run -- task clean`              | Remove build artifacts                                                |
| `devbox run -- task --list-all`         | Discover every task                                                   |

Interactive shell with the toolchain active: `devbox shell`.

## Project layout

- `cmd/smart-allow/` — Go entry point. Reads a PreToolUse JSON event on
  stdin, emits `hookSpecificOutput.permissionDecision` on stdout. Subcommand
  dispatch lives in `main.go`; actual logic split per concern:
  - `main.go` — dispatcher + `runHook` (the hook pipeline)
  - `install.go` — `runInstall`, `wizard`, `detectStatus`,
    `resolveProjectRoot`, `mergeHook`, `ensureBinaryAtHome`, `installPolicies`
  - `uninstall.go` — `runUninstall`, `removeHook`
  - `policy_cmd.go` — `runPolicy` (`list`/`show`/`set`/`edit`)
  - `fastpath.go`, `ai_exfil.go`, `cache.go`, `ollama.go`, `policy.go` —
    hook pipeline pieces (unchanged by the installer work)
  - `*_test.go` — unit tests (notably `ai_exfil_test.go`: ~70 table-driven
    cases covering sensitive reads, cloud AI providers, combined exfil,
    Ollama-is-local, and CLI-token boundaries)
- `internal/appinfo/` — build-time identity (`Version`, `Commit`) injected via
  `-ldflags`. Source of `smart-allow --version` output.
- `policies/` — Go package that owns the three English-language Markdown
  policies (`strict.md`, `normal.md`, `permissive.md`). `embed.go`'s
  `//go:embed *.md` ships them inside the binary, so the installer is
  offline-capable after the binary download.
- `.claude/settings.json` — **project-scoped** hook for this very repo. Wired
  by `devbox run -- task install:project` or by the installer's
  `--project` flag.
- `examples/test-project/` — self-contained sandbox to exercise the binary
  without touching `~/.claude/`.
- `.devcontainer/` — devbox-based devcontainer with Claude Code CLI
  auto-installed and the hook auto-wired at project scope.
- `.github/workflows/` — **tests.yml** (gofmt check + vet + tests on push/PR),
  **release.yml** (matrix build per goos/goarch × runner, SHA256, uploads to
  GitHub release), **version.yml** (release-it conventional-changelog bump on
  merged PR or manual dispatch).
- `package.json` — source of truth for the release version. Bumped by
  release-it.
- `.release-it.json` — release-it config: conventional commits, tag
  `v${version}`, GitHub release created after bump (release.yml then attaches
  built binaries).
- `docs/install.sh` — curl-pipe bootstrap (~100 lines, POSIX sh). Downloads
  the latest binary, verifies checksum, then `exec`s
  `smart-allow install` with whatever args the user piped. All actual
  install logic is in the Go binary.
- `install-host-ollama.sh` — one-shot host setup for Ollama.

## Pipeline inside the classifier

```
stdin (PreToolUse JSON)
    │
    ▼
1. Fast-path (deterministic)
    │  allowlist prefix → "allow"
    │  hard-deny substring (rm -rf /, mkfs, fork bomb, …) → "deny"
    │  AI-exfil: sensitive read + cloud LLM provider → "deny"
    │  AI-exfil: sensitive read OR cloud LLM provider alone → "ask"
    │  dangerous regex (curl|bash, …) → fall through
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

## Release flow

Versioning follows [iterion](https://github.com/SocialGouv/iterion)'s layout:

1. `version.yml` runs `release-it --ci` on merged PR to `main` (or via
   `workflow_dispatch`). release-it:
   - reads current version from `package.json`,
   - determines the next version via conventional commits,
   - bumps `package.json`, commits `chore: release vX.Y.Z`, tags `vX.Y.Z`,
   - creates an empty GitHub release `vX.Y.Z`,
   - pushes tag + branch.
2. The `push: tags: [v*]` event triggers `release.yml`, which builds
   `smart-allow-<goos>-<goarch>[.exe]` for 6 platforms (linux / darwin /
   windows × amd64 / arm64 — windows arm64 excluded, one less target), writes
   SHA256 files, uploads them to the existing GitHub release.
3. The release-notes body is generated by `softprops/action-gh-release` with
   `generate_release_notes: true`.

Adjust `main` release target by landing conventional commits (`feat:`, `fix:`,
`chore:`, etc.).

## Recurring session context

- **Claude Code 2.1+** expects the nested `hookSpecificOutput` envelope, not
  the legacy `{"decision": ...}`. Internally we still use `approve/ask/deny`
  labels; `emit()` translates `approve → allow`.
- **`rm -rf /` in any substring** triggers fast-path deny — including
  `echo "rm -rf /"` — because the check is a naive `strings.Contains`. This is
  conservative on purpose. Avoid it in fixtures; use `rm -r` or build the
  string at runtime if you need it in a test.
- **Ollama host resolution**: default `http://127.0.0.1:11434` (works on host).
  The devcontainer sets `OLLAMA_HOST=http://host.docker.internal:11434` via
  `containerEnv`. The binary reads the env variable only — no smart detection.
- **Paths**: `CLAUDE_CLASSIFIER_CACHE_DIR` and `CLAUDE_CLASSIFIER_LOG` default
  to `$HOME/.claude/...`. Project-scoped hooks (`.claude/settings.json` here)
  override them inline to stay inside the repo.
- **AI-exfil guard**: reading `.env`/`~/.ssh`/`*.pem`/…, referencing
  `$*TOKEN*`/`$*SECRET*`/… env vars, dumping env (`env`/`printenv`), or
  calling a cloud LLM provider (OpenAI/Anthropic/Cohere/Mistral/Groq/
  Gemini/Perplexity/DeepSeek/xAI/HuggingFace/Together/Fireworks/Replicate)
  short-circuits to `ask` at fast-path. The combination of both in the
  same command is `deny`. Ollama and local loopback endpoints
  (`localhost`/`127.0.0.1`/`host.docker.internal`) are explicitly out of
  scope — see `cmd/smart-allow/ai_exfil.go` for the exact substrings,
  CLIs, and env-var keywords.

## End-to-end validation

After any change to `cmd/smart-allow/`:

```bash
devbox run -- task check           # lint + unit tests
devbox run -- task install         # rebuild and update ~/.claude/bin/
devbox run -- task install:status  # confirm the hook is still wired
devbox run -- task smoke:project   # isolated end-to-end against Ollama
```

Then trigger a Bash command **in this Claude Code session** — the hook fires
automatically via `.claude/settings.json`. A command with the substring
`rm -rf /` should be blocked with `fast-path: hard-deny pattern`.

## Installer subcommands (inside the binary)

- `smart-allow install` — interactive wizard; prints status, then a
  numbered menu to install/reinstall globally, install/reinstall for the
  current project (git root walk-up), uninstall, or quit.
- `smart-allow install --global|--project|--here|--path DIR` —
  non-interactive, optionally with `--yes`.
- `smart-allow install --status` — just prints the current state.
- `smart-allow uninstall [--global|--project|--all|--here|--path DIR]`.
- `smart-allow policy list|show|set NAME|edit` — replaces the former
  `scripts/claude-policy` bash util.

Hook-mode invocation (what Claude Code runs) is unchanged: if the first arg
is empty or starts with `-`, the binary reads stdin and runs the classifier.
