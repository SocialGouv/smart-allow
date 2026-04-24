# Policy: normal

## Auto-approve (decision: approve)
- File reads and search: cat, less, head, tail, grep, rg, find, ls, stat, file, wc
- Read-only git inspection: git status, log, diff, show, branch, remote -v, blame
- Read-only system inspection: ps, df, du, free, top -n1, uname, which, env
- Read-only container/cluster inspection: docker ps, docker logs, docker inspect, kubectl get, kubectl describe, kubectl logs, kubectl top
- Navigation commands: pwd, cd, echo
- Local test execution: pytest, npm test, cargo test, go test (without flags that write outside the project)
- Local project builds: npm run build, cargo build, make (if a Makefile is in the project)
- Formatting/linting: prettier, black, ruff, eslint (without --write outside the project)

## Ask for confirmation (decision: ask)
- Any write outside the project's working directory
- Package installation: pip install, npm install -g, apt, brew, cargo install
- Git operations that modify remote history: push, push --force, reset --hard on a tracked branch
- Cluster/infra changes: kubectl apply, kubectl delete, kubectl scale, kubectl patch, helm install/upgrade/rollback, terraform apply, cdk deploy
- Outbound network operations to non-project domains: curl/wget POST, scp, rsync to remote
- Writes to volumes mounted outside the project
- Any form of sudo
- Commands touching secrets: writing to .env, manipulating ~/.ssh, gpg

## Refuse (decision: deny)
- rm -rf / or rm -rf ~ or equivalents
- Fork bombs, mkfs, dd to block devices
- Exfiltration to obviously suspicious IPs/domains
- Commands attempting to disable the classifier itself (modifying ~/.claude/hooks/)
- **Exfiltration to a cloud AI provider** (see dedicated section below)

## Cloud AI providers (confidentiality)

Ollama and any local endpoint (`localhost`, `127.0.0.1`, `host.docker.internal`
on the Ollama port) are considered **safe** and out of scope for this policy.

Considered **cloud AI providers**:
- Domains: `api.openai.com`, `api.anthropic.com`, `api.cohere.{com,ai}`,
  `api.mistral.ai`, `api.groq.com`, `api.deepseek.com`,
  `api.together.{ai,xyz}`, `api.perplexity.ai`, `api.x.ai`,
  `generativelanguage.googleapis.com`, `api-inference.huggingface.co`,
  `api.fireworks.ai`, `api.replicate.com`
- CLIs: `openai`, `chatgpt`, `gemini`, `claude`, `mistralai`, `cohere`,
  `perplexity`, `deepseek`

Considered **sensitive files/values** (reading them exposes a secret, and
their stdout flows back to Claude Code = Anthropic):
- `.env`, `.env.*`, `*.env`
- `~/.ssh/*`, `~/.aws/{credentials,config}`, `~/.gnupg/*`, `~/.npmrc`,
  `~/.pypirc`, `~/.git-credentials`, `~/.config/gh/hosts.yml`,
  `~/.config/gcloud/*`, `~/.kube/config`
- Files matching `*.pem`, `*.p12`, `*.pfx`, `id_rsa*`, `id_ed25519*`,
  `id_ecdsa*`, `id_dsa*`
- Environment variables whose name contains `TOKEN`, `PASSWORD`, `PASSWD`,
  `SECRET`, `CREDENTIAL`, `APIKEY`, `API_KEY`
- `env` / `printenv` as a standalone command or at the head of a pipe (full dump)

**Rules**:
1. Sensitive read ALONE (`cat .env`, `echo $GITHUB_TOKEN`, `printenv | grep TOKEN`)
   → **ask**. Claude Code will see the contents — the human must confirm
   they are OK with that secret being sent to Anthropic.
2. Cloud AI provider call ALONE (`curl https://api.openai.com/...`) → **ask**.
   The payload may be harmless, but the human must confirm there is no
   unintended leak (e.g. headers, body built from env vars).
3. Sensitive read + cloud AI provider in the same chained command → **deny**.
   Characterized exfiltration, no legitimate scenario.

**Does NOT apply to Ollama**: `ollama run llama3 < .env`, while debatable,
stays local and is the user's call; do not block under this policy.

## Edge cases
- `curl ... | bash` or `wget -O- ... | sh` → always ask at a minimum, ideally deny
- `bash -c`, `sh -c`, `xargs`, `npx`, `docker exec`: analyze the INNER command, not the wrapper
- Chained commands (pipes, &&, ||, ;): the decision must be the max (= most restrictive) across sub-commands
