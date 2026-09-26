package gtd_test

import (
	"context"
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/store"
)

// These run again under UTC+14 and UTC-12 (TestDatesFollowLocalCalendar), so
// a day computed from the UTC date fails in one of them.

// The day under test. Fixed, and far from now, so the rows written by the
// tests themselves (at the real "now") do not land on it.
var dayD = time.Date(2025, 3, 10, 0, 0, 0, 0, time.Local)

// at is a UTC timestamp, as stored, for a local wall-clock time on D+offset.
func at(offset, hour, min int) string {
	return time.Date(dayD.Year(), dayD.Month(), dayD.Day()+offset, hour, min, 0, 0, time.Local).
		UTC().Format("2006-01-02 15:04:05")
}

func mustExec(t *testing.T, db *store.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func day(t *testing.T, s *gtd.Service, offset int) *gtd.DayRecord {
	t.Helper()
	d, err := s.Day(context.Background(), dayD.AddDate(0, 0, offset))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func ids(ts []*gtd.DayTask) []int64 {
	out := []int64{}
	for _, t := range ts {
		out = append(out, t.Task.ID)
	}
	return out
}

func has(ts []*gtd.DayTask, id int64) bool {
	for _, t := range ts {
		if t.Task.ID == id {
			return true
		}
	}
	return false
}

// A task finished at 23:50 local is on D; one finished at 00:10 local the next
// morning is on D+1.
func TestDayDoneFollowsLocalCalendar(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	late := capture(t, s, "夜遅くに終えた")
	early := capture(t, s, "翌朝すぐ終えた")
	for _, tk := range []*gtd.Task{late, early} {
		if _, err := s.Complete(ctx, tk.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, `UPDATE tasks SET completed_at = ? WHERE id = ?`, at(0, 23, 50), late.ID)
	mustExec(t, db, `UPDATE tasks SET completed_at = ? WHERE id = ?`, at(1, 0, 10), early.ID)

	if d := day(t, s, 0); len(d.Done) != 1 || d.Done[0].Task.ID != late.ID {
		t.Errorf("D done = %v, want [%d]", ids(d.Done), late.ID)
	}
	if d := day(t, s, 1); len(d.Done) != 1 || d.Done[0].Task.ID != early.ID {
		t.Errorf("D+1 done = %v, want [%d]", ids(d.Done), early.ID)
	}
}

// A start on D-1 with no pause is working at the end of D-1 and of D. A pause
// on D takes it out of "working" on D but keeps it in "worked on".
func TestDayWorkingFromMarks(t *testing.T) {
	s, _, db := newSvc(t)
	open := capture(t, s, "続けている")
	paused := capture(t, s, "途中で止めた")
	st, _ := addLog(t, s, open.ID, gtd.LogStart, "")
	mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(-1, 16, 0), st.ID)
	st2, _ := addLog(t, s, paused.ID, gtd.LogStart, "")
	mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(-1, 17, 0), st2.ID)
	pa, _ := addLog(t, s, paused.ID, gtd.LogPause, "")
	mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(0, 10, 0), pa.ID)

	prev := day(t, s, -1)
	if !has(prev.Working, open.ID) || !has(prev.Working, paused.ID) {
		t.Errorf("D-1 working = %v, want both", ids(prev.Working))
	}
	d := day(t, s, 0)
	if !has(d.Working, open.ID) || has(d.Working, paused.ID) {
		t.Errorf("D working = %v, want only %d", ids(d.Working), open.ID)
	}
	if d.Working[0].Since != at(-1, 16, 0) {
		t.Errorf("since = %s, want the start on D-1", d.Working[0].Since)
	}
	if !has(d.Worked, paused.ID) || has(d.Worked, open.ID) {
		t.Errorf("D worked = %v, want only %d", ids(d.Worked), paused.ID)
	}
	if w := d.Worked[0]; len(w.Logs) != 1 || w.Logs[0].Kind != gtd.LogPause {
		t.Errorf("D worked logs = %+v, want the pause alone", w.Logs)
	}
	// Before the start, nothing
	if d := day(t, s, -2); !d.Empty() {
		t.Errorf("D-2 is not empty: %+v", d)
	}
}

// Completing a task during D ends its work: it is done, not working, and its
// entries of the day go with it.
func TestDayDoneWinsOverWorking(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "開始して終えた")
	st, _ := addLog(t, s, tk.ID, gtd.LogStart, "")
	note, _ := addLog(t, s, tk.ID, gtd.LogNote, "分かったこと")
	s.Complete(ctx, tk.ID, false)
	mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(0, 9, 0), st.ID)
	mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(0, 11, 0), note.ID)
	mustExec(t, db, `UPDATE tasks SET completed_at = ? WHERE id = ?`, at(0, 12, 0), tk.ID)

	d := day(t, s, 0)
	if len(d.Done) != 1 || len(d.Working) != 0 || len(d.Worked) != 0 {
		t.Fatalf("done=%v working=%v worked=%v", ids(d.Done), ids(d.Working), ids(d.Worked))
	}
	if logs := d.Done[0].Logs; len(logs) != 2 || logs[0].Kind != gtd.LogStart || logs[1].Body != "分かったこと" {
		t.Errorf("logs = %+v", logs)
	}
	// Finished on D+1 instead: working at the end of D
	mustExec(t, db, `UPDATE tasks SET completed_at = ? WHERE id = ?`, at(1, 9, 0), tk.ID)
	d = day(t, s, 0)
	if len(d.Done) != 0 || !has(d.Working, tk.ID) {
		t.Errorf("done=%v working=%v, want it working", ids(d.Done), ids(d.Working))
	}
}

// A task reopened after being done is not done on that day any more, and
// dropped is its own group.
func TestDayReopenedAndDropped(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	reopened := capture(t, s, "やり直し")
	dropped := capture(t, s, "やめた")
	s.Complete(ctx, reopened.ID, false)
	s.Complete(ctx, dropped.ID, true)
	mustExec(t, db, `UPDATE tasks SET completed_at = ?`, at(0, 15, 0))
	patch(t, s, reopened.ID, gtd.TaskPatch{State: str(gtd.StateNext)})

	d := day(t, s, 0)
	if len(d.Done) != 0 {
		t.Errorf("done = %v, want none", ids(d.Done))
	}
	if len(d.Dropped) != 1 || d.Dropped[0].Task.ID != dropped.ID {
		t.Errorf("dropped = %v, want [%d]", ids(d.Dropped), dropped.ID)
	}
}

// Entries belong to the day they were written, not the day they were edited.
func TestDayLogsByCreatedAt(t *testing.T) {
	s, _, db := newSvc(t)
	tk := capture(t, s, "前日に書いた")
	l, _ := addLog(t, s, tk.ID, gtd.LogNote, "前日のメモ")
	mustExec(t, db, `UPDATE task_logs SET created_at = ?, updated_at = ? WHERE id = ?`,
		at(-1, 23, 30), at(0, 0, 30), l.ID)

	if d := day(t, s, 0); !d.Empty() {
		t.Errorf("D is not empty: worked=%v", ids(d.Worked))
	}
	if d := day(t, s, -1); !has(d.Worked, tk.ID) {
		t.Errorf("D-1 worked = %v", ids(d.Worked))
	}
}

// The month calendar counts by local day.
func TestMonthActivity(t *testing.T) {
	s, _, db := newSvc(t)
	ctx := context.Background()
	tk := capture(t, s, "月の集計")
	s.Complete(ctx, tk.ID, false)
	mustExec(t, db, `UPDATE tasks SET completed_at = ? WHERE id = ?`, at(0, 23, 50), tk.ID)
	for _, h := range []int{0, 23} {
		l, _ := addLog(t, s, tk.ID, gtd.LogNote, "メモ")
		mustExec(t, db, `UPDATE task_logs SET created_at = ? WHERE id = ?`, at(0, h, 5), l.ID)
	}

	m, err := s.MonthActivity(ctx, dayD)
	if err != nil {
		t.Fatal(err)
	}
	a := m["2025-03-10"]
	if a == nil || a.Done != 1 || a.Logs != 2 || len(m) != 1 {
		t.Errorf("month = %v", m)
	}
}

// Working() is the current set, the same as the working flag on each task.
func TestWorkingNow(t *testing.T) {
	s, _, _ := newSvc(t)
	a := capture(t, s, "作業中")
	b := capture(t, s, "止めた")
	addLog(t, s, a.ID, gtd.LogStart, "")
	addLog(t, s, b.ID, gtd.LogStart, "")
	addLog(t, s, b.ID, gtd.LogPause, "")
	got, err := s.Working(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Task.ID != a.ID || !working(t, s, a.ID) {
		t.Errorf("working = %v", ids(got))
	}
	today, _ := s.Day(context.Background(), time.Now())
	if len(today.Working) != 1 || !has(today.Worked, b.ID) {
		t.Errorf("today working=%v worked=%v", ids(today.Working), ids(today.Worked))
	}
}
