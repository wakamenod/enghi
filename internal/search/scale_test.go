package search_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// TestSearchScale measures the whole Search() over synthetic corpora the size
// DESIGN 3 targets (20 ms per query). It takes minutes, so it is opt-in:
//
//	ENGHI_SEARCH_SCALE=1 CGO_ENABLED=1 go test -tags sqlite_fts5 \
//	  -run TestSearchScale -v -timeout 30m ./internal/search/
//
// It reports and never fails on timing: the numbers are for a person to judge.
func TestSearchScale(t *testing.T) {
	if os.Getenv("ENGHI_SEARCH_SCALE") == "" {
		t.Skip("set ENGHI_SEARCH_SCALE=1 to run")
	}
	// Two body lengths. "short" is calibrated to the corpus DESIGN 3 was measured
	// on (30k real articles, about 49 MB with the trigram index); "long" makes
	// every page and log entry a real article, 600 to 4,000 characters.
	type profile struct {
		name     string
		min, max int // characters per body
	}
	var corpora []struct {
		name        string
		pages, logs int
		tasks       int
		body        profile
	}
	for _, p := range []profile{{"short", 40, 400}, {"long", 600, 4000}} {
		corpora = append(corpora, []struct {
			name        string
			pages, logs int
			tasks       int
			body        profile
		}{
			{"15k pages + 15k logs, " + p.name, 15000, 15000, 3000, p},
			{"30k pages + 10k logs, " + p.name, 30000, 10000, 2000, p},
		}...)
	}
	for _, c := range corpora {
		t.Run(c.name, func(t *testing.T) {
			db := buildCorpus(t, c.pages, c.logs, c.tasks, c.body.min, c.body.max)
			s := search.New(db)
			ctx := context.Background()
			queries := []struct{ class, q string }{
				{"3+ rare", "量子化ビット数"},
				{"3+ rare (en)", "flamegraph"},
				{"3+ common", "オフィス"},
				{"3+ common (en)", "deploy"},
				{"2 ja", "会議"},
				{"2 ja", "設計"},
				{"2 ascii", "Go"},
				{"2 ascii", "DB"},
				{"zero-hit 3+", "存在しない語句です"},
				{"zero-hit 2", "鰻丼"},
			}
			// all: the whole Search(), every kind. base: every kind but log, i.e.
			// what Search() cost before the work log. log: kind=log alone.
			t.Logf("%-15s %-10s %9s %9s %9s %9s %9s %9s %5s", "class", "query",
				"all p50", "all p95", "base p50", "base p95", "log p50", "log p95", "hits")
			for _, q := range queries {
				all, n := measure(t, s, ctx, q.q, nil)
				base, _ := measure(t, s, ctx, q.q, []string{"page", "project", "task", "area"})
				logs, _ := measure(t, s, ctx, q.q, []string{"log"})
				t.Logf("%-15s %-10s %9s %9s %9s %9s %9s %9s %5d", q.class, q.q,
					ms(all[0]), ms(all[1]), ms(base[0]), ms(base[1]), ms(logs[0]), ms(logs[1]), n)
			}
		})
	}
}

// measure runs one query repeatedly and returns p50 and p95.
func measure(t *testing.T, s *search.Service, ctx context.Context, q string, kinds []string) ([2]time.Duration, int) {
	t.Helper()
	const warm, runs = 3, 40
	var n int
	var ds []time.Duration
	for i := 0; i < warm+runs; i++ {
		start := time.Now()
		rs, err := s.Search(ctx, q, kinds, 50, 0)
		d := time.Since(start)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		n = len(rs)
		if i >= warm {
			ds = append(ds, d)
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return [2]time.Duration{ds[len(ds)/2], ds[len(ds)*95/100]}, n
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000) }

// Vocabulary for the synthetic bodies: Japanese and English mixed, as in
// technical notes. 会議 / 設計 / オフィス / deploy are common; Go and DB appear
// both as words and inside other words (going, algorithm, DBA), so the
// word-boundary filter has work to do.
var (
	jaWords = []string{
		"会議", "設計", "実装", "移転", "経理", "採用", "見積", "契約", "課題", "予算",
		"オフィス", "レビュー", "リリース", "障害", "対応", "調査", "手順", "確認", "検討", "方針",
		"性能", "計測", "改善", "資料", "共有", "担当", "期限", "進捗", "報告", "議事録",
		"データベース", "サーバ", "クライアント", "テスト", "ログ", "キャッシュ", "索引", "検索",
	}
	jaGlue  = []string{"を", "の", "で", "は", "が", "に", "と", "から", "まで", "について"}
	jaEnds  = []string{"した。", "する。", "したい。", "を確認した。", "が必要。", "で決まった。", "を見直す。"}
	enWords = []string{
		"Go", "DB", "deploy", "cache", "index", "query", "latency", "server", "request",
		"going", "algorithm", "good", "DBA", "migration", "schema", "config", "build",
		"release", "review", "the", "and", "with", "from", "into", "after", "before",
	}
)

func sentence(r *rand.Rand) string {
	var b strings.Builder
	if r.Intn(3) == 0 {
		// An English line, the way code comments and commands get pasted in
		n := 6 + r.Intn(10)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(enWords[r.Intn(len(enWords))])
		}
		b.WriteString(".\n")
		return b.String()
	}
	n := 2 + r.Intn(4)
	for i := 0; i < n; i++ {
		b.WriteString(jaWords[r.Intn(len(jaWords))])
		if r.Intn(4) == 0 {
			b.WriteString(" " + enWords[r.Intn(len(enWords))] + " ")
		}
		b.WriteString(jaGlue[r.Intn(len(jaGlue))])
	}
	b.WriteString(jaWords[r.Intn(len(jaWords))])
	b.WriteString(jaEnds[r.Intn(len(jaEnds))])
	if r.Intn(5) == 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

// body is a text of min to max characters.
func body(r *rand.Rand, rare, min, max int) string {
	var b strings.Builder
	target := min + r.Intn(max-min)
	for utf8.RuneCountInString(b.String()) < target {
		b.WriteString(sentence(r))
	}
	if rare == 0 {
		b.WriteString("量子化ビット数を 16 に下げて flamegraph を取った。")
	}
	return b.String()
}

func buildCorpus(t *testing.T, pages, logs, tasks, min, max int) *store.DB {
	t.Helper()
	start := time.Now()
	db, err := store.Open(filepath.Join(t.TempDir(), "scale.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := rand.New(rand.NewSource(1))
	ctx := context.Background()
	err = db.Tx(ctx, func(tx *sql.Tx) error {
		for i := 1; i <= pages; i++ {
			title := fmt.Sprintf("%s%sの%s %d", jaWords[r.Intn(len(jaWords))],
				jaWords[r.Intn(len(jaWords))], jaWords[r.Intn(len(jaWords))], i)
			if _, err := tx.Exec(`INSERT INTO pages(id, slug, title, body) VALUES (?, ?, ?, ?)`,
				i, fmt.Sprintf("p%d", i), title, body(r, r.Intn(3000), min, max)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO page_titles(title, page_id, is_canonical) VALUES (?, ?, 1)`,
				title, i); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO titles_fts(rowid, title_bigram) VALUES (?, ?)`,
				i, wiki.Bigrams(title)); err != nil {
				return err
			}
		}
		for i := 1; i <= tasks; i++ {
			if _, err := tx.Exec(`INSERT INTO tasks(id, title, state) VALUES (?, ?, 'next')`,
				i, fmt.Sprintf("%sの%s %d", jaWords[r.Intn(len(jaWords))], jaWords[r.Intn(len(jaWords))], i)); err != nil {
				return err
			}
		}
		// Skewed: a tenth of the tasks carry most of the log, like the few
		// long-running pieces of work in a real one.
		for i := 1; i <= logs; i++ {
			task := 1 + r.Intn(tasks)
			if r.Intn(10) < 7 {
				task = 1 + r.Intn(tasks/10)
			}
			if _, err := tx.Exec(`INSERT INTO task_logs(task_id, kind, body, created_at, updated_at)
				VALUES (?, 'note', ?, datetime('now', ?), datetime('now', ?))`,
				task, body(r, r.Intn(3000), min, max), fmt.Sprintf("-%d minutes", logs-i), fmt.Sprintf("-%d minutes", logs-i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ANALYZE`); err != nil {
		t.Fatal(err)
	}
	var size int64
	var avgPage, avgLog float64
	db.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size)
	db.QueryRow(`SELECT avg(length(body)) FROM pages`).Scan(&avgPage)
	db.QueryRow(`SELECT avg(length(body)) FROM task_logs`).Scan(&avgLog)
	t.Logf("corpus built in %s: %d pages, %d logs over %d tasks; average body %.0f / %.0f characters; %d MB",
		time.Since(start).Round(time.Second), pages, logs, tasks, avgPage, avgLog, size>>20)
	return db
}
