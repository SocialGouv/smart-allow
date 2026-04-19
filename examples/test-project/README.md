# smart-allow — isolated test project

A self-contained sandbox to exercise the Go classifier **without touching your
global `~/.claude/`**. Cache and log stay inside this folder.

## How it is isolated

- [.claude/settings.json](.claude/settings.json) — project-scoped Claude Code
  settings. The hook command sets `CLAUDE_CLASSIFIER_CACHE_DIR` and
  `CLAUDE_CLASSIFIER_LOG` inline to point inside this folder, then invokes the
  binary at `$SMART_ALLOW_BIN` (or `~/.claude/bin/classify-command` if unset).
- [.claude/session-policy.md](.claude/session-policy.md) — the policy loaded in
  priority over any global one.

No files are written to `~/.claude/`.

## Run the test

```bash
./run-test.sh              # fast-path + LLM + cache + fail-safe
./run-test.sh --fast       # fast-path only (no Ollama required)
```

The script pipes synthetic `PreToolUse` events through the binary and asserts
the expected `permissionDecision` for each case:

| Command                                | Expected | Via         |
|----------------------------------------|----------|-------------|
| `ls -la`                               | allow    | fast-path   |
| `git status`                           | allow    | fast-path   |
| `rm -rf /`                             | deny     | fast-path   |
| `:(){ :\|:& };:`                        | deny     | fast-path   |
| (empty)                                | allow    | fast-path   |
| `kubectl apply -f deploy.yaml`         | ask/deny | LLM         |
| `kubectl apply -f deploy.yaml` (2nd)   | ask/deny | cache       |
| `curl … \| bash`                        | ask/deny | LLM         |
| unreachable Ollama + `pip install …`   | ask      | fail-safe   |

## Run Claude Code against this sandbox

```bash
cd examples/test-project
claude
```

Claude Code reads `.claude/settings.json` from this directory, wires the hook,
and every Bash command gets gated by the classifier — still with zero writes
to `~/.claude/`.

## Clean up

```bash
rm -r .claude/cache .claude/classifier.log 2>/dev/null
```
