package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveHook_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")

	// Install then remove.
	if err := mergeHook(p, "/x/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	if err := removeHook(p); err != nil {
		t.Fatalf("removeHook: %v", err)
	}
	if hasHookEntry(p) {
		t.Errorf("hook entry still present after removeHook")
	}
	// Second remove: no-op, no error.
	if err := removeHook(p); err != nil {
		t.Errorf("second removeHook failed: %v", err)
	}
}

func TestRemoveHook_PreservesSiblings(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	existing := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit",
        "hooks": [
          {"type": "command", "command": "some-other-hook.sh", "timeout": 5000}
        ]
      }
    ]
  }
}`
	os.WriteFile(p, []byte(existing), 0o644)

	if err := mergeHook(p, "/x/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	if err := removeHook(p); err != nil {
		t.Fatalf("removeHook: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("settings.json disappeared despite having sibling hook")
	}
	if !strings.Contains(string(raw), "some-other-hook.sh") {
		t.Errorf("sibling hook lost")
	}
	if strings.Contains(string(raw), hookSentinel) {
		t.Errorf("our hook not removed")
	}
}

func TestRemoveHook_DeletesEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	if err := mergeHook(p, "/x/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	if err := removeHook(p); err != nil {
		t.Fatalf("removeHook: %v", err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Errorf("empty settings.json should be removed, still present")
	}
}
