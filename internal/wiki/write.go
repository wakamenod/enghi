package wiki

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// CreateInput は POST /api/pages の入力。
type CreateInput struct {
	Title string   `json:"title"`
	Slug  string   `json:"slug,omitempty"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
}

// UpdateInput は PUT /api/pages/:slug の入力。Version は必須(楽観ロック)。
type UpdateInput struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	Version int      `json:"version"`
}

// Create はページを作る。
// ページ保存 + page_titles + page_tags + links + titles_fts + page_revisions を単一トランザクションで行う。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Page, error) {
	title := textnorm.NFC(strings.TrimSpace(in.Title))
	if title == "" {
		return nil, errors.New("タイトルが空です")
	}
	var out *Page
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		// タイトル名前空間の衝突。DB の PRIMARY KEY でも弾かれるが、
		// 衝突相手のページを返すために先に引く(DESIGN 4.2)。
		if p, err := s.titleOwner(ctx, tx, title); err != nil {
			return err
		} else if p != nil {
			return &TitleConflictError{Conflicting: p}
		}
		slug, err := s.uniqueSlug(ctx, tx, firstNonEmpty(in.Slug, title), 0)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO pages(slug, title, body) VALUES (?, ?, ?)`, slug, title, in.Body)
		if err != nil {
			return fmtErr("pages insert", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO page_titles(title, page_id, is_canonical) VALUES (?, ?, 1)`, title, id); err != nil {
			return fmtErr("page_titles insert", err)
		}
		if err := syncTitlesFTS(ctx, tx, id, title); err != nil {
			return err
		}
		if err := syncTags(ctx, tx, id, in.Tags); err != nil {
			return err
		}
		if err := saveLinks(ctx, tx, "page", id, in.Body); err != nil {
			return err
		}
		// 新しいタイトルを指していた未解決リンクを解決する(DESIGN 2.1)。
		if err := resolveIncoming(ctx, tx, id, title); err != nil {
			return err
		}
		if err := s.snapshot(ctx, tx, id, title, in.Body, 1); err != nil {
			return err
		}
		out, err = s.pageByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Tags = normalizeTags(in.Tags)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update はページを更新する。version 不一致は VersionConflictError、
// タイトル衝突は TitleConflictError(まったく別の意味なので混ぜないこと。DESIGN 4.2)。
func (s *Service) Update(ctx context.Context, slug string, in UpdateInput) (*Page, error) {
	newTitle := textnorm.NFC(strings.TrimSpace(in.Title))
	if newTitle == "" {
		return nil, errors.New("タイトルが空です")
	}
	var out *Page
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanPage(tx.QueryRowContext(ctx,
			`SELECT `+pageCols+` FROM pages p WHERE p.slug = ? COLLATE NOCASE`, slug))
		if err != nil {
			return err
		}
		if in.Version != cur.Version {
			cur.Tags, _ = s.tagsOf(ctx, cur.ID)
			return &VersionConflictError{Current: cur}
		}

		titleChanged := cur.Title != newTitle // COLLATE BINARY での比較。大小の変更も改名として扱う
		bodyChanged := cur.Body != in.Body

		if titleChanged {
			if err := s.rename(ctx, tx, cur.ID, newTitle); err != nil {
				return err
			}
		}

		curTags, err := s.tagsOf(ctx, cur.ID)
		if err != nil {
			return err
		}
		tagsChanged := !sameTags(curTags, in.Tags)

		// version はタグだけの変更でも上げる(Emacs 側の楽観ロックが見逃さないように)。
		// page_revisions は本文/タイトルが変わったときだけ作る(DESIGN 4.2)。
		version := cur.Version
		if titleChanged || bodyChanged || tagsChanged {
			version++
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pages SET title = ?, body = ?, version = ?, updated_at = datetime('now') WHERE id = ?`,
			newTitle, in.Body, version, cur.ID); err != nil {
			return fmtErr("pages update", err)
		}
		if titleChanged {
			if err := syncTitlesFTS(ctx, tx, cur.ID, newTitle); err != nil {
				return err
			}
		}
		if tagsChanged {
			if err := syncTags(ctx, tx, cur.ID, in.Tags); err != nil {
				return err
			}
		}
		if bodyChanged {
			if err := saveLinks(ctx, tx, "page", cur.ID, in.Body); err != nil {
				return err
			}
		}
		if titleChanged || bodyChanged {
			if err := s.snapshot(ctx, tx, cur.ID, newTitle, in.Body, version); err != nil {
				return err
			}
		}
		out, err = s.pageByIDTx(ctx, tx, cur.ID)
		if err != nil {
			return err
		}
		out.Tags = normalizeTags(in.Tags)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// rename は DESIGN 2.5 の手順を厳密に守る。順序を入れ替えないこと。
func (s *Service) rename(ctx context.Context, tx *sql.Tx, id int64, newTitle string) error {
	// 1. 新タイトルが他ページのものでないか、**降格より前に**確認する。
	owner, err := s.titleOwner(ctx, tx, newTitle)
	if err != nil {
		return err
	}
	if owner != nil && owner.ID != id {
		return &TitleConflictError{Conflicting: owner}
	}
	// 2. 現在の正式名を降格(旧タイトルは別名として残る)
	if _, err := tx.ExecContext(ctx,
		`UPDATE page_titles SET is_canonical = 0 WHERE page_id = ? AND is_canonical = 1`, id); err != nil {
		return fmtErr("demote canonical", err)
	}
	// 3. 自分の既存別名なら昇格、無ければ挿入(UPSERT)。
	//    これが無いと「元の名前に戻す」が必ず失敗する。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO page_titles(title, page_id, is_canonical) VALUES (?, ?, 1)
		   ON CONFLICT(title) DO UPDATE SET is_canonical = 1`, newTitle, id); err != nil {
		return fmtErr("promote title", err)
	}
	// 4. pages.title の同期は呼び出し側の UPDATE で行う。
	// 5. 事後アサーション: 正式名がちょうど1つであること。違えばエラーを返して ROLLBACK させる。
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM page_titles WHERE page_id = ? AND is_canonical = 1`, id).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("rename の事後条件が壊れた: 正式名が %d 件(page_id=%d)", n, id)
	}
	// 新タイトルを指していた未解決リンクを解決する。
	return resolveIncoming(ctx, tx, id, newTitle)
}

// Delete はページを消す。リンクのライフサイクルはアプリ側で明示的に管理する(DESIGN 2.1)。
func (s *Service) Delete(ctx context.Context, slug string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanPage(tx.QueryRowContext(ctx,
			`SELECT `+pageCols+` FROM pages p WHERE p.slug = ? COLLATE NOCASE`, slug))
		if err != nil {
			return err
		}
		// そこを指す行は削除せず未解決リンクに落とす(書き直しを促す)。
		if _, err := tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL WHERE dst_kind = 'page' AND dst_id = ?`, cur.ID); err != nil {
			return err
		}
		// そのページ発の行は削除する。
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM links WHERE src_kind = 'page' AND src_id = ?`, cur.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM titles_fts WHERE rowid = ?`, cur.ID); err != nil {
			return err
		}
		// page_titles / page_tags / page_revisions は ON DELETE CASCADE。
		if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, cur.ID); err != nil {
			return err
		}
		return nil
	})
}

// AddAlias は別名を明示登録する(「GNU Emacs / イーマックス」など。DESIGN 2.5)。
func (s *Service) AddAlias(ctx context.Context, pageID int64, alias string) error {
	alias = textnorm.NFC(strings.TrimSpace(alias))
	if alias == "" {
		return errors.New("別名が空です")
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if owner, err := s.titleOwner(ctx, tx, alias); err != nil {
			return err
		} else if owner != nil {
			if owner.ID == pageID {
				return nil // 既に自分のもの
			}
			return &TitleConflictError{Conflicting: owner}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO page_titles(title, page_id, is_canonical) VALUES (?, ?, 0)`, alias, pageID); err != nil {
			return err
		}
		return resolveIncoming(ctx, tx, pageID, alias)
	})
}

// DeleteAlias は別名を削除する。正式名は消せない(WHERE is_canonical = 0)。
func (s *Service) DeleteAlias(ctx context.Context, pageID int64, alias string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM page_titles WHERE title = ? AND page_id = ? AND is_canonical = 0`,
			textnorm.NFC(alias), pageID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		// この別名で解決されていたリンクを未解決に落とす。
		_, err = tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL
			  WHERE dst_kind = 'page' AND dst_id = ? AND dst_title = ? COLLATE NOCASE`, pageID, alias)
		return err
	})
}

// RewriteReferences は [[old]] を [[new]] に一括置換する。
// **自動では絶対に実行しない。ユーザが明示的に呼ぶ操作である**(DESIGN 2.5)。
// 置換したページ数を返す。
func (s *Service) RewriteReferences(ctx context.Context, oldTitle, newTitle string) (int, error) {
	oldTitle = textnorm.NFC(strings.TrimSpace(oldTitle))
	newTitle = textnorm.NFC(strings.TrimSpace(newTitle))
	if oldTitle == "" || newTitle == "" {
		return 0, errors.New("置換元/置換先が空です")
	}
	count := 0
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT DISTINCT src_id FROM links
			  WHERE src_kind = 'page' AND dst_kind = 'page' AND dst_title = ? COLLATE NOCASE`, oldTitle)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			p, err := s.pageByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			body := replaceWikilinkTarget(p.Body, oldTitle, newTitle)
			if body == p.Body {
				continue
			}
			version := p.Version + 1
			if _, err := tx.ExecContext(ctx,
				`UPDATE pages SET body = ?, version = ?, updated_at = datetime('now') WHERE id = ?`,
				body, version, id); err != nil {
				return err
			}
			if err := saveLinks(ctx, tx, "page", id, body); err != nil {
				return err
			}
			if err := s.snapshot(ctx, tx, id, p.Title, body, version); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// snapshot はリビジョンを記録する。
// **直前のリビジョンが RevisionCompactMinutes 以内なら無条件にそれを上書きする**(DESIGN 4.2)。
// 差分の大小は判定に使わない。時刻だけを見る決定的な規則にすること。
func (s *Service) snapshot(ctx context.Context, tx *sql.Tx, pageID int64, title, body string, version int) error {
	var lastID int64
	err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM page_revisions
		   WHERE page_id = ? AND created_at >= datetime('now', '-%d minutes')
		   ORDER BY id DESC LIMIT 1`, s.RevisionCompactMinutes), pageID).Scan(&lastID)
	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx,
			`UPDATE page_revisions SET title = ?, body = ?, version = ?, created_at = datetime('now')
			  WHERE id = ?`, title, body, version, lastID)
		return fmtErr("revision compact", err)
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx,
			`INSERT INTO page_revisions(page_id, title, body, version) VALUES (?, ?, ?, ?)`,
			pageID, title, body, version)
		return fmtErr("revision insert", err)
	default:
		return err
	}
}

// titleOwner はそのタイトル(正式名・別名を問わない)を持つページを返す。無ければ nil。
func (s *Service) titleOwner(ctx context.Context, tx *sql.Tx, title string) (*Page, error) {
	p, err := scanPage(tx.QueryRowContext(ctx,
		`SELECT `+pageCols+` FROM pages p JOIN page_titles t ON t.page_id = p.id WHERE t.title = ?`, title))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return p, err
}

// uniqueSlug は衝突しない slug を作る。UNIQUE は COLLATE NOCASE。
func (s *Service) uniqueSlug(ctx context.Context, tx *sql.Tx, base string, exclude int64) (string, error) {
	slug := Slugify(base)
	candidate := slug
	for i := 2; ; i++ {
		var id int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM pages WHERE slug = ? COLLATE NOCASE`, candidate).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && id == exclude) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", slug, i)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
