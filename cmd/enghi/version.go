package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is filled in from -ldflags for release builds (see Makefile / CI).
// When installed with go install it stays "dev", and vcsRevision() below picks
// the commit back up from debug.ReadBuildInfo.
var version = "dev"

// vcsRevision returns the commit when the binary was installed as a module.
// Without knowing which binary a bug report came from, nothing can be narrowed
// down.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified == "true" {
		rev += "-dirty"
	}
	return rev
}

// versionString is the version as shown to people: `enghi version`, the
// settings screen and /api/status. A make build carries the commit hash in it
// (git describe); a release build is the bare tag.
func versionString() string {
	return displayVersion(version, vcsRevision())
}

func displayVersion(v, rev string) string {
	// When ldflags filled it in, the commit is already there; do not repeat it.
	if v == "dev" && rev != "" {
		v += " (" + rev + ")"
	}
	return v
}

func cmdVersion([]string) error {
	fmt.Printf("enghi %s %s/%s %s\n", versionString(), runtime.GOOS, runtime.GOARCH, runtime.Version())
	return nil
}

// isGlobalFlag reports whether an argument that looks like a flag should be
// treated as a subcommand.
func isGlobalFlag(s string) bool {
	switch s {
	case "-h", "--help", "-v", "--version":
		return true
	}
	return false
}
