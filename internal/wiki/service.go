package wiki

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wakamenod/enghi/internal/store"
)

// Service はページに関するすべての書き込み経路。
// **すべての書き込みは単一トランザクションで行う**(DESIGN 10)。
type Service struct {
	db *store.DB
	// RevisionCompactMinutes: 直前のリビジョンがこの分数以内なら上書きする(DESIGN 4.2)。
	RevisionCompactMinutes int
}

func New(db *store.DB, compactMinutes int) *Service {
	if compactMinutes <= 0 {
		compactMinutes = 10
	}
	return &Service{db: db, RevisionCompactMinutes: compactMinutes}
}

func (s *Service) DB() *store.DB { return s.db }

// ---------------------------------------------------------------- 読み取り

const pageCols = `p.id, p.slug, p.title, p.body, p.version, p.archived, p.created_at, p.updated_at`

func scanPage(row interface{ Scan(...any) error }) (*Page, error) {
	var p Page
	var archived int
	if err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Body, &p.Version, &archived, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Archived = archived != 0
	return &p, nil
}

// BySlug はページを1件返す(タグ込み)。
func (s *Service) BySlug(ctx context.Context, slug string) (*Page, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+pageCols+` FROM pages p WHERE p.slug = ? COLLATE NOCASE`, slug)
	p, err := scanPage(row)
	if err != nil {
		return nil, err
	}
	if p.Tags, err = s.tagsOf(ctx, p.ID); err != nil {
		return nil, err
	}
	return p, nil
}

// ByTitle は正式名・別名を区別せず1クエリで解決する(DESIGN 2.5)。
func (s *Service) ByTitle(ctx context.Context, title string) (*Page, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+pageCols+` FROM pages p
		   JOIN page_titles t ON t.page_id = p.id
		  WHERE t.title = ?`, strings.TrimSpace(title))
	p, err := scanPage(row)
	if err != nil {
		return nil, err
	}
	if p.Tags, err = s.tagsOf(ctx, p.ID); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) tagsOf(ctx context.Context, pageID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.name FROM tags t JOIN page_tags pt ON pt.tag_id = t.id
		  WHERE pt.page_id = ? ORDER BY t.name`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		tags = append(tags, n)
	}
	return tags, rows.Err()
}

// List はページ一覧。sort は "updated" / "created" / "title"。
func (s *Service) List(ctx context.Context, sort string, limit, offset int) ([]*Page, error) {
	order := "p.updated_at DESC"
	switch sort {
	case "created":
		order = "p.created_at DESC"
	case "title":
		order = "p.title COLLATE NOCASE ASC"
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+pageCols+` FROM pages p WHERE p.archived = 0 ORDER BY `+order+` LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.collect(ctx, rows)
}

func (s *Service) collect(ctx context.Context, rows *sql.Rows) ([]*Page, error) {
	out := []*Page{}
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range out {
		t, err := s.tagsOf(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		p.Tags = t
	}
	return out, nil
}

// CountPages は記事の総数。
func (s *Service) CountPages(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE archived = 0`).Scan(&n)
	return n, err
}

// ByTag はタグでの絞り込み一覧(完全一致 JOIN。FTS には載せない。DESIGN 3.1)。
func (s *Service) ByTag(ctx context.Context, tag string, limit, offset int) ([]*Page, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+pageCols+` FROM pages p
		   JOIN page_tags pt ON pt.page_id = p.id
		   JOIN tags t ON t.id = pt.tag_id
		  WHERE t.name = ? AND p.archived = 0
		  ORDER BY p.updated_at DESC LIMIT ? OFFSET ?`, tag, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.collect(ctx, rows)
}

// Tags は使用中のタグと件数。
func (s *Service) Tags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.name, count(pt.page_id) c FROM tags t
		   LEFT JOIN page_tags pt ON pt.tag_id = t.id
		  GROUP BY t.id ORDER BY c DESC, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// Links はページ本文から出ているリンク(未解決を含む)。
func (s *Service) Links(ctx context.Context, pageID int64) ([]Link, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.dst_kind, l.dst_id, l.dst_title, COALESCE(p.slug, ''), COALESCE(p.title, '')
		   FROM links l LEFT JOIN pages p ON p.id = l.dst_id AND l.dst_kind = 'page'
		  WHERE l.src_kind = 'page' AND l.src_id = ?
		  ORDER BY l.dst_title COLLATE NOCASE`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		var id sql.NullInt64
		var slug, canonical string
		if err := rows.Scan(&l.Kind, &id, &l.Title, &slug, &canonical); err != nil {
			return nil, err
		}
		if id.Valid {
			v := id.Int64
			l.PageID, l.Slug, l.Resolved = &v, slug, true
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Backlinks は「このページを指している行」の逆引き1本(DESIGN 2.1)。
func (s *Service) Backlinks(ctx context.Context, pageID int64) ([]Backlink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.src_kind, l.src_id, l.dst_title,
		        COALESCE(p.title, ''), COALESCE(p.slug, '')
		   FROM links l
		   LEFT JOIN pages p ON p.id = l.src_id AND l.src_kind = 'page'
		  WHERE l.dst_kind = 'page' AND l.dst_id = ?
		  ORDER BY p.updated_at DESC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backlink{}
	for rows.Next() {
		var b Backlink
		if err := rows.Scan(&b.Kind, &b.ID, &b.DstTitle, &b.Title, &b.Slug); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UnresolvedLinks は「参照されているが実体の無いページ」。書くべき記事の示唆になる(DESIGN 5)。
func (s *Service) UnresolvedLinks(ctx context.Context, limit int) ([]Unresolved, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT dst_title, COUNT(*) c FROM links
		  WHERE dst_id IS NULL AND dst_kind = 'page'
		  GROUP BY dst_title COLLATE NOCASE ORDER BY c DESC, dst_title LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Unresolved{}
	for rows.Next() {
		var u Unresolved
		if err := rows.Scan(&u.Title, &u.Count); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Revisions は履歴一覧(本文は含めない)。
func (s *Service) Revisions(ctx context.Context, pageID int64) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, page_id, title, version, created_at FROM page_revisions
		  WHERE page_id = ? ORDER BY version DESC, id DESC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.PageID, &r.Title, &r.Version, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Revision は履歴1件(本文込み)。
func (s *Service) Revision(ctx context.Context, id int64) (*Revision, error) {
	var r Revision
	err := s.db.QueryRowContext(ctx,
		`SELECT id, page_id, title, body, version, created_at FROM page_revisions WHERE id = ?`, id).
		Scan(&r.ID, &r.PageID, &r.Title, &r.Body, &r.Version, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// Aliases はページの別名一覧(/wiki/:slug/history の「別名」セクション。DESIGN 2.5)。
func (s *Service) Aliases(ctx context.Context, pageID int64) ([]Alias, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT title, created_at FROM page_titles
		  WHERE page_id = ? AND is_canonical = 0 ORDER BY created_at`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alias{}
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.Title, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentlyCreated は最近作成した記事(ダッシュボード下段)。
func (s *Service) RecentlyCreated(ctx context.Context, limit int) ([]*Page, error) {
	return s.List(ctx, "created", limit, 0)
}

func (s *Service) pageByIDTx(ctx context.Context, tx *sql.Tx, id int64) (*Page, error) {
	return scanPage(tx.QueryRowContext(ctx, `SELECT `+pageCols+` FROM pages p WHERE p.id = ?`, id))
}

func fmtErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
