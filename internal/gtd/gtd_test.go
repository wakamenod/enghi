package gtd_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

func newSvc(t *testing.T) (*gtd.Service, *wiki.Service, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return gtd.New(db), wiki.New(db, 10), db
}

func capture(t *testing.T, s *gtd.Service, title string) *gtd.Task {
	t.Helper()
	tk, err := s.Capture(context.Background(), gtd.CaptureInput{Title: title})
	if err != nil {
		t.Fatalf("Capture(%q): %v", title, err)
	}
	return tk
}

func patch(t *testing.T, s *gtd.Service, id int64, p gtd.TaskPatch) *gtd.Task {
	t.Helper()
	tk, err := s.Patch(context.Background(), id, p)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	return tk
}

func str(s string) *string { return &s }

// 4.2: capture は {title} だけで作れること。state は inbox。
func TestCaptureNeedsOnlyTitle(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "何か思いついた")
	if tk.State != gtd.StateInbox {
		t.Fatalf("state = %q, want inbox", tk.State)
	}
	inbox, _ := s.Inbox(context.Background())
	if len(inbox) != 1 {
		t.Fatalf("inbox = %d 件", len(inbox))
	}
}

// 2.6: Next Actions の抽出条件は
// state='next' OR (state='scheduled' AND scheduled_on <= today)。
// **ビューの条件で表現し、state を書き換えるバッチ処理は作らない。**
func TestScheduledTaskAppearsInNextActionsWhenDue(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	past := capture(t, s, "昨日やるはずだった")
	patch(t, s, past.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today().AddDate(0, 0, -1)))})

	future := capture(t, s, "来週やる")
	patch(t, s, future.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today().AddDate(0, 0, 7)))})

	today := capture(t, s, "今日やる")
	patch(t, s, today.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today()))})

	next, err := s.NextActions(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tk := range next {
		got[tk.Title] = true
	}
	if !got["昨日やるはずだった"] {
		t.Error("期限を過ぎた scheduled が Next Actions に出ていない(見なかった日に消える)")
	}
	if !got["今日やる"] {
		t.Error("今日の scheduled が Next Actions に出ていない")
	}
	if got["来週やる"] {
		t.Error("未来の scheduled が Next Actions に出てしまっている")
	}
	// state は書き換えられていないこと
	again, _ := s.Task(ctx, past.ID)
	if again.State != gtd.StateScheduled {
		t.Errorf("state が書き換えられている: %q(バッチ処理を作ってはいけない)", again.State)
	}
}

// 2.4: Next Action が1つも無いアクティブなプロジェクトの検出。
// **これがシステムの価値の半分を担う。**
func TestStalledProjectDetection(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	stalled, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "止まっている"})
	healthy, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "動いている"})
	someday, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "いつか", Status: "someday"})

	// 止まっているプロジェクトには later のタスクしかない(Next ではない)
	t1 := capture(t, s, "後でやる")
	patch(t, s, t1.ID, gtd.TaskPatch{State: str(gtd.StateLater), ProjectID: &stalled.ID})
	// 動いているプロジェクトには next がある
	t2 := capture(t, s, "次の行動")
	patch(t, s, t2.ID, gtd.TaskPatch{State: str(gtd.StateNext), ProjectID: &healthy.ID})
	// someday には何も無いが、active ではないので検出対象外
	_ = someday

	got, err := s.StalledProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != stalled.ID {
		titles := []string{}
		for _, p := range got {
			titles = append(titles, p.Title)
		}
		t.Fatalf("停滞プロジェクト = %v, want [止まっている]", titles)
	}

	// waiting も scheduled も「次の行動がある」とみなす
	patch(t, s, t1.ID, gtd.TaskPatch{State: str(gtd.StateWaiting), WaitingFor: str("誰か")})
	got, _ = s.StalledProjects(ctx)
	if len(got) != 0 {
		t.Fatalf("waiting があるのに停滞扱い: %d 件", len(got))
	}
}

// 2.6: 完了を契機に次の1件だけを生成する。
// **生成される次インスタンスの state は必ず scheduled。next で作ってはいけない。**
func TestRecurringTaskGeneratesNextAsScheduled(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "ゴミ出し")
	tk = patch(t, s, tk.ID, gtd.TaskPatch{
		State:       str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today())),
		Recurrence:  str("weekly:tue,fri"),
	})

	res, err := s.Complete(ctx, tk.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Completed.State != gtd.StateDone {
		t.Errorf("現インスタンス = %q, want done", res.Completed.State)
	}
	if res.Next == nil {
		t.Fatal("次インスタンスが生成されていない")
	}
	if res.Next.State != gtd.StateScheduled {
		t.Fatalf("次インスタンス = %q, want scheduled(next で作ると Next Actions に居座る)", res.Next.State)
	}
	if res.Next.ScheduledOn == "" {
		t.Error("次インスタンスに scheduled_on が無い")
	}
	if res.Next.Recurrence != "weekly:tue,fri" {
		t.Errorf("recurrence が引き継がれていない: %q", res.Next.Recurrence)
	}
	// 系列で辿れること
	if res.Next.SeriesID == nil || *res.Next.SeriesID != tk.ID {
		t.Errorf("series_id = %v, want %d", res.Next.SeriesID, tk.ID)
	}
}

// **同じ系列で開いているインスタンスは常に高々1件**(先回り生成をしない)。
func TestRecurringKeepsAtMostOneOpenInstance(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "毎日の記録")
	tk = patch(t, s, tk.ID, gtd.TaskPatch{
		State:       str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today())),
		Recurrence:  str("+1d"),
	})

	cur := tk.ID
	for i := 0; i < 5; i++ {
		res, err := s.Complete(ctx, cur, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Next == nil {
			t.Fatalf("%d 回目で次が生成されなかった", i+1)
		}
		cur = res.Next.ID

		var open int
		if err := db.QueryRow(
			`SELECT count(*) FROM tasks WHERE state NOT IN ('done','dropped','filed')`).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open != 1 {
			t.Fatalf("%d 回目: 開いているインスタンスが %d 件(常に1件であること)", i+1, open)
		}
	}
}

// skip は専用の state を設けず、dropped にして次を生成する。
func TestSkipDropsAndGeneratesNext(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "週次の掃除")
	tk = patch(t, s, tk.ID, gtd.TaskPatch{
		State: str(gtd.StateScheduled), ScheduledOn: str(gtd.FormatDate(gtd.Today())),
		Recurrence: str("+1w")})

	res, err := s.Complete(ctx, tk.ID, true) // skip
	if err != nil {
		t.Fatal(err)
	}
	if res.Completed.State != gtd.StateDropped {
		t.Errorf("skip 後 = %q, want dropped", res.Completed.State)
	}
	if res.Next == nil {
		t.Fatal("skip でも次が生成されるはず")
	}
}

// 系列を終わらせたい場合は先に recurrence を NULL にする。
func TestEndSeriesStopsGeneration(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "もうやらない")
	tk = patch(t, s, tk.ID, gtd.TaskPatch{
		State: str(gtd.StateScheduled), ScheduledOn: str(gtd.FormatDate(gtd.Today())),
		Recurrence: str("+1w")})

	if _, err := s.EndSeries(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.Complete(ctx, tk.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Next != nil {
		t.Fatal("系列を終わらせたのに次が生成された")
	}
}

// recurrence_ends_on を過ぎたら生成しない。
func TestRecurrenceEndsOn(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "期限付きの定期")
	yesterday := gtd.FormatDate(gtd.Today().AddDate(0, 0, -1))
	tk = patch(t, s, tk.ID, gtd.TaskPatch{
		State: str(gtd.StateScheduled), ScheduledOn: str(gtd.FormatDate(gtd.Today())),
		Recurrence: str("+1w"), RecurrenceEndsOn: str(yesterday)})

	res, err := s.Complete(ctx, tk.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Next != nil {
		t.Fatalf("終了日を過ぎているのに生成された: %s", res.Next.ScheduledOn)
	}
}

// 8-13: 「資料」と判断したら Wiki ページを生成し、元タスクを filed にする。
// **done にも dropped にもしないこと。**
func TestFileAsReferenceUsesFiledState(t *testing.T) {
	s, pages, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "参考になる記事のURL")
	got, page, err := s.FileAsReference(ctx, pages, tk.ID, gtd.FileAsReferenceInput{
		Title: "参考資料", Body: "内容", Tags: []string{"メモ"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != gtd.StateFiled {
		t.Fatalf("state = %q, want filed(done でも dropped でもない)", got.State)
	}
	if page.Title != "参考資料" {
		t.Fatalf("ページ = %q", page.Title)
	}
	// links で繋がっていること
	links, err := s.LinkedPages(ctx, "task", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !links[0].Resolved || *links[0].PageID != page.ID {
		t.Fatalf("links で繋がっていない: %+v", links)
	}
	// filed は Inbox にも Next にも出ない
	inbox, _ := s.Inbox(ctx)
	if len(inbox) != 0 {
		t.Errorf("filed が Inbox に残っている")
	}
}

// waiting にしたら委譲日が自動で入る(経過日数の警告に使う)。
func TestWaitingGetsDelegatedAt(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "返事待ち")
	got := patch(t, s, tk.ID, gtd.TaskPatch{State: str(gtd.StateWaiting), WaitingFor: str("田中さん")})
	if got.DelegatedAt == "" {
		t.Fatal("delegated_at が入っていない")
	}
}

// state='scheduled' には scheduled_on が必須。
func TestScheduledRequiresDate(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "日付なし")
	_, err := s.Patch(context.Background(), tk.ID, gtd.TaskPatch{State: str(gtd.StateScheduled)})
	if err == nil {
		t.Fatal("scheduled_on 無しの scheduled が通ってしまった")
	}
}

// 楽観ロック。
func TestTaskVersionConflict(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "競合するタスク")
	patch(t, s, tk.ID, gtd.TaskPatch{Title: str("更新後")})

	_, err := s.Patch(ctx, tk.ID, gtd.TaskPatch{Title: str("別の更新"), Version: tk.Version})
	var vc *gtd.VersionConflictError
	if !errors.As(err, &vc) {
		t.Fatalf("version_conflict を期待したが %v", err)
	}
	if vc.Current.Title != "更新後" {
		t.Errorf("現行データが返っていない: %+v", vc.Current)
	}
}

// 2.2: someday の再検討日が到来したら浮上させる。
func TestSomedayDueReview(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	due, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "そろそろ見直す", Status: "someday",
		ReviewOn: gtd.FormatDate(gtd.Today().AddDate(0, 0, -1))})
	s.CreateProject(ctx, gtd.ProjectInput{Title: "まだ先", Status: "someday",
		ReviewOn: gtd.FormatDate(gtd.Today().AddDate(0, 0, 30))})
	s.CreateProject(ctx, gtd.ProjectInput{Title: "日付なし", Status: "someday"})

	got, err := s.SomedayDueReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("再検討日が到来した someday = %d 件", len(got))
	}
}

// 定期タスク系列の一覧(Weekly Review の棚卸し用)。
func TestSeriesList(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	tk := capture(t, s, "毎週の報告")
	tk = patch(t, s, tk.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today())), Recurrence: str("weekly:mon")})
	if _, err := s.Complete(ctx, tk.ID, false); err != nil {
		t.Fatal(err)
	}

	list, err := s.SeriesList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("系列 = %d 件, want 1", len(list))
	}
	if list[0].DoneCount != 1 || list[0].OpenTaskID == nil {
		t.Fatalf("系列の集計が合わない: %+v", list[0])
	}
}

// チェックリストのキーは標準のものだけを受け付ける。
func TestReviewChecklist(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	r, err := s.CurrentReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChecklistItem(ctx, r.ID, "review_projects", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChecklistItem(ctx, r.ID, "勝手に決めた項目", true); err == nil {
		t.Fatal("標準外のキーが通ってしまった")
	}
	got, _ := s.Review(ctx, r.ID)
	if !got.Checklist["review_projects"] {
		t.Fatal("チェックが保存されていない")
	}
	// 同じレビューが返ること(毎回新規作成しない)
	again, _ := s.CurrentReview(ctx)
	if again.ID != r.ID {
		t.Fatalf("未完了のレビューがあるのに新規作成された: %d → %d", r.ID, again.ID)
	}
}
