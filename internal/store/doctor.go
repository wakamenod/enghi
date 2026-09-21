package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// FixNormalization は NFD で入っている行を NFC に直す。
//
// **doctor が見つけても自動では直さない。**タイトルは名前空間そのものなので、
// 書き換えは利用者が明示的に選ぶ操作にする。
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

// Problem は doctor が見つけた不整合1件。
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

// Doctor は DB で表現できない不変条件を外から検査する(DESIGN 2.5)。
// 「正式名はちょうど1つ」は部分 UNIQUE インデックスでは「高々1つ」しか保証できないため、
// ここで必ず検査する。起動時にも実行すること。
func Doctor(ctx context.Context, db *DB) ([]Problem, error) {
	var problems []Problem

	// 正式名を持たないページ
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
			Detail: fmt.Sprintf("正式タイトルの行が page_titles に無い (pages.title=%q)", title)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// pages.title と page_titles の不一致。
	// COLLATE BINARY が必須 — 両列とも NOCASE のため、付けないと大小の食い違いを見逃す。
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
			Detail: fmt.Sprintf("pages.title=%q だが page_titles の正式名は %q", pt, tt)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 正規化されていない行(macOS 由来の NFD)。
	// 見た目が同じでも別の文字列なので、タイトルで引けなくなる。
	for _, c := range []struct{ table, col, label string }{
		{"pages", "title", "記事のタイトル"},
		{"pages", "slug", "記事の slug"},
		{"page_titles", "title", "タイトル/別名"},
		{"tags", "name", "タグ"},
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
				Detail: fmt.Sprintf("%s が正規化されていない(NFD): %q — `enghi doctor --fix` で直せる", c.label, v)})
		}
	}

	// titles_fts の欠落(2 文字クエリ経路が静かに壊れるため)
	var missing int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pages p WHERE NOT EXISTS
		   (SELECT 1 FROM titles_fts f WHERE f.rowid = p.id)`).Scan(&missing); err != nil {
		return nil, err
	}
	if missing > 0 {
		problems = append(problems, Problem{Kind: "titles_fts_missing",
			Detail: fmt.Sprintf("titles_fts に行の無いページが %d 件ある(2 文字検索が効かない)", missing)})
	}

	return problems, nil
}
