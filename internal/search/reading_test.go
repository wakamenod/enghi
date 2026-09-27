package search_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/readings"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// readingFixture is a small corpus with its readings built, as the server
// has them a few seconds after the writes.
type readingFixture struct {
	db *store.DB
	s  *search.Service
	w  *wiki.Service
	g  *gtd.Service
}

func newReadingFixture(t *testing.T) *readingFixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &readingFixture{db: db, s: search.New(db), w: wiki.New(db, 10), g: gtd.New(db)}
}

func (f *readingFixture) rebuild(t *testing.T) {
	t.Helper()
	if _, err := readings.Rebuild(context.Background(), f.db); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
}

func find(rs []search.Result, kind, title string) *search.Result {
	for i := range rs {
		if rs[i].Kind == kind && rs[i].Title == title {
			return &rs[i]
		}
	}
	return nil
}

// 3.8: a romaji query finds the title by its reading, for pages, tasks and
// projects.
func TestReadingFindsTitles(t *testing.T) {
	f := newReadingFixture(t)
	ctx := context.Background()
	create(t, f.w, "検索の設計", "trigram と bigram の使い分け")
	if _, err := f.g.Capture(ctx, gtd.CaptureInput{Title: "会議の準備"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.g.CreateProject(ctx, gtd.ProjectInput{Title: "オフィスの移転"}); err != nil {
		t.Fatal(err)
	}
	create(t, f.w, "東京の天気", "")
	create(t, f.w, "今日は休み", "")
	f.rebuild(t)

	for _, c := range []struct{ q, kind, title string }{
		{"kensaku", "page", "検索の設計"},
		{"KENSAKU", "page", "検索の設計"},
		{"kensakunosekkei", "page", "検索の設計"},
		{"kaigi", "task", "会議の準備"},
		{"junbi", "task", "会議の準備"},
		{"ofisu", "project", "オフィスの移転"},
		{"iten", "project", "オフィスの移転"}, // いて while the n is still being typed
		// The reading and the pronunciation both match
		{"toukyou", "page", "東京の天気"},
		{"tokyo", "page", "東京の天気"},
		{"kyouha", "page", "今日は休み"},
		{"kyowa", "page", "今日は休み"},
	} {
		rs, err := f.s.Search(ctx, c.q, nil, 50, 0)
		if err != nil {
			t.Fatalf("Search(%q): %v", c.q, err)
		}
		r := find(rs, c.kind, c.title)
		if r == nil {
			t.Errorf("Search(%q) = %v, want the %s %q", c.q, titles(rs), c.kind, c.title)
			continue
		}
		if r.Via != "reading" {
			t.Errorf("Search(%q): via = %q, want reading", c.q, r.Via)
		}
		if r.Snippet == "" {
			t.Errorf("Search(%q): the snippet should be the reading", c.q)
		}
	}
	// kind= is honoured
	rs, err := f.s.Search(ctx, "kensaku", []string{"task"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 0 {
		t.Errorf("kind=task: %v, want nothing", titles(rs))
	}
	// A single kana is below the threshold
	if rs, _ := f.s.Search(ctx, "ke", nil, 50, 0); find(rs, "page", "検索の設計") != nil {
		t.Errorf("ke (one kana) should not take the reading path")
	}
	// A page slug comes along, for the link
	rs, _ = f.s.Search(ctx, "kensaku", []string{"page"}, 50, 0)
	if len(rs) != 1 || rs[0].Slug == "" {
		t.Errorf("kensaku: %+v, want one page with its slug", rs)
	}
}

// 3.8: reading hits go last, so an English query that also happens to be
// romaji keeps every earlier result in its place.
func TestReadingKeepsEarlierOrder(t *testing.T) {
	f := newReadingFixture(t)
	ctx := context.Background()
	create(t, f.w, "API の設計", "REST API と gRPC")
	create(t, f.w, "Go のテスト", "go test と test fixtures の話")
	create(t, f.w, "テスト計画", "test plan の雛形")
	create(t, f.w, "going concern", "algorithm と api gateway")
	create(t, f.w, "アピールの方法", "") // あぴ: api as romaji
	create(t, f.w, "午後の予定", "")   // ご: go as romaji (one kana, below the threshold)
	queries := []string{"test", "api", "go", "API", "Go"}

	before := map[string][]search.Result{}
	for _, q := range queries {
		rs, err := f.s.Search(ctx, q, nil, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		before[q] = rs
	}
	f.rebuild(t)
	for _, q := range queries {
		rs, err := f.s.Search(ctx, q, nil, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var rest []search.Result
		for _, r := range rs {
			if r.Via != "reading" {
				rest = append(rest, r)
			}
		}
		if !reflect.DeepEqual(titles(rest), titles(before[q])) {
			t.Errorf("%s: %v before the readings, %v after", q, titles(before[q]), titles(rest))
		}
		for i, r := range rs {
			if r.Via == "reading" && i < len(rest) {
				t.Errorf("%s: reading hit %q at %d, before other results", q, r.Title, i)
			}
		}
	}
	if rs, _ := f.s.Search(ctx, "api", nil, 50, 0); find(rs, "page", "アピールの方法") == nil {
		t.Errorf("api: the reading hit アピール should come last, not be missing")
	}
}

// 3.8: when the reading path hits, the truncation fallback does not run -
// kensaku would otherwise also bring every body with "kens" in it.
func TestReadingSkipsTruncation(t *testing.T) {
	f := newReadingFixture(t)
	ctx := context.Background()
	create(t, f.w, "検索の設計", "")
	create(t, f.w, "旅行記", "Kensington で美術館を回った")

	// Without readings (before the rebuild), the truncation finds kens
	rs, err := f.s.Search(ctx, "kensaku", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "旅行記") {
		t.Fatalf("precondition: kensaku should reach Kensington by truncation, got %v", titles(rs))
	}
	f.rebuild(t)
	rs, err = f.s.Search(ctx, "kensaku", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rs, "検索の設計") || has(rs, "旅行記") {
		t.Errorf("kensaku = %v, want the reading hit and no truncation", titles(rs))
	}
}
