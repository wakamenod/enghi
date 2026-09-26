package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postForm submits a form the way the browser does from a screen.
func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return do(h, r)
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
}

// Clarify shows the log oldest first, rendered like an article, each entry
// with an anchor to land on.
func TestClarifyRendersWorkLog(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/pages", `{"title":"設計メモ","body":"x"}`)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"キャッシュを直す"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"start"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs",
		`{"body":"**原因**は [[設計メモ]] の通り\n\n`+"```go\\nfmt.Println(1)\\n```"+`\n\n<script>alert(1)</script>"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"kind":"pause","body":"昼休み"}`)

	w := do(h, req("GET", "/gtd/clarify/1", ""))
	body := w.Body.String()
	for _, want := range []string{
		`id="log"`, `id="log-1"`, `id="log-2"`, `id="log-3"`,
		`<strong>原因</strong>`, `href="/wiki/`, `<code class="language-go">`,
		`▶ 開始`, `⏸ 中断`, `昼休み`,
		`action="/ui/task-logs/2"`, `name="version" value="1"`,
		`action="/ui/tasks/1/logs"`, `formaction="/ui/tasks/1/start"`,
		`data-paste-upload`, `data-ctrl-enter`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Clarify lacks %s", want)
		}
	}
	if strings.Contains(body, "<script>alert(1)") {
		t.Error("a log entry is not sanitized")
	}
	if i, j := strings.Index(body, `id="log-1"`), strings.Index(body, `id="log-3"`); i > j {
		t.Error("the log is not oldest first")
	}
	// Only one textarea used to get the upload handler; the file-as-reference
	// body keeps it now that it is bound per textarea
	if n := strings.Count(body, "data-paste-upload"); n < 3 {
		t.Errorf("data-paste-upload on %d textareas, want the reference body, the editor and the composer", n)
	}
}

func TestWorkLogFormPosts(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"作業する"}`)
	mustJSON(t, h, "PATCH", "/api/tasks/1", `{"state":"next"}`)

	loc := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		if w.Code != http.StatusSeeOther {
			t.Fatalf("→ %d: %s", w.Code, w.Body.String())
		}
		return w.Header().Get("Location")
	}

	if got := loc(postForm(h, "/ui/tasks/1/logs", url.Values{"body": {"試したこと"}})); got != "/gtd/clarify/1#log-1" {
		t.Errorf("add → %s", got)
	}
	if w := postForm(h, "/ui/tasks/1/logs", url.Values{"body": {"  "}}); w.Code != http.StatusBadRequest {
		t.Errorf("a blank note → %d, want 400", w.Code)
	}
	if got := loc(postForm(h, "/ui/tasks/1/start", nil)); got != "/gtd/clarify/1#log-2" {
		t.Errorf("start → %s", got)
	}
	// A second start is a no-op landing on the start that is in effect
	if got := loc(postForm(h, "/ui/tasks/1/start", nil)); got != "/gtd/clarify/1#log-2" {
		t.Errorf("start again → %s", got)
	}

	// The key in a list comes back to the list
	body := do(h, req("GET", "/gtd/next", "")).Body.String()
	if !strings.Contains(body, `data-working="true"`) || !strings.Contains(body, `class="tag working"`) {
		t.Error("the row of a started task has no working marker")
	}
	if got := loc(postForm(h, "/ui/tasks/1/pause", url.Values{"return_to": {"/gtd/next"}})); got != "/gtd/next" {
		t.Errorf("pause from the list → %s", got)
	}
	body = do(h, req("GET", "/gtd/next", "")).Body.String()
	if !strings.Contains(body, `data-working="false"`) || strings.Contains(body, `class="tag working"`) {
		t.Error("a paused task still shows as working")
	}

	// Edit posts the version it was shown; a stale one changes nothing
	if got := loc(postForm(h, "/ui/task-logs/1", url.Values{"body": {"書き直した"}, "version": {"1"}})); got != "/gtd/clarify/1#log-1" {
		t.Errorf("edit → %s", got)
	}
	if w := postForm(h, "/ui/task-logs/1", url.Values{"body": {"古い画面から"}, "version": {"1"}}); w.Code != http.StatusConflict {
		t.Errorf("stale edit → %d, want 409", w.Code)
	}
	if !strings.Contains(do(h, req("GET", "/gtd/clarify/1", "")).Body.String(), "書き直した") {
		t.Error("the edit is not shown")
	}

	if got := loc(postForm(h, "/ui/task-logs/1/delete", nil)); got != "/gtd/clarify/1#log" {
		t.Errorf("delete → %s", got)
	}
	if w := postForm(h, "/ui/task-logs/1/delete", nil); w.Code != http.StatusNotFound {
		t.Errorf("deleting again → %d, want 404", w.Code)
	}
	if w := postForm(h, "/ui/tasks/99/logs", url.Values{"body": {"x"}}); w.Code != http.StatusNotFound {
		t.Errorf("a missing task → %d, want 404", w.Code)
	}
}

func TestWorkLogAPI(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"API から記録する"}`)

	w := do(h, req("POST", "/api/tasks/1/logs", `{"body":"メモ"}`))
	var add struct {
		Log     map[string]any `json:"log"`
		Created bool           `json:"created"`
		Working bool           `json:"working"`
	}
	decode(t, w, &add)
	if w.Code != http.StatusCreated || !add.Created || add.Log["kind"] != "note" || add.Working {
		t.Errorf("POST note → %d %+v", w.Code, add)
	}

	w = do(h, req("POST", "/api/tasks/1/logs", `{"kind":"start"}`))
	decode(t, w, &add)
	if w.Code != http.StatusCreated || !add.Working {
		t.Errorf("POST start → %d %+v", w.Code, add)
	}
	w = do(h, req("POST", "/api/tasks/1/logs", `{"kind":"start"}`))
	decode(t, w, &add)
	if w.Code != http.StatusOK || add.Created || add.Log["kind"] != "start" {
		t.Errorf("POST start again → %d %+v, want 200 with created=false", w.Code, add)
	}

	var task struct {
		Task map[string]any `json:"task"`
	}
	decode(t, do(h, req("GET", "/api/tasks/1", "")), &task)
	if task.Task["working"] != true {
		t.Errorf("task JSON: working = %v", task.Task["working"])
	}

	var list struct {
		Working bool             `json:"working"`
		Logs    []map[string]any `json:"logs"`
	}
	decode(t, do(h, req("GET", "/api/tasks/1/logs", "")), &list)
	if !list.Working || len(list.Logs) != 2 || list.Logs[0]["body"] != "メモ" {
		t.Errorf("GET logs = %+v", list)
	}

	if w := do(h, req("PATCH", "/api/task-logs/1", `{"body":"x"}`)); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "version_required") {
		t.Errorf("PATCH without version → %d %s", w.Code, w.Body.String())
	}
	if w := do(h, req("PATCH", "/api/task-logs/1", `{"body":"直した","version":1}`)); w.Code != http.StatusOK {
		t.Errorf("PATCH → %d %s", w.Code, w.Body.String())
	}
	w = do(h, req("PATCH", "/api/task-logs/1", `{"body":"古い","version":1}`))
	var conflict struct {
		Error   string         `json:"error"`
		Current map[string]any `json:"current"`
	}
	decode(t, w, &conflict)
	if w.Code != http.StatusConflict || conflict.Error != "version_conflict" || conflict.Current["body"] != "直した" {
		t.Errorf("stale PATCH → %d %+v", w.Code, conflict)
	}

	// Writes without a JSON Content-Type are refused, as for every API write
	r := httptest.NewRequest("POST", "/api/tasks/1/logs", strings.NewReader(`{"body":"x"}`))
	r.Host = "127.0.0.1:7777"
	if w := do(h, r); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("POST without Content-Type → %d, want 415", w.Code)
	}

	if w := do(h, req("DELETE", "/api/task-logs/1", "")); w.Code != http.StatusOK {
		t.Errorf("DELETE → %d", w.Code)
	}
	if w := do(h, req("DELETE", "/api/task-logs/1", "")); w.Code != http.StatusNotFound {
		t.Errorf("DELETE again → %d, want 404", w.Code)
	}
	if w := do(h, req("GET", "/api/tasks/99/logs", "")); w.Code != http.StatusNotFound {
		t.Errorf("GET logs of a missing task → %d, want 404", w.Code)
	}
}

// A log hit links to its entry on Clarify, and kind=log narrows the API.
func TestSearchFindsLogEntries(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"ビルドを直す"}`)
	mustJSON(t, h, "POST", "/api/tasks/1/logs", `{"body":"リンカのフラグを見直した"}`)

	body := do(h, req("GET", "/search?q="+url.QueryEscape("フラグを見直"), "")).Body.String()
	if !strings.Contains(body, `href="/gtd/clarify/1#log-1"`) || !strings.Contains(body, `badge log`) {
		t.Errorf("the search screen does not link the log entry:\n%s", body)
	}

	var res struct {
		Results []map[string]any `json:"results"`
	}
	decode(t, do(h, req("GET", "/api/search?kind=log&q="+url.QueryEscape("リンカ"), "")), &res)
	if len(res.Results) != 1 || res.Results[0]["kind"] != "log" || res.Results[0]["task_id"] != float64(1) {
		t.Errorf("GET /api/search?kind=log = %+v", res.Results)
	}
}
