package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SocialGouv/smart-allow/internal/appinfo"
	"github.com/SocialGouv/smart-allow/policies"
)

// ---------- hook command template ----------

// hookSentinel is the idempotency marker we look for in settings.json
// entries. Any matcher entry whose command string contains this substring is
// considered "ours".
const hookSentinel = "classify-command"

// hookCommandFor formats the exact shell command Claude Code invokes for each
// PreToolUse Bash event. Kept byte-identical to what the legacy bash
// installer wrote, so pre-existing settings.json files stay valid.
func hookCommandFor(binaryPath string) string {
	return fmt.Sprintf(
		`CLAUDE_CLASSIFIER_CACHE_DIR="$CLAUDE_PROJECT_DIR/.claude/cache" `+
			`CLAUDE_CLASSIFIER_LOG="$CLAUDE_PROJECT_DIR/.claude/classifier.log" `+
			`"%s"`,
		binaryPath,
	)
}

// ---------- Status ----------

type status struct {
	BinaryPath    string
	BinaryVersion string
	Home          string

	GlobalPath      string
	GlobalInstalled bool

	ProjectRoot      string
	ProjectPath      string
	ProjectInstalled bool
	ProjectFromGit   bool
}

// ---------- flags ----------

type installFlags struct {
	global  bool
	project bool
	here    bool
	path    string
	status  bool
	yes     bool
}

func countProjectFlags(f *installFlags) int {
	n := 0
	if f.project {
		n++
	}
	if f.here {
		n++
	}
	if f.path != "" {
		n++
	}
	return n
}

func parseInstallFlags(args []string) (*installFlags, error) {
	fs2 := flag.NewFlagSet("install", flag.ContinueOnError)
	fs2.SetOutput(io.Discard)
	f := &installFlags{}
	fs2.BoolVar(&f.global, "global", false, "")
	fs2.BoolVar(&f.project, "project", false, "")
	fs2.BoolVar(&f.here, "here", false, "")
	fs2.StringVar(&f.path, "path", "", "")
	fs2.BoolVar(&f.status, "status", false, "")
	fs2.BoolVar(&f.yes, "yes", false, "")
	fs2.BoolVar(&f.yes, "y", false, "")
	if err := fs2.Parse(args); err != nil {
		return nil, err
	}
	if f.global && (f.project || f.here || f.path != "") {
		return nil, errors.New("--global cannot be combined with --project/--here/--path")
	}
	if countProjectFlags(f) > 1 {
		return nil, errors.New("--project, --here and --path are mutually exclusive")
	}
	return f, nil
}

// ---------- runInstall ----------

func runInstall(args []string) int {
	f, err := parseInstallFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 2
	}
	st, err := detectStatus(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}
	if f.status {
		printStatus(st)
		return 0
	}
	// No scope → interactive wizard.
	if !f.global && !f.project && !f.here && f.path == "" {
		return wizard(st)
	}

	binPath, err := ensureBinaryAtHome(st.Home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}
	st.BinaryPath = binPath

	if err := installPolicies(st.Home); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}

	var targets []string
	if f.global {
		targets = append(targets, st.GlobalPath)
	} else {
		if st.ProjectRoot == "" {
			fmt.Fprintln(os.Stderr,
				"install: no git repository found from CWD.\n"+
					"  → pass --here to install into CWD anyway,\n"+
					"  → pass --path <dir> for an arbitrary directory,\n"+
					"  → pass --global to install for every Claude Code session.")
			return 2
		}
		if st.ProjectPath == st.GlobalPath {
			fmt.Fprintln(os.Stderr,
				"install: project path equals global path — refusing to install.\n"+
					"  CWD ("+st.ProjectRoot+") resolves to the same settings.json as --global.\n"+
					"  Use --global directly, or --path DIR to pick a different target.")
			return 2
		}
		targets = append(targets, st.ProjectPath)
	}

	for _, t := range targets {
		if !f.yes {
			verb := "write"
			if fileExists(t) {
				verb = "modify"
			}
			msg := fmt.Sprintf("About to %s %s. Continue?", verb, t)
			if !promptYN(msg, true) {
				fmt.Fprintln(os.Stderr, "aborted.")
				return 1
			}
		}
		if err := mergeHook(t, binPath); err != nil {
			fmt.Fprintln(os.Stderr, "install:", err)
			return 1
		}
		fmt.Printf("  hook added to %s\n", t)
	}
	fmt.Printf("\nbinary: %s\n", binPath)
	fmt.Printf("policy: %s (run `%s policy set <name>` to switch)\n",
		activePolicyName(st.Home), appinfo.Name)
	return 0
}

// ---------- resolveProjectRoot ----------

// resolveProjectRoot picks the directory that will host the project-scoped
// .claude/settings.json. Priority: --path > --here > git walk-up.
// Returns (root, fromGit, err). When no explicit flag is set and no .git is
// found walking up from CWD, returns ("", false, nil) — i.e. "no project
// detected" — instead of silently falling back to CWD (which on a fresh
// shell in $HOME would collide with the global scope).
func resolveProjectRoot(flagHere bool, flagPath string) (string, bool, error) {
	if flagPath != "" {
		abs, err := filepath.Abs(flagPath)
		return abs, false, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	if flagHere {
		return cwd, false, nil
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil // no .git anywhere up the tree
		}
		dir = parent
	}
}

// ---------- detectStatus ----------

func detectStatus(f *installFlags) (*status, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	bin, _ := os.Executable()
	st := &status{
		BinaryPath:    bin,
		BinaryVersion: appinfo.FullVersion(),
		Home:          home,
		GlobalPath:    filepath.Join(home, ".claude", "settings.json"),
	}
	st.GlobalInstalled = hasHookEntry(st.GlobalPath)

	if root, fromGit, err := resolveProjectRoot(f.here, f.path); err == nil && root != "" {
		st.ProjectRoot = root
		st.ProjectFromGit = fromGit
		st.ProjectPath = filepath.Join(root, ".claude", "settings.json")
		st.ProjectInstalled = hasHookEntry(st.ProjectPath)
	}
	return st, nil
}

func hasHookEntry(settingsPath string) bool {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	hooks, _ := obj["hooks"].(map[string]interface{})
	pre, _ := hooks["PreToolUse"].([]interface{})
	for _, m := range pre {
		mm, _ := m.(map[string]interface{})
		inner, _ := mm["hooks"].([]interface{})
		for _, h := range inner {
			hh, _ := h.(map[string]interface{})
			if cmd, _ := hh["command"].(string); strings.Contains(cmd, hookSentinel) {
				return true
			}
		}
	}
	return false
}

// ---------- mergeHook ----------

// mergeHook is the write-side counterpart of hasHookEntry. It ensures
// settings.json has exactly one PreToolUse Bash entry referencing
// binaryPath, backs up the existing file, and is idempotent: running it
// twice in a row yields the same content (modulo the backup).
func mergeHook(settingsPath, binaryPath string) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	var obj map[string]interface{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		stamp := time.Now().Format("20060102-150405")
		if err := os.WriteFile(settingsPath+".bak-"+stamp, raw, 0o644); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if obj == nil {
		obj = map[string]interface{}{}
	}

	hooks, _ := obj["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}
	pre, _ := hooks["PreToolUse"].([]interface{})

	// Strip any matcher entry that contains our sentinel; we'll append a
	// fresh one with the (possibly updated) binary path.
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
		if len(kept) == 0 && len(inner) > 0 {
			// This matcher entry only contained our hook; drop the whole entry.
			continue
		}
		if len(kept) != len(inner) {
			mm["hooks"] = kept
		}
		newPre = append(newPre, mm)
	}
	newPre = append(newPre, map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": hookCommandFor(binaryPath),
				"timeout": float64(15000),
			},
		},
	})
	hooks["PreToolUse"] = newPre
	obj["hooks"] = hooks

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0o644)
}

// ---------- binary self-install ----------

// ensureBinaryAtHome copies the running binary to $HOME/.claude/bin/
// classify-command if it isn't already there. Returns the final stable
// path, which is what settings.json should reference.
func ensureBinaryAtHome(home string) (string, error) {
	dest := filepath.Join(home, ".claude", "bin", appinfo.Name)
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	absSelf, _ := filepath.Abs(self)
	absDest, _ := filepath.Abs(dest)
	if absSelf == absDest {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	src, err := os.Open(self)
	if err != nil {
		return "", err
	}
	defer src.Close()
	tmp := dest + ".new"
	d, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(d, src); err != nil {
		d.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := d.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// ---------- policies deployment ----------

// installPolicies writes the three embedded Markdown policies to
// $HOME/.claude/policies/ (unless they already exist, so user tweaks
// survive reinstalls) and points $HOME/.claude/active-policy.md at
// normal.md if no active-policy symlink is set.
func installPolicies(home string) error {
	dir := filepath.Join(home, ".claude", "policies")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := policies.Names()
	sort.Strings(names)
	for _, n := range names {
		dst := filepath.Join(dir, n+".md")
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		body, err := policies.Read(n)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return err
		}
	}
	active := filepath.Join(home, ".claude", "active-policy.md")
	if _, err := os.Lstat(active); errors.Is(err, fs.ErrNotExist) {
		return os.Symlink(filepath.Join(dir, "normal.md"), active)
	}
	return nil
}

// activePolicyName returns the basename (without .md) of the currently
// active policy, or "(none)" if no symlink exists.
func activePolicyName(home string) string {
	target, err := os.Readlink(filepath.Join(home, ".claude", "active-policy.md"))
	if err != nil {
		return "(none)"
	}
	return strings.TrimSuffix(filepath.Base(target), ".md")
}

// ---------- prompts ----------

func promptYN(msg string, def bool) bool {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	fmt.Printf("%s %s ", msg, hint)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	s := strings.TrimSpace(strings.ToLower(line))
	if s == "" {
		return def
	}
	return s == "y" || s == "yes"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ---------- status printer ----------

func printStatus(st *status) {
	fmt.Printf("smart-allow — %s %s\n\n", appinfo.Name, st.BinaryVersion)
	fmt.Println("Status:")
	fmt.Printf("  binary:  %s\n", st.BinaryPath)
	fmt.Printf("  global:  %s (%s)\n", installLabel(st.GlobalInstalled), st.GlobalPath)
	if st.ProjectRoot == "" {
		fmt.Println("  project: no project detected (no .git above CWD; use --here or --path to override)")
		return
	}
	src := ", git root"
	if !st.ProjectFromGit {
		src = ", forced by flag"
	}
	label := installLabel(st.ProjectInstalled)
	if st.ProjectPath == st.GlobalPath {
		label += " — same file as global scope"
	}
	fmt.Printf("  project: %s (%s%s)\n", label, st.ProjectPath, src)
}

func installLabel(b bool) string {
	if b {
		return "installed"
	}
	return "not installed"
}

// ---------- wizard ----------

func wizard(st *status) int {
	printStatus(st)

	type choice struct {
		label string
		args  []string
		fn    func() int
	}
	var cs []choice
	if !st.GlobalInstalled {
		cs = append(cs, choice{"Install globally (all Claude Code sessions)", []string{"--global", "--yes"}, nil})
	} else {
		cs = append(cs, choice{"Reinstall globally (refresh binary path)", []string{"--global", "--yes"}, nil})
	}
	if st.ProjectRoot != "" && st.ProjectPath != st.GlobalPath {
		label := fmt.Sprintf("Install for this project only (%s)", st.ProjectRoot)
		if st.ProjectInstalled {
			label = fmt.Sprintf("Reinstall for this project (%s)", st.ProjectRoot)
		}
		cs = append(cs, choice{label, []string{"--project", "--yes"}, nil})
	}
	if st.GlobalInstalled || st.ProjectInstalled {
		cs = append(cs, choice{"Uninstall (interactive)", nil, func() int { return runUninstall(nil) }})
	}
	cs = append(cs, choice{"Quit", nil, func() int { return 0 }})

	fmt.Println()
	fmt.Println("What do you want to do?")
	for i, c := range cs {
		fmt.Printf("  [%d] %s\n", i+1, c.label)
	}
	def := len(cs)
	fmt.Printf("Choice [%d]: ", def)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	idx := def - 1
	if line != "" {
		var n int
		if _, err := fmt.Sscanf(line, "%d", &n); err != nil {
			fmt.Fprintln(os.Stderr, "invalid choice")
			return 2
		}
		idx = n - 1
	}
	if idx < 0 || idx >= len(cs) {
		fmt.Fprintln(os.Stderr, "out of range")
		return 2
	}
	c := cs[idx]
	if c.fn != nil {
		return c.fn()
	}
	return runInstall(c.args)
}
