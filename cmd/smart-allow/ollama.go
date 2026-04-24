package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

const systemPrompt = `You are a security classifier for an autonomous coding agent.

You receive a shell command and a security policy. You must decide between:
- "approve": the command is safe AND compliant with the policy. No human validation needed.
- "ask": the command is not clearly forbidden but has a notable or irreversible effect, or falls outside the obvious perimeter. Request human confirmation.
- "deny": the command is clearly forbidden by the policy or manifestly dangerous.

IMPORTANT RULES:
1. When in doubt, answer "ask". Never "approve" if you are not sure.
2. For chained commands (pipes, &&, ;), the decision is the most restrictive of all sub-commands.
3. For wrappers (bash -c, sh -c, xargs, npx, docker exec): analyze the INNER command, not the wrapper.
4. Beware of obfuscation: variable substitution, base64, eval, commands split across tokens.

Respond with ONLY a JSON object on a single line, no surrounding text, no backticks:
{"decision":"approve","reason":"..."} OR {"decision":"ask","reason":"..."} OR {"decision":"deny","reason":"..."}

"reason" must be short (< 120 characters), in English, explaining the triggering criterion.`

type ollamaRequest struct {
	Model   string                 `json:"model"`
	System  string                 `json:"system"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Format  string                 `json:"format"`
	Options map[string]interface{} `json:"options"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

// callOllama invokes the local LLM and returns a parsed decision.
func callOllama(host, model, command, policy, cwd string, timeout time.Duration) (cacheEntry, error) {
	userPrompt := fmt.Sprintf(`ACTIVE POLICY:
%s

---

WORKING DIRECTORY: %s

COMMAND TO CLASSIFY:
%s

Respond with A SINGLE JSON object, nothing else.`, policy, cwd, command)

	reqBody, _ := json.Marshal(ollamaRequest{
		Model:  model,
		System: systemPrompt,
		Prompt: userPrompt,
		Stream: false,
		Format: "json",
		Options: map[string]interface{}{
			"temperature": 0.0,
			"num_predict": 150,
		},
	})

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(host+"/api/generate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return cacheEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return cacheEntry{}, fmt.Errorf("ollama %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return cacheEntry{}, err
	}
	var or ollamaResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return cacheEntry{}, fmt.Errorf("parse outer: %w", err)
	}

	return parseDecision(or.Response)
}

var decisionFallback = regexp.MustCompile(`\{[^{}]*"decision"[^{}]*\}`)

func parseDecision(responseText string) (cacheEntry, error) {
	var out cacheEntry
	text := responseText

	if err := json.Unmarshal([]byte(text), &out); err != nil {
		// Fallback: extract the first {"decision":...} object.
		m := decisionFallback.FindString(text)
		if m == "" {
			return cacheEntry{}, fmt.Errorf("no JSON in response: %s", truncate(text, 200))
		}
		if err := json.Unmarshal([]byte(m), &out); err != nil {
			return cacheEntry{}, fmt.Errorf("parse inner: %w", err)
		}
	}

	switch out.Decision {
	case "approve", "ask", "deny":
	default:
		return cacheEntry{}, fmt.Errorf("invalid decision: %q", out.Decision)
	}
	if len(out.Reason) > 200 {
		out.Reason = out.Reason[:200]
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
