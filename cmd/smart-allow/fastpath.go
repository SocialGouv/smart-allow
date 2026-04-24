package main

import (
	"regexp"
	"strings"
)

// fastPathVerdict is the deterministic verdict the fast-path produces before
// any LLM call. Decision is one of "approve" / "ask" / "deny", or "" when the
// fast-path can't decide and the command must fall through to cache+LLM.
// Reason is the short tag shipped as permissionDecisionReason, so the caller
// does not have to re-derive why a verdict was reached.
type fastPathVerdict struct {
	Decision string
	Reason   string
}

// undecided is a zero-value verdict that tells runHook to fall through to
// cache + LLM. Used wherever the fast-path refuses to commit (e.g. a
// dangerous regex matched but no hard rule applies).
var undecided = fastPathVerdict{}

func fastPath(command string) fastPathVerdict {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return fastPathVerdict{"approve", "fast-path: empty after trim"}
	}

	for _, pat := range hardDenySubstrings {
		if strings.Contains(cmd, pat) {
			return fastPathVerdict{"deny", "fast-path: hard-deny pattern"}
		}
	}

	for _, re := range dangerousPatterns {
		if re.MatchString(cmd) {
			return undecided
		}
	}

	// AI-exfil checks. Must run before safe-prefix approval because commands
	// like `cat .env` match the safe-prefix "cat " but their stdout flows
	// back into Claude Code's context (= a cloud LLM provider).
	sens := mentionsSensitiveRead(cmd)
	ai := mentionsAIProvider(cmd)
	if sens && ai {
		return fastPathVerdict{"deny", "fast-path: AI-exfil (sensitive read + cloud provider)"}
	}
	if sens || ai {
		// Relax when the command is clearly aimed at a local LLM (Ollama CLI
		// or loopback on the Ollama port). Running inference through your
		// own local model is not exfiltration; don't friction the user.
		if sens && !ai && mentionsLocalLLM(cmd) {
			return undecided
		}
		// Otherwise we emit "ask" deterministically instead of falling
		// through to the LLM: small local models tend to approve
		// `cat .env` because the file is in the working directory,
		// missing the secret-exfil framing entirely.
		return fastPathVerdict{"ask", "fast-path: AI-exfil guard (secret or cloud provider)"}
	}

	hasComplexity := false
	for _, c := range complexityChars {
		if strings.Contains(cmd, c) {
			hasComplexity = true
			break
		}
	}

	if _, ok := safeExact[cmd]; ok {
		return fastPathVerdict{"approve", "fast-path: safe exact"}
	}

	if !hasComplexity {
		for _, p := range safePrefixes {
			if cmd == strings.TrimSpace(p) || strings.HasPrefix(cmd, p) {
				return fastPathVerdict{"approve", "fast-path: safe prefix"}
			}
		}
	}

	return undecided
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
