package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SocialGouv/smart-allow/policies"
)

func TestPolicyNamesFromEmbed(t *testing.T) {
	names := policies.Names()
	for _, want := range []string{"normal", "strict", "permissive"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("embedded policies missing %q (got %v)", want, names)
		}
	}
}

func TestInstallPoliciesAndSetActive(t *testing.T) {
	// Redirect HOME so the installer writes into a sandbox.
	sandbox := t.TempDir()
	t.Setenv("HOME", sandbox)

	if err := installPolicies(sandbox); err != nil {
		t.Fatalf("installPolicies: %v", err)
	}

	// The 3 policies must be on disk.
	dir := filepath.Join(sandbox, ".claude", "policies")
	for _, n := range []string{"normal", "strict", "permissive"} {
		if _, err := os.Stat(filepath.Join(dir, n+".md")); err != nil {
			t.Errorf("policy %s not written: %v", n, err)
		}
	}
	// active-policy.md must point at normal.md.
	target, err := os.Readlink(filepath.Join(sandbox, ".claude", "active-policy.md"))
	if err != nil {
		t.Fatalf("active-policy symlink: %v", err)
	}
	if filepath.Base(target) != "normal.md" {
		t.Errorf("active symlink points at %s, want normal.md", target)
	}

	// runPolicy("set", "strict") should re-point the symlink.
	if rc := runPolicy([]string{"set", "strict"}); rc != 0 {
		t.Errorf("runPolicy set strict exit %d", rc)
	}
	target2, err := os.Readlink(filepath.Join(sandbox, ".claude", "active-policy.md"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if filepath.Base(target2) != "strict.md" {
		t.Errorf("active symlink now %s, want strict.md", target2)
	}
}
