package main

import (
	"os"
	"path/filepath"
)

// loadPolicy returns (content, source). Priority:
//
//	$projectDir/.claude/session-policy.md
//	$projectDir/.claude/policy.md
//	$HOME/.claude/active-policy.md   (a symlink maintained by claude-policy)
//	$HOME/.claude/policies/normal.md (baseline)
//	<built-in default>
func loadPolicy(projectDir, home string) (content, source string) {
	candidates := []string{
		filepath.Join(projectDir, ".claude", "session-policy.md"),
		filepath.Join(projectDir, ".claude", "policy.md"),
		filepath.Join(home, ".claude", "active-policy.md"),
		filepath.Join(home, ".claude", "policies", "normal.md"),
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b), p
		}
	}
	return defaultPolicy, "<default>"
}

const defaultPolicy = "Politique par défaut : demander confirmation pour toute action " +
	"destructive ou qui touche des ressources hors du répertoire courant."
