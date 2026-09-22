package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// FixNormalization rewrites rows stored as NFD into NFC.
//
// **doctor reports these but never repairs them on its own.** Titles are the
// namespace itself, so rewriting them stays an action the user chooses.
func FixNormalization(ctx context.Context, db *DB) (int, error) {
	fixed := 0
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		for _, c := range []struct{ table, key, col string }{
			{"pages", "id", "title"},
			{"pages", "id", "slug"},
			{"page_titles", "rowid", "title"},
			{"tags", "id", "name"},
		} {
			rows, err := tx.QueryContext(ctx,
				fmt.Sprintf(`SELECT %s, %s FROM %s`, c.key, c.col, c.table))
			if err != nil {
				return err
			}
			type row struct {
				key any
				val string
			}
			var todo []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.key, &r.val); err != nil {
					rows.Close()
					return err
				}
				if !textnorm.IsNFC(r.val) {
					todo = append(todo, r)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, r := range todo {
				if _, err := tx.ExecContext(ctx,
					fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, c.table, c.col, c.key),
					textnorm.NFC(r.val), r.key); err != nil {
					return err
				}
				fixed++
			}
		}
		return nil
	})
	return fixed, err
}

// Problem is one inconsistency found by doctor.
type Problem struct {
	Kind   string `json:"kind"`
	PageID int64  `json:"page_id,omitempty"`
	Detail string `json:"detail"`
}

func (p Problem) String() string {
	if p.PageID != 0 {
		return fmt.Sprintf("[%s] page %d: %s", p.Kind, p.PageID, p.Detail)
	}
	return fmt.Sprintf("[%s] %s", p.Kind, p.Detail)
}

// Doctor checks, from outside, the invariants the database cannot express
// (DESIGN 2.5). A partial UNIQUE index can only guarantee "at most one"
// canonical title, never "exactly one", so that is checked here. Run it at
// start-up as well.
func Doctor(ctx context.Context, db *DB) ([]Problem, error) {
	var problems []Problem

	// Pages with no canonical title
	rows, err := db.QueryContext(ctx,
		`SELECT id, title FROM pages
		  WHERE id NOT IN (SELECT page_id FROM page_titles WHERE is_canonical = 1)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			rows.Close()
			return nil, err
		}
		problems = append(problems, Problem{Kind: "no_canonical_title", PageID: id,
			Detail: fmt.Sprintf("no canonical row in page_titles (pages.title=%q)", title)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Mismatch between pages.title and page_titles.
	// COLLATE BINARY is required: both columns are NOCASE, so without it a
	// difference in case slips through.
	rows, err = db.QueryContext(ctx,
		`SELECT p.id, p.title, COALESCE(t.title, '')
		   FROM pages p
		   LEFT JOIN page_titles t ON t.page_id = p.id AND t.is_canonical = 1
		  WHERE t.title IS NOT p.title COLLATE BINARY`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var pt, tt string
		if err := rows.Scan(&id, &pt, &tt); err != nil {
			rows.Close()
			return nil, err
		}
		problems = append(problems, Problem{Kind: "title_mismatch", PageID: id,
			Detail: fmt.Sprintf("pages.title=%q but the canonical title in page_titles is %q", pt, tt)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Rows that are not normalized (NFD, typically from macOS).
	// They look identical but are different strings, so lookups by title fail.
	for _, c := range []struct{ table, col, label string }{
		{"pages", "title", "article title"},
		{"pages", "slug", "article slug"},
		{"page_titles", "title", "title/alias"},
		{"tags", "name", "tag"},
	} {
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf(`SELECT %s FROM %s`, c.col, c.table))
		if err != nil {
			return nil, err
		}
		var bad []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, err
			}
			if !textnorm.IsNFC(v) {
				bad = append(bad, v)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, v := range bad {
			problems = append(problems, Problem{Kind: "not_nfc",
				Detail: fmt.Sprintf("%s is not normalized (NFD): %q - repair with `enghi doctor --fix`", c.label, v)})
		}
	}

	// Missing titles_fts rows (the two-character query path breaks silently)
	var missing int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pages p WHERE NOT EXISTS
		   (SELECT 1 FROM titles_fts f WHERE f.rowid = p.id)`).Scan(&missing); err != nil {
		return nil, err
	}
	if missing > 0 {
		problems = append(problems, Problem{Kind: "titles_fts_missing",
			Detail: fmt.Sprintf("%d page(s) have no titles_fts row (two-character search will not match)", missing)})
	}

	return problems, nil
}
