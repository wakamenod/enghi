package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/config"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/web"
)

// stubShortcuts stands in for /usr/bin/shortcuts; tests never run the real one.
type stubShortcuts struct {
	mu        sync.Mutex
	out       string
	installed bool
	opened    []string
}

func (s *stubShortcuts) Run(context.Context, string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []byte(s.out), nil
}
func (s *stubShortcuts) Installed(context.Context, string) (bool, error) { return s.installed, nil }
func (s *stubShortcuts) Open(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = append(s.opened, path)
	return nil
}

// newCalendarServer is newServer with the calendar sync on a stub runner.
// A nil runner leaves the server as main would build it off macOS.
func newCalendarServer(t *testing.T, run *stubShortcuts, edit func(*config.Config)) http.Handler {
	t.Helper()
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
	cfg := config.Default()
	cfg.ExportDir, cfg.BackupDir, cfg.SkillsDir = filepath.Join(dir, "export"), filepath.Join(dir, "backup"), filepath.Join(dir, "skills")
	if edit != nil {
		edit(&cfg)
	}
	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if run != nil {
		srv.UseShortcuts(run, true)
	}
	return srv.Handler()
}

// en asks for English, which the assertions below are written in.
func en(r *http.Request) *http.Request {
	r.Header.Set("Accept-Language", "en")
	return r
}

func form(path string, vals url.Values) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(vals.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Host = "127.0.0.1:7777"
	return r
}

func dayOffset(offset int) time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day()+offset, 0, 0, 0, 0, time.Local)
}

// ev is an event on today+offset from hour to hour+1, as JSON.
func ev(title, cal string, offset, hour int) string {
	d := dayOffset(offset)
	s := d.Add(time.Duration(hour) * time.Hour)
	return fmt.Sprintf(`{"title":%q,"calendar":%q,"start":%q,"end":%q,"location":""}`,
		title, cal, s.Format(time.RFC3339), s.Add(time.Hour).Format(time.RFC3339))
}

func putEvents(t *testing.T, h http.Handler, source string, from, to int, evs ...string) {
	t.Helper()
	q := fmt.Sprintf("/api/calendar/events?source=%s&from=%s&to=%s", source,
		dayOffset(from).Format("2006-01-02"), dayOffset(to).Format("2006-01-02"))
	w := do(h, req("PUT", q, "["+strings.Join(evs, ",")+"]"))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT → %d %s", w.Code, w.Body)
	}
}

type apiEvent struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Calendar string `json:"calendar"`
	AllDay   bool   `json:"all_day"`
	TaskID   int64  `json:"task_id"`
}

func getEvents(t *testing.T, h http.Handler, from, to int) []apiEvent {
	t.Helper()
	w := do(h, req("GET", fmt.Sprintf("/api/calendar/events?from=%s&to=%s",
		dayOffset(from).Format("2006-01-02"), dayOffset(to).Format("2006-01-02")), ""))
	if w.Code != http.StatusOK {
		t.Fatalf("GET events → %d %s", w.Code, w.Body)
	}
	var out struct{ Events []apiEvent }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Events
}

func TestCalendarPanelIsLocalOnly(t *testing.T) {
	run := &stubShortcuts{}
	h := newCalendarServer(t, run, func(c *config.Config) { c.AllowedHosts = []string{"macbook.local"} })

	body := do(h, en(req("GET", "/settings", ""))).Body.String()
	for _, want := range []string{`id="calendar"`, `action="/ui/calendar/install"`, `action="/ui/calendar"`,
		`action="/ui/calendar/sync"`, "/guide/enghi#calendar-manual", "Add shortcut", "not installed"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings lacks %q", want)
		}
	}

	r := en(req("GET", "/settings", ""))
	r.Host = "macbook.local"
	body = do(h, r).Body.String()
	if strings.Contains(body, `action="/ui/calendar/install"`) || !strings.Contains(body, "only available from there") {
		t.Error("a request through allowed_hosts is offered Add shortcut")
	}
	r = form("/ui/calendar/install", nil)
	r.Host = "macbook.local"
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("install via allowed_hosts → %d, want 403", got)
	}
	if len(run.opened) != 0 {
		t.Fatal("the shortcut was opened for a remote request")
	}

	if got := do(h, form("/ui/calendar/install", nil)).Code; got != http.StatusSeeOther {
		t.Fatalf("install → %d", got)
	}
	if len(run.opened) != 1 || filepath.Base(run.opened[0]) != "enghi-events.shortcut" {
		t.Errorf("opened %v", run.opened)
	}
}

func TestCalendarPanelOffMacOS(t *testing.T) {
	h := newCalendarServer(t, nil, nil)
	body := do(h, en(req("GET", "/settings", ""))).Body.String()
	if !strings.Contains(body, "This needs macOS") || strings.Contains(body, `action="/ui/calendar/install"`) {
		t.Error("the panel does not say it needs macOS")
	}
	if got := do(h, form("/ui/calendar/install", nil)).Code; got == http.StatusSeeOther {
		t.Error("install succeeded off macOS")
	}
}

func TestCalendarSyncFromSettings(t *testing.T) {
	run := &stubShortcuts{installed: true, out: ev("Standup", "Work", 0, 9) + "\n" + ev("Dentist", "Home", 1, 10)}
	h := newCalendarServer(t, run, nil)

	if got := do(h, form("/ui/calendar/sync", nil)).Code; got != http.StatusSeeOther {
		t.Fatalf("sync → %d", got)
	}
	body := do(h, en(req("GET", "/settings", ""))).Body.String()
	if !strings.Contains(body, "2 events") {
		t.Error("the status does not show the count")
	}
	for _, c := range []string{`value="Work"`, `value="Home"`} {
		if !strings.Contains(body, c) {
			t.Errorf("no checkbox %s", c)
		}
	}

	// Enable, and hide Home: filtered on read, still stored
	if got := do(h, form("/ui/calendar", url.Values{"enabled": {"1"},
		"known": {"Work", "Home"}, "show": {"Work"}})).Code; got != http.StatusSeeOther {
		t.Fatalf("save → %d", got)
	}
	if evs := getEvents(t, h, 0, 1); len(evs) != 1 || evs[0].Title != "Standup" {
		t.Errorf("with Home hidden: %+v", evs)
	}
	w := do(h, req("GET", "/api/calendar/status", ""))
	if !strings.Contains(w.Body.String(), `"enabled":true`) || !strings.Contains(w.Body.String(), `"count":2`) {
		t.Errorf("status = %s", w.Body)
	}
	do(h, form("/ui/calendar", url.Values{"enabled": {"1"}, "known": {"Work", "Home"}, "show": {"Work", "Home"}}))
	if evs := getEvents(t, h, 0, 1); len(evs) != 2 {
		t.Errorf("shown again: %+v", evs)
	}
	w = do(h, req("POST", "/api/calendar/sync", "{}"))
	if w.Code != http.StatusOK {
		t.Errorf("POST /api/calendar/sync → %d %s", w.Code, w.Body)
	}
}

func TestDashboardTimeline(t *testing.T) {
	h := newCalendarServer(t, &stubShortcuts{}, nil)

	// Off and empty: no section
	if body := do(h, en(req("GET", "/", ""))).Body.String(); strings.Contains(body, "Today&#39;s schedule") {
		t.Error("the schedule shows while the sync is off and there are no events")
	}
	do(h, form("/ui/calendar", url.Values{"enabled": {"1"}}))
	if body := do(h, en(req("GET", "/", ""))).Body.String(); !strings.Contains(body, "Nothing on the calendar today.") {
		t.Error("no quiet line for an empty day")
	}

	d := dayOffset(0)
	allDay := fmt.Sprintf(`{"title":"Holiday","calendar":"Home","start":%q,"end":%q}`,
		d.Format(time.RFC3339), d.Add(24*time.Hour-time.Second).Format(time.RFC3339))
	putEvents(t, h, "test", 0, 0, ev("Standup", "Work", 0, 9), allDay, ev("Review", "Work", 0, 16))

	body := do(h, en(req("GET", "/", ""))).Body.String()
	iHol, iStand, iRev := strings.Index(body, "Holiday"), strings.Index(body, "Standup"), strings.Index(body, "Review")
	if iHol < 0 || iStand < 0 || iRev < 0 || !(iHol < iStand && iStand < iRev) {
		t.Errorf("order: all-day %d, 09:00 %d, 16:00 %d", iHol, iStand, iRev)
	}
	for _, want := range []string{"data-timeline-live", `class="tl-now"`, "09:00–10:00", "all day",
		`class="tag tl-cal" title="Work">Work`, "/ui/calendar/events/"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
	var dash struct {
		Events []apiEvent `json:"events"`
	}
	if err := json.Unmarshal(do(h, req("GET", "/api/dashboard", "")).Body.Bytes(), &dash); err != nil {
		t.Fatal(err)
	}
	if len(dash.Events) != 3 || !dash.Events[0].AllDay {
		t.Errorf("/api/dashboard events = %+v", dash.Events)
	}
}

func TestMakeTaskFromEvent(t *testing.T) {
	h := newCalendarServer(t, &stubShortcuts{}, nil)
	putEvents(t, h, "test", 1, 1, ev("Planning", "Work", 1, 15))
	evs := getEvents(t, h, 1, 1)
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	path := fmt.Sprintf("/api/calendar/events/%d/task", evs[0].ID)

	w := do(h, req("POST", path, "{}"))
	if w.Code != http.StatusCreated {
		t.Fatalf("make a task → %d %s", w.Code, w.Body)
	}
	var tk struct {
		ID          int64
		Title       string
		State       string
		ScheduledOn string `json:"scheduled_on"`
	}
	json.Unmarshal(w.Body.Bytes(), &tk)
	if tk.Title != "15:00–16:00 Planning" || tk.State != "scheduled" || tk.ScheduledOn != dayOffset(1).Format("2006-01-02") {
		t.Errorf("task = %+v", tk)
	}

	// Again, by the form: the same task, no second one
	if got := do(h, form(fmt.Sprintf("/ui/calendar/events/%d/task", evs[0].ID), nil)).Code; got != http.StatusSeeOther {
		t.Errorf("form → %d", got)
	}
	w = do(h, req("POST", path, "{}"))
	var again struct{ ID int64 }
	json.Unmarshal(w.Body.Bytes(), &again)
	if w.Code != http.StatusOK || again.ID != tk.ID {
		t.Errorf("second → %d, id %d want %d", w.Code, again.ID, tk.ID)
	}
	if n := strings.Count(do(h, req("GET", "/api/tasks", "")).Body.String(), `"title":"15:00–16:00 Planning"`); n != 1 {
		t.Errorf("%d tasks from one event", n)
	}

	// The day page shows the event, linked to its task, and no button
	body := do(h, en(req("GET", "/gtd/day/"+dayOffset(1).Format("2006-01-02"), ""))).Body.String()
	if !strings.Contains(body, fmt.Sprintf(`href="/gtd/clarify/%d">task</a>`, tk.ID)) ||
		strings.Contains(body, "/ui/calendar/events/") {
		t.Error("the day page does not link the event to its task")
	}
	if got := do(h, req("POST", "/api/calendar/events/99999/task", "{}")).Code; got != http.StatusNotFound {
		t.Errorf("unknown event → %d", got)
	}
}

func TestDayShowsStoredEvents(t *testing.T) {
	h := newCalendarServer(t, &stubShortcuts{}, nil)
	// A past day, outside any sync window: the stored history
	putEvents(t, h, "test", -20, -20, ev("Old meeting", "Work", -20, 11))
	date := dayOffset(-20).Format("2006-01-02")

	body := do(h, en(req("GET", "/gtd/day/"+date, ""))).Body.String()
	if !strings.Contains(body, "Old meeting") || !strings.Contains(body, `id="schedule"`) ||
		strings.Contains(body, "data-timeline-live") {
		t.Error("the past day does not show its stored event")
	}
	w := do(h, req("GET", "/api/day?date="+date, ""))
	if !strings.Contains(w.Body.String(), `"events":[{`) {
		t.Errorf("/api/day lacks events: %s", w.Body)
	}
	md := do(h, en(req("GET", "/api/day?format=markdown&date="+date, ""))).Body.String()
	if !strings.Contains(md, "## Schedule\n\n- 11:00–12:00 Old meeting\n") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestPutCalendarEventsValidates(t *testing.T) {
	h := newCalendarServer(t, nil, nil)
	for _, c := range []struct{ path, body string }{
		{"/api/calendar/events?from=2026-09-01&to=2026-09-02", "[]"},
		{"/api/calendar/events?source=x&from=2026-09-01", "[]"},
		{"/api/calendar/events?source=x&from=2026-09-02&to=2026-09-01", "[]"},
		{"/api/calendar/events?source=x&from=2026-09-01&to=2026-09-02", `{"title":"not an array"}`},
		{"/api/calendar/events?source=x&from=2026-09-01&to=2026-09-02", `[{"title":"no start"}]`},
	} {
		if got := do(h, req("PUT", c.path, c.body)).Code; got != http.StatusBadRequest {
			t.Errorf("%s %s → %d, want 400", c.path, c.body, got)
		}
	}
}
