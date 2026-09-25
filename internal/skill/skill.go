// Package skill writes the Claude Code skill (skills/enghi) embedded in the
// binary, and reports whether an installed copy matches it.
package skill

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wakamenod/enghi"
)

// DefaultBaseURL is the server address written in skills/enghi/SKILL.md.
// Write replaces it with the configured one, so the file in the repository
// stays usable as is (for instance through a symlink) on the default port.
const DefaultBaseURL = "http://127.0.0.1:7777"

// State is what Status found at the destination.
type State string

const (
	NotInstalled State = "not_installed"
	UpToDate     State = "up_to_date"
	Outdated     State = "outdated" // present, but differs from this binary's version
	Linked       State = "linked"   // a symlink, which Write leaves alone
)

// Info is the result of Status. Target is the link target when State is Linked.
type Info struct {
	State  State
	Target string
}

// files returns the embedded skill with baseURL substituted, keyed by slash
// path relative to the skill directory. Directories map to nil.
func files(baseURL string) (map[string][]byte, error) {
	sub, err := fs.Sub(enghi.SkillFS, "skills/enghi")
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[path] = nil
			return nil
		}
		b, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		out[path] = []byte(strings.ReplaceAll(string(b), DefaultBaseURL, baseURL))
		return nil
	})
	return out, err
}

// linkTarget returns the target when dest is a symlink, and "" otherwise.
func linkTarget(dest string) (string, error) {
	fi, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return "", nil
	}
	return os.Readlink(dest)
}

// Write copies the embedded skill into dest, pointing it at baseURL, and
// returns the path of SKILL.md.
//
// **It refuses when dest is a symlink.** The README suggests linking the
// repository's skills/enghi there while editing it; writing through the link
// would rewrite the tracked SKILL.md with this configuration's address.
func Write(dest, baseURL string) (string, error) {
	target, err := linkTarget(dest)
	if err != nil {
		return "", err
	}
	if target != "" {
		return "", fmt.Errorf("%s is a symlink to %s; remove it first to install a copy", dest, target)
	}
	fsys, err := files(baseURL)
	if err != nil {
		return "", err
	}
	// A directory sorts before its contents, so parents exist when files are
	// written.
	for _, p := range slices.Sorted(maps.Keys(fsys)) {
		t := filepath.Join(dest, filepath.FromSlash(p))
		if fsys[p] == nil {
			if err := os.MkdirAll(t, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.WriteFile(t, fsys[p], 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(dest, "SKILL.md"), nil
}

// Status compares dest with the embedded skill pointed at baseURL. Extra files
// in dest are ignored; only the embedded ones must match byte for byte.
func Status(dest, baseURL string) (Info, error) {
	target, err := linkTarget(dest)
	if err != nil {
		return Info{}, err
	}
	if target != "" {
		return Info{State: Linked, Target: target}, nil
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return Info{State: NotInstalled}, nil
		}
		return Info{}, err
	}
	fsys, err := files(baseURL)
	if err != nil {
		return Info{}, err
	}
	for p, want := range fsys {
		if want == nil {
			continue
		}
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(p)))
		if err != nil {
			if os.IsNotExist(err) {
				return Info{State: Outdated}, nil
			}
			return Info{}, err
		}
		if !bytes.Equal(got, want) {
			return Info{State: Outdated}, nil
		}
	}
	return Info{State: UpToDate}, nil
}
