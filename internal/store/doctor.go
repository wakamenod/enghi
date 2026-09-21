package store

import (
	"context"
	"fmt"
)

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
