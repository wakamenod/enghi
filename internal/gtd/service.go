package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wakamenod/enghi/internal/store"
)

// Service is every read and write on the GTD side.
type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// ---------------------------------------------------------------- task reads

// sqlToday is today's date on the local calendar, the same day Today() gives.
// **Never compare dates with a bare date('now').** That is the UTC date, so in
// a zone ahead of UTC a task scheduled for today stays hidden until UTC catches
// up (until 09:00 in Japan).
const sqlToday = `date('now','localtime')`

const taskCols = `t.id, t.title, t.note, t.state, t.project_id, t.context_id, t.area_id,
	COALESCE(t.scheduled_on,''), COALESCE(t.deadline_on,''), COALESCE(t.waiting_for,''),
	COALESCE(t.delegated_at,''), COALESCE(t.energy,''), t.time_estimate, t.priority,
	COALESCE(t.recurrence,''), t.series_id, COALESCE(t.recurrence_ends_on,''),
	t.sort_order, t.version, COALESCE(t.completed_at,''), t.created_at, t.updated_at,
	COALESCE(p.title,''), COALESCE(c.name,''), COALESCE(a.name,''),
	CASE WHEN t.state = 'waiting' AND t.delegated_at IS NOT NULL
	     THEN CAST(julianday(` + sqlToday + `) - julianday(t.delegated_at) AS INTEGER) ELSE 0 END,
	CASE WHEN t.deadline_on IS NOT NULL
	     THEN CAST(julianday(t.deadline_on) - julianday(` + sqlToday + `) AS INTEGER) END,
	` + workingExpr

const taskFrom = `FROM tasks t
	LEFT JOIN projects p ON p.id = t.project_id
	LEFT JOIN contexts c ON c.id = t.context_id
	LEFT JOIN areas    a ON a.id = t.area_id`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.Title, &t.Note, &t.State, &t.ProjectID, &t.ContextID, &t.AreaID,
		&t.ScheduledOn, &t.DeadlineOn, &t.WaitingFor, &t.DelegatedAt, &t.Energy, &t.TimeEstimate,
		&t.Priority, &t.Recurrence, &t.SeriesID, &t.RecurrenceEndsOn,
		&t.SortOrder, &t.Version, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectTitle, &t.ContextName, &t.AreaName, &t.WaitingDays, &t.DeadlineDays, &t.Working)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *Service) tasks(ctx context.Context, where string, args ...any) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskCols+` `+taskFrom+` `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Task returns one task.
func (s *Service) Task(ctx context.Context, id int64) (*Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
}

// Inbox are the unprocessed items.
func (s *Service) Inbox(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'inbox' ORDER BY t.created_at`)
}

// nextActionsWhere is the condition that selects next actions.
//
// **A task with state='scheduled' appears in this list once
// scheduled_on <= today.** That is expressed as a query condition; **there is no
// batch job that rewrites state** (DESIGN 2.6), so a day when the server was
// down cannot make tasks disappear.
const nextActionsWhere = `(t.state = 'next' OR (t.state = 'scheduled' AND t.scheduled_on <= ` + sqlToday + `))`

// NextActions lists next actions, optionally filtered by context.
func (s *Service) NextActions(ctx context.Context, contextID *int64) ([]*Task, error) {
	where := `WHERE ` + nextActionsWhere
	args := []any{}
	if contextID != nil {
		where += ` AND t.context_id = ?`
		args = append(args, *contextID)
	}
	where += ` ORDER BY t.priority DESC, COALESCE(t.deadline_on,'9999-12-31'), t.sort_order, t.id`
	return s.tasks(ctx, where, args...)
}

// Waiting are the items waiting on someone else, with days elapsed.
func (s *Service) Waiting(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'waiting' ORDER BY t.delegated_at, t.id`)
}

// Scheduled are the dated items, including those still in the future.
func (s *Service) Scheduled(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'scheduled' ORDER BY t.scheduled_on, t.id`)
}

// Someday are the someday/maybe items.
func (s *Service) Someday(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'someday' ORDER BY t.updated_at DESC`)
}

// TasksOfProject are the tasks under a project.
func (s *Service) TasksOfProject(ctx context.Context, projectID int64) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.project_id = ? ORDER BY
		CASE t.state WHEN 'next' THEN 0 WHEN 'scheduled' THEN 1 WHEN 'waiting' THEN 2
		             WHEN 'later' THEN 3 WHEN 'someday' THEN 4 ELSE 9 END,
		t.sort_order, t.id`, projectID)
}

// TaskQuery is the filter of /api/tasks.
type TaskQuery struct {
	State     string
	ContextID *int64
	ProjectID *int64
	AreaID    *int64
	DueBefore string
	Limit     int
}

// QueryTasks is the general-purpose query behind the API.
func (s *Service) QueryTasks(ctx context.Context, q TaskQuery) ([]*Task, error) {
	var conds []string
	var args []any
	switch {
	case q.State == "next_actions":
		conds = append(conds, nextActionsWhere)
	case q.State != "":
		if !validStates[q.State] {
			return nil, fmt.Errorf("invalid state: %q", q.State)
		}
		conds = append(conds, `t.state = ?`)
		args = append(args, q.State)
	}
	if q.ContextID != nil {
		conds = append(conds, `t.context_id = ?`)
		args = append(args, *q.ContextID)
	}
	if q.ProjectID != nil {
		conds = append(conds, `t.project_id = ?`)
		args = append(args, *q.ProjectID)
	}
	if q.AreaID != nil {
		conds = append(conds, `t.area_id = ?`)
		args = append(args, *q.AreaID)
	}
	if q.DueBefore != "" {
		conds = append(conds, `(t.deadline_on IS NOT NULL AND t.deadline_on <= ?)`)
		args = append(args, q.DueBefore)
	}
	where := ``
	if len(conds) > 0 {
		where = `WHERE ` + strings.Join(conds, ` AND `)
	}
	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	where += fmt.Sprintf(` ORDER BY t.sort_order, t.id LIMIT %d`, limit)
	return s.tasks(ctx, where, args...)
}

// CompletedBetween are the tasks completed in a period, for the weekly
// review's look back at last week. from and to are local dates, both
// included; completed_at is UTC, so they are turned into a UTC range first.
func (s *Service) CompletedBetween(ctx context.Context, from, to string) ([]*Task, error) {
	f, err := ParseDay(from)
	if err != nil {
		return nil, err
	}
	t, err := ParseDay(to)
	if err != nil {
		return nil, err
	}
	lo, _ := DayBounds(f)
	_, hi := DayBounds(t)
	return s.tasks(ctx,
		`WHERE t.completed_at >= ? AND t.completed_at < ?
		 ORDER BY t.completed_at DESC`, lo, hi)
}

// UpcomingBetween are the upcoming scheduled dates and deadlines.
func (s *Service) UpcomingBetween(ctx context.Context, from, to string) ([]*Task, error) {
	return s.tasks(ctx,
		`WHERE t.state NOT IN ('done','dropped','filed')
		   AND ((t.scheduled_on IS NOT NULL AND t.scheduled_on BETWEEN ? AND ?)
		     OR (t.deadline_on  IS NOT NULL AND t.deadline_on  BETWEEN ? AND ?))
		 ORDER BY COALESCE(t.scheduled_on, t.deadline_on)`, from, to, from, to)
}

// Today is what to do today: a deadline or scheduled date of today or earlier
// (DESIGN 5).
// **Never use `=`.** A task scheduled for a day nobody looked at would vanish
// from then on.
func (s *Service) Today(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx,
		`WHERE t.state NOT IN ('done','dropped','filed','someday')
		   AND ((t.deadline_on  IS NOT NULL AND t.deadline_on  <= `+sqlToday+`)
		     OR (t.scheduled_on IS NOT NULL AND t.scheduled_on <= `+sqlToday+`))
		 ORDER BY COALESCE(t.deadline_on, t.scheduled_on), t.priority DESC`)
}

// UpcomingDeadlines are the open tasks whose deadline falls within the next
// `days` days, tomorrow through today+days: the warning ahead of a deadline,
// like org-deadline-warning-days.
// **Anything Today() already shows is left out**, so no task appears twice: a
// deadline of today or earlier, or a scheduled date that has arrived. Someday
// is excluded for the same reason it is excluded from Today().
func (s *Service) UpcomingDeadlines(ctx context.Context, days int) ([]*Task, error) {
	if days <= 0 {
		return []*Task{}, nil
	}
	return s.tasks(ctx,
		`WHERE t.state NOT IN ('done','dropped','filed','someday')
		   AND t.deadline_on IS NOT NULL
		   AND t.deadline_on >  `+sqlToday+`
		   AND t.deadline_on <= date('now','localtime',?)
		   AND NOT (t.scheduled_on IS NOT NULL AND t.scheduled_on <= `+sqlToday+`)
		 ORDER BY t.deadline_on, t.priority DESC, t.id`, fmt.Sprintf("+%d days", days))
}

// WaitingOverdue are waiting-for items delegated more than a number of days
// ago (7 by default).
func (s *Service) WaitingOverdue(ctx context.Context, days int) ([]*Task, error) {
	if days <= 0 {
		days = 7
	}
	return s.tasks(ctx,
		fmt.Sprintf(`WHERE t.state = 'waiting' AND t.delegated_at IS NOT NULL
		   AND julianday(`+sqlToday+`) - julianday(t.delegated_at) >= %d
		 ORDER BY t.delegated_at`, days))
}
