package search

import (
	"context"
	"sort"
	"strings"

	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Result は検索結果1件。種別バッジ付きで1つのリストに混ぜて返す(DESIGN 3.1)。
type Result struct {
	Kind      string  `json:"kind"` // page / project / task / area
	ID        int64   `json:"id"`
	Slug      string  `json:"slug,omitempty"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
	Via       string  `json:"via"` // tag / title / alias / body — どの経路で当たったか
	Score     float64 `json:"score,omitempty"`

	bucket int // 0:tag 1:title 2:alias 3:body
	korder int // 種別の安定順
}

// Service は検索。**ランキングは SQL 側で行う。アプリ側スコアリングはしない**(DESIGN 3.5)。
type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

const (
	bucketTag = iota
	bucketTitle
	bucketAlias
	bucketBody
)

// Search は横断検索。kind が空なら全種別。
func (s *Service) Search(ctx context.Context, q string, kinds []string, limit, offset int) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Result{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50 // 既定 50 件(DESIGN 3.7)
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
	// 必要分より多めに集めてから合成する。SQL 側の LIMIT は外さない。
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
	if want("area") {
		rs, err := s.searchAreas(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}

	// bucket → 種別 → 元の順(SQL 側のランキング)の安定ソート。
	// bm25 は表をまたいで比較できないので、スコアの大小で混ぜない。
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].bucket != all[j].bucket {
			return all[i].bucket < all[j].bucket
		}
		return all[i].korder < all[j].korder
	})

	// 種別をまたいだ重複除去(同一種別・同一 ID は先勝ち = より良い bucket が残る)
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

// searchPages は 3.4 のフォールバックの梯子と 3.4/3.6 のマージ規則を実装する。
func (s *Service) searchPages(ctx context.Context, q string, fetch int) ([]Result, error) {
	var out []Result

	// (1) タグは FTS に載せない。完全一致/前方一致で別途引き、結果の先頭に足す(DESIGN 3.1)。
	tagHits, err := s.pagesByTagMatch(ctx, q, fetch)
	if err != nil {
		return nil, err
	}
	out = append(out, tagHits...)

	// (2) 本体の検索経路。クエリ長で分岐する。
	var core []Result
	if RuneLen(q) >= 3 {
		core, err = s.pagesFTS(ctx, q, fetch)
		if err != nil {
			return nil, err
		}
		// 0 件のときはクエリを後ろから切り詰めて再試行する(2 段まで)。
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

	// (3) 別名は FTS に載らないので直接照合する(DESIGN 3.6)。
	//     合流位置は「正式タイトルのヒットの直後」= bucketAlias。
	aliasHits, err := s.pagesByAlias(ctx, q, fetch)
	if err != nil {
		return nil, err
	}

	out = append(out, core...)
	out = append(out, aliasHits...)
	return out, nil
}

// pagesFTS は 3 文字以上の経路。ランキングは SQL 側(bm25)で行う(DESIGN 3.5)。
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
		// タイトルに含まれるものは「タイトルのヒット」として前に出す。
		if containsFold(r.Title, q) {
			r.bucket, r.Via = bucketTitle, "title"
		} else {
			r.bucket, r.Via = bucketBody, "body"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// pagesShortQuery は 2 文字以下の経路。trigram では引けないため
// titles_fts(bigram)と本文 LIKE の2つの結果集合を合成する(DESIGN 3.4)。
// **日本語は2文字語が主力なので、これは例外ではなく常用経路である。**
func (s *Service) pagesShortQuery(ctx context.Context, q string, fetch int) ([]Result, error) {
	ascii2 := IsASCII2(q)

	// 1. titles_fts のヒットを bm25 昇順で全件、先頭に置く
	match := Phrase(wiki.Bigrams(q))
	if RuneLen(q) == 1 {
		// 1 文字は bigram トークンにならないので前方一致で引く
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
		// ASCII 2 文字のときだけ語境界で再フィルタする(algorithm が go に当たるのを防ぐ)。
		// 日本語 2 文字には適用しない。
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

	// 2. その後ろに本文 LIKE のヒットを updated_at 降順で置く
	//    3. 両方に現れるページはタイトル側を採用し、本文側から除く
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

// pagesByAlias は page_titles の別名を直接照合する。**部分一致とする**(DESIGN 3.6)。
// LIKE は ASCII について既定で大小を区別しない。%  と _ は必ずエスケープする。
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
		r.Snippet = "別名: " + alias
		out = append(out, r)
	}
	return out, rows.Err()
}

// pagesByTagMatch はクエリ文字列を tags.name と完全一致/前方一致で照合する。
// **FTS には任せない**(DESIGN 3.1)。trigram は日本語の 2 文字タグに対して何も機能しない。
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
		r.Snippet = "タグ: " + tag
		out = append(out, r)
	}
	return out, rows.Err()
}

// searchSimpleFTS は tasks / projects 用。列構成が同じなので共通化する。
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
	// 2 文字以下: これらの表は件数が桁違いに少ないので LIKE で十分。
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

// searchAreas は areas を名前で引く(FTS 表は持たない。件数が少ないため)。
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
		r.Kind, r.korder = "area", 3
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

// excerpt は LIKE 経路のための簡易スニペット(FTS の snippet() が使えないため)。
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
	// バイト位置を文字位置に直す
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
