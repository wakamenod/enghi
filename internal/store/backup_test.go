package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/wakamenod/enghi/internal/store"
)

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// バックアップが中身のある一貫した DB になっていること。
// **単なるファイルコピーでは WAL の内容を取りこぼす**ので、
// 「書き込んだ直後の行がバックアップから読めること」まで確かめる。
func TestBackupContainsCommittedData(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := db.Exec(
		`INSERT INTO pages(slug, title, body) VALUES ('backup-test', 'バックアップ対象', '本文')`); err != nil {
		t.Fatal(err)
	}

	b, err := store.RunBackup(ctx, db, dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if b.Bytes == 0 {
		t.Fatal("バックアップが空")
	}

	// 取ったファイルを開いて中身を確かめる
	copied, err := sql.Open("sqlite3", b.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var title string
	if err := copied.QueryRow(
		`SELECT title FROM pages WHERE slug = 'backup-test'`).Scan(&title); err != nil {
		t.Fatalf("バックアップから読めない: %v", err)
	}
	if title != "バックアップ対象" {
		t.Fatalf("title = %q", title)
	}
	// FTS の索引も一緒に来ていること
	var n int
	if err := copied.QueryRow(
		`SELECT count(*) FROM pages_fts WHERE pages_fts MATCH '"バックアップ"'`).Scan(&n); err != nil {
		t.Fatalf("バックアップ側で FTS が引けない: %v", err)
	}
	if n != 1 {
		t.Fatalf("FTS のヒット = %d, want 1", n)
	}
}

// 同じ日に何度走っても増えないこと(名前が日付で決まる)。
func TestBackupIsIdempotentPerDay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		if _, err := store.RunBackup(ctx, db, dir, 7); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("バックアップ = %d 件, want 1", len(list))
	}
}

// 世代の上限を超えたら古いものから消えること。
func TestBackupRotation(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	// 過去の日付のファイルを作って世代を用意する
	for _, day := range []string{"01", "02", "03", "04", "05"} {
		p := filepath.Join(dir, fmt.Sprintf("enghi-2026-01-%s.db", day))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 今日の分を取ると 6 件になり、3 世代に切り詰められる
	b, err := store.RunBackup(context.Background(), db, dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if b.Removed != 3 {
		t.Errorf("削除数 = %d, want 3", b.Removed)
	}
	list, err := store.Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("残った数 = %d, want 3", len(list))
	}
	// 新しい順に残っていること(今日の分が先頭)
	if filepath.Base(list[0].Path) != filepath.Base(b.Path) {
		t.Errorf("先頭 = %s, want %s", list[0].Path, b.Path)
	}
	for _, x := range list[1:] {
		name := filepath.Base(x.Path)
		if name == "enghi-2026-01-01.db" || name == "enghi-2026-01-02.db" {
			t.Errorf("古いものが残っている: %s", name)
		}
	}
}

// バックアップ先が無くても作られること。
func TestBackupCreatesDir(t *testing.T) {
	db := openTestDB(t)
	dir := filepath.Join(t.TempDir(), "まだ無い", "階層")
	if _, err := store.RunBackup(context.Background(), db, dir, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("ディレクトリが作られていない: %v", err)
	}
}
