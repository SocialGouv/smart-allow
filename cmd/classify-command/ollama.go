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

const systemPrompt = `Tu es un classifieur de sécurité pour un agent de coding autonome.

Tu reçois une commande shell et une politique de sécurité. Tu dois décider entre :
- "approve" : la commande est sûre ET conforme à la politique. Aucun humain n'a besoin de la valider.
- "ask" : la commande n'est pas clairement interdite mais a un effet notable, irréversible, ou sort du périmètre évident. Demander confirmation à l'humain.
- "deny" : la commande est clairement interdite par la politique ou manifestement dangereuse.

RÈGLES IMPORTANTES :
1. En cas de doute, réponds "ask". Ne jamais "approve" si tu n'es pas sûr.
2. Pour les commandes chaînées (pipes, &&, ;), la décision est la plus restrictive de toutes les sous-commandes.
3. Pour les wrappers (bash -c, sh -c, xargs, npx, docker exec) : analyse la commande INTERNE, pas le wrapper.
4. Méfie-toi des obfuscations : variables de substitution, base64, eval, commandes en plusieurs mots collés.

Réponds UNIQUEMENT avec un objet JSON sur une seule ligne, sans texte autour, sans backticks :
{"decision":"approve","reason":"..."} OU {"decision":"ask","reason":"..."} OU {"decision":"deny","reason":"..."}

"reason" doit être court (< 120 caractères), en français, expliquant le critère déclencheur.`

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
	userPrompt := fmt.Sprintf(`POLITIQUE ACTIVE :
%s

---

WORKING DIRECTORY : %s

COMMANDE À CLASSIFIER :
%s

Réponds par UN SEUL objet JSON, rien d'autre.`, policy, cwd, command)

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
