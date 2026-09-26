package web_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A day with one of each: done, working, worked on, dropped.
func dayFixture(t *testing.T, h http.Handler) {
	t.Helper()
	mustJSON(t, h, "POST", "/api/projects", `{"title":"Office move","outcome":"Moved in"}`)
	for _, title := range []string{"Call the agent", "Pack the books", "Check the lease", "Old idea"} {
		mustJSON(t, h, "POST", "/api/tasks", `{"title":"`+title+`"}`)
	}
	mustJSON(t, h, "PATCH", "/api/tasks/1", `{"state":"next","project_id":1}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"start"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"body":"Line one\n\nLine *two*"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/complete", `{}`)
	mustJSON(t, h, "POST", "/api/tasks/2/logs", `{"kind":"start","body":"from the top shelf"}`)
	mustJSON(t, h, "POST", "/api/tasks/3/logs", `{"body":"Clause 4 is the one"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/4", `{"state":"dropped"}`)
}

type dayEntryJSON struct {
	Task struct {
		ID           int64  `json:"id"`
		ProjectTitle string `json:"project_title"`
		Working      bool   `json:"working"`
	} `json:"task"`
	CompletedAt string `json:"completed_at"`
	Since       string `json:"since"`
	Logs        []struct {
		Kind      string `json:"kind"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
	} `json:"logs"`
}

func TestDayAPI(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)

	w := do(h, req("GET", "/api/day", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/day → %d %s", w.Code, w.Body)
	}
	var got struct {
		Date     string                    `json:"date"`
		Weekday  string                    `json:"weekday"`
		Timezone string                    `json:"timezone"`
		Done     []dayEntryJSON            `json:"done"`
		Working  []dayEntryJSON            `json:"working"`
		Worked   []dayEntryJSON            `json:"worked"`
		Dropped  []dayEntryJSON            `json:"dropped"`
		Groups   map[string][]dayEntryJSON `json:"-"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	got.Groups = map[string][]dayEntryJSON{
		"done": got.Done, "working": got.Working, "worked": got.Worked, "dropped": got.Dropped}
	for k, g := range got.Groups {
		if len(g) != 1 {
			t.Fatalf("%s has %d tasks, want 1: %s", k, len(g), w.Body)
		}
	}

	if want := time.Now().Format("2006-01-02"); got.Date != want {
		t.Errorf("date = %s, want %s", got.Date, want)
	}
	if got.Weekday != time.Now().Weekday().String()[:3] || got.Timezone == "" {
		t.Errorf("weekday = %q, timezone = %q", got.Weekday, got.Timezone)
	}
	iso := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(Z|[+-]\d\d:\d\d)$`)
	done := got.Groups["done"][0]
	if done.Task.ID != 1 || done.Task.ProjectTitle != "Office move" || !iso.MatchString(done.CompletedAt) {
		t.Errorf("done = %+v", done)
	}
	if len(done.Logs) != 2 || done.Logs[0].Kind != "start" || done.Logs[1].Body != "Line one\n\nLine *two*" ||
		!iso.MatchString(done.Logs[1].CreatedAt) {
		t.Errorf("done logs = %+v", done.Logs)
	}
	if wk := got.Groups["working"][0]; wk.Task.ID != 2 || !wk.Task.Working || !iso.MatchString(wk.Since) {
		t.Errorf("working = %+v", wk)
	}
	if got.Groups["worked"][0].Task.ID != 3 || got.Groups["dropped"][0].Task.ID != 4 {
		t.Errorf("worked / dropped = %+v / %+v", got.Groups["worked"], got.Groups["dropped"])
	}

	// A past day is there, and empty
	w = do(h, req("GET", "/api/day?date=2020-01-01", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"done":[]`) {
		t.Errorf("past day → %d %s", w.Code, w.Body)
	}
}

func TestDayMarkdown(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)
	r := req("GET", "/api/day?format=markdown", "")
	r.Header.Set("Accept-Language", "en")
	w := do(h, r)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("→ %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	md := w.Body.String()
	hm := `\d\d:\d\d`
	for _, re := range []string{
		`^# Work record \d{4}-\d\d-\d\d \(\w{3}\)\n`,
		`\n## Done\n\n- ` + hm + ` Call the agent — Office move\n  - ` + hm + ` ▶ Started\n  - ` + hm + ` Line one\n\n    Line \*two\*\n`,
		`\n## In progress\n\n- Pack the books \(since ` + hm + `\)\n  - ` + hm + ` ▶ Started from the top shelf\n`,
		`\n## Worked on\n\n- Check the lease\n  - ` + hm + ` Clause 4 is the one\n`,
		`\n## Dropped\n\n- ` + hm + ` Old idea\n$`,
	} {
		if !regexp.MustCompile(re).MatchString(md) {
			t.Errorf("markdown does not match %q:\n%s", re, md)
		}
	}

	// The page's copy button carries the same text
	r = req("GET", "/gtd/day", "")
	r.Header.Set("Accept-Language", "en")
	page := do(h, r).Body.String()
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(md)
	if !strings.Contains(page, `<textarea id="day-md" class="day-md" readonly hidden>`+esc+`</textarea>`) {
		t.Errorf("the page's Markdown differs from the API's")
	}
}

func TestDayBadDate(t *testing.T) {
	h := newServer(t)
	for _, path := range []string{"/gtd/day/2020-13-01", "/gtd/day/yesterday", "/gtd/day?date=2020-1-1"} {
		r := req("GET", path, "")
		r.Header.Set("Accept-Language", "en")
		w := do(h, r)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
			t.Errorf("GET %s → %d %s", path, w.Code, w.Body)
		}
	}
	w := do(h, req("GET", "/api/day?date=nope", ""))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"bad_date"`) {
		t.Errorf("/api/day bad date → %d %s", w.Code, w.Body)
	}
	if w := do(h, req("GET", "/api/days?month=2020-13", "")); w.Code != http.StatusBadRequest {
		t.Errorf("/api/days bad month → %d", w.Code)
	}
}

func TestDaysAPI(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)
	w := do(h, req("GET", "/api/days", ""))
	var got struct {
		Month string `json:"month"`
		Days  map[string]struct {
			Done    int `json:"done"`
			Dropped int `json:"dropped"`
			Logs    int `json:"logs"`
		} `json:"days"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	today := got.Days[time.Now().Format("2006-01-02")]
	if got.Month != time.Now().Format("2006-01") || len(got.Days) != 1 ||
		today.Done != 1 || today.Dropped != 1 || today.Logs != 4 {
		t.Errorf("days = %+v", got)
	}
	w = do(h, req("GET", "/api/days?month=2020-01", ""))
	if !strings.Contains(w.Body.String(), `"days":{}`) {
		t.Errorf("an empty month = %s", w.Body)
	}
}

func TestDayPageNavigation(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)
	body := do(h, req("GET", "/gtd/day/2024-03-01", "")).Body.String()
	for _, want := range []string{
		`href="/gtd/day/2024-02-29" data-key="prev-day"`,
		`href="/gtd/day/2024-03-02" data-key="next-day"`,
		`href="/gtd/day" data-key="today"`,
		`<input type="date" name="date" value="2024-03-01"`,
		`href="/gtd/day/2024-03-01?month=2024-02"`,
		`<td class=" sel"><a href="/gtd/day/2024-03-01" aria-current="date">1</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	// Today: no Today link, the groups, a dot on the calendar, and the links in
	body = do(h, req("GET", "/gtd/day", "")).Body.String()
	if strings.Contains(body, `data-key="today"`) {
		t.Error("today links to today")
	}
	for _, want := range []string{`id="done"`, `id="working"`, `id="worked"`, `<details class="panel day-dropped"`,
		`<span class="dot"`, `href="/gtd/clarify/1"`, `data-copy="day-md"`} {
		if !strings.Contains(body, want) {
			t.Errorf("today's page lacks %s", want)
		}
	}
	for _, path := range []string{"/", "/gtd", "/gtd/review"} {
		if !strings.Contains(do(h, req("GET", path, "")).Body.String(), `href="/gtd/day`) {
			t.Errorf("%s has no link to the day page", path)
		}
	}
}

// The dashboard lists what is being worked on now.
func TestDashboardWorking(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)
	var d struct {
		GTD struct {
			Working []struct {
				ID int64 `json:"id"`
			} `json:"working"`
		} `json:"gtd"`
	}
	json.Unmarshal(do(h, req("GET", "/api/dashboard", "")).Body.Bytes(), &d)
	if len(d.GTD.Working) != 1 || d.GTD.Working[0].ID != 2 {
		t.Errorf("working = %+v", d.GTD.Working)
	}
}
