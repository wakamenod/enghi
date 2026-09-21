package wiki

import (
	"context"
	"database/sql"
	"strings"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// TitleSuggestion は [[...]] 補完の候補1件。
//
// Title は **そのまま [[ ]] の中に書ける文字列**であること。候補に別名が出たときは
// 別名をそのまま挿入する(正式名に置き換えない)。[[...]] の解決は page_titles の
// 1クエリで正式名と別名を区別しないため、どちらを書いても同じページに解決される。
// 本文の表記を勝手に正式名へ寄せないのは DESIGN 2.5 の方針でもある。
type TitleSuggestion struct {
	Title     string `json:"title"`     // 挿入する文字列(正式名または別名)
	Slug      string `json:"slug"`      // 解決先ページの slug
	Canonical string `json:"canonical"` // 解決先ページの正式名
	IsAlias   bool   `json:"is_alias"`  // Title が別名か
}

// SuggestTitles は [[...]] 補完の候補を返す。**page_titles だけを引く**。
//
// 本文検索(pages_fts)は使わない。候補に出たものが必ず解決されることを保証したいためで、
// 本文ヒットを混ぜると「候補から選んだのにタイトルではないので未解決リンクになる」という
// 経路ができてしまう。
//
// 並びは「前方一致 → タイトルが短い順 → 更新が新しい順」。打ち始めの数文字で目的の
// ページに辿り着くのが補完の主な使い方なので、前方一致を先に出す。
// q が空のときは最近更新されたページを返す(`[[` を打った直後に何も出ないのを避ける)。
func (s *Service) SuggestTitles(ctx context.Context, q string, limit int) ([]TitleSuggestion, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	q = textnorm.NFC(strings.TrimSpace(q))

	const cols = `t.title, t.is_canonical, p.slug, c.title`
	const joins = `FROM page_titles t
	                 JOIN pages p ON p.id = t.page_id
	                 JOIN page_titles c ON c.page_id = t.page_id AND c.is_canonical = 1`

	var rows *sql.Rows
	var err error
	if q == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` `+joins+`
			  WHERE t.is_canonical = 1
			  ORDER BY p.updated_at DESC LIMIT ?`, limit)
	} else {
		// LIKE のワイルドカードは無効化する。ESCAPE '\' と必ず併用すること(DESIGN 3.6)。
		esc := likeEscape(q)
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` `+joins+`
			  WHERE t.title LIKE ? ESCAPE '\'
			  ORDER BY CASE WHEN t.title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END,
			           length(t.title), p.updated_at DESC
			  LIMIT ?`, "%"+esc+"%", esc+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TitleSuggestion{}
	for rows.Next() {
		var v TitleSuggestion
		var canonical int
		if err := rows.Scan(&v.Title, &canonical, &v.Slug, &v.Canonical); err != nil {
			return nil, err
		}
		v.IsAlias = canonical == 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// likeEscape は LIKE のワイルドカードを無効化する。ESCAPE '\' と併用すること(DESIGN 3.6)。
// search.LikeEscape と同じ処理だが、search が wiki を import するためここに持つ。
func likeEscape(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}
