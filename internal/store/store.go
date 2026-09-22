// Package store owns the SQLite connection, migrations and consistency checks.
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

// DB is a thin wrapper around *sql.DB.
type DB struct {
	*sql.DB
	Path string
}

// Open opens the database, sets the PRAGMAs, verifies FTS5/trigram and applies
// the migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// foreign_keys is a per-connection setting, so it must be in the DSN
	// (DESIGN 10).
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000&_txlock=immediate",
		url.PathEscape(path))
	sqldb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// Serialize writes. Even with WAL there is a single writer, so a small pool
	// avoids lock contention.
	sqldb.SetMaxOpenConns(8)
	db := &DB{DB: sqldb, Path: path}

	if err := db.Ping(); err != nil {
		sqldb.Close()
		return nil, err
	}
	// journal_mode is persistent, so setting it once is enough.
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

// verifyFTS confirms at start-up that FTS5 and the trigram tokenizer are
// available (DESIGN 1).
func (db *DB) verifyFTS() error {
	var ver string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&ver); err != nil {
		return fmt.Errorf("cannot read sqlite_version: %w", err)
	}
	if _, err := db.Exec("CREATE VIRTUAL TABLE temp.enghi_fts_check USING fts5(x, tokenize='trigram')"); err != nil {
		return fmt.Errorf("FTS5 trigram tokenizer is unavailable (sqlite %s): %w\n"+
			"  build go-sqlite3 with the 'sqlite_fts5' build tag and make sure SQLite is 3.34 or newer", ver, err)
	}
	_, _ = db.Exec("DROP TABLE temp.enghi_fts_check")
	return nil
}

// migrate applies pending migrations in version order, one transaction each.
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
			return fmt.Errorf("migration %s: %w", name, err)
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

// Tx runs fn in a single transaction. **Every write goes through this**
// (DESIGN 10).
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
