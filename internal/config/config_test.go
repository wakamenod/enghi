package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/config"
)

// **Change db_path and the image database follows.**
// Applying defaults too early makes "was this set?" impossible to answer, and
// the images are left behind somewhere else.
func TestFilesDBFollowsDBPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`db_path = "`+dir+`/custom.db"`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "custom-files.db")
	if c.FilesDBPath != want {
		t.Fatalf("files_db_path = %q, want %q", c.FilesDBPath, want)
	}
}

// An explicit value is used as given.
func TestFilesDBExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "db_path = \"" + dir + "/a.db\"\nfiles_db_path = \"" + dir + "/別の場所.db\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.FilesDBPath != filepath.Join(dir, "別の場所.db") {
		t.Fatalf("files_db_path = %q", c.FilesDBPath)
	}
}

// Without a configuration file, the defaults stand on their own.
func TestDefaults(t *testing.T) {
	c := config.Default()
	if c.Port != 7777 || c.Host != "127.0.0.1" {
		t.Fatalf("wrong defaults: %+v", c)
	}
	if !strings.HasSuffix(c.FilesDBPath, "-files.db") {
		t.Fatalf("files_db_path = %q", c.FilesDBPath)
	}
	if c.BackupKeep != 7 || !c.BackupOn() {
		t.Fatalf("wrong backup defaults: keep=%d on=%v", c.BackupKeep, c.BackupOn())
	}
}

// Nothing but loopback may be bound (DESIGN 4.4).
func TestRejectsNonLoopbackHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`host = "0.0.0.0"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("0.0.0.0 was accepted")
	}
}

// allowed_hosts accepts literal names only, one at a time.
// Host validation is the only effective defense against DNS rebinding
// (DESIGN 4.4), so there is no wildcard through which to loosen it.
func TestAllowedHostsRejectsWildcard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`allowed_hosts = ["*.local"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("a wildcard was accepted")
	}
}

// The names written down become comparable, ignoring case and any port.
func TestAllowedHostsNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`allowed_hosts = ["MacBook.local:443", " enghi.example.com "]`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.NormalizedAllowedHosts(), ",")
	if got != "macbook.local,enghi.example.com" {
		t.Fatalf("NormalizedAllowedHosts = %q", got)
	}
}

// Empty by default: nothing changes unless it is configured.
func TestAllowedHostsEmptyByDefault(t *testing.T) {
	c := config.Default()
	if len(c.NormalizedAllowedHosts()) != 0 {
		t.Fatalf("the allow list is not empty by default: %v", c.AllowedHosts)
	}
}

// Paths under the home directory are shown with ~; others are left alone.
func TestAbbrev(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	for in, want := range map[string]string{
		filepath.Join(home, ".claude", "skills", "enghi"): "~/.claude/skills/enghi",
		home:                        "~",
		home + "-other/x":           home + "-other/x",
		"/opt/enghi/skills":         "/opt/enghi/skills",
		filepath.Dir(home) + "/bob": filepath.Dir(home) + "/bob",
	} {
		if got := config.Abbrev(in); got != want {
			t.Errorf("Abbrev(%q) = %q, want %q", in, got, want)
		}
	}
}
