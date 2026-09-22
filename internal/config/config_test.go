package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/config"
)

// **db_path を変えたら画像用の DB もそれに追従すること。**
// 既定値を先に入れてしまうと「未設定かどうか」が判別できなくなり、
// 画像だけ別の場所に取り残される。
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

// 明示した場合はそちらを使うこと。
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

// 設定ファイルが無くても既定値で成立すること。
func TestDefaults(t *testing.T) {
	c := config.Default()
	if c.Port != 7777 || c.Host != "127.0.0.1" {
		t.Fatalf("既定値が違う: %+v", c)
	}
	if !strings.HasSuffix(c.FilesDBPath, "-files.db") {
		t.Fatalf("files_db_path = %q", c.FilesDBPath)
	}
	if c.BackupKeep != 7 || !c.BackupOn() {
		t.Fatalf("バックアップの既定値が違う: keep=%d on=%v", c.BackupKeep, c.BackupOn())
	}
}

// ループバック以外には bind させないこと(DESIGN 4.4)。
func TestRejectsNonLoopbackHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`host = "0.0.0.0"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("0.0.0.0 が通ってしまった")
	}
}

// allowed_hosts は「名前を1つずつ」だけ受け付ける。
// Host 検証は DNS rebinding に対する唯一有効な防御なので(DESIGN 4.4)、
// ワイルドカードで緩められる口を作らない。
func TestAllowedHostsRejectsWildcard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`allowed_hosts = ["*.local"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("ワイルドカードが通ってしまった")
	}
}

// 書かれた名前は、大小とポートを無視して比較できる形になること。
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

// 既定では空。設定しない限り挙動は変わらない。
func TestAllowedHostsEmptyByDefault(t *testing.T) {
	c := config.Default()
	if len(c.NormalizedAllowedHosts()) != 0 {
		t.Fatalf("既定で許可リストが空でない: %v", c.AllowedHosts)
	}
}
