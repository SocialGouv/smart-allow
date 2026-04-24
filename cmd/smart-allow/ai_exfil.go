package main

import (
	"regexp"
	"strings"
)

// aiProviderSubstrings: hostnames of cloud LLM providers we refuse to leak
// secrets to. Ollama is LOCAL by design and is NOT in this list, nor are
// localhost/loopback/host.docker.internal endpoints, so `curl
// http://127.0.0.1:11434/...` and `ollama run ...` remain fine.
var aiProviderSubstrings = []string{
	"api.openai.com",
	"api.anthropic.com",
	"api.cohere.com", "api.cohere.ai",
	"api.mistral.ai",
	"api.groq.com",
	"api.deepseek.com",
	"api.together.ai", "api.together.xyz",
	"api.perplexity.ai",
	"api.x.ai",
	"generativelanguage.googleapis.com",
	"api-inference.huggingface.co",
	"api.fireworks.ai",
	"api.replicate.com",
}

// aiProviderCLIs: CLIs that send their input to a cloud LLM. Matched as a
// whole token, i.e. at the start of the command or after a shell separator.
// `ollama` is intentionally absent.
var aiProviderCLIs = []string{
	"openai", "chatgpt", "gemini", "claude",
	"mistralai", "cohere", "perplexity", "deepseek",
}

// sensitivePathSubstrings: reading these paths typically exposes a secret,
// and because stdout flows back to Claude Code (= Anthropic), even a bare
// `cat .env` counts as exfiltration to a cloud LLM provider.
var sensitivePathSubstrings = []string{
	".env",
	"/.ssh/",
	"/.aws/credentials", "/.aws/config",
	"/.gnupg/",
	"/.npmrc", "/.pypirc",
	"/.git-credentials",
	"/.config/gh/hosts.yml",
	"/.config/gcloud/",
	"/.kube/config",
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
	".pem", ".p12", ".pfx",
}

// envVarRefRE matches any `$NAME` or `${NAME}` reference. The name is then
// checked against secretKeywordSubstrings to decide if it is secret-shaped.
var envVarRefRE = regexp.MustCompile(`\$\{?([A-Z][A-Z0-9_]*)\}?`)

// secretKeywordSubstrings: substrings of an env-var name that strongly
// suggest it holds a credential. We check by Contains, so `GITHUB_TOKEN`,
// `AWS_SECRET_ACCESS_KEY`, `API_KEY` all match; `PATH`, `HOME`, `MAP_KEY`
// do not.
var secretKeywordSubstrings = []string{
	"TOKEN", "PASSWORD", "PASSWD", "SECRET",
	"CREDENTIAL", "APIKEY", "API_KEY",
}

// envDumpRE matches commands that dump the whole environment (`env`,
// `printenv`) as a standalone command or at the head of a pipe. `env VAR=1
// cmd` is the exec prefix form and is NOT matched.
var envDumpRE = regexp.MustCompile(`(?:^|[;&|]\s*)(env|printenv)\s*(?:$|[;|&])`)

func mentionsSensitiveRead(cmd string) bool {
	for _, s := range sensitivePathSubstrings {
		if strings.Contains(cmd, s) {
			return true
		}
	}
	if envDumpRE.MatchString(cmd) {
		return true
	}
	for _, m := range envVarRefRE.FindAllStringSubmatch(cmd, -1) {
		name := m[1]
		for _, kw := range secretKeywordSubstrings {
			if strings.Contains(name, kw) {
				return true
			}
		}
	}
	return false
}

func mentionsAIProvider(cmd string) bool {
	for _, s := range aiProviderSubstrings {
		if strings.Contains(cmd, s) {
			return true
		}
	}
	for _, cli := range aiProviderCLIs {
		if hasCLIToken(cmd, cli) {
			return true
		}
	}
	return false
}

// hasCLIToken is true when `cli` appears as a standalone command, i.e. at
// the very start of cmd or immediately after a shell command separator
// (`;`, `|`, `&`, `(`, `\n`), optionally preceded by whitespace. It is NOT
// matched when cli appears as an argument to another command
// (`pip install openai` → false), nor as part of a bigger word
// (`./claude-code` → false).
func hasCLIToken(cmd, cli string) bool {
	n := len(cli)
	if n == 0 || len(cmd) < n {
		return false
	}
	for i := 0; i <= len(cmd)-n; i++ {
		if cmd[i:i+n] != cli {
			continue
		}
		if !leftIsCommandStart(cmd, i) {
			continue
		}
		if !rightIsTokenEnd(cmd, i+n) {
			continue
		}
		return true
	}
	return false
}

// leftIsCommandStart returns true iff the position `at` is at the start of
// cmd, or after a shell command separator (optionally with whitespace
// between). Plain whitespace alone does NOT qualify, because after a space
// the next token is usually an argument, not a new command.
func leftIsCommandStart(cmd string, at int) bool {
	if at == 0 {
		return true
	}
	j := at - 1
	for j >= 0 && (cmd[j] == ' ' || cmd[j] == '\t') {
		j--
	}
	if j < 0 {
		return true
	}
	switch cmd[j] {
	case ';', '|', '&', '(', '\n':
		return true
	}
	return false
}

// rightIsTokenEnd returns true iff position `at` is at the end of cmd, or
// just before a character that ends a shell token (whitespace or separator).
func rightIsTokenEnd(cmd string, at int) bool {
	if at >= len(cmd) {
		return true
	}
	switch cmd[at] {
	case ' ', '\t', '\n', ';', '|', '&', ')':
		return true
	}
	return false
}
