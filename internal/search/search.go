package search

import (
	"context"
	"sort"
	"strings"

	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/textnorm"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Result is one search hit. Every kind is mixed into a single list, each with
// its own badge (DESIGN 3.1).
type Result struct {
	Kind      string  `json:"kind"`              // page / project / task / log / area
	ID        int64   `json:"id"`                // for a log, the entry's id
	TaskID    int64   `json:"task_id,omitempty"` // the task a log entry belongs to
	Slug      string  `json:"slug,omitempty"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
	Via       string  `json:"via"` // tag / title / alias / body - which path matched
	Score     float64 `json:"score,omitempty"`

	bucket int // 0:tag 1:title 2:alias 3:body
	korder int // stable order between kinds
}

// Service is search. **Ranking happens in SQL; there is no scoring in the
// application** (DESIGN 3.5).
type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

const (
	bucketTag = iota
	bucketTitle
	bucketAlias
	bucketBody
)

// Search searches across everything. An empty kind means every kind.
func (s *Service) Search(ctx context.Context, q string, kinds []string, limit, offset int) ([]Result, error) {
	// Some input paths deliver NFD, so normalize to NFC as the index is
	q = textnorm.NFC(strings.TrimSpace(q))
	if q == "" {
		return []Result{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50 // default of 50 (DESIGN 3.7)
	}
	want := func(k string) bool {
		if len(kinds) == 0 {
			return true
		}
		for _, x := range kinds {
			if x == k {
				return true
			}
		}
		return false
	}
	// Collect more than needed and combine. The LIMIT in SQL stays.
	fetch := limit + offset
	if fetch < 50 {
		fetch = 50
	}
	if fetch > 300 {
		fetch = 300
	}

	var all []Result
	if want("page") {
		rs, err := s.searchPages(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	if want("project") {
		rs, err := s.searchSimpleFTS(ctx, "project", "projects", "projects_fts", "outcome", q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	if want("task") {
		rs, err := s.searchSimpleFTS(ctx, "task", "tasks", "tasks_fts", "note", q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	if want("log") {
		rs, err := s.searchLogs(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	if want("area") {
		rs, err := s.searchAreas(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}

	// Stable sort by bucket, then kind, then the original order (SQL's ranking).
	// bm25 is not comparable across tables, so scores never decide the mix.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].bucket != all[j].bucket {
			return all[i].bucket < all[j].bucket
		}
		return all[i].korder < all[j].korder
	})

	// Deduplicate across kinds: same kind and id, first wins, which keeps the
	// better bucket
	seen := map[string]bool{}
	out := make([]Result, 0, len(all))
	for _, r := range all {
		key := r.Kind + ":" + itoa(r.ID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	if offset >= len(out) {
		return []Result{}, nil
	}
	out = out[offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// searchPages implements the fallback ladder of 3.4 and the merge rules of
// 3.4 / 3.6.
func (s *Service) searchPages(ctx context.Context, q string, fetch int) ([]Result, error) {
	var out []Result

	// (1) Tags are not in the FTS index. They are matched exactly or by prefix
	//     separately and put at the head of the results (DESIGN 3.1).
	tagHits, err := s.pagesByTagMatch(ctx, q, fetch)
	if err != nil {
		return nil, err
	}
	out = append(out, tagHits...)

	// (2) The main search path, which branches on query length.
	var core []Result
	if RuneLen(q) >= 3 {
		core, err = s.pagesFTS(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		// With no hits, retry with the query trimmed from the end (two steps).
		if len(core) == 0 {
			for _, t := range Truncations(q) {
				core, err = s.pagesFTS(ctx, t, fetch)
				if err != nil {
					return nil, err
				}
				if len(core) > 0 {
					break
				}
			}
		}
	} else {
		core, err = s.pagesShortQuery(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
	}

	// (3) Aliases are not in the FTS index, so they are matched directly
	//     (DESIGN 3.6). They join right after canonical-title hits, as
	//     bucketAlias.
	aliasHits, err := s.pagesByAlias(ctx, q, fetch)
	if err != nil {
		return nil, err
	}

	out = append(out, core...)
	out = append(out, aliasHits...)
	return out, nil
}

// pagesFTS is the path for three characters or more. Ranking is done in SQL by
// bm25 (DESIGN 3.5).
func (s *Service) pagesFTS(ctx context.Context, q string, fetch int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.slug, p.title, p.updated_at,
		        snippet(pages_fts, 1, char(1), char(2), '…', 12) AS snip,
		        bm25(pages_fts, 10.0, 1.0) AS score
		   FROM pages_fts JOIN pages p ON p.id = pages_fts.rowid
		  WHERE pages_fts MATCH ?
		  ORDER BY score, p.updated_at DESC
		  LIMIT ?`, Phrase(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Slug, &r.Title, &r.UpdatedAt, &r.Snippet, &r.Score); err != nil {
			return nil, err
		}
		r.Kind, r.korder = "page", 0
		// Hits whose title contains the query move up as title hits.
		if containsFold(r.Title, q) {
			r.bucket, r.Via = bucketTitle, "title"
		} else {
			r.bucket, r.Via = bucketBody, "body"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// pagesShortQuery is the path for two characters or fewer. trigram cannot match
// those, so it combines two result sets: titles_fts (bigram) and a body LIKE
// (DESIGN 3.4).
// **Japanese is full of two-character words, so this is not an edge case but an
// everyday path.**
func (s *Service) pagesShortQuery(ctx context.Context, q string, fetch int) ([]Result, error) {
	ascii2 := IsASCII2(q)

	// 1. Every titles_fts hit, bm25 ascending, at the head
	match := Phrase(wiki.Bigrams(q))
	if RuneLen(q) == 1 {
		// A single character is not a bigram token, so match by prefix
		match = Phrase(q) + "*"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.slug, p.title, p.updated_at, bm25(titles_fts) AS score
		   FROM titles_fts JOIN pages p ON p.id = titles_fts.rowid
		  WHERE titles_fts MATCH ?
		  ORDER BY score, p.updated_at DESC
		  LIMIT ?`, match, fetch)
	if err != nil {
		return nil, err
	}
	var out []Result
	inTitle := map[int64]bool{}
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Slug, &r.Title, &r.UpdatedAt, &r.Score); err != nil {
			rows.Close()
			return nil, err
		}
		// Re-filter on word boundaries for two-character ASCII only, so that
		// algorithm does not match go. Never for two Japanese characters.
		if ascii2 && !WordBoundaryMatch(r.Title, q) {
			continue
		}
		r.Kind, r.korder, r.bucket, r.Via = "page", 0, bucketTitle, "title"
		inTitle[r.ID] = true
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Then the body LIKE hits, updated_at descending.
	// 3. A page in both sets keeps its title hit and is dropped from the body
	//    set.
	rows, err = s.db.QueryContext(ctx,
		`SELECT p.id, p.slug, p.title, p.updated_at, p.body
		   FROM pages p
		  WHERE p.body LIKE '%' || ? || '%' ESCAPE '\'
		  ORDER BY p.updated_at DESC
		  LIMIT ?`, LikeEscape(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Result
		var body string
		if err := rows.Scan(&r.ID, &r.Slug, &r.Title, &r.UpdatedAt, &body); err != nil {
			return nil, err
		}
		if inTitle[r.ID] {
			continue
		}
		if ascii2 && !WordBoundaryMatch(body, q) {
			continue
		}
		r.Kind, r.korder, r.bucket, r.Via = "page", 0, bucketBody, "body"
		r.Snippet = excerpt(body, q)
		out = append(out, r)
	}
	return out, rows.Err()
}

// pagesByAlias matches aliases in page_titles directly, **as a substring match**
// (DESIGN 3.6). LIKE is case-insensitive for ASCII by default. % and _ must
// always be escaped.
func (s *Service) pagesByAlias(ctx context.Context, q string, fetch int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.page_id, t.title, p.slug, p.title, p.updated_at
		   FROM page_titles t JOIN pages p ON p.id = t.page_id
		  WHERE t.is_canonical = 0 AND t.title LIKE '%' || ? || '%' ESCAPE '\'
		  ORDER BY p.updated_at DESC LIMIT ?`, LikeEscape(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var alias string
		if err := rows.Scan(&r.ID, &alias, &r.Slug, &r.Title, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if IsASCII2(q) && !WordBoundaryMatch(alias, q) {
			continue
		}
		r.Kind, r.korder, r.bucket, r.Via = "page", 0, bucketAlias, "alias"
		// The label ("alias match") comes from i18n on the template side; only
		// the value belongs here.
		r.Snippet = alias
		out = append(out, r)
	}
	return out, rows.Err()
}

// pagesByTagMatch matches the query against tags.name exactly or by prefix.
// **Never leave this to FTS** (DESIGN 3.1): trigram does nothing at all for a
// two-character Japanese tag.
func (s *Service) pagesByTagMatch(ctx context.Context, q string, fetch int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT p.id, p.slug, p.title, p.updated_at, t.name
		   FROM tags t JOIN page_tags pt ON pt.tag_id = t.id JOIN pages p ON p.id = pt.page_id
		  WHERE t.name = ? OR t.name LIKE ? || '%' ESCAPE '\'
		  ORDER BY p.updated_at DESC LIMIT ?`, q, LikeEscape(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var tag string
		if err := rows.Scan(&r.ID, &r.Slug, &r.Title, &r.UpdatedAt, &tag); err != nil {
			return nil, err
		}
		r.Kind, r.korder, r.bucket, r.Via = "page", 0, bucketTag, "tag"
		// As above: the label lives in i18n, keyed off Via.
		r.Snippet = tag
		out = append(out, r)
	}
	return out, rows.Err()
}

// searchSimpleFTS serves tasks and projects, which share the same column
// layout.
func (s *Service) searchSimpleFTS(ctx context.Context, kind, table, ftsTable, secondCol, q string, fetch int) ([]Result, error) {
	korder := 1
	if kind == "task" {
		korder = 2
	}
	var out []Result
	if RuneLen(q) >= 3 {
		rows, err := s.db.QueryContext(ctx,
			`SELECT t.id, t.title, t.updated_at,
			        snippet(`+ftsTable+`, 1, char(1), char(2), '…', 12),
			        bm25(`+ftsTable+`, 10.0, 1.0) AS score
			   FROM `+ftsTable+` JOIN `+table+` t ON t.id = `+ftsTable+`.rowid
			  WHERE `+ftsTable+` MATCH ?
			  ORDER BY score, t.updated_at DESC LIMIT ?`, Phrase(q), fetch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r Result
			if err := rows.Scan(&r.ID, &r.Title, &r.UpdatedAt, &r.Snippet, &r.Score); err != nil {
				return nil, err
			}
			r.Kind, r.korder = kind, korder
			if containsFold(r.Title, q) {
				r.bucket, r.Via = bucketTitle, "title"
			} else {
				r.bucket, r.Via = bucketBody, "body"
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	// Two characters or fewer: these tables are orders of magnitude smaller, so
	// LIKE is enough.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, updated_at, `+secondCol+`
		   FROM `+table+`
		  WHERE title LIKE '%' || ? || '%' ESCAPE '\' OR `+secondCol+` LIKE '%' || ? || '%' ESCAPE '\'
		  ORDER BY updated_at DESC LIMIT ?`, LikeEscape(q), LikeEscape(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Result
		var second string
		if err := rows.Scan(&r.ID, &r.Title, &r.UpdatedAt, &second); err != nil {
			return nil, err
		}
		if IsASCII2(q) && !WordBoundaryMatch(r.Title, q) && !WordBoundaryMatch(second, q) {
			continue
		}
		r.Kind, r.korder = kind, korder
		if containsFold(r.Title, q) {
			r.bucket, r.Via = bucketTitle, "title"
		} else {
			r.bucket, r.Via = bucketBody, "body"
			r.Snippet = excerpt(second, q)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// searchLogs searches the work log on tasks. Each hit is titled with its task
// and links to the entry; **several hits in one task collapse to its best
// entry**, so one long-running task cannot flood the list.
//
// The body is the only indexed column, so every hit lands in the body bucket.
func (s *Service) searchLogs(ctx context.Context, q string, fetch int) ([]Result, error) {
	if RuneLen(q) >= 3 {
		return s.logsFTS(ctx, q, fetch)
	}
	return s.logsShortQuery(ctx, q, fetch)
}

// logsFTS is the path for three characters or more, ranked by bm25 in SQL
// (DESIGN 3.5). The collapse to one entry per task happens in SQL too, before
// the LIMIT; collapsing afterwards would let one task use up the LIMIT.
//
// **The ranking query touches only the index and the head of each row.**
// bm25() cannot be called inside an aggregate, so the scores are materialized
// first; with min() as the only aggregate, SQLite then takes the bare id from
// the row holding the minimum, i.e. the task's best entry (measured faster than
// a row_number() window). updated_at sits after the body in the row, so reading
// it for every hit means reading every body: the tie-break is the id (newer
// first), and title, updated_at and the body for the snippet are read
// afterwards, for the rows that survived.
func (s *Service) logsFTS(ctx context.Context, q string, fetch int) ([]Result, error) {
	match := Phrase(q)
	rows, err := s.db.QueryContext(ctx,
		`WITH hits AS MATERIALIZED (
		   SELECT l.id, l.task_id, bm25(task_logs_fts) AS score
		     FROM task_logs_fts JOIN task_logs l ON l.id = task_logs_fts.rowid
		    WHERE task_logs_fts MATCH ?)
		 SELECT id, task_id, min(score) AS score FROM hits
		  GROUP BY task_id
		  ORDER BY score, id DESC
		  LIMIT ?`, match, fetch)
	if err != nil {
		return nil, err
	}
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Score); err != nil {
			rows.Close()
			return nil, err
		}
		r.Kind, r.korder, r.bucket, r.Via = "log", 3, bucketBody, "body"
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}

	// The rows that survived, by primary key. snippet() would need a second
	// MATCH, measured slower than the whole ranking query; a trigram phrase hit
	// is a literal substring, so excerpt() finds the same spot in the body.
	ids := make([]any, len(out))
	for i, r := range out {
		ids[i] = r.ID
	}
	rows, err = s.db.QueryContext(ctx,
		`SELECT l.id, t.title, l.updated_at, l.body
		   FROM task_logs l JOIN tasks t ON t.id = l.task_id
		  WHERE l.id IN (?`+strings.Repeat(",?", len(out)-1)+`)`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type detail struct{ title, updated, body string }
	details := make(map[int64]detail, len(out))
	for rows.Next() {
		var id int64
		var d detail
		if err := rows.Scan(&id, &d.title, &d.updated, &d.body); err != nil {
			return nil, err
		}
		details[id] = d
	}
	for i := range out {
		d := details[out[i].ID]
		out[i].Title, out[i].UpdatedAt, out[i].Snippet = d.title, d.updated, excerpt(d.body, q)
	}
	return out, rows.Err()
}

// logsShortQuery is the path for two characters or fewer: a body LIKE,
// updated_at descending, with the ASCII-2 word-boundary filter (DESIGN 3.4).
// **Entries can be article-length, so this is the same linear scan as for page
// bodies** - not the small-table shortcut of searchSimpleFTS.
//
// One task can hold many matching entries, so a LIMIT in SQL would let it use
// up the whole LIMIT before the collapse to one entry per task. The collapse
// needs the boundary filter, which only the application can apply, so this
// runs in two steps: the scan returns just (id, task_id) in order, keeping the
// sort small, and bodies are then read one by one for the entries that could
// still make the list.
func (s *Service) logsShortQuery(ctx context.Context, q string, fetch int) ([]Result, error) {
	ascii2 := IsASCII2(q)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id FROM task_logs
		  WHERE body LIKE '%' || ? || '%' ESCAPE '\'
		  ORDER BY updated_at DESC, id DESC`, LikeEscape(q))
	if err != nil {
		return nil, err
	}
	type cand struct{ id, taskID int64 }
	var cands []cand
	seen := map[int64]bool{}
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.taskID); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []Result
	// Bound the body reads as the page path bounds its LIKE: without it, a query
	// like "go" that the boundary filter mostly rejects could read every entry.
	reads := 0
	for _, c := range cands {
		if len(out) >= fetch || reads >= fetch*3 {
			break
		}
		if seen[c.taskID] {
			continue
		}
		r := Result{ID: c.id, TaskID: c.taskID}
		var body string
		if err := s.db.QueryRowContext(ctx,
			`SELECT t.title, l.updated_at, l.body
			   FROM task_logs l JOIN tasks t ON t.id = l.task_id WHERE l.id = ?`, c.id).
			Scan(&r.Title, &r.UpdatedAt, &body); err != nil {
			return nil, err
		}
		reads++
		if ascii2 && !WordBoundaryMatch(body, q) {
			continue
		}
		seen[c.taskID] = true
		r.Kind, r.korder, r.bucket, r.Via = "log", 3, bucketBody, "body"
		r.Snippet = excerpt(body, q)
		out = append(out, r)
	}
	return out, nil
}

// searchAreas looks areas up by name. They have no FTS table, being few.
func (s *Service) searchAreas(ctx context.Context, q string, fetch int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, updated_at FROM areas
		  WHERE name LIKE '%' || ? || '%' ESCAPE '\' OR description LIKE '%' || ? || '%' ESCAPE '\'
		  ORDER BY updated_at DESC LIMIT ?`, LikeEscape(q), LikeEscape(q), fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var desc string
		if err := rows.Scan(&r.ID, &r.Title, &desc, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if IsASCII2(q) && !WordBoundaryMatch(r.Title, q) && !WordBoundaryMatch(desc, q) {
			continue
		}
		r.Kind, r.korder = "area", 4
		if containsFold(r.Title, q) {
			r.bucket, r.Via = bucketTitle, "title"
		} else {
			r.bucket, r.Via = bucketBody, "body"
			r.Snippet = excerpt(desc, q)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func containsFold(s, q string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(q))
}

// excerpt is a simple snippet for the LIKE paths, where FTS snippet() is not
// available.
func excerpt(body, q string) string {
	i := strings.Index(strings.ToLower(body), strings.ToLower(q))
	if i < 0 {
		rs := []rune(body)
		if len(rs) > 60 {
			return string(rs[:60]) + "…"
		}
		return body
	}
	rs := []rune(body)
	// Convert a byte offset into a character offset
	pos := len([]rune(body[:i]))
	start := pos - 20
	if start < 0 {
		start = 0
	}
	end := pos + 40
	if end > len(rs) {
		end = len(rs)
	}
	qlen := len([]rune(q))
	out := string(rs[start:pos]) + markStart + string(rs[pos:min(pos+qlen, end)]) + markEnd +
		string(rs[min(pos+qlen, end):end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(rs) {
		out += "…"
	}
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
