package readings_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/readings"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

func open(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func pending(t *testing.T, db *store.DB) int {
	t.Helper()
	n, err := readings.Pending(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func doctorKinds(t *testing.T, db *store.DB) map[string]bool {
	t.Helper()
	ps, err := store.Doctor(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, p := range ps {
		out[p.Kind] = true
	}
	return out
}

func reading(t *testing.T, db *store.DB, table, key string, id int64) (source, rd, pr string) {
	t.Helper()
	if err := db.QueryRow(`SELECT source_title, reading, pronunciation FROM `+table+` WHERE `+key+` = ?`, id).
		Scan(&source, &rd, &pr); err != nil {
		t.Fatalf("%s %d: %v", table, id, err)
	}
	return
}

// Missing and stale rows are found - by doctor too - and rebuilt; a second
// run has nothing to do.
func TestRebuildFixesMissingAndStale(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	w, g := wiki.New(db, 10), gtd.New(db)
	p, err := w.Create(ctx, wiki.CreateInput{Title: "検索の設計"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := g.Capture(ctx, gtd.CaptureInput{Title: "会議の準備"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := g.CreateProject(ctx, gtd.ProjectInput{Title: "東京出張"})
	if err != nil {
		t.Fatal(err)
	}

	if n := pending(t, db); n != 3 {
		t.Fatalf("pending = %d, want 3", n)
	}
	if k := doctorKinds(t, db); !k["readings_missing"] || k["readings_stale"] {
		t.Errorf("doctor: %v, want readings_missing only", k)
	}
	n, err := readings.Rebuild(ctx, db)
	if err != nil || n != 3 {
		t.Fatalf("Rebuild = %d, %v; want 3", n, err)
	}
	if src, rd, pr := reading(t, db, "project_readings", "project_id", proj.ID); src != "東京出張" ||
		rd != "とうきょうしゅっちょう" || pr != "ときょしゅっちょ" {
		t.Errorf("project reading = %q %q %q", src, rd, pr)
	}
	if k := doctorKinds(t, db); k["readings_missing"] || k["readings_stale"] {
		t.Errorf("doctor after the rebuild: %v", k)
	}
	if n, _ := readings.Rebuild(ctx, db); n != 0 {
		t.Errorf("second Rebuild = %d, want 0", n)
	}

	// Renames leave the readings stale until the next run
	if _, err := w.Update(ctx, p.Slug, wiki.UpdateInput{Title: "索引の設計", Version: p.Version}); err != nil {
		t.Fatal(err)
	}
	title := "予算の見直し"
	if _, err := g.Patch(ctx, task.ID, gtd.TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if k := doctorKinds(t, db); !k["readings_stale"] || k["readings_missing"] {
		t.Errorf("doctor after renames: %v, want readings_stale only", k)
	}
	if n, err := readings.Rebuild(ctx, db); err != nil || n != 2 {
		t.Fatalf("Rebuild after renames = %d, %v; want 2", n, err)
	}
	if src, rd, _ := reading(t, db, "page_readings", "page_id", p.ID); src != "索引の設計" || rd != "さくいんのせっけい" {
		t.Errorf("page reading = %q %q", src, rd)
	}
	if src, rd, _ := reading(t, db, "task_readings", "task_id", task.ID); src != "予算の見直し" || rd != "よさんのみなおし" {
		t.Errorf("task reading = %q %q", src, rd)
	}

	// A change in case alone still refreshes source_title (pages.title is NOCASE)
	p2, err := w.Create(ctx, wiki.CreateInput{Title: "emacs"})
	if err != nil {
		t.Fatal(err)
	}
	readings.Rebuild(ctx, db)
	if _, err := w.Update(ctx, p2.Slug, wiki.UpdateInput{Title: "Emacs", Version: p2.Version}); err != nil {
		t.Fatal(err)
	}
	if n := pending(t, db); n != 1 {
		t.Errorf("pending after a case-only rename = %d, want 1", n)
	}

	// Deletes cascade
	if err := g.Delete(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	db.QueryRow(`SELECT count(*) FROM task_readings WHERE task_id = ?`, task.ID).Scan(&left)
	if left != 0 {
		t.Errorf("task_readings kept %d row(s) of a deleted task", left)
	}
}

// The worker runs once at start and again shortly after a kick, with bursts
// of kicks folded into one run.
func TestWorker(t *testing.T) {
	db := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := wiki.New(db, 10)
	if _, err := w.Create(ctx, wiki.CreateInput{Title: "起動前のページ"}); err != nil {
		t.Fatal(err)
	}
	runs := make(chan int, 16)
	worker := readings.NewWorker(db, func(ctx context.Context) error {
		n, err := readings.Rebuild(ctx, db)
		runs <- n
		return err
	})
	worker.Delay = 50 * time.Millisecond
	go worker.Loop(ctx)

	wait := func() int {
		select {
		case n := <-runs:
			return n
		case <-time.After(5 * time.Second):
			t.Fatal("the worker did not run")
			return 0
		}
	}
	if n := wait(); n != 1 {
		t.Errorf("start-up run rebuilt %d, want 1", n)
	}
	for _, title := range []string{"一件目", "二件目", "三件目"} {
		if _, err := w.Create(ctx, wiki.CreateInput{Title: title}); err != nil {
			t.Fatal(err)
		}
		worker.Kick()
	}
	if n := wait(); n != 3 {
		t.Errorf("run after the kicks rebuilt %d, want 3", n)
	}
	// A kick with nothing pending does not start a rebuild
	worker.Kick()
	select {
	case n := <-runs:
		t.Errorf("a kick with nothing pending ran a rebuild (%d)", n)
	case <-time.After(300 * time.Millisecond):
	}
	if n := pending(t, db); n != 0 {
		t.Errorf("pending = %d, want 0", n)
	}
}
