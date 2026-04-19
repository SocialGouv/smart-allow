// Classifier de commandes Bash pour Claude Code (hook PreToolUse).
// Lit un événement JSON sur stdin, émet le verdict au format
// hookSpecificOutput.permissionDecision (Claude Code ≥ 2.1).
//
// Pipeline:
//  1. fast-path déterministe (allowlist/denylist)
//  2. cache local (TTL 1h par défaut)
//  3. LLM local via Ollama
//  4. fail-safe → "ask" si le LLM échoue
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/SocialGouv/smart-allow/internal/appinfo"
)

var (
	envOllamaHost    = envOr("OLLAMA_HOST", "http://127.0.0.1:11434")
	envOllamaModel   = envOr("CLAUDE_CLASSIFIER_MODEL", "qwen2.5-coder:7b")
	envOllamaTimeout = envDurationSec("CLAUDE_CLASSIFIER_TIMEOUT", 12)
	envCacheTTL      = envDurationSec("CLAUDE_CLASSIFIER_CACHE_TTL", 3600)
	envDebug         = os.Getenv("CLAUDE_HOOK_DEBUG") == "1"
)

func main() {
	// Minimal flag handling: --version / -v prints the build identity and exits.
	// Everything else falls through to the hook pipeline (which reads stdin).
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v":
			fmt.Printf("%s %s\n", appinfo.Name, appinfo.FullVersion())
			return
		case "--help", "-h":
			fmt.Fprintf(os.Stderr,
				"%s %s — Claude Code PreToolUse Bash classifier.\n\n"+
					"Usage: %s  (reads a PreToolUse JSON event on stdin, writes a\n"+
					"         hookSpecificOutput decision JSON on stdout)\n\n"+
					"Flags:\n  -v, --version   print version and exit\n  -h, --help      this help\n",
				appinfo.Name, appinfo.FullVersion(), appinfo.Name)
			return
		}
	}

	home, _ := os.UserHomeDir()

	cacheDir := envOr("CLAUDE_CLASSIFIER_CACHE_DIR", filepath.Join(home, ".claude", "classifier-cache"))
	logFile := envOr("CLAUDE_CLASSIFIER_LOG", filepath.Join(home, ".claude", "classifier.log"))

	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		emit("ask", fmt.Sprintf("hook read error: %v", err))
		return
	}

	var event struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(inputBytes, &event); err != nil {
		emit("ask", fmt.Sprintf("invalid hook input: %v", err))
		return
	}

	command := event.ToolInput.Command
	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	projectDir := envOr("CLAUDE_PROJECT_DIR", cwd)

	if command == "" {
		emit("approve", "empty command")
		return
	}

	// 1. Fast-path
	switch fastPath(command) {
	case "approve":
		debugf("fast-path APPROVE: %s", head(command, 80))
		emit("approve", "fast-path: safe prefix")
		logEvent(logFile, map[string]interface{}{
			"cmd":      command,
			"decision": "approve",
			"via":      "fast-path",
		})
		return
	case "deny":
		debugf("fast-path DENY: %s", head(command, 80))
		emit("deny", "fast-path: hard-deny pattern")
		logEvent(logFile, map[string]interface{}{
			"cmd":      command,
			"decision": "deny",
			"via":      "fast-path",
		})
		return
	}

	// 2. Cache
	policy, policySource := loadPolicy(projectDir, home)
	pHash := policyHash(policy)
	key := cacheKey(command, pHash, envOllamaModel)

	if e := cacheGet(cacheDir, key, envCacheTTL); e != nil {
		debugf("cache HIT: %+v", *e)
		emit(e.Decision, e.Reason)
		logEvent(logFile, map[string]interface{}{
			"cmd":      command,
			"decision": e.Decision,
			"via":      "cache",
			"policy":   policySource,
		})
		return
	}

	// 3. Ollama
	entry, err := callOllama(envOllamaHost, envOllamaModel, command, policy, cwd, envOllamaTimeout)
	if err != nil {
		debugf("ollama FAILED: %v", err)
		reason := fmt.Sprintf("classifier unavailable: %s", head(err.Error(), 80))
		emit("ask", reason)
		logEvent(logFile, map[string]interface{}{
			"cmd":      command,
			"decision": "ask",
			"via":      "fail-safe",
			"error":    head(err.Error(), 200),
		})
		return
	}

	cacheSet(cacheDir, key, entry)
	debugf("llm: %+v", entry)
	emit(entry.Decision, entry.Reason)
	logEvent(logFile, map[string]interface{}{
		"cmd":      command,
		"decision": entry.Decision,
		"reason":   entry.Reason,
		"via":      "ollama",
		"model":    envOllamaModel,
		"policy":   policySource,
	})
}

// emit writes the Claude Code hookSpecificOutput JSON envelope to stdout.
// Internal decision values approve/ask/deny map to Claude Code's allow/ask/deny.
func emit(decision, reason string) {
	perm := decision
	if decision == "approve" {
		perm = "allow"
	}
	payload := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       perm,
			"permissionDecisionReason": reason,
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(payload)
}

func logEvent(path string, record map[string]interface{}) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	record["ts"] = float64(time.Now().Unix())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(record)
}

func debugf(format string, args ...interface{}) {
	if envDebug {
		fmt.Fprintf(os.Stderr, "[classifier] "+format+"\n", args...)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDurationSec(key string, defSec int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(defSec) * time.Second
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
