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

// 3.3: C++ や a"b を素で MATCH に渡すと FTS5 の構文エラーになる。
// **全クエリをフレーズリテラル化すること。**
func TestQueryEscapingNeverErrors(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "C++ の話", `C++ のテンプレートと a"b という文字列について`)

	for _, q := range []string{`C++`, `a"b`, `"`, `""`, `AND OR NOT`, `*`, `foo(bar)`, `a b`, `:`, `^x`} {
		if _, err := s.Search(ctx, q, nil, 10, 0); err != nil {
			t.Errorf("Search(%q) がエラー: %v", q, err)
		}
	}
	rs, err := s.Search(ctx, `C++`, nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "C++ の話") {
		t.Fatalf(`"C++" が引けない: %v`, titles(rs))
	}
}

// 3.2: trigram の MATCH は実質的に部分一致。自然文クエリもそのまま渡してよい。
// **クエリを空白や句読点で分割して AND で結ぶ前処理をしてはいけない。**
func TestNaturalLanguageQueryMatchesSubstring(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "オフィス移転の記録", "来月のオフィスの移転について打ち合わせた")

	rs, err := s.Search(ctx, "オフィスの移転", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "オフィス移転の記録") {
		t.Fatalf("自然文クエリが引けない: %v", titles(rs))
	}
}

// 3.4: 2 文字以下は trigram で引けない。titles_fts(bigram)+ 本文 LIKE で対応する。
// **日本語は 2 文字語が主力なのでこれは常用経路。**
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
		t.Fatalf("2 文字クエリで両方引けていない: %v", titles(rs))
	}
	// マージ規則: titles_fts のヒットを先頭に置く
	if rs[0].Title != "移転の計画" {
		t.Fatalf("タイトル一致が先頭に来ていない: %v", titles(rs))
	}
}

// 3.4: ASCII 2 文字クエリは語境界で再フィルタする。
// LIKE '%go%' は algorithm や going に当たるため。
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
		t.Fatalf("Go が引けない: %v", titles(rs))
	}
	if has(rs, "アルゴリズム") {
		t.Fatalf("語境界フィルタが効いていない(algorithm/going に当たった): %v", titles(rs))
	}
}

// 日本語 2 文字クエリには語境界フィルタを適用しない(適用すると全部落ちる)。
func TestJapaneseTwoCharNotFilteredByWordBoundary(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "議事録", "本日の会議の内容をまとめた")

	rs, err := s.Search(ctx, "会議", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "議事録") {
		t.Fatalf("日本語 2 文字が語境界フィルタで落ちている: %v", titles(rs))
	}
}

// 3.1: タグは FTS に載せず完全一致/前方一致で引き、結果の先頭に足す。
// trigram は日本語の 2 文字タグに対して何も機能しないため。
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
		t.Fatalf("タグ一致が先頭に来ていない: %v", titles(rs))
	}
	if rs[0].Via != "tag" {
		t.Fatalf("via = %q, want tag", rs[0].Via)
	}
}

// 3.6: 別名は FTS に載らないので直接照合する。放置すると「イーマックス」で引けない。
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
		t.Fatalf("別名で引けない: %v", titles(rs))
	}
	if rs[0].Via != "alias" {
		t.Fatalf("via = %q, want alias", rs[0].Via)
	}
	// 部分一致であること(前方一致だけにしない)
	rs, _ = s.Search(ctx, "マック", nil, 10, 0)
	if !has(rs, "Emacs") {
		t.Fatalf("別名の部分一致で引けない: %v", titles(rs))
	}
}

// 3.5: ランキングは SQL 側。ORDER BY なしの LIMIT だとタイトル一致が落ちる。
func TestTitleMatchRanksAboveBodyMatch(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	// 本文にだけ語を持つ記事を先に作る(rowid 順なら先頭に来る)
	for i := 0; i < 30; i++ {
		create(t, w, "雑多な記事"+string(rune('A'+i)), "オフィスの移転について少し触れた")
	}
	create(t, w, "オフィス移転の総括", "まとめ")

	rs, err := s.Search(ctx, "オフィス", nil, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "オフィス移転の総括") {
		t.Fatalf("タイトル一致が上位に入っていない: %v", titles(rs))
	}
}

// 3.4: 0 件のときはクエリを後ろから切り詰めて再試行する(2 段まで)。
func TestTruncationFallback(t *testing.T) {
	s, w := setup(t)
	ctx := context.Background()
	create(t, w, "オフィス移転", "オフィスの移転について")

	rs, err := s.Search(ctx, "オフィスの移転についての詳細な議事録", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) == 0 {
		t.Fatal("切り詰めフォールバックが効いていない")
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
		t.Fatal("引けていない")
	}
	html := string(search.SnippetHTML(rs[0].Snippet))
	if strings.Contains(html, "<script>") {
		t.Fatalf("本文の HTML がエスケープされずに出ている: %q", html)
	}
	if !strings.Contains(html, "<mark>") {
		t.Fatalf("強調が失われている: %q", html)
	}
}

func TestPhraseAndBoundaryHelpers(t *testing.T) {
	if got := search.Phrase(`a"b`); got != `"a""b"` {
		t.Errorf("Phrase = %q", got)
	}
	if !search.IsASCII2("Go") || search.IsASCII2("移転") || search.IsASCII2("abc") {
		t.Error("IsASCII2 の判定が違う")
	}
	if search.WordBoundaryMatch("algorithm", "go") {
		t.Error("algorithm が go に当たっている")
	}
	if !search.WordBoundaryMatch("Go は速い", "go") {
		t.Error("語頭の Go が落ちている")
	}
	if !search.WordBoundaryMatch("using Go.", "Go") {
		t.Error("句点の前の Go が落ちている")
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
		t.Fatalf("limit が効いていない: %d 件", len(first))
	}
	second, err := s.Search(ctx, "共通の検索語", nil, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 3 || second[0].Title == first[0].Title {
		t.Fatalf("offset が効いていない: %v / %v", titles(first), titles(second))
	}
}
