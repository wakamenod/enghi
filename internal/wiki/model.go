package wiki

import (
	"errors"
	"fmt"
)

// Page は記事1件。GTD 由来のフィールドは持たない(DESIGN 0)。
type Page struct {
	ID        int64    `json:"id"`
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Version   int      `json:"version"`
	Archived  bool     `json:"archived"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Tags      []string `json:"tags"`
}

// Link は本文中の [[...]] 1件の解決結果。
type Link struct {
	Title    string `json:"title"`           // [[...]] に書かれた生の文字列
	Label    string `json:"label,omitempty"` // [[Title|Label]] の Label
	Kind     string `json:"kind"`            // page / project / task / area
	PageID   *int64 `json:"page_id,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Resolved bool   `json:"resolved"`
}

// Backlink は「このページを指している何か」。種別をまたいで扱える(DESIGN 2.1)。
type Backlink struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug,omitempty"`
	// DstTitle は参照側が書いた表記。別名で参照されている場合に効く。
	DstTitle string `json:"dst_title"`
}

// Revision は本文/タイトルのスナップショット。
type Revision struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
}

// Alias は page_titles の is_canonical = 0 の行。
type Alias struct {
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// TagCount はタグ一覧用。
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Unresolved は未解決リンク(まだ存在しないページ)の集計。
type Unresolved struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

// ErrNotFound はページが無いとき。
var ErrNotFound = errors.New("not found")

// VersionConflictError は楽観ロックの版不一致(HTTP 409 / error="version_conflict")。
// クライアントは入力を捨てず、現行データとの差分を提示してマージさせること(DESIGN 4.2)。
type VersionConflictError struct {
	Current *Page
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: 現行の版は %d", e.Current.Version)
}

// TitleConflictError は新タイトルが他ページの正式名/別名と衝突した場合
// (HTTP 409 / error="title_conflict")。version_conflict とはまったく別の意味を持つ。
type TitleConflictError struct {
	Conflicting *Page
}

func (e *TitleConflictError) Error() string {
	return "同名(大小を区別しない)のページが既に存在します"
}
