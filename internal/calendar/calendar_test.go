package calendar_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/calendar"
	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/settings"
	"github.com/wakamenod/enghi/internal/store"
)

// These run again under UTC+14 and UTC-12 (TestDatesFollowLocalCalendar), so
// a date taken from UTC fails in one of them.

func newDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// iso is a local wall-clock time on today+offset as the shortcut prints it.
func iso(offset, hour, min, sec int) string {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day()+offset, hour, min, sec, 0, time.Local).Format(time.RFC3339)
}

func line(title, start, end, cal string) string {
	return fmt.Sprintf(`{"title":%q,"start":%q,"end":%q,"calendar":%q,"location":""}`, title, start, end, cal)
}

func today(offset int) time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day()+offset, 0, 0, 0, 0, time.Local)
}

func TestParseLinesSkipsBadLines(t *testing.T) {
	in := strings.Join([]string{
		line("会議 | 定例", iso(0, 15, 0, 0), iso(0, 16, 0, 0), "職場"),
		`not json`,
		``,
		line("no start", "", iso(0, 16, 0, 0), "職場"),
		line("backwards", iso(0, 16, 0, 0), iso(0, 15, 0, 0), "職場"),
		`{"title":"改行\nあり","start":"` + iso(0, 9, 0, 0) + `","end":"` + iso(0, 9, 30, 0) + `","calendar":"x"}`,
	}, "\n")
	evs, bad := calendar.ParseLines(strings.NewReader(in))
	if len(evs) != 2 || len(bad) != 3 {
		t.Fatalf("got %d events, %d bad: %v", len(evs), len(bad), bad)
	}
	if evs[0].Title != "会議 | 定例" || evs[1].Title != "改行\nあり" {
		t.Errorf("titles = %q, %q", evs[0].Title, evs[1].Title)
	}
	if bad[0].Line != 2 || bad[1].Line != 4 || bad[2].Line != 5 {
		t.Errorf("bad lines = %v", bad)
	}
	if evs, bad := calendar.ParseLines(strings.NewReader("")); len(evs) != 0 || len(bad) != 0 {
		t.Errorf("empty output: %v %v", evs, bad)
	}
}

func TestAllDayIsDerivedFromTimes(t *testing.T) {
	for _, c := range []struct {
		name       string
		start, end string
		allDay     bool
		days       int // length, for all-day ones
	}{
		{"calendar's 23:59:59", iso(0, 0, 0, 0), iso(0, 23, 59, 59), true, 1},
		{"multi-day", iso(3, 0, 0, 0), iso(4, 23, 59, 59), true, 2},
		{"next midnight", iso(0, 0, 0, 0), iso(1, 0, 0, 0), true, 1},
		{"dates", today(0).Format("2006-01-02"), today(1).Format("2006-01-02"), true, 2},
		{"timed from midnight", iso(0, 0, 0, 0), iso(0, 1, 0, 0), false, 0},
		{"timed", iso(0, 9, 0, 0), iso(0, 23, 59, 59), false, 0},
	} {
		e, err := calendar.Normalize(calendar.Input{Title: c.name, Start: c.start, End: c.end})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if e.AllDay != c.allDay {
			t.Errorf("%s: all_day = %v", c.name, e.AllDay)
			continue
		}
		if c.allDay {
			want := e.Start.AddDate(0, 0, c.days)
			if !e.End.Equal(want) {
				t.Errorf("%s: end = %s, want %s", c.name, e.End, want)
			}
		}
	}
}

func TestEventKeyIsStable(t *testing.T) {
	a, _ := calendar.Normalize(calendar.Input{Title: "定例", Start: "2026-09-26T15:00:00+09:00", End: "2026-09-26T16:00:00+09:00", Calendar: "職場"})
	// The same instant written in another offset, and a new end and place
	b, _ := calendar.Normalize(calendar.Input{Title: "定例", Start: "2026-09-26T06:00:00Z", End: "2026-09-26T17:00:00+09:00", Calendar: "職場", Location: "A"})
	c, _ := calendar.Normalize(calendar.Input{Title: "定例", Start: "2026-09-26T16:00:00+09:00", Calendar: "職場"})
	if a.Key != b.Key {
		t.Error("the key changed with the end, the place or the offset")
	}
	if a.Key == c.Key {
		t.Error("a moved event kept its key")
	}
}

func replace(t *testing.T, svc *calendar.Service, source string, lines ...string) bool {
	t.Helper()
	evs, bad := calendar.ParseLines(strings.NewReader(strings.Join(lines, "\n")))
	if len(bad) > 0 {
		t.Fatal(bad)
	}
	from, to := calendar.Window(time.Now())
	changed, err := svc.Replace(context.Background(), source, from, to, evs)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func titles(evs []*calendar.Event) string {
	var s []string
	for _, e := range evs {
		s = append(s, e.Title)
	}
	return strings.Join(s, ",")
}

func TestWindowReplaceKeepsHistory(t *testing.T) {
	db := newDB(t)
	svc := calendar.New(db)
	ctx := context.Background()

	// A week ago is outside the window; it was stored by an earlier sync
	old, _ := calendar.Normalize(calendar.Input{Title: "old", Start: iso(-7, 10, 0, 0), End: iso(-7, 11, 0, 0), Calendar: "c"})
	if _, err := svc.Replace(ctx, calendar.SourceShortcuts, today(-7), today(-6), []calendar.Event{old}); err != nil {
		t.Fatal(err)
	}

	if !replace(t, svc, calendar.SourceShortcuts,
		line("yesterday", iso(-1, 10, 0, 0), iso(-1, 11, 0, 0), "c"),
		line("deleted", iso(2, 10, 0, 0), iso(2, 11, 0, 0), "c"),
		line("day14", iso(14, 10, 0, 0), iso(14, 11, 0, 0), "c")) {
		t.Error("the first sync reported no change")
	}
	// Another source is left alone by this source's replace
	replace(t, svc, "screenshot", line("other source", iso(2, 12, 0, 0), iso(2, 13, 0, 0), "c"))

	// The next sync no longer has "deleted"
	if !replace(t, svc, calendar.SourceShortcuts,
		line("yesterday", iso(-1, 10, 0, 0), iso(-1, 11, 0, 0), "c"),
		line("day14", iso(14, 10, 0, 0), iso(14, 11, 0, 0), "c")) {
		t.Error("a deletion reported no change")
	}
	kept, _ := svc.Day(ctx, today(14), nil)
	if replace(t, svc, calendar.SourceShortcuts,
		line("yesterday", iso(-1, 10, 0, 0), iso(-1, 11, 0, 0), "c"),
		line("day14", iso(14, 10, 0, 0), iso(14, 11, 0, 0), "c"),
		line("day14", iso(14, 10, 0, 0), iso(14, 11, 0, 0), "c")) {
		t.Error("the same events again reported a change")
	}
	if again, _ := svc.Day(ctx, today(14), nil); again[0].ID != kept[0].ID {
		t.Error("an unchanged event got a new id")
	}

	all, err := svc.Events(ctx, today(-30), today(30), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(all); got != "old,yesterday,other source,day14" {
		t.Errorf("events = %s", got)
	}
}

func TestDayIncludesMultiDayAndHidesCalendars(t *testing.T) {
	db := newDB(t)
	svc := calendar.New(db)
	ctx := context.Background()
	replace(t, svc, calendar.SourceShortcuts,
		line("trip", iso(1, 0, 0, 0), iso(3, 23, 59, 59), "private"),
		line("standup", iso(2, 9, 30, 0), iso(2, 9, 45, 0), "work"),
		line("late", iso(2, 23, 0, 0), iso(3, 1, 0, 0), "work"),
		line("next day", iso(3, 0, 0, 0), iso(3, 0, 30, 0), "work"))

	for _, c := range []struct {
		offset int
		hidden []string
		want   string
	}{
		{0, nil, ""},
		{1, nil, "trip"},
		{2, nil, "trip,standup,late"},
		{3, nil, "trip,late,next day"},
		{4, nil, ""},
		{2, []string{"private"}, "standup,late"},
	} {
		evs, err := svc.Day(ctx, today(c.offset), c.hidden)
		if err != nil {
			t.Fatal(err)
		}
		if got := titles(evs); got != c.want {
			t.Errorf("day %+d hidden %v = %q, want %q", c.offset, c.hidden, got, c.want)
		}
	}
	cals, _ := svc.Calendars(ctx)
	if strings.Join(cals, ",") != "private,work" {
		t.Errorf("calendars = %v", cals)
	}
}

func TestMakeTask(t *testing.T) {
	db := newDB(t)
	svc := calendar.New(db)
	g := gtd.New(db)
	ctx := context.Background()
	replace(t, svc, calendar.SourceShortcuts,
		line("定例", iso(1, 15, 0, 0), iso(1, 16, 30, 0), "work"),
		line("休暇", iso(2, 0, 0, 0), iso(2, 23, 59, 59), "private"),
		line("深夜", iso(1, 23, 30, 0), iso(2, 0, 30, 0), "work"))
	evs, _ := svc.Events(ctx, today(0), today(5), nil)
	byTitle := map[string]*calendar.Event{}
	for _, e := range evs {
		byTitle[e.Title] = e
	}

	for title, want := range map[string]string{
		"定例": "15:00–16:30 定例",
		"休暇": "休暇",
		"深夜": "23:30 深夜",
	} {
		tk, created, err := svc.MakeTask(ctx, byTitle[title].ID, g)
		if err != nil || !created {
			t.Fatalf("%s: %v %v", title, created, err)
		}
		if tk.Title != want {
			t.Errorf("title = %q, want %q", tk.Title, want)
		}
		if tk.State != gtd.StateScheduled || tk.ScheduledOn != byTitle[title].Start.Format("2006-01-02") {
			t.Errorf("%s: state %s on %s", title, tk.State, tk.ScheduledOn)
		}
	}
	if byTitle["定例"].Date() != today(1).Format("2006-01-02") {
		t.Errorf("date = %s", byTitle["定例"].Date())
	}

	// A second click hands back the same task
	again, created, err := svc.MakeTask(ctx, byTitle["定例"].ID, g)
	if err != nil || created {
		t.Fatalf("second MakeTask: created=%v %v", created, err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM tasks`).Scan(&n)
	if n != 3 {
		t.Errorf("%d tasks, want 3", n)
	}

	// The link survives a resync, even one that drops the row and brings it
	// back
	replace(t, svc, calendar.SourceShortcuts)
	replace(t, svc, calendar.SourceShortcuts,
		line("定例", iso(1, 15, 0, 0), iso(1, 16, 30, 0), "work"))
	evs, _ = svc.Day(ctx, today(1), nil)
	if len(evs) != 1 || evs[0].TaskID != again.ID {
		t.Fatalf("after resync: %+v", *evs[0])
	}
	if evs[0].ID == byTitle["定例"].ID {
		t.Error("a new row reused an old id")
	}

	// Deleting the task frees the event
	if err := g.Delete(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	evs, _ = svc.Day(ctx, today(1), nil)
	if evs[0].TaskID != 0 {
		t.Error("the link outlived its task")
	}
	if _, _, err := svc.MakeTask(ctx, 99999, g); !errors.Is(err, calendar.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

// stubRunner stands in for /usr/bin/shortcuts.
type stubRunner struct {
	out       string
	err       error
	installed bool
	runs      atomic.Int32
}

func (s *stubRunner) Run(context.Context, string) ([]byte, error) {
	s.runs.Add(1)
	return []byte(s.out), s.err
}
func (s *stubRunner) Installed(context.Context, string) (bool, error) { return s.installed, nil }
func (s *stubRunner) Open(context.Context, string) error              { return nil }

func TestSyncStatus(t *testing.T) {
	db := newDB(t)
	svc := calendar.New(db)
	set := settings.New(db)
	ctx := context.Background()
	run := &stubRunner{installed: true, out: strings.Join([]string{
		line("a", iso(0, 9, 0, 0), iso(0, 10, 0, 0), "work"),
		"garbage",
		line("a", iso(0, 9, 0, 0), iso(0, 10, 0, 0), "work"),
	}, "\n")}
	y := calendar.NewSyncer(svc, set, run, "enghi-events", time.Hour, true)
	var changes atomic.Int32
	y.OnChange = func() { changes.Add(1) }

	if err := y.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := y.Status(ctx)
	if st.Count != 1 || st.Skipped != 1 || st.Error != "" || st.LastSuccess == "" || changes.Load() != 1 {
		t.Errorf("status = %+v, changes %d", st, changes.Load())
	}
	_ = y.Sync(ctx)
	if changes.Load() != 1 {
		t.Error("an unchanged sync reported a change")
	}

	// Empty output is no events, and clears the window
	run.out = ""
	if err := y.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := y.Status(ctx); st.Count != 0 || changes.Load() != 2 {
		t.Errorf("empty: %+v", st)
	}

	// A missing shortcut is a clear error, and the last success stays
	run.err, run.installed = errors.New("exit status 1"), false
	err := y.Sync(ctx)
	st = y.Status(ctx)
	if !errors.Is(err, calendar.ErrNotInstalled) || !st.Missing || st.LastSuccess == "" {
		t.Errorf("missing: %v %+v", err, st)
	}

	// Not macOS: nothing runs
	off := calendar.NewSyncer(svc, set, run, "enghi-events", time.Hour, false)
	before := run.runs.Load()
	if err := off.Sync(ctx); err == nil || run.runs.Load() != before {
		t.Error("an unsupported syncer ran the shortcut")
	}
}

func TestDaemonRunsOnlyWhenEnabled(t *testing.T) {
	db := newDB(t)
	set := settings.New(db)
	run := &stubRunner{installed: true}
	y := calendar.NewSyncer(calendar.New(db), set, run, "enghi-events", time.Hour, true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { y.Daemon(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if run.runs.Load() != 0 {
		t.Fatal("ran while off")
	}

	if err := set.Set(context.Background(), settings.KeyCalendar, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { y.Daemon(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for run.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if run.runs.Load() != 1 {
		t.Fatalf("runs = %d, want 1 at start-up", run.runs.Load())
	}
}

func TestDatesFollowLocalCalendar(t *testing.T) {
	if os.Getenv("ENGHI_TZ_CHILD") != "" {
		t.Skip("already running in a child")
	}
	for _, tz := range []string{"Etc/GMT-14", "Etc/GMT+12"} {
		cmd := exec.Command(os.Args[0], "-test.run=^Test(AllDay|Window|Day|MakeTask)", "-test.count=1")
		cmd.Env = append(os.Environ(), "TZ="+tz, "ENGHI_TZ_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("TZ=%s: %v\n%s", tz, err, out)
		}
	}
}
