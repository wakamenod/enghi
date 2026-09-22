package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/wakamenod/enghi/internal/files"
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

// A backup must be a consistent database with the data in it.
// **A plain file copy misses the contents of the WAL**, so the test goes as far
// as reading a just-written row back out of the backup.
func TestBackupContainsCommittedData(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := db.Exec(
		`INSERT INTO pages(slug, title, body) VALUES ('backup-test', 'バックアップ対象', '本文')`); err != nil {
		t.Fatal(err)
	}

	b, err := store.RunBackup(ctx, db, nil, dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if b.Bytes == 0 {
		t.Fatal("backup is empty")
	}

	// Open the file that was written and check its contents
	copied, err := sql.Open("sqlite3", b.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var title string
	if err := copied.QueryRow(
		`SELECT title FROM pages WHERE slug = 'backup-test'`).Scan(&title); err != nil {
		t.Fatalf("cannot read from the backup: %v", err)
	}
	if title != "バックアップ対象" {
		t.Fatalf("title = %q", title)
	}
	// The FTS index must come along too
	var n int
	if err := copied.QueryRow(
		`SELECT count(*) FROM pages_fts WHERE pages_fts MATCH '"バックアップ"'`).Scan(&n); err != nil {
		t.Fatalf("FTS is not queryable in the backup: %v", err)
	}
	if n != 1 {
		t.Fatalf("FTS hits = %d, want 1", n)
	}
}

// Running several times in one day must not add files (the name is the date).
func TestBackupIsIdempotentPerDay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		if _, err := store.RunBackup(ctx, db, nil, dir, 7); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("backups = %d, want 1", len(list))
	}
}

// Beyond the generation limit, the oldest ones are deleted.
func TestBackupRotation(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	// Create files with past dates to have generations to prune
	for _, day := range []string{"01", "02", "03", "04", "05"} {
		p := filepath.Join(dir, fmt.Sprintf("enghi-2026-01-%s.db", day))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Taking today's backup makes six, which is then trimmed to three
	b, err := store.RunBackup(context.Background(), db, nil, dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if b.Removed != 3 {
		t.Errorf("removed = %d, want 3", b.Removed)
	}
	list, err := store.Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("remaining = %d, want 3", len(list))
	}
	// They stay newest first (today at the head)
	if filepath.Base(list[0].Path) != filepath.Base(b.Path) {
		t.Errorf("head = %s, want %s", list[0].Path, b.Path)
	}
	for _, x := range list[1:] {
		name := filepath.Base(x.Path)
		if name == "enghi-2026-01-01.db" || name == "enghi-2026-01-02.db" {
			t.Errorf("an old backup is still there: %s", name)
		}
	}
}

// The image database is copied as well: having split them, one without the
// other cannot restore anything.
func TestBackupIncludesFilesDB(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	blobs, err := files.Open(filepath.Join(t.TempDir(), "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	if _, err := blobs.Put(context.Background(), []byte("画像の中身"), "image/png", "a.png"); err != nil {
		t.Fatal(err)
	}

	b, err := store.RunBackup(context.Background(), db, blobs, dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if b.FilesPath == "" || b.FilesBytes == 0 {
		t.Fatalf("the image copy was not taken: %+v", b)
	}
	if _, err := os.Stat(b.FilesPath); err != nil {
		t.Fatalf("the image copy is missing: %v", err)
	}

	// The second run changes nothing, so it must not be retaken
	b2, err := store.RunBackup(context.Background(), db, blobs, dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !b2.FilesSkip {
		t.Error("retaken even though nothing changed")
	}
}

// The backup directory is created when it does not exist.
func TestBackupCreatesDir(t *testing.T) {
	db := openTestDB(t)
	dir := filepath.Join(t.TempDir(), "まだ無い", "階層")
	if _, err := store.RunBackup(context.Background(), db, nil, dir, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory was not created: %v", err)
	}
}
