package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

type uninstallFlags struct {
	global  bool
	project bool
	here    bool
	path    string
	all     bool
	yes     bool
}

func runUninstall(args []string) int {
	fs2 := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs2.SetOutput(io.Discard)
	u := &uninstallFlags{}
	fs2.BoolVar(&u.global, "global", false, "")
	fs2.BoolVar(&u.project, "project", false, "")
	fs2.BoolVar(&u.here, "here", false, "")
	fs2.StringVar(&u.path, "path", "", "")
	fs2.BoolVar(&u.all, "all", false, "")
	fs2.BoolVar(&u.yes, "yes", false, "")
	fs2.BoolVar(&u.yes, "y", false, "")
	if err := fs2.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		return 2
	}

	st, err := detectStatus(&installFlags{here: u.here, path: u.path})
	if err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		return 1
	}

	var targets []string
	switch {
	case u.all:
		if st.GlobalInstalled {
			targets = append(targets, st.GlobalPath)
		}
		if st.ProjectInstalled {
			targets = append(targets, st.ProjectPath)
		}
	case u.global:
		targets = append(targets, st.GlobalPath)
	case u.project || u.here || u.path != "":
		targets = append(targets, st.ProjectPath)
	default:
		// Interactive.
		printStatus(st)
		fmt.Println()
		if st.GlobalInstalled && promptYN("Remove global hook?", false) {
			targets = append(targets, st.GlobalPath)
		}
		if st.ProjectInstalled && promptYN(
			fmt.Sprintf("Remove project hook (%s)?", st.ProjectRoot), false) {
			targets = append(targets, st.ProjectPath)
		}
	}
	if len(targets) == 0 {
		fmt.Println("nothing to uninstall.")
		return 0
	}
	for _, p := range targets {
		if err := removeHook(p); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall:", err)
			return 1
		}
		fmt.Printf("  hook removed from %s\n", p)
	}
	return 0
}

// removeHook deletes any PreToolUse matcher entry whose command contains the
// sentinel, backs the file up, collapses empty structures, and removes the
// file entirely if the resulting JSON object is empty.
func removeHook(settingsPath string) error {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	if err := os.WriteFile(settingsPath+".bak-"+stamp, raw, 0o644); err != nil {
		return err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	hooks, _ := obj["hooks"].(map[string]interface{})
	if hooks == nil {
		return nil
	}
	pre, _ := hooks["PreToolUse"].([]interface{})
	newPre := make([]interface{}, 0, len(pre))
	for _, m := range pre {
		mm, ok := m.(map[string]interface{})
		if !ok {
			newPre = append(newPre, m)
			continue
		}
		inner, _ := mm["hooks"].([]interface{})
		kept := make([]interface{}, 0, len(inner))
		for _, h := range inner {
			hh, ok := h.(map[string]interface{})
			if !ok {
				kept = append(kept, h)
				continue
			}
			if cmd, _ := hh["command"].(string); strings.Contains(cmd, hookSentinel) {
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		mm["hooks"] = kept
		newPre = append(newPre, mm)
	}
	if len(newPre) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = newPre
	}
	if len(hooks) == 0 {
		delete(obj, "hooks")
	} else {
		obj["hooks"] = hooks
	}
	if len(obj) == 0 {
		return os.Remove(settingsPath)
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0o644)
}
