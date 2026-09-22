// Package files stores the binaries pasted into articles, such as images.
//
// **They live in a SQLite file of their own, separate from the main database.**
// The principle that the main database is the source of truth for articles and
// tasks does not change, but mixing binaries into the same file would make the
// daily `VACUUM INTO` copy every image too, and the cost of a backup would grow
// with the content. Kept apart, the main database stays tens of megabytes and
// can be backed up often and cheaply.
//
// Storage is content-addressed (SHA-256): pasting the same image again and
// again stores it once.
package files

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// MaxBytes is the per-file limit.
const MaxBytes = 32 << 20 // 32 MiB

// ErrNotFound is returned when the target does not exist.
var ErrNotFound = errors.New("not found")

// ErrTooLarge is returned when the limit is exceeded.
var ErrTooLarge = errors.New("the file is too large")

// ErrUnsupportedType is returned for a media type that is not accepted.
var ErrUnsupportedType = errors.New("this media type is not accepted")

// allowed maps accepted media types to their extensions.
//
// **SVG is not accepted.** It can carry script, and since files are served from
// the same origin, an SVG pasted into an article would have a path to cookies
// and the DOM.
var allowed = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"application/pdf": ".pdf",
}

// MediaTypeAllowed reports whether a media type is accepted.
func MediaTypeAllowed(t string) bool { _, ok := allowed[t]; return ok }

// Ext returns the extension for a media type.
func Ext(mediaType string) string {
	if e, ok := allowed[mediaType]; ok {
		return e
	}
	return ".bin"
}

// File is the metadata, without the content itself.
type File struct {
	Hash         string `json:"hash"`
	MediaType    string `json:"media_type"`
	Bytes        int64  `json:"bytes"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	// URL is the path an article references it by.
	URL string `json:"url"`
}

// Store is the binary store.
type Store struct {
	db   *sql.DB
	Path string
}

// Open opens the file database, creating the schema when needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_txlock=immediate", url.PathEscape(path))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		// Larger than the 4KB default because the values are large. Only takes
		// effect on an empty database.
		"PRAGMA page_size = 8192",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
		  hash          TEXT    NOT NULL PRIMARY KEY,   -- sha256 in hex
		  media_type    TEXT    NOT NULL,
		  bytes         INTEGER NOT NULL,
		  width         INTEGER NOT NULL DEFAULT 0,
		  height        INTEGER NOT NULL DEFAULT 0,
		  original_name TEXT    NOT NULL DEFAULT '',
		  data          BLOB    NOT NULL,
		  created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
		) WITHOUT ROWID;
		CREATE INDEX IF NOT EXISTS idx_files_created ON files(created_at DESC);`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, Path: path}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SourcePath is where the file itself lives; the backup uses it to decide
// whether a copy is needed.
func (s *Store) SourcePath() string { return s.Path }

// Put stores the content and returns its metadata, doing nothing when the same
// content is already there.
func (s *Store) Put(ctx context.Context, data []byte, mediaType, originalName string) (*File, error) {
	if len(data) == 0 {
		return nil, errors.New("the content is empty")
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	if !MediaTypeAllowed(mediaType) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedType, mediaType)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Record the dimensions of an image, for display
	var w, h int
	if cfg, _, err := image.DecodeConfig(strings.NewReader(string(data))); err == nil {
		w, h = cfg.Width, cfg.Height
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO files(hash, media_type, bytes, width, height, original_name, data)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(hash) DO NOTHING`,
		hash, mediaType, len(data), w, h, originalName, data); err != nil {
		return nil, err
	}
	return s.Meta(ctx, hash)
}

// Meta returns the metadata alone.
func (s *Store) Meta(ctx context.Context, hash string) (*File, error) {
	var f File
	err := s.db.QueryRowContext(ctx,
		`SELECT hash, media_type, bytes, width, height, original_name, created_at
		   FROM files WHERE hash = ?`, hash).
		Scan(&f.Hash, &f.MediaType, &f.Bytes, &f.Width, &f.Height, &f.OriginalName, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	f.URL = "/files/" + f.Hash
	return &f, nil
}

// Get returns the content together with the metadata.
func (s *Store) Get(ctx context.Context, hash string) ([]byte, *File, error) {
	f, err := s.Meta(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	var data []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT data FROM files WHERE hash = ?`, hash).Scan(&data); err != nil {
		return nil, nil, err
	}
	return data, f, nil
}

// List returns metadata, newest first.
func (s *Store) List(ctx context.Context, limit int) ([]File, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT hash, media_type, bytes, width, height, original_name, created_at
		   FROM files ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Hash, &f.MediaType, &f.Bytes, &f.Width, &f.Height,
			&f.OriginalName, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.URL = "/files/" + f.Hash
		out = append(out, f)
	}
	return out, rows.Err()
}

// Hashes returns every stored hash, for the clean-up pass.
func (s *Store) Hashes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hash FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Delete removes one file.
func (s *Store) Delete(ctx context.Context, hash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE hash = ?`, hash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats returns the count and the total size.
func (s *Store) Stats(ctx context.Context) (count int, bytes int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*), COALESCE(sum(bytes), 0) FROM files`).Scan(&count, &bytes)
	return
}

// Vacuum writes a consistent snapshot for the backup.
func (s *Store) VacuumInto(ctx context.Context, path string) error {
	if strings.ContainsAny(path, "'\x00") {
		return fmt.Errorf("the path contains characters that cannot be used: %q", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", path))
	return err
}
