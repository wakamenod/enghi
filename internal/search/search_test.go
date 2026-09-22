package search_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

func setup(t *testing.T) (*search.Service, *wiki.Service) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return search.New(db), wiki.New(db, 10)
}

func create(t *testing.T, w *wiki.Service, title, body string, tags ...string) *wiki.Page {
	t.Helper()
	p, err := w.Create(context.Background(), wiki.CreateInput{Title: title, Body: body, Tags: tags})
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return p
}

func titles(rs []search.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	return out
}

func has(rs []search.Result, title string) bool {
	for _, r := range rs {
		if r.Title == title {
			return true
		}
	}
	return false
}

// 3.3: passing C++ or a"b to MATCH as is, is an FTS5 syntax error.
// **Every query must be turned into a phrase literal.**
func TestQueryEscapingNeverErrors(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "C++ の話", `C++ のテンプレートと a"b という文字列について`)

	for _, q := range []string{`C++`, `a"b`, `"`, `""`, `AND OR NOT`, `*`, `foo(bar)`, `a b`, `:`, `^x`} {
		if _, err := s.Search(ctx, q, nil, 10, 0); err != nil {
			t.Errorf("Search(%q) failed: %v", q, err)
		}
	}
	rs, err := s.Search(ctx, `C++`, nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "C++ の話") {
		t.Fatalf(`"C++" is not found: %v`, titles(rs))
	}
}

// 3.2: a trigram MATCH is effectively a substring match, so natural-language
// queries can be passed through as is.
// **Never pre-split the query on spaces or punctuation and AND the pieces.**
func TestNaturalLanguageQueryMatchesSubstring(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "オフィス移転の記録", "来月のオフィスの移転について打ち合わせた")

	rs, err := s.Search(ctx, "オフィスの移転", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "オフィス移転の記録") {
		t.Fatalf("a natural-language query finds nothing: %v", titles(rs))
	}
}

// 3.4: two characters or fewer cannot be matched by trigram; titles_fts
// (bigram) plus a body LIKE covers them.
// **Japanese is full of two-character words, so this path is in constant use.**
func TestTwoCharJapaneseQuery(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "移転の計画", "本文はどうでもよい")
	create(t, w, "無関係な記事", "ここに移転という語が本文にだけある")

	rs, err := s.Search(ctx, "移転", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "移転の計画") || !has(rs, "無関係な記事") {
		t.Fatalf("a two-character query did not find both: %v", titles(rs))
	}
	// Merge rule: titles_fts hits go first
	if rs[0].Title != "移転の計画" {
		t.Fatalf("title hits are not at the head: %v", titles(rs))
	}
}

// 3.4: two-character ASCII queries are re-filtered on word boundaries, because
// LIKE '%go%' matches algorithm and going.
func TestASCIITwoCharWordBoundary(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "Go の話", "Go は速い")
	create(t, w, "アルゴリズム", "algorithm と going について")

	rs, err := s.Search(ctx, "Go", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "Go の話") {
		t.Fatalf("Go is not found: %v", titles(rs))
	}
	if has(rs, "アルゴリズム") {
		t.Fatalf("the word-boundary filter is not working (algorithm/going matched): %v", titles(rs))
	}
}

// The word-boundary filter is never applied to two Japanese characters:
// everything would be dropped.
func TestJapaneseTwoCharNotFilteredByWordBoundary(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "議事録", "本日の会議の内容をまとめた")

	rs, err := s.Search(ctx, "会議", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "議事録") {
		t.Fatalf("two Japanese characters were dropped by the word-boundary filter: %v", titles(rs))
	}
}

// 3.1: tags are matched exactly or by prefix, outside the FTS index, and put at
// the head of the results - trigram does nothing for a two-character Japanese
// tag.
func TestTagMatchComesFirst(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "タグ付きの記事", "本文に検索語はない", "仕事")
	create(t, w, "仕事について書いた記事", "仕事の話")

	rs, err := s.Search(ctx, "仕事", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) == 0 || rs[0].Title != "タグ付きの記事" {
		t.Fatalf("tag hits are not at the head: %v", titles(rs))
	}
	if rs[0].Via != "tag" {
		t.Fatalf("via = %q, want tag", rs[0].Via)
	}
}

// 3.6: aliases are not in the FTS index and are matched directly. Without
// that, 「イーマックス」 finds nothing.
func TestAliasIsSearchable(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	p := create(t, w, "Emacs", "テキストエディタ")
	if err := w.AddAlias(ctx, p.ID, "イーマックス"); err != nil {
		t.Fatal(err)
	}

	rs, err := s.Search(ctx, "イーマックス", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "Emacs") {
		t.Fatalf("an alias finds nothing: %v", titles(rs))
	}
	if rs[0].Via != "alias" {
		t.Fatalf("via = %q, want alias", rs[0].Via)
	}
	// It is a substring match, not only a prefix match
	rs, _ = s.Search(ctx, "マック", nil, 10, 0)
	if !has(rs, "Emacs") {
		t.Fatalf("a substring of an alias finds nothing: %v", titles(rs))
	}
}

// 3.5: ranking happens in SQL. A LIMIT without ORDER BY drops title hits.
func TestTitleMatchRanksAboveBodyMatch(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	// Create articles with the word only in the body first, so they would come
	// first in rowid order
	for i := 0; i < 30; i++ {
		create(t, w, "雑多な記事"+string(rune('A'+i)), "オフィスの移転について少し触れた")
	}
	create(t, w, "オフィス移転の総括", "まとめ")

	rs, err := s.Search(ctx, "オフィス", nil, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "オフィス移転の総括") {
		t.Fatalf("title hits are not near the top: %v", titles(rs))
	}
}

// 3.4: with no hits, retry with the query trimmed from the end (two steps).
func TestTruncationFallback(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "オフィス移転", "オフィスの移転について")

	rs, err := s.Search(ctx, "オフィスの移転についての詳細な議事録", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) == 0 {
		t.Fatal("the truncation fallback is not working")
	}
}

func TestSnippetIsEscapedNotInjected(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "HTML の記事", `危険な文字列 <script>alert(1)</script> を含む本文`)

	rs, err := s.Search(ctx, "script", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) == 0 {
		t.Fatal("nothing was found")
	}
	html := string(search.SnippetHTML(rs[0].Snippet))
	if strings.Contains(html, "<script>") {
		t.Fatalf("HTML from the body came through unescaped: %q", html)
	}
	if !strings.Contains(html, "<mark>") {
		t.Fatalf("the highlight was lost: %q", html)
	}
}

func TestPhraseAndBoundaryHelpers(t *testing.T) {
	if got := search.Phrase(`a"b`); got != `"a""b"` {
		t.Errorf("Phrase = %q", got)
	}
	if !search.IsASCII2("Go") || search.IsASCII2("移転") || search.IsASCII2("abc") {
		t.Error("IsASCII2 judged wrongly")
	}
	if search.WordBoundaryMatch("algorithm", "go") {
		t.Error("algorithm matched go")
	}
	if !search.WordBoundaryMatch("Go は速い", "go") {
		t.Error("a leading Go was dropped")
	}
	if !search.WordBoundaryMatch("using Go.", "Go") {
		t.Error("a Go before a full stop was dropped")
	}
}

func TestLimitAndOffset(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		create(t, w, "記事"+string(rune('A'+i)), "共通の検索語を含む本文")
	}
	first, err := s.Search(ctx, "共通の検索語", nil, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("limit is not honoured: %d results", len(first))
	}
	second, err := s.Search(ctx, "共通の検索語", nil, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 3 || second[0].Title == first[0].Title {
		t.Fatalf("offset is not honoured: %v / %v", titles(first), titles(second))
	}
}
