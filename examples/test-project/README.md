# smart-allow — isolated test project

A self-contained sandbox to exercise the classifier **without touching your global
`~/.claude/`**. The hook, cache, and log all live inside this folder.

## How it is isolated

- `.claude/settings.json` — a **project-scoped** Claude Code settings file that registers
  the hook only when Claude Code runs with this directory as its project root.
- `.claude/hook.sh` — wrapper that:
  - locates the classifier at `<repo-root>/home/.claude/hooks/classify-command.py`,
  - sets `CLAUDE_CLASSIFIER_CACHE_DIR=$CLAUDE_PROJECT_DIR/.claude/cache`,
  - sets `CLAUDE_CLASSIFIER_LOG=$CLAUDE_PROJECT_DIR/.claude/classifier.log`,
  - then exec's the classifier.
- `.claude/session-policy.md` — the policy used for this test, loaded in priority
  over any global policy.

No files are written to `~/.claude/`.

## Run the test

```bash
./run-test.sh              # full: fast-path + LLM + cache + fail-safe
./run-test.sh --fast       # fast-path only (no Ollama required)
```

The script pipes synthetic `PreToolUse` events through the hook and asserts the
expected decision for each case:

| Command                                | Expected | Via         |
|----------------------------------------|----------|-------------|
| `ls -la`                               | approve  | fast-path   |
| `git status`                           | approve  | fast-path   |
| `rm -rf /`                             | deny     | fast-path   |
| `:(){ :\|:& };:`                        | deny     | fast-path   |
| (empty)                                | approve  | fast-path   |
| `kubectl apply -f deploy.yaml`         | ask/deny | LLM         |
| `kubectl apply -f deploy.yaml` (2nd)   | ask/deny | cache       |
| `curl … \| bash`                        | ask/deny | LLM         |
| unreachable Ollama + `pip install …`   | ask      | fail-safe   |

## Run Claude Code against this sandbox

If you have `claude` installed and want an end-to-end test:

```bash
cd examples/test-project
claude
```

Claude Code reads `.claude/settings.json` from this directory, wires the hook,
and every Bash command it tries will be gated by the local classifier — still with
zero writes to `~/.claude/`.

## Clean up

```bash
rm -rf .claude/cache .claude/classifier.log
```
