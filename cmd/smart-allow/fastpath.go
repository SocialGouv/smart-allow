package main

import (
	"regexp"
	"strings"
)

// FastPathDecision: deterministic verdict before any LLM call.
// Returns "approve", "ask", "deny", or "" (undecided → fall through to LLM).
func fastPath(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return "approve"
	}

	for _, pat := range hardDenySubstrings {
		if strings.Contains(cmd, pat) {
			return "deny"
		}
	}

	for _, re := range dangerousPatterns {
		if re.MatchString(cmd) {
			return ""
		}
	}

	// AI-exfil checks. Must run before safe-prefix approval because commands
	// like `cat .env` match the safe-prefix "cat " but their stdout flows
	// back into Claude Code's context (= a cloud LLM provider).
	sens := mentionsSensitiveRead(cmd)
	ai := mentionsAIProvider(cmd)
	if sens && ai {
		return "deny"
	}
	// Sensitive-read OR provider call alone: the policy says "ask". We emit it
	// deterministically instead of falling through to the LLM because small
	// local models tend to approve `cat .env` on the grounds that the file is
	// in the working directory, missing the secret-exfil framing entirely.
	if sens || ai {
		return "ask"
	}

	hasComplexity := false
	for _, c := range complexityChars {
		if strings.Contains(cmd, c) {
			hasComplexity = true
			break
		}
	}

	if _, ok := safeExact[cmd]; ok {
		return "approve"
	}

	if !hasComplexity {
		for _, p := range safePrefixes {
			if cmd == strings.TrimSpace(p) || strings.HasPrefix(cmd, p) {
				return "approve"
			}
		}
	}

	return ""
}

var safeExact = map[string]struct{}{
	"pwd": {}, "whoami": {}, "hostname": {}, "date": {}, "uptime": {}, "id": {},
}

var safePrefixes = []string{
	"ls ", "ls\n", "ls\t", "cat ", "less ", "head ", "tail ", "stat ", "file ",
	"grep ", "rg ", "egrep ", "fgrep ", "find ", "wc ", "which ", "whereis ",
	"echo ", "printf ",
	"git status", "git log", "git diff", "git show", "git branch", "git remote",
	"git blame", "git reflog", "git stash list", "git config --get",
	"docker ps", "docker logs ", "docker inspect ", "docker images",
	"kubectl get ", "kubectl describe ", "kubectl logs ", "kubectl top ",
	"kubectl events", "kubectl version", "kubectl config view",
	"helm list", "helm history", "helm get ", "helm status ",
	"terraform plan", "terraform show", "terraform state list",
	"npm list", "npm ls", "pip list", "pip show", "cargo tree",
	"python --version", "node --version", "go version",
}

var hardDenySubstrings = []string{
	"rm -rf /", "rm -rf /*", "rm -rf /.", "rm -rf ~", "rm -rf $HOME",
	":(){ :|:& };:",
	"mkfs.", "mkfs ",
	"dd if=/dev/zero of=/dev/", "dd if=/dev/random of=/dev/",
	"chmod -R 777 /", "chown -R ",
	"> /dev/sda", "> /dev/nvme",
}

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\|\s*(bash|sh|zsh)\b`),
	regexp.MustCompile(`curl\s+[^|]+\|\s*(bash|sh)`),
	regexp.MustCompile(`wget\s+[^|]+\|\s*(bash|sh)`),
	regexp.MustCompile(`eval\s+\$\(`),
}

var complexityChars = []string{
	"|", "&&", "||", ";", ">", "<", "`", "$(", "&",
	"bash -c", "sh -c", "xargs", "exec", "npx",
}
