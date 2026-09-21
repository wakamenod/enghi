package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wakamenod/enghi/internal/store"
)

// Service は GTD 側のすべての読み書き。
type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// ---------------------------------------------------------------- Task 読み取り

const taskCols = `t.id, t.title, t.note, t.state, t.project_id, t.context_id, t.area_id,
	COALESCE(t.scheduled_on,''), COALESCE(t.deadline_on,''), COALESCE(t.waiting_for,''),
	COALESCE(t.delegated_at,''), COALESCE(t.energy,''), t.time_estimate, t.priority,
	COALESCE(t.recurrence,''), t.series_id, COALESCE(t.recurrence_ends_on,''),
	t.sort_order, t.version, COALESCE(t.completed_at,''), t.created_at, t.updated_at,
	COALESCE(p.title,''), COALESCE(c.name,''), COALESCE(a.name,''),
	CASE WHEN t.state = 'waiting' AND t.delegated_at IS NOT NULL
	     THEN CAST(julianday('now') - julianday(t.delegated_at) AS INTEGER) ELSE 0 END`

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
		&t.ProjectTitle, &t.ContextName, &t.AreaName, &t.WaitingDays)
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

// Task は1件取得。
func (s *Service) Task(ctx context.Context, id int64) (*Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
}

// Inbox は未処理の項目。
func (s *Service) Inbox(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'inbox' ORDER BY t.created_at`)
}

// nextActionsWhere は Next Actions の抽出条件。
//
// **state='scheduled' のタスクは scheduled_on <= today になったらこのリストに現れる。**
// これはビューの条件で表現し、**state を書き換えるバッチ処理は作らない**(DESIGN 2.6)。
// 常駐サーバが落ちていた日にタスクが消える、という壊れ方を避けるため。
const nextActionsWhere = `(t.state = 'next' OR (t.state = 'scheduled' AND t.scheduled_on <= date('now')))`

// NextActions はコンテキストで絞り込める Next Action 一覧。
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

// Waiting は他者待ち。経過日数付き。
func (s *Service) Waiting(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'waiting' ORDER BY t.delegated_at, t.id`)
}

// Scheduled は日付付き(まだ来ていないものも含む)。
func (s *Service) Scheduled(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'scheduled' ORDER BY t.scheduled_on, t.id`)
}

// Someday はいつかやる/たぶんやる。
func (s *Service) Someday(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.state = 'someday' ORDER BY t.updated_at DESC`)
}

// TasksOfProject はプロジェクト配下のタスク。
func (s *Service) TasksOfProject(ctx context.Context, projectID int64) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.project_id = ? ORDER BY
		CASE t.state WHEN 'next' THEN 0 WHEN 'scheduled' THEN 1 WHEN 'waiting' THEN 2
		             WHEN 'later' THEN 3 WHEN 'someday' THEN 4 ELSE 9 END,
		t.sort_order, t.id`, projectID)
}

// TaskQuery は /api/tasks の絞り込み。
type TaskQuery struct {
	State     string
	ContextID *int64
	ProjectID *int64
	AreaID    *int64
	DueBefore string
	Limit     int
}

// QueryTasks は API 用の汎用検索。
func (s *Service) QueryTasks(ctx context.Context, q TaskQuery) ([]*Task, error) {
	var conds []string
	var args []any
	switch {
	case q.State == "next_actions":
		conds = append(conds, nextActionsWhere)
	case q.State != "":
		if !validStates[q.State] {
			return nil, fmt.Errorf("state が不正です: %q", q.State)
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

// CompletedBetween は期間内に完了したタスク(週次レビューの「先週の振り返り」)。
func (s *Service) CompletedBetween(ctx context.Context, from, to string) ([]*Task, error) {
	return s.tasks(ctx,
		`WHERE t.completed_at IS NOT NULL AND date(t.completed_at) >= ? AND date(t.completed_at) <= ?
		 ORDER BY t.completed_at DESC`, from, to)
}

// UpcomingBetween は今後の予定と締切。
func (s *Service) UpcomingBetween(ctx context.Context, from, to string) ([]*Task, error) {
	return s.tasks(ctx,
		`WHERE t.state NOT IN ('done','dropped','filed')
		   AND ((t.scheduled_on IS NOT NULL AND t.scheduled_on BETWEEN ? AND ?)
		     OR (t.deadline_on  IS NOT NULL AND t.deadline_on  BETWEEN ? AND ?))
		 ORDER BY COALESCE(t.scheduled_on, t.deadline_on)`, from, to, from, to)
}

// Today は今日やるもの: 締切または予定日が今日以前(DESIGN 5)。
// **`=` にしないこと。** 見なかった日に予定されていたタスクが翌日以降に消える。
func (s *Service) Today(ctx context.Context) ([]*Task, error) {
	return s.tasks(ctx,
		`WHERE t.state NOT IN ('done','dropped','filed','someday')
		   AND ((t.deadline_on  IS NOT NULL AND t.deadline_on  <= date('now'))
		     OR (t.scheduled_on IS NOT NULL AND t.scheduled_on <= date('now')))
		 ORDER BY COALESCE(t.deadline_on, t.scheduled_on), t.priority DESC`)
}

// WaitingOverdue は委譲から一定日数が経過した Waiting For(既定 7 日)。
func (s *Service) WaitingOverdue(ctx context.Context, days int) ([]*Task, error) {
	if days <= 0 {
		days = 7
	}
	return s.tasks(ctx,
		fmt.Sprintf(`WHERE t.state = 'waiting' AND t.delegated_at IS NOT NULL
		   AND julianday('now') - julianday(t.delegated_at) >= %d
		 ORDER BY t.delegated_at`, days))
}
