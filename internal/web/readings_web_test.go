package web_test

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/config"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/readings"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/web"
)

// OnWrite fires after every successful write, from the JSON API and from the
// forms alike, and never after a read or a failed request. main hangs the
// title-reading rebuild on it (DESIGN 3.8); here the rebuild runs in-process
// so the search can be checked end to end.
func TestOnWriteRebuildsReadings(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	blobs, err := filestore.Open(filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	srv, err := web.New(config.Default(), db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	srv.OnWrite = func() {
		writes++
		if _, err := readings.Rebuild(context.Background(), db); err != nil {
			t.Error(err)
		}
	}
	h := srv.Handler()

	if w := do(h, req("POST", "/api/pages", `{"title":"検索の設計","body":""}`)); w.Code >= 400 {
		t.Fatalf("POST /api/pages: %d %s", w.Code, w.Body)
	}
	if w := postForm(h, "/ui/tasks", url.Values{"title": {"会議の準備"}}); w.Code >= 400 {
		t.Fatalf("POST /ui/tasks: %d %s", w.Code, w.Body)
	}
	if writes != 2 {
		t.Errorf("writes = %d after two writes, want 2", writes)
	}
	// Reads and failures do not count
	do(h, req("GET", "/api/search?q=kensaku", ""))
	do(h, req("POST", "/api/pages", `{"title":""}`))
	if writes != 2 {
		t.Errorf("writes = %d after a read and a failed write, want 2", writes)
	}

	for q, want := range map[string]string{"kensaku": `"kind":"page"`, "kaigi": `"kind":"task"`} {
		w := do(h, req("GET", "/api/search?q="+q, ""))
		if body := w.Body.String(); !strings.Contains(body, want) || !strings.Contains(body, `"via":"reading"`) {
			t.Errorf("/api/search?q=%s = %s, want a %s reading hit", q, body, want)
		}
	}
	// The screen labels the hit
	w := do(h, req("GET", "/search?q=kensaku", ""))
	if !strings.Contains(w.Body.String(), "読みで一致") && !strings.Contains(w.Body.String(), "reading match") {
		t.Errorf("/search?q=kensaku does not label the reading hit:\n%s", w.Body)
	}
}
