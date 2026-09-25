package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skill is written with the configured address in place of the default one.
func TestWriteUsesBaseURL(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "enghi")
	path, err := Write(dest, "http://127.0.0.1:8888")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\nname: enghi\ndescription: ") {
		t.Fatalf("SKILL.md does not start with its frontmatter:\n%.200s", s)
	}
	if strings.Contains(s, DefaultBaseURL) {
		t.Fatalf("%s is left in SKILL.md", DefaultBaseURL)
	}
	if !strings.Contains(s, "http://127.0.0.1:8888/api/tasks") {
		t.Fatal("the configured address is not in SKILL.md")
	}
}

// Running it again overwrites the previous version.
func TestWriteOverwrites(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "enghi")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := Write(dest, DefaultBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "old" {
		t.Fatal("SKILL.md was not overwritten")
	}
}

// A symlinked skill (the README's setup for editing it) is neither written
// through nor reported as installed.
func TestWriteRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := []byte("repository copy")
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), orig, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "enghi")
	if err := os.Symlink(repo, dest); err != nil {
		t.Fatal(err)
	}

	_, err := Write(dest, "http://127.0.0.1:8888")
	if err == nil || !strings.Contains(err.Error(), repo) {
		t.Fatalf("Write through a symlink: err = %v, want one naming %s", err, repo)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "SKILL.md")); string(b) != string(orig) {
		t.Fatal("the link target was modified")
	}

	info, err := Status(dest, DefaultBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != Linked || info.Target != repo {
		t.Fatalf("Status = %+v, want linked to %s", info, repo)
	}
}

func TestStatus(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "enghi")
	check := func(baseURL string, want State) {
		t.Helper()
		info, err := Status(dest, baseURL)
		if err != nil {
			t.Fatal(err)
		}
		if info.State != want {
			t.Fatalf("Status(%s) = %s, want %s", baseURL, info.State, want)
		}
	}

	check(DefaultBaseURL, NotInstalled)
	if _, err := Write(dest, DefaultBaseURL); err != nil {
		t.Fatal(err)
	}
	check(DefaultBaseURL, UpToDate)
	// Another port means the installed copy points at the wrong server
	check("http://127.0.0.1:8888", Outdated)

	// So does an edited file
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	check(DefaultBaseURL, Outdated)
}
