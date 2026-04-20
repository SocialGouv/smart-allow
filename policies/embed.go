// Package policies exposes the shipped Markdown policies to the rest of the
// binary via Go's embed directive. Keeping this file next to the Markdown
// files is required — //go:embed cannot escape the package directory.
package policies

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed *.md
var Files embed.FS

// Names returns the policy names without the ".md" suffix, sorted by embed's
// natural order (alphabetical).
func Names() []string {
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".md") {
			out = append(out, strings.TrimSuffix(n, ".md"))
		}
	}
	return out
}

// Read returns the body of a policy by its name ("normal", "strict", …).
func Read(name string) ([]byte, error) {
	return Files.ReadFile(name + ".md")
}
