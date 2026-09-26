package gtd

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The per-day work record: what was finished, what was being worked on and
// what was touched on one local calendar day. It is what org-agenda's log mode
// used to show, and what a daily report is written from.
//
// **A day is the user's local calendar day, as a UTC range.** Timestamps are
// stored in UTC, so the day is [D 00:00 local, D+1 00:00 local) converted to
// UTC and every query compares with >= and <. That keeps the indexes in use,
// and never compares a UTC date with a local one (the bug fixed in 302f6bc).

const tsLayout = "2006-01-02 15:04:05" // how SQLite's datetime('now') writes

// DayBounds are the UTC timestamps that bound a local calendar day, for
// `created_at >= from AND created_at < to`. Going through time.Local handles a
// day that is 23 or 25 hours long.
func DayBounds(day time.Time) (from, to string) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	return start.UTC().Format(tsLayout), end.UTC().Format(tsLayout)
}

// LocalTime turns a stored UTC timestamp into local time; the zero time if it
// cannot be parsed.
func LocalTime(ts string) time.Time {
	t, err := time.ParseInLocation(tsLayout, ts, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t.In(time.Local)
}

// DayTask is one task on a day, with the entries of its log written that day,
// oldest first.
type DayTask struct {
	Task *Task
	// CompletedAt is set for Done and Dropped; Since, for Working, is the start
	// mark that is still open at the end of the day.
	CompletedAt string
	Since       string
	Logs        []*TaskLog
}

// DayRecord is one day. The groups are disjoint, in this order:
//   - Done    ... completed within the day, by completed_at
//   - Working ... started and not paused, and still open, at the end of the day
//   - Worked  ... anything else with a log entry written that day
//   - Dropped ... dropped within the day, skipped recurring instances included
type DayRecord struct {
	Date    string // YYYY-MM-DD, local
	Done    []*DayTask
	Working []*DayTask
	Worked  []*DayTask
	Dropped []*DayTask
}

// Empty reports whether nothing at all happened on the day.
func (d *DayRecord) Empty() bool {
	return len(d.Done)+len(d.Working)+len(d.Worked)+len(d.Dropped) == 0
}

// Day gathers the work record of a local calendar day. Only the date part of
// day is used.
//
// completed_at is cleared when a task leaves done/dropped, so a task reopened
// later no longer counts as done on the day it was first finished. That is
// intended: the record follows what is true now.
func (s *Service) Day(ctx context.Context, day time.Time) (*DayRecord, error) {
	from, to := DayBounds(day)
	d := &DayRecord{Date: day.Format(DateLayout)}

	// Every entry written that day, grouped by task. The time range is served by
	// idx_task_logs_created.
	logs, err := s.logsBetween(ctx, from, to)
	if err != nil {
		return nil, err
	}
	byTask := map[int64][]*TaskLog{}
	var order []int64 // tasks in the order of their first entry of the day
	for _, l := range logs {
		if _, ok := byTask[l.TaskID]; !ok {
			order = append(order, l.TaskID)
		}
		byTask[l.TaskID] = append(byTask[l.TaskID], l)
	}
	seen := map[int64]bool{}

	// 1 and 4. Completed or dropped within the day. The unary + keeps the
	// planner on idx_tasks_completed: idx_tasks_state would walk every task
	// ever finished.
	closed, err := s.tasks(ctx,
		`WHERE +t.state IN ('done','dropped') AND t.completed_at >= ? AND t.completed_at < ?
		 ORDER BY t.completed_at, t.id`, from, to)
	if err != nil {
		return nil, err
	}
	for _, t := range closed {
		dt := &DayTask{Task: t, CompletedAt: t.CompletedAt, Logs: byTask[t.ID]}
		if t.State == StateDone {
			d.Done = append(d.Done, dt)
		} else {
			d.Dropped = append(d.Dropped, dt)
		}
		seen[t.ID] = true
	}

	// 2. Working at the end of the day.
	working, err := s.workingAt(ctx, to)
	if err != nil {
		return nil, err
	}
	for _, w := range working {
		if seen[w.Task.ID] {
			continue
		}
		w.Logs = byTask[w.Task.ID]
		d.Working = append(d.Working, w)
		seen[w.Task.ID] = true
	}

	// 3. Everything else that has an entry written that day.
	var rest []int64
	for _, id := range order {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	if len(rest) > 0 {
		ph := strings.Repeat(",?", len(rest))[1:]
		args := make([]any, len(rest))
		for i, id := range rest {
			args[i] = id
		}
		ts, err := s.tasks(ctx, `WHERE t.id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		byID := map[int64]*Task{}
		for _, t := range ts {
			byID[t.ID] = t
		}
		for _, id := range rest {
			if t := byID[id]; t != nil {
				d.Worked = append(d.Worked, &DayTask{Task: t, Logs: byTask[id]})
			}
		}
	}
	return d, nil
}

// workingAt are the tasks being worked on at the UTC moment `at`,
// reconstructed from the marks: the latest start/pause written before it is a
// start, and the task had not been closed by then. Since is that start.
//
// **Only tasks with a mark are candidates**; the window runs over the marks,
// never over every task. A filed task is left out: when it was filed is not
// recorded.
func (s *Service) workingAt(ctx context.Context, at string) ([]*DayTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
		  SELECT task_id, kind, created_at FROM (
		    SELECT task_id, kind, created_at,
		           row_number() OVER (PARTITION BY task_id ORDER BY created_at DESC, id DESC) AS rn
		      FROM task_logs
		     WHERE kind IN ('start','pause') AND created_at < ?)
		   WHERE rn = 1)
		SELECT `+taskCols+`, m.created_at
		  FROM latest m JOIN tasks t ON t.id = m.task_id
		  LEFT JOIN projects p ON p.id = t.project_id
		  LEFT JOIN contexts c ON c.id = t.context_id
		  LEFT JOIN areas    a ON a.id = t.area_id
		 WHERE m.kind = 'start' AND t.state <> 'filed'
		   AND (t.completed_at IS NULL OR t.completed_at >= ?)
		 ORDER BY m.created_at, t.id`, at, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DayTask{}
	for rows.Next() {
		var since string
		t, err := scanTask(extraScanner{row: rows, extra: []any{&since}})
		if err != nil {
			return nil, err
		}
		out = append(out, &DayTask{Task: t, Since: since})
	}
	return out, rows.Err()
}

// extraScanner lets scanTask read a row that carries more columns after the
// task's own.
type extraScanner struct {
	row   interface{ Scan(...any) error }
	extra []any
}

func (e extraScanner) Scan(dest ...any) error { return e.row.Scan(append(dest, e.extra...)...) }

// logsBetween are the entries written in [from, to), oldest first. Grouping is
// by created_at, which an edit never moves.
func (s *Service) logsBetween(ctx context.Context, from, to string) ([]*TaskLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+logCols+` FROM task_logs WHERE created_at >= ? AND created_at < ?
		  ORDER BY created_at, id`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TaskLog{}
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DayActivity is how much happened on one day, for the dots on the month
// calendar.
type DayActivity struct {
	Done    int `json:"done"`
	Dropped int `json:"dropped"`
	Logs    int `json:"logs"`
}

// MonthActivity counts completions and log entries per local day of a month,
// keyed by YYYY-MM-DD; days with nothing are absent. month is any time in the
// month.
//
// The rows are picked by a UTC range, so the indexes do the work, and only
// then grouped with date(x,'localtime'). SQLite's localtime is the process's
// zone, the same one time.Local reads.
func (s *Service) MonthActivity(ctx context.Context, month time.Time) (map[string]*DayActivity, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	from := first.UTC().Format(tsLayout)
	to := first.AddDate(0, 1, 0).UTC().Format(tsLayout)

	out := map[string]*DayActivity{}
	get := func(day string) *DayActivity {
		a := out[day]
		if a == nil {
			a = &DayActivity{}
			out[day] = a
		}
		return a
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT date(completed_at,'localtime'), state, count(*) FROM tasks
		  WHERE completed_at >= ? AND completed_at < ? AND +state IN ('done','dropped')
		  GROUP BY 1, 2`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day, state string
		var n int
		if err := rows.Scan(&day, &state, &n); err != nil {
			return nil, err
		}
		if state == StateDone {
			get(day).Done = n
		} else {
			get(day).Dropped = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := s.db.QueryContext(ctx,
		`SELECT date(created_at,'localtime'), count(*) FROM task_logs
		  WHERE created_at >= ? AND created_at < ? GROUP BY 1`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var day string
		var n int
		if err := rows2.Scan(&day, &n); err != nil {
			return nil, err
		}
		get(day).Logs = n
	}
	return out, rows2.Err()
}

// Working are the tasks being worked on now, oldest start first.
func (s *Service) Working(ctx context.Context) ([]*DayTask, error) {
	// A second ahead, so a start written this very second counts
	return s.workingAt(ctx, time.Now().Add(time.Second).UTC().Format(tsLayout))
}

// ParseDay reads a YYYY-MM-DD date for the day page.
func ParseDay(s string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date: %q", s)
	}
	return t, nil
}
