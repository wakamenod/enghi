// Package files は記事に貼る画像などのバイナリを保管する。
//
// **本体の DB とは別の SQLite ファイルに置く。**
// 記事もタスクも本体 DB が正本であるという原則は変えないが、バイナリを同じ
// ファイルに混ぜると、毎日の `VACUUM INTO` が画像ごと全部コピーすることになり、
// バックアップの費用が中身の量に比例して増えていく。分けておけば本体は数十 MB の
// ままで、頻繁に安く取れる。
//
// 内容でアドレスする(SHA-256)。同じ画像を何度貼っても実体は1つ。
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

// MaxBytes は1ファイルの上限。
const MaxBytes = 32 << 20 // 32 MiB

// ErrNotFound は対象が無いとき。
var ErrNotFound = errors.New("not found")

// ErrTooLarge は上限を超えたとき。
var ErrTooLarge = errors.New("ファイルが大きすぎる")

// ErrUnsupportedType は受け付けない種別のとき。
var ErrUnsupportedType = errors.New("この種別は受け付けない")

// allowed は受け付ける media type と拡張子。
//
// **SVG は受け付けない。**スクリプトを含められるうえ、同一オリジンで配信するため、
// 記事に貼った SVG から Cookie や DOM に触れる経路ができてしまう。
var allowed = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"application/pdf": ".pdf",
}

// MediaTypeAllowed は受け付ける種別かどうか。
func MediaTypeAllowed(t string) bool { _, ok := allowed[t]; return ok }

// Ext は media type に対応する拡張子を返す。
func Ext(mediaType string) string {
	if e, ok := allowed[mediaType]; ok {
		return e
	}
	return ".bin"
}

// File はメタデータ(本体は含めない)。
type File struct {
	Hash         string `json:"hash"`
	MediaType    string `json:"media_type"`
	Bytes        int64  `json:"bytes"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	// URL は記事から参照するときのパス。
	URL string `json:"url"`
}

// Store はバイナリの保管庫。
type Store struct {
	db   *sql.DB
	Path string
}

// Open はファイル用の DB を開き、必要ならスキーマを作る。
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
		// 大きな値を扱うので既定の 4KB より大きくする。空の DB にのみ効く。
		"PRAGMA page_size = 8192",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
		  hash          TEXT    NOT NULL PRIMARY KEY,   -- sha256 の16進
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

// SourcePath は実体のファイルの場所(バックアップの要否判定に使う)。
func (s *Store) SourcePath() string { return s.Path }

// Put は内容を保存し、メタデータを返す。同じ内容が既にあれば何もしない。
func (s *Store) Put(ctx context.Context, data []byte, mediaType, originalName string) (*File, error) {
	if len(data) == 0 {
		return nil, errors.New("中身が空")
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	if !MediaTypeAllowed(mediaType) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedType, mediaType)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// 画像なら寸法を控えておく(表示のときに使う)
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

// Meta はメタデータだけを返す。
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

// Get は本体とメタデータを返す。
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

// List は新しい順にメタデータを返す。
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

// Hashes は保管しているすべての hash を返す(掃除に使う)。
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

// Delete は1件消す。
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

// Stats は件数と合計サイズ。
func (s *Store) Stats(ctx context.Context) (count int, bytes int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*), COALESCE(sum(bytes), 0) FROM files`).Scan(&count, &bytes)
	return
}

// Vacuum はバックアップ用に一貫したスナップショットを書き出す。
func (s *Store) VacuumInto(ctx context.Context, path string) error {
	if strings.ContainsAny(path, "'\x00") {
		return fmt.Errorf("パスに使えない文字がある: %q", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", path))
	return err
}
