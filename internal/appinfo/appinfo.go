// Package appinfo carries build-time identity (version, commit) injected via
// -ldflags. Mirrors the iterion pattern — a single source of truth for the
// binary's self-reported version.
package appinfo

import (
	"runtime/debug"
	"strings"
)

const (
	Name    = "smart-allow"
	RepoURL = "https://github.com/SocialGouv/smart-allow"
)

// Version is intended to be overridden at build time.
//
// Example:
//
//	go build -ldflags "-X github.com/SocialGouv/smart-allow/internal/appinfo.Version=v0.1.0 \
//	                   -X github.com/SocialGouv/smart-allow/internal/appinfo.Commit=$(git rev-parse --short HEAD)"
var Version = "dev"

// Commit optionally carries a VCS revision (preferably short SHA).
// It can be set via -ldflags or inferred from Go build settings.
var Commit = ""

func init() {
	if strings.TrimSpace(Commit) != "" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			c := strings.TrimSpace(s.Value)
			if len(c) > 12 {
				c = c[:12]
			}
			Commit = c
			break
		}
	}
}

// FullVersion returns "vX.Y.Z+<shortsha>" when both are known, else just the version.
func FullVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		v = "dev"
	}
	c := strings.TrimSpace(Commit)
	if c == "" {
		return v
	}
	if len(c) > 12 {
		c = c[:12]
	}
	return v + "+" + c
}
