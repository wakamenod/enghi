package wiki

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// syncTitlesFTS updates titles_fts.
// **IMPORTANT** The convention is rowid = pages.id; never add a page_id column
// (DESIGN 3.4).
func syncTitlesFTS(ctx context.Context, tx *sql.Tx, pageID int64, title string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM titles_fts WHERE rowid = ?`, pageID); err != nil {
		return fmtErr("titles_fts delete", err)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO titles_fts(rowid, title_bigram) VALUES (?, ?)`, pageID, Bigrams(title))
	return fmtErr("titles_fts insert", err)
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = textnorm.NFC(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func sameTags(a, b []string) bool {
	x, y := normalizeTags(a), normalizeTags(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// syncTags replaces a page's tags and cleans up tag rows that fall out of use.
func syncTags(ctx context.Context, tx *sql.Tx, pageID int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_tags WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	for _, name := range normalizeTags(tags) {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
		if err == sql.ErrNoRows {
			res, err := tx.ExecContext(ctx, `INSERT INTO tags(name) VALUES (?)`, name)
			if err != nil {
				return err
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO page_tags(page_id, tag_id) VALUES (?, ?)`, pageID, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM page_tags)`)
	return err
}

// saveLinks deletes every row for (src_kind, src_id), re-parses the body and
// re-inserts. **It must run in the caller's transaction**; otherwise link rows
// multiply on every save (DESIGN 2.1).
func saveLinks(ctx context.Context, tx *sql.Tx, srcKind string, srcID int64, body string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM links WHERE src_kind = ? AND src_id = ?`, srcKind, srcID); err != nil {
		return fmtErr("links delete", err)
	}
	for _, l := range ParseLinks(body) {
		// Resolving [[...]] is one query, canonical titles and aliases alike
		// (DESIGN 2.5).
		var dstID sql.NullInt64
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT page_id FROM page_titles WHERE title = ?`, l.Title).Scan(&id)
		switch {
		case err == nil:
			dstID = sql.NullInt64{Int64: id, Valid: true}
		case err == sql.ErrNoRows:
			// Keep it as an unresolved link
		default:
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO links(src_kind, src_id, dst_kind, dst_id, dst_title)
			 VALUES (?, ?, 'page', ?, ?)`, srcKind, srcID, dstID, l.Title); err != nil {
			return fmtErr("links insert", err)
		}
	}
	return nil
}

// resolveIncoming resolves the unresolved links that pointed at the given title
// (canonical or alias).
func resolveIncoming(ctx context.Context, tx *sql.Tx, pageID int64, title string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE links SET dst_id = ?
		  WHERE dst_kind = 'page' AND dst_id IS NULL AND dst_title = ? COLLATE NOCASE`, pageID, title)
	return fmtErr("resolve incoming links", err)
}

// replaceWikilinkTarget rewrites only the target of [[old]] / [[old|label]] in a
// body, leaving other occurrences of the same string alone.
func replaceWikilinkTarget(body, oldTitle, newTitle string) string {
	return wikilinkRe.ReplaceAllStringFunc(body, func(m string) string {
		sub := wikilinkRe.FindStringSubmatch(m)
		if !strings.EqualFold(strings.TrimSpace(sub[1]), oldTitle) {
			return m
		}
		if sub[2] != "" {
			return "[[" + newTitle + "|" + sub[2] + "]]"
		}
		return "[[" + newTitle + "]]"
	})
}
