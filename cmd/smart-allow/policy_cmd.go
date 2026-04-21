package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/SocialGouv/smart-allow/policies"
)

func runPolicy(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: classify-command policy {list|show|set NAME|edit}")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "policy:", err)
		return 1
	}
	switch args[0] {
	case "list":
		for _, n := range policies.Names() {
			fmt.Println(n)
		}
		return 0

	case "show":
		active := filepath.Join(home, ".claude", "active-policy.md")
		target, err := os.Readlink(active)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				fmt.Println("no active policy (fallback: normal)")
				return 0
			}
			fmt.Fprintln(os.Stderr, "policy:", err)
			return 1
		}
		fmt.Println(target)
		return 0

	case "set":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "policy set: missing policy name")
			return 2
		}
		name := args[1]
		target := filepath.Join(home, ".claude", "policies", name+".md")
		if _, err := os.Stat(target); err != nil {
			fmt.Fprintf(os.Stderr,
				"policy set: %s not found (run `classify-command install` first)\n", target)
			return 1
		}
		active := filepath.Join(home, ".claude", "active-policy.md")
		_ = os.Remove(active)
		if err := os.Symlink(target, active); err != nil {
			fmt.Fprintln(os.Stderr, "policy:", err)
			return 1
		}
		fmt.Printf("policy active: %s\n", name)
		return 0

	case "edit":
		active := filepath.Join(home, ".claude", "active-policy.md")
		editor := os.Getenv("EDITOR")
		if editor == "" {
			for _, c := range []string{"vi", "nano"} {
				if _, err := exec.LookPath(c); err == nil {
					editor = c
					break
				}
			}
		}
		if editor == "" {
			fmt.Fprintln(os.Stderr,
				"policy edit: no $EDITOR set and neither vi nor nano found on PATH")
			return 1
		}
		cmd := exec.Command(editor, active)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "policy:", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "policy: unknown subcommand %q\n", args[0])
		return 2
	}
}
