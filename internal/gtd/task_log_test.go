package gtd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wakamenod/enghi/internal/gtd"
)

func addLog(t *testing.T, s *gtd.Service, taskID int64, kind, body string) (*gtd.TaskLog, bool) {
	t.Helper()
	l, created, err := s.AddLog(context.Background(), taskID, kind, body)
	if err != nil {
		t.Fatalf("AddLog(%s, %q): %v", kind, body, err)
	}
	return l, created
}

func working(t *testing.T, s *gtd.Service, id int64) bool {
	t.Helper()
	tk, err := s.Task(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Working
}

func TestLogAddEditDelete(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "調査する")
	before, _ := s.Task(ctx, tk.ID)

	first, created := addLog(t, s, tk.ID, "", "最初に試したこと")
	if !created || first.Kind != gtd.LogNote || first.Version != 1 {
		t.Fatalf("AddLog = %+v, created=%v", first, created)
	}
	addLog(t, s, tk.ID, gtd.LogNote, "## 分かったこと\n\n- [[設計メモ]]")

	if _, _, err := s.AddLog(ctx, tk.ID, gtd.LogNote, "  \n\t"); err == nil {
		t.Error("a blank note was accepted")
	}
	if _, _, err := s.AddLog(ctx, tk.ID, "done", "x"); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, _, err := s.AddLog(ctx, 9999, gtd.LogNote, "x"); !errors.Is(err, gtd.ErrNotFound) {
		t.Errorf("AddLog on a missing task: %v, want ErrNotFound", err)
	}

	logs, err := s.Logs(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].ID != first.ID {
		t.Fatalf("Logs is not oldest first: %+v", logs)
	}

	edited, err := s.EditLog(ctx, first.ID, "書き直した", first.Version)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Body != "書き直した" || edited.Version != 2 || edited.CreatedAt != first.CreatedAt {
		t.Errorf("EditLog = %+v; created_at must not change", edited)
	}

	// A stale version is a conflict carrying the current entry, and changes nothing
	_, err = s.EditLog(ctx, first.ID, "古い版からの上書き", first.Version)
	var vc *gtd.LogVersionConflictError
	if !errors.As(err, &vc) || vc.Current.Version != 2 {
		t.Fatalf("stale EditLog: %v, want a LogVersionConflictError at version 2", err)
	}
	if l, _ := s.Log(ctx, first.ID); l.Body != "書き直した" {
		t.Errorf("a conflicting edit was written: %q", l.Body)
	}
	if _, err := s.EditLog(ctx, first.ID, "", 2); err == nil {
		t.Error("a note was edited down to nothing")
	}

	del, err := s.DeleteLog(ctx, first.ID)
	if err != nil || del.TaskID != tk.ID {
		t.Fatalf("DeleteLog = %+v, %v", del, err)
	}
	if _, err := s.DeleteLog(ctx, first.ID); !errors.Is(err, gtd.ErrNotFound) {
		t.Errorf("second DeleteLog: %v, want ErrNotFound", err)
	}
	if logs, _ := s.Logs(ctx, tk.ID); len(logs) != 1 {
		t.Errorf("after delete: %d entries", len(logs))
	}

	// Log writes count as a change to the task, but leave its version alone so a
	// pending Undo still applies
	after, _ := s.Task(ctx, tk.ID)
	if after.Version != before.Version {
		t.Errorf("a log write bumped the task version %d → %d", before.Version, after.Version)
	}
}

func TestLogBodyIsNormalized(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "正規化")
	nfd := "ビル" // ビル, decomposed
	l, _ := addLog(t, s, tk.ID, gtd.LogNote, "\r\n"+nfd+"\r\n    code\r\n\n")
	if want := "ビル\n    code"; l.Body != want {
		t.Errorf("body = %q, want %q", l.Body, want)
	}
	e, err := s.EditLog(context.Background(), l.ID, nfd, l.Version)
	if err != nil {
		t.Fatal(err)
	}
	if e.Body != "ビル" {
		t.Errorf("edited body = %q, want NFC", e.Body)
	}
}

// Start and pause only record a change: a repeated press writes nothing.
func TestStartPauseNoOps(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "作業")

	if l, created := addLog(t, s, tk.ID, gtd.LogPause, ""); created || l != nil {
		t.Errorf("pausing a never-started task wrote %+v", l)
	}
	if working(t, s, tk.ID) {
		t.Fatal("a fresh task is working")
	}

	start, created := addLog(t, s, tk.ID, gtd.LogStart, "")
	if !created || !working(t, s, tk.ID) {
		t.Fatalf("start: created=%v working=%v", created, working(t, s, tk.ID))
	}
	again, created := addLog(t, s, tk.ID, gtd.LogStart, "")
	if created || again.ID != start.ID {
		t.Errorf("a second start wrote an entry: %+v", again)
	}

	// A comment sent with a no-op is kept, as a note
	kept, created := addLog(t, s, tk.ID, gtd.LogStart, "まだ続けている")
	if !created || kept.Kind != gtd.LogNote {
		t.Errorf("the comment on a no-op start: %+v, created=%v", kept, created)
	}

	if _, created := addLog(t, s, tk.ID, gtd.LogPause, "昼休み"); !created || working(t, s, tk.ID) {
		t.Error("pause did not stop the work")
	}
	if _, created := addLog(t, s, tk.ID, gtd.LogPause, ""); created {
		t.Error("a second pause wrote an entry")
	}

	logs, _ := s.Logs(ctx, tk.ID)
	var kinds []string
	for _, l := range logs {
		kinds = append(kinds, l.Kind)
	}
	if got := len(kinds); got != 3 {
		t.Errorf("entries = %v, want start, note, pause", kinds)
	}
}

// Working is derived: closing the task clears it without writing a pause, and
// reopening it brings it back.
func TestWorkingIsDerived(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	a := capture(t, s, "終わらせる")
	b := capture(t, s, "やめる")
	c := capture(t, s, "触らない")
	patch(t, s, a.ID, gtd.TaskPatch{State: str(gtd.StateNext)})
	addLog(t, s, a.ID, gtd.LogStart, "")
	addLog(t, s, b.ID, gtd.LogStart, "")
	addLog(t, s, c.ID, gtd.LogNote, "メモだけ")

	next, _ := s.NextActions(ctx, nil)
	if len(next) != 1 || !next[0].Working {
		t.Fatalf("the list query does not carry working: %+v", next)
	}
	if working(t, s, c.ID) {
		t.Error("a note alone made a task working")
	}

	count := func() int {
		var n int
		db.QueryRow(`SELECT count(*) FROM task_logs`).Scan(&n)
		return n
	}
	n := count()
	if _, err := s.Complete(ctx, a.ID, false); err != nil {
		t.Fatal(err)
	}
	patch(t, s, b.ID, gtd.TaskPatch{State: str(gtd.StateDropped)})
	if working(t, s, a.ID) || working(t, s, b.ID) {
		t.Error("a done or dropped task is still working")
	}
	if count() != n {
		t.Error("closing a task wrote log entries")
	}
	if _, _, err := s.AddLog(ctx, a.ID, gtd.LogStart, ""); err == nil {
		t.Error("a done task was started")
	}

	// Undo puts it back, and the work it was in the middle of with it
	patch(t, s, b.ID, gtd.TaskPatch{State: str(gtd.StateNext)})
	if !working(t, s, b.ID) {
		t.Error("reopening a task did not restore working")
	}

	// Deleting the start takes a mistaken press back
	logs, _ := s.Logs(ctx, b.ID)
	if _, err := s.DeleteLog(ctx, logs[0].ID); err != nil {
		t.Fatal(err)
	}
	if working(t, s, b.ID) {
		t.Error("still working after its start was deleted")
	}
}

func TestLogsCascadeWithTask(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "消すタスク")
	addLog(t, s, tk.ID, gtd.LogNote, "消える記録")
	if err := s.Delete(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM task_logs`).Scan(&n)
	if n != 0 {
		t.Errorf("%d log entries outlived their task", n)
	}
	// The FTS index must follow the external content table
	db.QueryRow(`SELECT count(*) FROM task_logs_fts WHERE task_logs_fts MATCH '"消える記録"'`).Scan(&n)
	if n != 0 {
		t.Errorf("the FTS index still has %d entries of the deleted task", n)
	}
}

// An entry written now belongs to today on the local calendar. Run again under
// UTC+14 and UTC-12 by TestDatesFollowLocalCalendar.
func TestLogDayFollowsLocalCalendar(t *testing.T) {
	s, _, _ := newSvc(t)
	tk := capture(t, s, "今日の作業")
	l, _ := addLog(t, s, tk.ID, gtd.LogStart, "")
	if got, want := l.Day(), gtd.FormatDate(gtd.Today()); got != want {
		t.Errorf("Day() = %s, want %s (created_at %s UTC)", got, want, l.CreatedAt)
	}
}
