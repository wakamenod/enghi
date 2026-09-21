package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// すべての画面が最後まで描画されること。
// **HTTP 200 だけでは足りない。** テンプレートの実行時エラーは、
// head を書き出した後に body の途中で止まるため 200 のまま壊れた HTML が返る。
func TestAllScreensRenderCompletely(t *testing.T) {
	h := newServer(t)

	// 各画面に中身がある状態を作る(空のときだけ通る、という取りこぼしを避ける)
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

	screens := []string{
		"/", "/wiki", "/wiki/参考資料", "/wiki/参考資料/edit", "/wiki/参考資料/history",
		"/wiki/new", "/tags", "/search?q=参考",
		"/gtd", "/gtd/inbox", "/gtd/next", "/gtd/next?context=@電話",
		"/gtd/waiting", "/gtd/scheduled", "/gtd/someday",
		"/gtd/projects", "/gtd/projects?status=active", "/gtd/project/1",
		"/gtd/areas", "/gtd/area/1", "/gtd/review", "/gtd/clarify/5",
	}
	for _, path := range screens {
		w := do(h, req("GET", path, ""))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s → %d", path, w.Code)
			continue
		}
		body := w.Body.Bytes()
		if !bytes.Contains(body, []byte("</html>")) {
			// テンプレートのエラーは途中で混ざるので、末尾を出すと原因が分かる
			tail := string(body)
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			t.Errorf("GET %s: HTML が途中で切れている。末尾:\n%s", path, tail)
		}
		if bytes.Contains(body, []byte("can't evaluate field")) ||
			bytes.Contains(body, []byte("no such template")) {
			t.Errorf("GET %s: テンプレートのエラーが本文に出ている", path)
		}
	}
}

// 2.6: 完了すると次の1件が scheduled で生成される(API 経由)。
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
		t.Errorf("完了後 = %v", res.Completed["state"])
	}
	if res.Next == nil {
		t.Fatal("次インスタンスが返っていない")
	}
	if res.Next["state"] != "scheduled" {
		t.Errorf("次インスタンス = %v, want scheduled", res.Next["state"])
	}
}

// 8-13: 資料化すると Wiki ページができ、タスクは filed になる。
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
		t.Error("ページが作られていない")
	}
	// 作られたページが実際に開けること
	if got := do(h, req("GET", "/wiki/"+res.Page["slug"].(string), "")).Code; got != http.StatusOK {
		t.Errorf("生成されたページが開けない: %d", got)
	}
}

// ダッシュボードは必要な集計を1発で返す(個別に N 本投げない)。
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
		t.Error("GTD のデータがあるのに enabled=false")
	}
	if d.GTD.InboxCount != 1 {
		t.Errorf("inbox_count = %d, want 1", d.GTD.InboxCount)
	}
	if len(d.GTD.Stalled) != 1 {
		t.Errorf("停滞プロジェクト = %d 件, want 1", len(d.GTD.Stalled))
	}
}

// GTD を一切使っていなくても Wiki は完全に機能し、画面も崩れない(DESIGN 0)。
func TestWikiWorksWithoutGTD(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/pages", `{"title":"記事","body":"本文"}`)

	w := do(h, req("GET", "/", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
		t.Fatalf("ダッシュボード → %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "まだ何もありません") {
		t.Error("GTD 領域が空であることが示されていない")
	}
	var d struct {
		GTD struct {
			Enabled bool `json:"enabled"`
		} `json:"gtd"`
	}
	json.Unmarshal(do(h, req("GET", "/api/dashboard", "")).Body.Bytes(), &d)
	if d.GTD.Enabled {
		t.Error("GTD のデータが無いのに enabled=true")
	}
}

func mustJSON(t *testing.T, h http.Handler, method, path, body string) {
	t.Helper()
	w := do(h, req(method, path, body))
	if w.Code >= 400 {
		t.Fatalf("%s %s → %d: %s", method, path, w.Code, w.Body.String())
	}
}
