package wiki

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// CreateInput is the input of POST /api/pages.
type CreateInput struct {
	Title string   `json:"title"`
	Slug  string   `json:"slug,omitempty"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
}

// UpdateInput is the input of PUT /api/pages/:slug. Version is required for
// the optimistic lock.
type UpdateInput struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	Version int      `json:"version"`
}

// Create creates a page. The page row, page_titles, page_tags, links,
// titles_fts and page_revisions are all written in a single transaction.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Page, error) {
	title := textnorm.NFC(strings.TrimSpace(in.Title))
	if title == "" {
		return nil, errors.New("the title is empty")
	}
	var out *Page
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		// A title-namespace collision. The PRIMARY KEY would reject it anyway;
		// we look it up first so we can report which page it collides with
		// (DESIGN 4.2).
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
		// Resolve the unresolved links that pointed at this new title
		// (DESIGN 2.1).
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

// Update updates a page. A version mismatch is a VersionConflictError and a
// title collision is a TitleConflictError: they mean entirely different things
// and must not be conflated (DESIGN 4.2).
func (s *Service) Update(ctx context.Context, slug string, in UpdateInput) (*Page, error) {
	newTitle := textnorm.NFC(strings.TrimSpace(in.Title))
	if newTitle == "" {
		return nil, errors.New("the title is empty")
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

		titleChanged := cur.Title != newTitle // binary comparison: a case change is a rename too
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

		// version goes up even for a tag-only change, so the optimistic lock on
		// the Emacs side does not miss it. page_revisions, in contrast, is written
		// only when the body or title changed (DESIGN 4.2).
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

// rename follows the procedure in DESIGN 2.5 exactly. Do not reorder the steps.
func (s *Service) rename(ctx context.Context, tx *sql.Tx, id int64, newTitle string) error {
	// 1. Check that the new title does not belong to another page, **before**
	//    demoting anything.
	owner, err := s.titleOwner(ctx, tx, newTitle)
	if err != nil {
		return err
	}
	if owner != nil && owner.ID != id {
		return &TitleConflictError{Conflicting: owner}
	}
	// 2. Demote the current canonical title; the old title stays as an alias.
	if _, err := tx.ExecContext(ctx,
		`UPDATE page_titles SET is_canonical = 0 WHERE page_id = ? AND is_canonical = 1`, id); err != nil {
		return fmtErr("demote canonical", err)
	}
	// 3. Promote one of this page's own aliases, or insert (UPSERT).
	//    Without this, renaming back to the previous name always fails.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO page_titles(title, page_id, is_canonical) VALUES (?, ?, 1)
		   ON CONFLICT(title) DO UPDATE SET is_canonical = 1`, newTitle, id); err != nil {
		return fmtErr("promote title", err)
	}
	// 4. Keeping pages.title in sync is done by the caller's UPDATE.
	// 5. Post-condition: exactly one canonical title. Anything else returns an
	//    error so the transaction rolls back.
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM page_titles WHERE page_id = ? AND is_canonical = 1`, id).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("rename post-condition broken: %d canonical titles (page_id=%d)", n, id)
	}
	// Resolve the unresolved links that pointed at the new title.
	return resolveIncoming(ctx, tx, id, newTitle)
}

// Delete removes a page. Link lifecycle is managed explicitly here, not by the
// database (DESIGN 2.1).
func (s *Service) Delete(ctx context.Context, slug string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanPage(tx.QueryRowContext(ctx,
			`SELECT `+pageCols+` FROM pages p WHERE p.slug = ? COLLATE NOCASE`, slug))
		if err != nil {
			return err
		}
		// Rows pointing at it are demoted to unresolved links rather than deleted,
		// which invites rewriting them.
		if _, err := tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL WHERE dst_kind = 'page' AND dst_id = ?`, cur.ID); err != nil {
			return err
		}
		// Rows originating from this page are deleted.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM links WHERE src_kind = 'page' AND src_id = ?`, cur.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM titles_fts WHERE rowid = ?`, cur.ID); err != nil {
			return err
		}
		// page_titles / page_tags / page_revisions are ON DELETE CASCADE.
		if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, cur.ID); err != nil {
			return err
		}
		return nil
	})
}

// AddAlias registers an alias explicitly, such as "GNU Emacs" for "Emacs"
// (DESIGN 2.5).
func (s *Service) AddAlias(ctx context.Context, pageID int64, alias string) error {
	alias = textnorm.NFC(strings.TrimSpace(alias))
	if alias == "" {
		return errors.New("the alias is empty")
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if owner, err := s.titleOwner(ctx, tx, alias); err != nil {
			return err
		} else if owner != nil {
			if owner.ID == pageID {
				return nil // already ours
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

// DeleteAlias removes an alias. The canonical title cannot be removed
// (WHERE is_canonical = 0).
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
		// Demote the links that resolved through this alias to unresolved.
		_, err = tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL
			  WHERE dst_kind = 'page' AND dst_id = ? AND dst_title = ? COLLATE NOCASE`, pageID, alias)
		return err
	})
}

// RewriteReferences rewrites [[old]] to [[new]] across bodies.
// **It never runs automatically; the user invokes it explicitly** (DESIGN 2.5).
// It returns the number of pages rewritten.
func (s *Service) RewriteReferences(ctx context.Context, oldTitle, newTitle string) (int, error) {
	oldTitle = textnorm.NFC(strings.TrimSpace(oldTitle))
	newTitle = textnorm.NFC(strings.TrimSpace(newTitle))
	if oldTitle == "" || newTitle == "" {
		return 0, errors.New("the source or target of the rewrite is empty")
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

// snapshot records a revision.
// **If the previous revision is newer than RevisionCompactMinutes, it is simply
// overwritten** (DESIGN 4.2). The size of the change plays no part: the rule
// looks only at time, so that it stays deterministic.
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

// titleOwner returns the page owning that title, canonical or alias, or nil.
func (s *Service) titleOwner(ctx context.Context, tx *sql.Tx, title string) (*Page, error) {
	p, err := scanPage(tx.QueryRowContext(ctx,
		`SELECT `+pageCols+` FROM pages p JOIN page_titles t ON t.page_id = p.id WHERE t.title = ?`, title))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return p, err
}

// uniqueSlug builds a slug that does not collide. The UNIQUE index is
// COLLATE NOCASE.
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
