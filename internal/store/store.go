// Package store は SQLite への接続、マイグレーション、整合性検査を担う。
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB は *sql.DB の薄いラッパ。
type DB struct {
	*sql.DB
	Path string
}

// Open は DB を開き、PRAGMA を設定し、FTS5/trigram を検証し、マイグレーションを適用する。
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// foreign_keys は接続ごとの設定なので DSN で必ず指定する(DESIGN 10)。
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000&_txlock=immediate",
		url.PathEscape(path))
	sqldb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// 書き込みの直列化。WAL でも writer は1つなので、プールを絞ってロック競合を避ける。
	sqldb.SetMaxOpenConns(8)
	db := &DB{DB: sqldb, Path: path}

	if err := db.Ping(); err != nil {
		sqldb.Close()
		return nil, err
	}
	// journal_mode は永続設定なので一度でよい。
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(p); err != nil {
			sqldb.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if err := db.verifyFTS(); err != nil {
		sqldb.Close()
		return nil, err
	}
	if err := db.migrate(); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

// verifyFTS は FTS5 と trigram tokenizer が使えることを起動時に確認する(DESIGN 1)。
func (db *DB) verifyFTS() error {
	var ver string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&ver); err != nil {
		return fmt.Errorf("sqlite_version の取得に失敗: %w", err)
	}
	if _, err := db.Exec("CREATE VIRTUAL TABLE temp.enghi_fts_check USING fts5(x, tokenize='trigram')"); err != nil {
		return fmt.Errorf("FTS5 の trigram tokenizer が使えない (sqlite %s): %w\n"+
			"  go-sqlite3 を build tag 'sqlite_fts5' 付きでビルドし、SQLite 3.34 以降であることを確認すること", ver, err)
	}
	_, _ = db.Exec("DROP TABLE temp.enghi_fts_check")
	return nil
}

// migrate は未適用のマイグレーションを版番号順に1トランザクションずつ適用する。
func (db *DB) migrate() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version = ?", name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("マイグレーション %s: %w", name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Tx は fn を単一トランザクションで実行する。すべての書き込みはこれを通すこと(DESIGN 10)。
func (db *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
