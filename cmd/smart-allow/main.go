// Classifier de commandes Bash pour Claude Code (hook PreToolUse).
// Lit un événement JSON sur stdin, émet le verdict au format
// hookSpecificOutput.permissionDecision (Claude Code ≥ 2.1).
//
// Subcommands:
//
//	classify-command            # hook mode (stdin: PreToolUse JSON)
//	classify-command install    # interactive / scoped installer
//	classify-command uninstall  # remove hook from global / project settings
//	classify-command policy …   # switch / inspect / edit active policy
//	classify-command --version
//	classify-command --help
//
// Pipeline of hook mode:
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
	first := ""
	if len(os.Args) > 1 {
		first = os.Args[1]
	}
	switch first {
	case "--version", "-v":
		fmt.Printf("%s %s\n", appinfo.Name, appinfo.FullVersion())
		return
	case "--help", "-h":
		printHelp()
		return
	case "install":
		os.Exit(runInstall(os.Args[2:]))
	case "uninstall":
		os.Exit(runUninstall(os.Args[2:]))
	case "enable":
		os.Exit(runEnable(os.Args[2:]))
	case "disable":
		os.Exit(runDisable(os.Args[2:]))
	case "policy":
		os.Exit(runPolicy(os.Args[2:]))
	default:
		// Hook mode expects a PreToolUse JSON event on stdin. If stdin is a
		// terminal and no flag was passed, the user almost certainly ran the
		// command by hand wanting help — don't hang waiting for input.
		if first == "" && isTerminal(os.Stdin) {
			printHelp()
			return
		}
		os.Exit(runHook(os.Args[1:]))
	}
}

// runEnable is the friendly alias for `install --project --yes`. Flags pass
// through, so `smart-allow enable --global` still works, and `--yes` is only
// added when not already present.
func runEnable(args []string) int {
	return runInstall(augmentArgs(args, "--project", "--yes"))
}

// runDisable is the friendly alias for `uninstall --project --yes`.
func runDisable(args []string) int {
	return runUninstall(augmentArgs(args, "--project", "--yes"))
}

// augmentArgs prepends a default scope flag when none of
// {--global, --project, --here, --path} was provided, and appends --yes when
// it wasn't already present. Used to build the argv for `enable` / `disable`
// from the user-typed args.
func augmentArgs(args []string, defaultScope, yesFlag string) []string {
	hasScope := false
	hasYes := false
	for _, a := range args {
		switch a {
		case "--global", "--project", "--here", "--path":
			hasScope = true
		case "--yes", "-y":
			hasYes = true
		}
	}
	out := make([]string, 0, len(args)+2)
	if !hasScope {
		out = append(out, defaultScope)
	}
	out = append(out, args...)
	if !hasYes {
		out = append(out, yesFlag)
	}
	return out
}

func printHelp() {
	n := appinfo.Name
	fmt.Fprintf(os.Stderr,
		`%s %s — Claude Code PreToolUse Bash classifier.

Usage:
  %s                                     # hook mode (stdin: PreToolUse JSON)
  %s --version
  %s --help

  %s install [--global|--project|--here|--path DIR|--status|--yes]
  %s uninstall [--global|--project|--here|--path DIR|--all|--yes]

  %s enable  [--global|--project|--here|--path DIR]   # alias: install --project --yes
  %s disable [--global|--project|--here|--path DIR]   # alias: uninstall --project --yes

  %s policy list
  %s policy show
  %s policy set {strict|normal|permissive}
  %s policy edit

Env:
  OLLAMA_HOST                  default http://127.0.0.1:11434
  CLAUDE_CLASSIFIER_MODEL      default qwen2.5-coder:7b
  CLAUDE_CLASSIFIER_TIMEOUT    seconds, default 12
  CLAUDE_CLASSIFIER_CACHE_TTL  seconds, default 3600
  CLAUDE_CLASSIFIER_CACHE_DIR  default $HOME/.claude/classifier-cache
  CLAUDE_CLASSIFIER_LOG        default $HOME/.claude/classifier.log
  CLAUDE_HOOK_DEBUG=1          stderr debug trace
`,
		n, appinfo.FullVersion(), n, n, n, n, n, n, n, n, n, n, n)
}

// runHook is the original hook pipeline, extracted so the main dispatcher can
// route non-subcommand invocations here. Reads a PreToolUse JSON event on
// stdin, writes a hookSpecificOutput JSON envelope on stdout.
func runHook(_ []string) int {
	home, _ := os.UserHomeDir()

	cacheDir := envOr("CLAUDE_CLASSIFIER_CACHE_DIR", filepath.Join(home, ".claude", "classifier-cache"))
	logFile := envOr("CLAUDE_CLASSIFIER_LOG", filepath.Join(home, ".claude", "classifier.log"))

	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		emit("ask", fmt.Sprintf("hook read error: %v", err))
		return 0
	}

	var event struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(inputBytes, &event); err != nil {
		emit("ask", fmt.Sprintf("invalid hook input: %v", err))
		return 0
	}

	command := event.ToolInput.Command
	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	projectDir := envOr("CLAUDE_PROJECT_DIR", cwd)

	if command == "" {
		emit("approve", "empty command")
		return 0
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
		return 0
	case "deny":
		debugf("fast-path DENY: %s", head(command, 80))
		emit("deny", "fast-path: hard-deny pattern")
		logEvent(logFile, map[string]interface{}{
			"cmd":      command,
			"decision": "deny",
			"via":      "fast-path",
		})
		return 0
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
		return 0
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
		return 0
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
	return 0
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
