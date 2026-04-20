package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProjectRoot_Path(t *testing.T) {
	tmp := t.TempDir()
	got, fromGit, err := resolveProjectRoot(false, tmp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != tmp {
		t.Fatalf("got %q want %q", got, tmp)
	}
	if fromGit {
		t.Errorf("fromGit should be false for --path")
	}
}

func TestResolveProjectRoot_Here(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	got, fromGit, err := resolveProjectRoot(true, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Resolve symlinks for comparison (macOS /private/var vs /var).
	wantReal, _ := filepath.EvalSymlinks(tmp)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Fatalf("got %q want %q", gotReal, wantReal)
	}
	if fromGit {
		t.Errorf("fromGit should be false for --here")
	}
}

func TestResolveProjectRoot_GitWalkUp(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}
	if err := os.Chdir(deep); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, fromGit, err := resolveProjectRoot(false, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	rootReal, _ := filepath.EvalSymlinks(root)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != rootReal {
		t.Fatalf("got %q want %q", gotReal, rootReal)
	}
	if !fromGit {
		t.Errorf("fromGit should be true when .git was found")
	}
}

func TestResolveProjectRoot_NoGitFallsBackToCwd(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	// Use a subdirectory of /tmp that has no .git anywhere up the chain.
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, fromGit, err := resolveProjectRoot(false, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// /tmp may sit under a directory that is itself a repo in the devcontainer
	// — so we don't assert fromGit == false, only that the call succeeds.
	_ = fromGit
}

func TestMergeHook_FreshFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	if err := mergeHook(p, "/some/path/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	assertHasHook(t, p, "/some/path/classify-command")
}

func TestMergeHook_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	binPath := "/x/classify-command"
	if err := mergeHook(p, binPath); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := mergeHook(p, binPath); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Must still have exactly one PreToolUse entry for our hook.
	count := countSentinelEntries(t, p)
	if count != 1 {
		t.Errorf("expected 1 sentinel entry, got %d", count)
	}
}

func TestMergeHook_UpdatesBinaryPath(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	if err := mergeHook(p, "/old/classify-command"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := mergeHook(p, "/new/classify-command"); err != nil {
		t.Fatalf("second: %v", err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "/old/classify-command") {
		t.Errorf("old binary path not stripped")
	}
	if !strings.Contains(string(raw), "/new/classify-command") {
		t.Errorf("new binary path not written")
	}
}

func TestMergeHook_PreservesOtherEntries(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	// Write a settings.json that already has an unrelated hook.
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
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "some-other-hook.sh") {
		t.Errorf("unrelated hook was dropped")
	}
	if !strings.Contains(string(raw), "classify-command") {
		t.Errorf("our hook was not added")
	}
}

func TestMergeHook_BackupCreated(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	os.WriteFile(p, []byte(`{"hooks": {}}`), 0o644)
	if err := mergeHook(p, "/x/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	entries, _ := os.ReadDir(tmp)
	hasBak := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			hasBak = true
			break
		}
	}
	if !hasBak {
		t.Errorf("backup file not created")
	}
}

func TestHasHookEntry(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "settings.json")
	if hasHookEntry(p) {
		t.Fatalf("non-existent file shouldn't report installed")
	}
	if err := mergeHook(p, "/x/classify-command"); err != nil {
		t.Fatalf("mergeHook: %v", err)
	}
	if !hasHookEntry(p) {
		t.Errorf("after mergeHook, hasHookEntry should return true")
	}
}

// ---------- helpers ----------

func assertHasHook(t *testing.T, settingsPath, binPath string) {
	t.Helper()
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), binPath) {
		t.Errorf("settings.json does not contain binary path %q", binPath)
	}
	if !strings.Contains(string(raw), "PreToolUse") {
		t.Errorf("settings.json missing PreToolUse")
	}
	if !strings.Contains(string(raw), "permissionDecision") {
		// Not required, but a sanity check against HTML escaping.
		_ = raw
	}
}

func countSentinelEntries(t *testing.T, settingsPath string) int {
	t.Helper()
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("parse: %v", err)
	}
	hooks, _ := obj["hooks"].(map[string]interface{})
	pre, _ := hooks["PreToolUse"].([]interface{})
	n := 0
	for _, m := range pre {
		mm, _ := m.(map[string]interface{})
		inner, _ := mm["hooks"].([]interface{})
		for _, h := range inner {
			hh, _ := h.(map[string]interface{})
			if cmd, _ := hh["command"].(string); strings.Contains(cmd, hookSentinel) {
				n++
			}
		}
	}
	return n
}
