package gtd_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// 4.2: capture must work with {title} alone, and the state is inbox.
func TestCaptureNeedsOnlyTitle(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "何か思いついた")
	if tk.State != gtd.StateInbox {
		t.Fatalf("state = %q, want inbox", tk.State)
	}
	inbox, _ := s.Inbox(context.Background())
	if len(inbox) != 1 {
		t.Fatalf("inbox = %d items", len(inbox))
	}
}

// 2.6: the condition selecting next actions is
// state='next' OR (state='scheduled' AND scheduled_on <= today)。
// **a query condition; there is no batch job rewriting state.**
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
		t.Error("an overdue scheduled task is missing from next actions (it vanishes on days nobody looked)")
	}
	if !got["今日やる"] {
		t.Error("today's scheduled task is missing from next actions")
	}
	if got["来週やる"] {
		t.Error("a future scheduled task showed up in next actions")
	}
	// state must not have been rewritten
	again, _ := s.Task(ctx, past.ID)
	if again.State != gtd.StateScheduled {
		t.Errorf("state was rewritten: %q (no batch job may do this)", again.State)
	}
}

// 2.4: detecting active projects with no next action.
// **This carries half the value of the system.**
func TestStalledProjectDetection(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()

	stalled, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "止まっている"})
	healthy, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "動いている"})
	someday, _ := s.CreateProject(ctx, gtd.ProjectInput{Title: "いつか", Status: "someday"})

	// The stalled project has only later tasks, which are not next actions
	t1 := capture(t, s, "後でやる")
	patch(t, s, t1.ID, gtd.TaskPatch{State: str(gtd.StateLater), ProjectID: &stalled.ID})
	// The moving project has a next action
	t2 := capture(t, s, "次の行動")
	patch(t, s, t2.ID, gtd.TaskPatch{State: str(gtd.StateNext), ProjectID: &healthy.ID})
	// The someday project has nothing, but it is not active, so it is not
	// reported
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
		t.Fatalf("stalled projects = %v, want [止まっている]", titles)
	}

	// waiting and scheduled both count as "there is a next action"
	patch(t, s, t1.ID, gtd.TaskPatch{State: str(gtd.StateWaiting), WaitingFor: str("誰か")})
	got, _ = s.StalledProjects(ctx)
	if len(got) != 0 {
		t.Fatalf("reported as stalled despite a waiting task: %d", len(got))
	}
}

// 2.6: completing generates exactly one next instance.
// **That instance is always state=scheduled, never next.**
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
		t.Errorf("current instance = %q, want done", res.Completed.State)
	}
	if res.Next == nil {
		t.Fatal("no next instance was generated")
	}
	if res.Next.State != gtd.StateScheduled {
		t.Fatalf("next instance = %q, want scheduled (as next it would sit in next actions forever)", res.Next.State)
	}
	if res.Next.ScheduledOn == "" {
		t.Error("the next instance has no scheduled_on")
	}
	if res.Next.Recurrence != "weekly:tue,fri" {
		t.Errorf("recurrence was not carried over: %q", res.Next.Recurrence)
	}
	// The series must be followable
	if res.Next.SeriesID == nil || *res.Next.SeriesID != tk.ID {
		t.Errorf("series_id = %v, want %d", res.Next.SeriesID, tk.ID)
	}
}

// **At most one open instance per series, always** - nothing is generated
// ahead of time.
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
			t.Fatalf("round %d generated no next instance", i+1)
		}
		cur = res.Next.ID

		var open int
		if err := db.QueryRow(
			`SELECT count(*) FROM tasks WHERE state NOT IN ('done','dropped','filed')`).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open != 1 {
			t.Fatalf("round %d: %d open instances (there must always be one)", i+1, open)
		}
	}
}

// skip has no state of its own: the instance is dropped and the next one
// generated.
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
		t.Errorf("after skip = %q, want dropped", res.Completed.State)
	}
	if res.Next == nil {
		t.Fatal("skip must generate a next instance too")
	}
}

// To end a series, recurrence is set to NULL first.
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
		t.Fatal("a next instance was generated after ending the series")
	}
}

// Past recurrence_ends_on, nothing is generated.
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
		t.Fatalf("generated even though the end date has passed: %s", res.Next.ScheduledOn)
	}
}

// 8-13: judged to be reference material, an item becomes a wiki page and the
// original task becomes filed.
// **Never done, never dropped.**
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
		t.Fatalf("state = %q, want filed (neither done nor dropped)", got.State)
	}
	if page.Title != "参考資料" {
		t.Fatalf("page = %q", page.Title)
	}
	// They must be connected through links
	links, err := s.LinkedPages(ctx, "task", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !links[0].Resolved || *links[0].PageID != page.ID {
		t.Fatalf("not connected through links: %+v", links)
	}
	// filed appears in neither the inbox nor next actions
	inbox, _ := s.Inbox(ctx)
	if len(inbox) != 0 {
		t.Errorf("a filed item is still in the inbox")
	}
}

// Moving to waiting fills in the delegation date, which the days-elapsed
// warning needs.
func TestWaitingGetsDelegatedAt(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "返事待ち")
	got := patch(t, s, tk.ID, gtd.TaskPatch{State: str(gtd.StateWaiting), WaitingFor: str("田中さん")})
	if got.DelegatedAt == "" {
		t.Fatal("delegated_at was not filled in")
	}
	if today := gtd.FormatDate(gtd.Today()); got.DelegatedAt != today {
		t.Errorf("delegated_at = %s, want today (%s) on the local calendar", got.DelegatedAt, today)
	}
}

// A deadline of today shows up in Today.
func TestTodayIncludesDeadlineOfToday(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "今日が締め切り")
	patch(t, s, tk.ID, gtd.TaskPatch{State: str(gtd.StateNext),
		DeadlineOn: str(gtd.FormatDate(gtd.Today()))})

	got, err := s.Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != tk.ID {
		t.Fatalf("today's deadline is missing from Today (%d tasks)", len(got))
	}
}

// Deadlines ahead (org-deadline-warning-days) and overdue ones. Each task
// appears in one of Today and UpcomingDeadlines at most.
func TestUpcomingDeadlines(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	due := func(title string, days int) *gtd.Task {
		tk := capture(t, s, title)
		return patch(t, s, tk.ID, gtd.TaskPatch{State: str(gtd.StateNext),
			DeadlineOn: str(gtd.FormatDate(gtd.Today().AddDate(0, 0, days)))})
	}
	yesterday := due("yesterday", -1)
	today := due("today", 0)
	tomorrow := due("tomorrow", 1)
	edge := due("in a week", 7)
	due("too far", 8)
	done := due("done already", 2)
	if _, err := s.Complete(ctx, done.ID, false); err != nil {
		t.Fatal(err)
	}
	someday := due("someday", 3)
	patch(t, s, someday.ID, gtd.TaskPatch{State: str(gtd.StateSomeday)})
	// Scheduled for today, so it is in Today already; not repeated below.
	sched := due("scheduled today", 4)
	patch(t, s, sched.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today()))})
	// A past scheduled date alone is not overdue.
	late := capture(t, s, "scheduled yesterday")
	late = patch(t, s, late.ID, gtd.TaskPatch{State: str(gtd.StateScheduled),
		ScheduledOn: str(gtd.FormatDate(gtd.Today().AddDate(0, 0, -1)))})

	ids := func(list []*gtd.Task) []int64 {
		out := []int64{}
		for _, tk := range list {
			out = append(out, tk.ID)
		}
		return out
	}

	up, err := s.UpcomingDeadlines(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids(up), []int64{tomorrow.ID, edge.ID}; !slices.Equal(got, want) {
		t.Errorf("upcoming = %v, want %v (tomorrow, today+7)", got, want)
	}
	if len(up) == 2 && (up[0].DaysLeft() != 1 || up[1].DaysLeft() != 7) {
		t.Errorf("days left = %d, %d; want 1, 7", up[0].DaysLeft(), up[1].DaysLeft())
	}

	td, err := s.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	overdue := map[int64]int{}
	for _, tk := range td {
		if tk.Overdue() {
			overdue[tk.ID] = tk.DaysOverdue()
		}
	}
	got, want := ids(td), []int64{yesterday.ID, today.ID, late.ID, sched.ID}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("today = %v, want %v", got, want)
	}
	if len(overdue) != 1 || overdue[yesterday.ID] != 1 {
		t.Errorf("overdue = %v, want only yesterday's deadline, 1 day", overdue)
	}
}

// "Today" is the local calendar day, in SQL as in Go. The tests above are run
// again in two zones: at any moment one of UTC+14 and UTC-12 is on a different
// date from UTC, so a query that uses the UTC date fails whatever the time.
func TestDatesFollowLocalCalendar(t *testing.T) {
	if os.Getenv("ENGHI_TZ_CHILD") != "" {
		t.Skip("already running in a child")
	}
	const tests = `^(TestScheduledTaskAppearsInNextActionsWhenDue|TestWaitingGetsDelegatedAt|` +
		`TestTodayIncludesDeadlineOfToday|TestSomedayDueReview|TestUpcomingDeadlines)$`
	for _, tz := range []string{"Etc/GMT-14", "Etc/GMT+12"} {
		cmd := exec.Command(os.Args[0], "-test.run="+tests, "-test.count=1")
		cmd.Env = append(os.Environ(), "TZ="+tz, "ENGHI_TZ_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("TZ=%s: %v\n%s", tz, err, out)
		}
	}
}

// state='scheduled' requires scheduled_on.
func TestScheduledRequiresDate(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "日付なし")
	_, err := s.Patch(context.Background(), tk.ID, gtd.TaskPatch{State: str(gtd.StateScheduled)})
	if err == nil {
		t.Fatal("scheduled without scheduled_on was accepted")
	}
}

// Optimistic locking.
func TestTaskVersionConflict(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "競合するタスク")
	patch(t, s, tk.ID, gtd.TaskPatch{Title: str("更新後")})

	_, err := s.Patch(ctx, tk.ID, gtd.TaskPatch{Title: str("別の更新"), Version: tk.Version})
	var vc *gtd.VersionConflictError
	if !errors.As(err, &vc) {
		t.Fatalf("expected version_conflict, got %v", err)
	}
	if vc.Current.Title != "更新後" {
		t.Errorf("the current data was not returned: %+v", vc.Current)
	}
}

// 2.2: a someday project resurfaces when its review date arrives.
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
		t.Fatalf("someday items due for review = %d", len(got))
	}
}

// The list of recurring series, for taking stock in the Weekly Review.
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
		t.Fatalf("series = %d, want 1", len(list))
	}
	if list[0].DoneCount != 1 || list[0].OpenTaskID == nil {
		t.Fatalf("the series aggregate is wrong: %+v", list[0])
	}
}

// Only the standard checklist keys are accepted.
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
		t.Fatal("a non-standard key was accepted")
	}
	got, _ := s.Review(ctx, r.ID)
	if !got.Checklist["review_projects"] {
		t.Fatal("the check was not saved")
	}
	// The same review comes back; a new one is not created every time
	again, _ := s.CurrentReview(ctx)
	if again.ID != r.ID {
		t.Fatalf("a new review was created although one was unfinished: %d -> %d", r.ID, again.ID)
	}
}
