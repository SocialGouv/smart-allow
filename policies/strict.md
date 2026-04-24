# Policy: strict

Maximum defensive mode. When in doubt, always ask.

## Auto-approve (decision: approve)
- Only PURE and LOCAL reads: cat, head, tail, ls, pwd, echo, which, file, stat
- git status, git log, git diff (without --cached on remote branches)
- kubectl get / describe / logs ONLY in namespaces listed in session-policy.md

## Everything else → ask or deny
- No auto-approved writes, not even inside the project
- No auto-approved installations
- No kubectl commands that modify state
- No auto-approved SSH/SCP outbound connections

## Systematic deny
- Any command containing rm -rf
- Any command targeting a namespace/cluster marked "prod" in session-policy.md
- Any disabling/modification of the hooks themselves
- Any read of a sensitive file (`.env`, `~/.ssh/*`, `~/.aws/credentials`,
  `~/.gnupg/*`, `~/.git-credentials`, `~/.npmrc`, `~/.pypirc`,
  `~/.kube/config`, `id_rsa*`, `*.pem`, `*.p12`, `*.pfx`)
- Any standalone `env` / `printenv` (full environment dump)
- Any reference to a secret-shaped environment variable (`$*TOKEN*`,
  `$*SECRET*`, `$*PASSWORD*`, `$*CREDENTIAL*`, `$*API_KEY*`, `$*APIKEY*`)
- Any call to a cloud AI provider (`api.openai.com`, `api.anthropic.com`,
  `api.cohere.{com,ai}`, `api.mistral.ai`, `api.groq.com`, `api.deepseek.com`,
  `api.together.{ai,xyz}`, `api.perplexity.ai`, `api.x.ai`,
  `generativelanguage.googleapis.com`, `api-inference.huggingface.co`,
  `api.fireworks.ai`, `api.replicate.com`) or any associated CLI (`openai`,
  `chatgpt`, `gemini`, `claude`, `mistralai`, `cohere`, `perplexity`, `deepseek`)

Ollama and local endpoints (`localhost`, `127.0.0.1`,
`host.docker.internal` on port 11434) are explicitly out of this ban.
