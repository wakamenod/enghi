package web_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/config"
	"github.com/wakamenod/enghi/internal/gtd"
)

// Every screen must render to the end.
// **HTTP 200 alone is not enough.** A template error at run time stops partway
// through the body, after the head has been written, so broken HTML comes back
// with a 200.
func TestAllScreensRenderCompletely(t *testing.T) {
	h := newServer(t)
	enableFeatures(t, h)

	// Give every screen something to show, so nothing passes only when empty
	mustJSON(t, h, "POST", "/api/pages", `{"title":"参考資料","body":"本文"}`)
	mustJSON(t, h, "POST", "/api/contexts", `{"name":"@電話"}`)
	mustJSON(t, h, "POST", "/api/areas", `{"name":"経理"}`)
	mustJSON(t, h, "POST", "/api/projects", `{"title":"オフィス移転","outcome":"移転完了","area_id":1}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"不動産屋に電話する"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1", `{"state":"next","project_id":1,"context_id":1}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"ゴミ出し"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/2",
		`{"state":"scheduled","scheduled_on":"2020-01-01","recurrence":"weekly:tue,fri"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"他の人の返事待ち"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/3", `{"state":"waiting","waiting_for":"田中さん"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"いつかやる"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/4", `{"state":"someday"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"Inbox に残すもの"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"start"}`)
	mustJSON(t, h, "POST", "/api/tasks/5/logs", `{"body":"**調べた**こと [[参考資料]]"}`)
	mustJSON(t, h, "POST", "/api/tasks/5/logs", `{"kind":"start","body":"着手"}`)
	addDeadlines(t, h)

	screens := []string{
		"/", "/wiki", "/wiki/参考資料", "/wiki/参考資料/edit", "/wiki/参考資料/history",
		"/wiki/new", "/tags", "/search?q=参考",
		"/gtd", "/gtd/inbox", "/gtd/next", "/gtd/next?context=@電話",
		"/gtd/waiting", "/gtd/scheduled", "/gtd/someday",
		"/gtd/projects", "/gtd/projects?status=active", "/gtd/project/1",
		"/gtd/areas", "/gtd/area/1", "/gtd/review", "/gtd/clarify/5",
		"/gtd/day", "/gtd/day/2020-01-01", "/gtd/day?date=2020-02-29",
		"/guide", "/guide/gtd", "/guide/enghi", "/settings",
	}
	for _, path := range screens {
		w := do(h, req("GET", path, ""))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s → %d", path, w.Code)
			continue
		}
		body := w.Body.Bytes()
		if !bytes.Contains(body, []byte("</html>")) {
			// A template error is mixed in partway through, so the tail shows the
			// cause
			tail := string(body)
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			t.Errorf("GET %s: the HTML is cut off. Tail:\n%s", path, tail)
		}
		if bytes.Contains(body, []byte("can't evaluate field")) ||
			bytes.Contains(body, []byte("no such template")) {
			t.Errorf("GET %s: a template error is printed in the body", path)
		}
	}
}

// 2.6: completing generates the next instance as scheduled, through the API.
func TestCompleteRecurringViaAPI(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"ゴミ出し"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1",
		`{"state":"scheduled","scheduled_on":"2020-01-07","recurrence":"weekly:tue,fri"}`)

	w := do(h, req("POST", "/api/tasks/1/complete", `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("complete → %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Completed map[string]any `json:"completed"`
		Next      map[string]any `json:"next"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Completed["state"] != "done" {
		t.Errorf("after completion = %v", res.Completed["state"])
	}
	if res.Next == nil {
		t.Fatal("no next instance was returned")
	}
	if res.Next["state"] != "scheduled" {
		t.Errorf("next instance = %v, want scheduled", res.Next["state"])
	}
}

// 8-13: filing as reference creates a wiki page and leaves the task filed.
func TestFileAsReferenceViaAPI(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"読むべき記事"}`)

	w := do(h, req("POST", "/api/tasks/1/file", `{"title":"記事のメモ","body":"内容","tags":["メモ"]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("file → %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Task map[string]any `json:"task"`
		Page map[string]any `json:"page"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Task["state"] != "filed" {
		t.Errorf("state = %v, want filed", res.Task["state"])
	}
	if res.Page["slug"] == nil {
		t.Error("no page was created")
	}
	// The created page must actually open
	if got := do(h, req("GET", "/wiki/"+res.Page["slug"].(string), "")).Code; got != http.StatusOK {
		t.Errorf("the generated page does not open: %d", got)
	}
}

// The dashboard returns every aggregate in one call, not N separate queries.
func TestDashboardIncludesGTD(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/projects", `{"title":"止まっているプロジェクト"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"未処理"}`)

	w := do(h, req("GET", "/api/dashboard", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard → %d", w.Code)
	}
	var d struct {
		GTD struct {
			InboxCount int              `json:"inbox_count"`
			Stalled    []map[string]any `json:"stalled_projects"`
			Contexts   []map[string]any `json:"contexts"`
			Enabled    bool             `json:"enabled"`
		} `json:"gtd"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if !d.GTD.Enabled {
		t.Error("enabled=false although there is GTD data")
	}
	if d.GTD.InboxCount != 1 {
		t.Errorf("inbox_count = %d, want 1", d.GTD.InboxCount)
	}
	if len(d.GTD.Stalled) != 1 {
		t.Errorf("stalled projects = %d, want 1", len(d.GTD.Stalled))
	}
}

// addDeadlines adds one overdue task and one with a deadline coming up, so the
// dashboard's Overdue and Upcoming groups have something to render. It returns
// their IDs.
func addDeadlines(t *testing.T, h http.Handler) (overdue, upcoming int64) {
	t.Helper()
	add := func(title string, days int) int64 {
		var tk struct {
			ID int64 `json:"id"`
		}
		w := do(h, req("POST", "/api/tasks", `{"title":"`+title+`"}`))
		json.Unmarshal(w.Body.Bytes(), &tk)
		mustJSON(t, h, "PATCH", fmt.Sprintf("/api/tasks/%d", tk.ID),
			fmt.Sprintf(`{"state":"next","deadline_on":%q}`, gtd.FormatDate(gtd.Today().AddDate(0, 0, days))))
		return tk.ID
	}
	return add("Past deadline", -2), add("Deadline ahead", 3)
}

// Overdue deadlines are flagged within Today, and deadlines within
// deadline_warning_days come back as upcoming.
func TestDashboardDeadlines(t *testing.T) {
	h, _ := newServerWith(t, func(c *config.Config) { c.DeadlineWarningDays = 3 })
	overdue, upcoming := addDeadlines(t, h)
	var d struct {
		GTD struct {
			Today []struct {
				ID           int64 `json:"id"`
				DeadlineDays *int  `json:"deadline_days"`
			} `json:"today"`
			Upcoming []struct {
				ID           int64 `json:"id"`
				DeadlineDays *int  `json:"deadline_days"`
			} `json:"upcoming"`
			WarningDays int `json:"deadline_warning_days"`
		} `json:"gtd"`
	}
	if err := json.Unmarshal(do(h, req("GET", "/api/dashboard", "")).Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	g := d.GTD
	if len(g.Today) != 1 || g.Today[0].ID != overdue || g.Today[0].DeadlineDays == nil || *g.Today[0].DeadlineDays != -2 {
		t.Errorf("today = %+v, want the overdue task with deadline_days -2", g.Today)
	}
	if len(g.Upcoming) != 1 || g.Upcoming[0].ID != upcoming || *g.Upcoming[0].DeadlineDays != 3 {
		t.Errorf("upcoming = %+v, want the task due in 3 days", g.Upcoming)
	}
	if g.WarningDays != 3 {
		t.Errorf("deadline_warning_days = %d, want 3", g.WarningDays)
	}

	r := req("GET", "/", "")
	r.Header.Set("Accept-Language", "en")
	body := do(h, r).Body.String()
	for _, want := range []string{"Overdue", "2 day(s) overdue", "Deadlines in the next 3 day(s)", "in 3 days"} {
		if !strings.Contains(body, want) {
			t.Errorf("the dashboard does not show %q", want)
		}
	}
}

// Without GTD in use at all, the wiki still works fully and the screens hold
// up (DESIGN 0).
func TestWikiWorksWithoutGTD(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/pages", `{"title":"記事","body":"本文"}`)

	w := do(h, req("GET", "/", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
		t.Fatalf("dashboard -> %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "まだ何もありません") {
		t.Error("the GTD area does not say that it is empty")
	}
	var d struct {
		GTD struct {
			Enabled bool `json:"enabled"`
		} `json:"gtd"`
	}
	json.Unmarshal(do(h, req("GET", "/api/dashboard", "")).Body.Bytes(), &d)
	if d.GTD.Enabled {
		t.Error("enabled=true although there is no GTD data")
	}
}

func mustJSON(t *testing.T, h http.Handler, method, path, body string) {
	t.Helper()
	w := do(h, req(method, path, body))
	if w.Code >= 400 {
		t.Fatalf("%s %s → %d: %s", method, path, w.Code, w.Body.String())
	}
}

// In English too, every screen renders to the end with no Japanese left on it.
func TestEnglishScreensHaveNoJapanese(t *testing.T) {
	h := newServer(t)
	enableFeatures(t, h)
	mustJSON(t, h, "POST", "/api/pages", `{"title":"Article","body":"body"}`)
	mustJSON(t, h, "POST", "/api/contexts", `{"name":"@phone"}`)
	mustJSON(t, h, "POST", "/api/areas", `{"name":"Finances"}`)
	mustJSON(t, h, "POST", "/api/projects", `{"title":"Office move","outcome":"Moved in"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"Call the agent"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"body":"Tried the *office* line first"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"start"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"pause","body":"lunch"}`)
	addDeadlines(t, h)

	japanese := regexp.MustCompile(`[ぁ-んァ-ヶ一-龠]`)
	screens := []string{
		"/", "/wiki", "/wiki/article", "/wiki/article/edit", "/wiki/article/history",
		"/wiki/new", "/tags", "/search?q=Article", "/gtd", "/gtd/inbox", "/gtd/next",
		"/gtd/waiting", "/gtd/scheduled", "/gtd/someday", "/gtd/projects",
		"/gtd/project/1", "/gtd/areas", "/gtd/area/1", "/gtd/review", "/gtd/clarify/1",
		"/search?q=office", "/gtd/day", "/gtd/day/2020-01-01",
	}
	for _, path := range screens {
		r := req("GET", path, "")
		r.Header.Set("Accept-Language", "en-US,en;q=0.9")
		w := do(h, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s → %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if !strings.Contains(body, "</html>") {
			t.Errorf("GET %s: the HTML is cut off", path)
			continue
		}
		// The language switcher prints the other language's name in that
		// language, so it is excluded
		body = regexp.MustCompile(`(?s)<div class="lang-switch".*?</div>`).ReplaceAllString(body, "")
		if m := japanese.FindString(body); m != "" {
			// Japanese inside an article is normal, so only the chrome is checked
			idx := japanese.FindStringIndex(body)
			from := idx[0] - 60
			if from < 0 {
				from = 0
			}
			t.Errorf("GET %s: Japanese left in the English rendering: ...%s...", path, body[from:idx[1]+20])
		}
	}
}

// The language comes from Accept-Language and the cookie, the cookie winning.
func TestLanguageSelection(t *testing.T) {
	h := newServer(t)

	r := req("GET", "/", "")
	r.Header.Set("Accept-Language", "en")
	if body := do(h, r).Body.String(); !strings.Contains(body, "Dashboard") {
		t.Error("Accept-Language: en had no effect")
	}

	r = req("GET", "/", "")
	r.Header.Set("Accept-Language", "ja")
	if body := do(h, r).Body.String(); !strings.Contains(body, "ダッシュボード") {
		t.Error("Accept-Language: ja had no effect")
	}

	// The explicit choice in the cookie outranks Accept-Language
	r = req("GET", "/", "")
	r.Header.Set("Accept-Language", "ja")
	r.AddCookie(&http.Cookie{Name: "enghi-lang", Value: "en"})
	if body := do(h, r).Body.String(); !strings.Contains(body, "Dashboard") {
		t.Error("the cookie choice did not outrank Accept-Language")
	}

	// The switcher itself
	w := do(h, req("GET", "/ui/lang?set=en&return_to=/wiki", ""))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("language switch -> %d", w.Code)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "enghi-lang" && c.Value == "en" {
			found = true
		}
	}
	if !found {
		t.Error("no cookie was set")
	}
	if loc := w.Header().Get("Location"); loc != "/wiki" {
		t.Errorf("redirect target = %q", loc)
	}
}

// Error messages follow the language too.
func TestErrorMessagesAreLocalized(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/pages", `{"title":"Conflict","body":"x"}`)

	r := req("PUT", "/api/pages/conflict", `{"title":"Conflict","body":"y","version":99}`)
	r.Header.Set("Accept-Language", "en")
	w := do(h, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("→ %d", w.Code)
	}
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	msg, _ := res["message"].(string)
	if regexp.MustCompile(`[ぁ-んァ-ヶ一-龠]`).MatchString(msg) {
		t.Errorf("Japanese where English was expected: %q", msg)
	}
}

// enableFeatures turns contexts and areas on.
// **They are off by default, so a screen test that skips this sees a 404.**
func enableFeatures(t *testing.T, h http.Handler) {
	t.Helper()
	form := strings.NewReader("gtd.contexts=1&gtd.areas=1")
	r := httptest.NewRequest("POST", "/ui/settings", form)
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if got := do(h, r).Code; got != http.StatusSeeOther {
		t.Fatalf("POST /ui/settings → %d", got)
	}
}

// Task rows carry what the move modal needs to prefill its second step, and a
// Details link to Clarify beside the title.
func TestTaskRowsCarryMoveData(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/projects", `{"title":"オフィス移転","outcome":"移転完了"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"返事待ち"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1",
		`{"state":"waiting","waiting_for":"田中さん","project_id":1}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"ゴミ出し"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/2",
		`{"state":"scheduled","scheduled_on":"2099-01-06","recurrence":"weekly:tue","recurrence_ends_on":"2099-12-31"}`)

	body := do(h, req("GET", "/gtd/scheduled", "")).Body.String()
	for _, want := range []string{
		`data-recurrence="weekly:tue"`, `data-recurrence-ends-on="2099-12-31"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the scheduled row lacks %s", want)
		}
	}

	body = do(h, req("GET", "/gtd/waiting", "")).Body.String()
	for _, want := range []string{
		`data-task-id="1"`, `data-state="waiting"`, `data-title="返事待ち"`,
		`data-project-id="1"`, `data-project-title="オフィス移転"`, `data-context-id=""`,
		`data-waiting-for="田中さん"`, `data-scheduled-on=""`, `data-recurrence=""`,
		`data-delegated-at="` + gtd.FormatDate(gtd.Today()) + `"`, `data-version="2"`,
		`<a class="task-title" href="/gtd/clarify/1">`,
		`<a class="row-detail" href="/gtd/clarify/1">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the waiting row lacks %s", want)
		}
	}
}

// The move modal submits ordinary form posts to POST /ui/tasks/{id}.
func TestMoveTaskByFormPost(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"動かすもの"}`)

	form := func(body string) int {
		r := httptest.NewRequest("POST", "/ui/tasks/1", strings.NewReader(body+"&return_to=/gtd/inbox"))
		r.Host = "127.0.0.1:7777"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		return do(h, r).Code
	}
	state := func() map[string]any {
		var got struct {
			Task map[string]any `json:"task"`
		}
		w := do(h, req("GET", "/api/tasks/1", ""))
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET /api/tasks/1: %v", err)
		}
		return got.Task
	}

	if got := form("state=waiting&waiting_for=X"); got != http.StatusSeeOther {
		t.Fatalf("state=waiting → %d", got)
	}
	if got := state(); got["state"] != "waiting" || got["waiting_for"] != "X" {
		t.Errorf("after waiting: %v", got)
	}

	// The date is required; the task must stay as it was
	if got := form("state=scheduled"); got != http.StatusBadRequest {
		t.Errorf("state=scheduled without a date → %d, want 400", got)
	}
	if got := state(); got["state"] != "waiting" {
		t.Errorf("a rejected move changed the state to %v", got["state"])
	}

	// Drop keeps the row (state=dropped), unlike /delete
	if got := form("state=dropped"); got != http.StatusSeeOther {
		t.Fatalf("state=dropped → %d", got)
	}
	if got := state(); got["state"] != "dropped" {
		t.Errorf("after drop: %v", got["state"])
	}
}

// Undo posts the previous state with the version it saw after the move. An
// edit made elsewhere in between makes it a conflict that changes nothing.
func TestUndoByFormPostChecksVersion(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"うっかり動かした"}`)

	form := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/ui/tasks/1", strings.NewReader(body+"&return_to=/gtd/inbox"))
		r.Host = "127.0.0.1:7777"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		return do(h, r)
	}
	task := func() map[string]any {
		var got struct {
			Task map[string]any `json:"task"`
		}
		w := do(h, req("GET", "/api/tasks/1", ""))
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET /api/tasks/1: %v", err)
		}
		return got.Task
	}

	// version 1 → the move makes it 2
	if w := form("state=someday"); w.Code != http.StatusSeeOther {
		t.Fatalf("move → %d", w.Code)
	}
	// Edited elsewhere: version 3
	mustJSON(t, h, "PATCH", "/api/tasks/1", `{"title":"Emacs で直した"}`)

	w := form("state=inbox&scheduled_on=&waiting_for=&project_id=&context_id=&recurrence=&recurrence_ends_on=&delegated_at=&version=2")
	if w.Code != http.StatusConflict {
		t.Fatalf("stale undo → %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "別の場所で更新") {
		t.Errorf("the conflict message is not the localized one: %q", w.Body.String())
	}
	if got := task(); got["state"] != "someday" || got["title"] != "Emacs で直した" {
		t.Errorf("a rejected undo changed the task: %v", got)
	}

	// With the current version it goes through
	if w := form("state=inbox&version=3"); w.Code != http.StatusSeeOther {
		t.Fatalf("undo → %d", w.Code)
	}
	if got := task(); got["state"] != "inbox" {
		t.Errorf("after undo: %v", got["state"])
	}
}

// The Scheduled step of the move modal also sends a recurrence rule. The task
// waits in Scheduled, and completing it adds the next scheduled instance.
func TestScheduleRecurringByFormPost(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"ゴミ出し"}`)

	post := func(path, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body+"&return_to=/gtd/inbox"))
		r.Host = "127.0.0.1:7777"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		return do(h, r).Code
	}
	task := func(id string) map[string]any {
		var got struct {
			Task map[string]any `json:"task"`
		}
		w := do(h, req("GET", "/api/tasks/"+id, ""))
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET /api/tasks/%s: %v", id, err)
		}
		return got.Task
	}

	// 2099-01-06 is a Tuesday
	if got := post("/ui/tasks/1",
		"state=scheduled&scheduled_on=2099-01-06&recurrence=weekly:tue&recurrence_ends_on=2099-12-31"); got != http.StatusSeeOther {
		t.Fatalf("scheduled with a rule → %d", got)
	}
	got := task("1")
	if got["state"] != "scheduled" || got["recurrence"] != "weekly:tue" ||
		got["recurrence_ends_on"] != "2099-12-31" {
		t.Errorf("after the move: %v", got)
	}
	if strings.Contains(do(h, req("GET", "/gtd/next", "")).Body.String(), "ゴミ出し") {
		t.Error("a task scheduled for 2099 is already in Next Actions")
	}

	// A bad rule is rejected by the server and changes nothing
	if code := post("/ui/tasks/1", "state=scheduled&scheduled_on=2099-01-06&recurrence=weekly:"); code != http.StatusBadRequest {
		t.Errorf("a bad rule → %d, want 400", code)
	}

	if code := post("/ui/tasks/1/complete", "x=1"); code != http.StatusSeeOther {
		t.Fatalf("complete → %d", code)
	}
	if got := task("1"); got["state"] != "done" {
		t.Errorf("after completion: %v", got["state"])
	}
	next := task("2")
	if next["state"] != "scheduled" || next["scheduled_on"] != "2099-01-13" ||
		next["recurrence"] != "weekly:tue" {
		t.Errorf("next instance: %v", next)
	}

	// An empty rule ("Does not repeat") clears it
	if code := post("/ui/tasks/2", "state=scheduled&scheduled_on=2099-01-13&recurrence=&recurrence_ends_on="); code != http.StatusSeeOther {
		t.Fatalf("clearing the rule → %d", code)
	}
	if got := task("2"); got["recurrence"] != nil || got["recurrence_ends_on"] != nil {
		t.Errorf("after clearing: %v", got)
	}
}

// Clarify keeps the rule in a text field (for no-JS) and carries its end date,
// both of which the picker reads and writes back.
func TestClarifyCarriesRecurrenceFields(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"経費精算"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1",
		`{"state":"scheduled","scheduled_on":"2099-01-25","recurrence":"monthly:25","recurrence_ends_on":"2099-12-31"}`)

	body := do(h, req("GET", "/gtd/clarify/1", "")).Body.String()
	for _, want := range []string{
		`id="recurrence" name="recurrence" value="monthly:25"`,
		`id="recurrence_ends_on" name="recurrence_ends_on" value="2099-12-31"`,
		`id="scheduled_on"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("clarify lacks %s", want)
		}
	}
}
