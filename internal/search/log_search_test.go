package search_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
)

func setupLogs(t *testing.T) (*search.Service, *gtd.Service) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return search.New(db), gtd.New(db)
}

// logTask captures a task and writes the given notes to its log.
func logTask(t *testing.T, g *gtd.Service, title string, notes ...string) (*gtd.Task, []*gtd.TaskLog) {
	t.Helper()
	ctx := context.Background()
	tk, err := g.Capture(ctx, gtd.CaptureInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	var out []*gtd.TaskLog
	for _, n := range notes {
		l, _, err := g.AddLog(ctx, tk.ID, gtd.LogNote, n)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return tk, out
}

// mustSearch fails the test on a search error. An error must never pass for
// "no hits".
func mustSearch(t *testing.T, s *search.Service, q string, kinds []string) []search.Result {
	t.Helper()
	rs, err := s.Search(context.Background(), q, kinds, 10, 0)
	if err != nil {
		t.Fatalf("Search(%q, %v): %v", q, kinds, err)
	}
	return rs
}

func logHits(rs []search.Result) []search.Result {
	var out []search.Result
	for _, r := range rs {
		if r.Kind == "log" {
			out = append(out, r)
		}
	}
	return out
}

func TestLogFTSHit(t *testing.T) {
	s, g := setupLogs(t)
	tk, logs := logTask(t, g, "ビルドを直す", "キャッシュを消したらリンカエラーが消えた")

	rs, err := s.Search(context.Background(), "リンカエラー", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	hits := logHits(rs)
	if len(hits) != 1 {
		t.Fatalf("log hits = %+v", rs)
	}
	h := hits[0]
	if h.ID != logs[0].ID || h.TaskID != tk.ID || h.Title != "ビルドを直す" || h.Via != "body" {
		t.Errorf("hit = %+v", h)
	}
	if !strings.Contains(string(search.SnippetHTML(h.Snippet)), "<mark>") {
		t.Errorf("snippet = %q", h.Snippet)
	}
}

func TestLogTwoCharJapanese(t *testing.T) {
	s, g := setupLogs(t)
	logTask(t, g, "週次の打ち合わせ", "今日の会議で方針が決まった")

	rs, err := s.Search(context.Background(), "会議", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hits := logHits(rs); len(hits) != 1 || hits[0].Title != "週次の打ち合わせ" {
		t.Fatalf("a two-character Japanese query missed the log: %+v", rs)
	}
}

func TestLogASCIITwoCharWordBoundary(t *testing.T) {
	s, g := setupLogs(t)
	logTask(t, g, "言語を選ぶ", "結局 Go で書くことにした")
	logTask(t, g, "並べ替え", "algorithm を見直して going concern も確認")

	rs, err := s.Search(context.Background(), "go", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	hits := logHits(rs)
	if len(hits) != 1 || hits[0].Title != "言語を選ぶ" {
		t.Fatalf("the word-boundary filter is not applied to logs: %+v", hits)
	}
}

// DESIGN 3.3: quotes and C++ go through the phrase literal on both paths.
func TestLogQueryEscaping(t *testing.T) {
	s, g := setupLogs(t)
	logTask(t, g, "テンプレート", `C++ の a"b という書き方で詰まった`)
	ctx := context.Background()
	for _, q := range []string{`C++`, `a"b`, `"`, `""`, `%`, `_`, `AND OR`} {
		if _, err := s.Search(ctx, q, []string{"log"}, 10, 0); err != nil {
			t.Errorf("Search(%q) failed: %v", q, err)
		}
	}
	for _, q := range []string{`C++`, `a"b`} {
		rs := mustSearch(t, s, q, []string{"log"})
		if len(rs) != 1 {
			t.Errorf("Search(%q) = %d hits, want 1", q, len(rs))
		}
	}
	// LIKE wildcards are literal: "%" matches nothing here
	if rs := mustSearch(t, s, "%", []string{"log"}); len(rs) != 0 {
		t.Errorf("%% matched as a wildcard: %+v", rs)
	}
}

// Several hits in one task collapse to its best entry, on both paths, so the
// other task still makes the list.
func TestLogOneResultPerTask(t *testing.T) {
	s, g := setupLogs(t)
	notes := make([]string, 60)
	for i := range notes {
		notes[i] = "移行作業の続き、移行スクリプトを直した"
	}
	busy, _ := logTask(t, g, "長く続く移行", notes...)
	other, _ := logTask(t, g, "別件", "こちらも移行スクリプトの話")

	for _, q := range []string{"移行スクリプト", "移行"} {
		rs, err := s.Search(context.Background(), q, []string{"log"}, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		per := map[int64]int{}
		for _, r := range rs {
			per[r.TaskID]++
		}
		if per[busy.ID] != 1 || per[other.ID] != 1 || len(rs) != 2 {
			t.Errorf("%s: hits per task = %v, want one each for %d and %d", q, per, busy.ID, other.ID)
		}
	}
}

func TestLogKindFilter(t *testing.T) {
	s, g := setupLogs(t)
	logTask(t, g, "障害対応の記録", "障害対応の手順を残す")

	rs := mustSearch(t, s, "障害対応", []string{"log"})
	if len(rs) != 1 || rs[0].Kind != "log" {
		t.Errorf("kind=log: %+v", rs)
	}
	rs = mustSearch(t, s, "障害対応", []string{"task"})
	if len(rs) != 1 || rs[0].Kind != "task" {
		t.Errorf("kind=task: %+v", rs)
	}
	// No kind: both, the task (a title hit) ahead of its log
	rs = mustSearch(t, s, "障害対応", nil)
	if len(rs) != 2 || rs[0].Kind != "task" || rs[1].Kind != "log" {
		t.Errorf("all kinds: %+v", rs)
	}
}

// An edited or deleted entry is found by its new text only (the FTS triggers).
func TestLogIndexFollowsEdits(t *testing.T) {
	s, g := setupLogs(t)
	ctx := context.Background()
	_, logs := logTask(t, g, "調べもの", "古い仮説を立てた")
	if _, err := g.EditLog(ctx, logs[0].ID, "新しい仮説に変えた", logs[0].Version); err != nil {
		t.Fatal(err)
	}
	if rs := mustSearch(t, s, "古い仮説", []string{"log"}); len(rs) != 0 {
		t.Errorf("the old text is still indexed: %+v", rs)
	}
	if rs := mustSearch(t, s, "新しい仮説", []string{"log"}); len(rs) != 1 {
		t.Errorf("the new text is not indexed: %+v", rs)
	}
	if _, err := g.DeleteLog(ctx, logs[0].ID); err != nil {
		t.Fatal(err)
	}
	if rs := mustSearch(t, s, "新しい仮説", []string{"log"}); len(rs) != 0 {
		t.Errorf("a deleted entry is still found: %+v", rs)
	}
}
