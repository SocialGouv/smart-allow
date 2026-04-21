package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/SocialGouv/smart-allow/internal/appinfo"
)

// Shared paths under $HOME/.claude/. Factored out because they appear in
// install.go, uninstall.go and policy_cmd.go; keeping one source of truth
// avoids drift if the layout ever changes.

func globalSettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}
func policiesDir(home string) string { return filepath.Join(home, ".claude", "policies") }
func policyPath(home, name string) string {
	return filepath.Join(policiesDir(home), name+".md")
}
func activePolicyPath(home string) string {
	return filepath.Join(home, ".claude", "active-policy.md")
}
func installedBinaryPath(home string) string {
	return filepath.Join(home, ".claude", "bin", appinfo.Name)
}

// setActivePolicy repoints $HOME/.claude/active-policy.md at the named
// policy file. Returns an error if the target doesn't exist. Used by
// installPolicies (first-install default to "normal") and `policy set`.
func setActivePolicy(home, name string) error {
	target := policyPath(home, name)
	if _, err := os.Stat(target); err != nil {
		return err
	}
	active := activePolicyPath(home)
	_ = os.Remove(active)
	return os.Symlink(target, active)
}

// backupSettings writes raw next to settingsPath as
// settings.json.bak-<YYYYMMDD-HHMMSS>. Called before every in-place
// modification of a user's settings.json so a manual rollback is always
// one `mv` away.
func backupSettings(settingsPath string, raw []byte) error {
	stamp := time.Now().Format("20060102-150405")
	return os.WriteFile(settingsPath+".bak-"+stamp, raw, 0o644)
}
