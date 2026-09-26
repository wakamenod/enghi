package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		`\n## In progress\n\n- Pack the books \(since ` + hm + `\)\n  - ` + hm + ` ▶ Started — from the top shelf\n`,
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

// A done task carries its last entries from before the day, apart from the
// day's own: earlier_logs in JSON, a dated sub-list in Markdown, a folded
// block on the page.
func TestDayEarlierLogs(t *testing.T) {
	h, db := newServerDB(t)
	dayFixture(t, h)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"body":"The background"}`)
	if _, err := db.Exec(`UPDATE task_logs SET created_at = datetime('now','-2 days') WHERE body = 'The background'`); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Done []struct {
			Logs        []struct{ Body string } `json:"logs"`
			EarlierLogs []struct {
				Body      string `json:"body"`
				CreatedAt string `json:"created_at"`
			} `json:"earlier_logs"`
		} `json:"done"`
	}
	w := do(h, req("GET", "/api/day", ""))
	decode(t, w, &got)
	if len(got.Done) != 1 || len(got.Done[0].Logs) != 2 || len(got.Done[0].EarlierLogs) != 1 ||
		got.Done[0].EarlierLogs[0].Body != "The background" || got.Done[0].EarlierLogs[0].CreatedAt == "" {
		t.Errorf("done = %+v", got.Done)
	}
	// Only done items have the field
	var raw map[string][]map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &raw)
	if _, ok := raw["working"][0]["earlier_logs"]; ok {
		t.Error("a working item has earlier_logs")
	}

	r := req("GET", "/api/day?format=markdown", "")
	r.Header.Set("Accept-Language", "en")
	md := do(h, r).Body.String()
	re := `    Line \*two\*\n  - Earlier entries\n    - \d{4}-\d\d-\d\d \d\d:\d\d The background\n`
	if !regexp.MustCompile(re).MatchString(md) {
		t.Errorf("markdown does not match %q:\n%s", re, md)
	}

	r = req("GET", "/gtd/day", "")
	r.Header.Set("Accept-Language", "en")
	page := do(h, r).Body.String()
	if !regexp.MustCompile(`<details class="day-earlier" open>\s*<summary>Earlier entries <span class="count">1</span>`).MatchString(page) ||
		!regexp.MustCompile(`#log-\d+">\d{4}-\d\d-\d\d \d\d:\d\d</a>`).MatchString(page) {
		t.Errorf("the page lacks the earlier entries:\n%s", page)
	}
}

// Rows on the day page are task rows: the cursor, the single keys and the
// move modal read these.
func TestDayRowsAreTaskRows(t *testing.T) {
	h := newServer(t)
	dayFixture(t, h)
	page := do(h, req("GET", "/gtd/day", "")).Body.String()
	for _, want := range []string{
		`data-task-id="1" data-state="done" data-title="Call the agent"`,
		`data-task-id="2" data-state="inbox"`,
		`data-working="true"`, `data-version="`, `data-project-title="Office move"`,
		`class="task-title day-title" href="/gtd/clarify/2"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the day page lacks %s", want)
		}
	}
}

// Moving a working task out of Next pauses it, the page says why, and Undo
// puts the work back without leaving a trace.
func TestAutoPauseOnMove(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"Write the report"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1", `{"state":"next"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"start"}`)

	var tk struct {
		Working bool `json:"working"`
		Version int  `json:"version"`
	}
	w := do(h, req("PATCH", "/api/tasks/1", `{"state":"someday"}`))
	decode(t, w, &tk)
	if tk.Working {
		t.Fatal("still working after the move to someday")
	}
	w = do(h, req("PATCH", "/api/tasks/1", `{"state":"waiting","waiting_for":"Ann"}`))
	decode(t, w, &tk)
	var logs struct {
		Logs []struct {
			Kind    string `json:"kind"`
			MovedTo string `json:"moved_to"`
		} `json:"logs"`
	}
	decode(t, do(h, req("GET", "/api/tasks/1/logs", "")), &logs)
	if len(logs.Logs) != 2 || logs.Logs[1].Kind != "pause" || logs.Logs[1].MovedTo != "someday" {
		t.Errorf("logs = %+v, want start and one automatic pause", logs.Logs)
	}

	for path, want := range map[string]string{
		"/gtd/day":                 `⏸ paused `,
		"/gtd/clarify/1":           `(moved to Someday)`,
		"/api/day?format=markdown": `⏸ Paused (moved to Someday)`,
		"/api/day":                 `"moved_to":"someday"`,
	} {
		r := req("GET", path, "")
		r.Header.Set("Accept-Language", "en")
		if body := do(h, r).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s lacks %s", path, want)
		}
	}

	// Undo of the first move: back to next, working again, the pause gone
	if w := postForm(h, "/ui/tasks/1", url.Values{"state": {"next"}, "resume": {"1"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("undo → %d %s", w.Code, w.Body)
	}
	var one struct {
		Task struct {
			Working bool `json:"working"`
		} `json:"task"`
	}
	decode(t, do(h, req("GET", "/api/tasks/1", "")), &one)
	tk.Working = one.Task.Working
	decode(t, do(h, req("GET", "/api/tasks/1/logs", "")), &logs)
	if !tk.Working || len(logs.Logs) != 1 {
		t.Errorf("after the undo: working = %v, logs = %+v", tk.Working, logs.Logs)
	}
	// An undo that does not resume writes nothing
	postForm(h, "/ui/tasks/1", url.Values{"state": {"next"}})
	decode(t, do(h, req("GET", "/api/tasks/1/logs", "")), &logs)
	if len(logs.Logs) != 1 {
		t.Errorf("logs = %+v", logs.Logs)
	}
}

// The review's look back links the same days its completions come from: the
// seven days before today.
func TestReviewPastDaysMatchCompletions(t *testing.T) {
	h, db := newServerDB(t)
	today := time.Now()
	for i, ago := range []int{0, 1, 7, 8} {
		mustJSON(t, h, "POST", "/api/tasks", `{"title":"t"}`)
		id := fmt.Sprint(i + 1)
		mustJSON(t, h, "POST", "/api/tasks/"+id+"/complete", `{}`)
		// Noon local on that day, as UTC
		d := today.AddDate(0, 0, -ago)
		noon := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.Local).UTC().Format("2006-01-02 15:04:05")
		if _, err := db.Exec(`UPDATE tasks SET completed_at = ?, title = ? WHERE id = ?`, noon, "ago "+fmt.Sprint(ago), id); err != nil {
			t.Fatal(err)
		}
	}
	var rv struct {
		Completed []struct{ Title string } `json:"completed_last_week"`
	}
	decode(t, do(h, req("GET", "/api/review", "")), &rv)
	var titles []string
	for _, c := range rv.Completed {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, ",") != "ago 1,ago 7" {
		t.Errorf("completed_last_week = %v, want ago 1 and ago 7", titles)
	}

	page := do(h, req("GET", "/gtd/review", "")).Body.String()
	links := regexp.MustCompile(`<a href="/gtd/day/(\d{4}-\d\d-\d\d)">`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range links {
		got = append(got, m[1])
	}
	var want []string
	for i := 7; i >= 1; i-- {
		want = append(want, today.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("day links = %v, want %v", got, want)
	}
}
