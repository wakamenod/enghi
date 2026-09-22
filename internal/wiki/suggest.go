package wiki

import (
	"context"
	"database/sql"
	"strings"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// TitleSuggestion is one candidate for [[...]] completion.
//
// Title must be **a string that can be written inside [[ ]] as is**. When an
// alias is offered, the alias itself is inserted, not the canonical title.
// Resolving [[...]] is a single page_titles query that treats canonical titles
// and aliases alike, so either spelling resolves to the same page. Not pulling
// body text towards the canonical title is also the policy in DESIGN 2.5.
type TitleSuggestion struct {
	Title     string `json:"title"`     // the string to insert (canonical title or alias)
	Slug      string `json:"slug"`      // slug of the page it resolves to
	Canonical string `json:"canonical"` // canonical title of that page
	IsAlias   bool   `json:"is_alias"`  // whether Title is an alias
}

// SuggestTitles returns candidates for [[...]] completion. **It queries
// page_titles only.**
//
// Body search (pages_fts) is deliberately not used: every candidate offered
// must be guaranteed to resolve. Mixing in body hits would create a path where
// "I picked it from the list, but it was not a title, so it became an
// unresolved link".
//
// The order is: prefix matches first, then shorter titles, then most recently
// updated. Completion is mostly used to reach a page from its first few
// characters, so prefix matches come first.
// An empty q returns recently updated pages, so that typing `[[` does not show
// an empty list.
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
		// Neutralize LIKE wildcards. Always pair this with ESCAPE '\' (DESIGN 3.6).
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

// likeEscape neutralizes LIKE wildcards; pair it with ESCAPE '\' (DESIGN 3.6).
// It does the same as search.LikeEscape, duplicated here because search imports
// wiki.
func likeEscape(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}
