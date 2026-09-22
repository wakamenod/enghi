package gtd

import (
	"context"
	"database/sql"
)

// CompleteResult is the result of completing or skipping. Next, when present,
// is the instance that was generated.
type CompleteResult struct {
	Completed *Task `json:"completed"`
	Next      *Task `json:"next,omitempty"`
}

// Complete completes (or skips) a task and, for a recurring one, generates the
// next single instance.
//
// **Never materialize several instances ahead of time from a scheduler**
// (DESIGN 2.6). Doing so piles up whatever was not done, fills next actions
// with recurring tasks and stops GTD working.
// **At most one open instance per series, always.**
//
// skip=true means "skip this one". There is no dedicated state: the current
// instance is dropped and the next one generated.
func (s *Service) Complete(ctx context.Context, id int64, skip bool) (*CompleteResult, error) {
	res := &CompleteResult{}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanTask(tx.QueryRowContext(ctx,
			`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
		if err != nil {
			return err
		}

		// 1. Mark the current instance done, or dropped when skipping
		newState := StateDone
		if skip {
			newState = StateDropped
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET state = ?, completed_at = datetime('now'),
			        version = version + 1, updated_at = datetime('now')
			  WHERE id = ?`, newState, id); err != nil {
			return err
		}

		// Without a recurrence, we are done
		if cur.Recurrence == "" {
			res.Completed, err = scanTask(tx.QueryRowContext(ctx,
				`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
			return err
		}

		rule, err := ParseRecurrence(cur.Recurrence)
		if err != nil {
			// A broken rule still lets the completion through; it just generates
			// nothing. Failing here would make the task impossible to close.
			res.Completed, _ = scanTask(tx.QueryRowContext(ctx,
				`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
			return nil
		}

		// 2. Compute the next date
		today := Today()
		scheduled, _ := ParseDate(cur.ScheduledOn)
		nextOn := rule.Next(scheduled, today, today)

		// 3. Past recurrence_ends_on, generate nothing
		if cur.RecurrenceEndsOn != "" {
			if ends, err := ParseDate(cur.RecurrenceEndsOn); err == nil && nextOn.After(ends) {
				res.Completed, err = scanTask(tx.QueryRowContext(ctx,
					`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
				return err
			}
		}

		// The series id is the id of the first task; without one, this task is the
		// start of the series.
		seriesID := cur.ID
		if cur.SeriesID != nil {
			seriesID = *cur.SeriesID
		}

		// If the series already has an open instance, generate nothing (the
		// at-most-one invariant).
		var open int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM tasks
			  WHERE series_id = ? AND state NOT IN ('done','dropped','filed')`, seriesID).Scan(&open); err != nil {
			return err
		}
		if open > 0 {
			res.Completed, err = scanTask(tx.QueryRowContext(ctx,
				`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
			return err
		}

		// 4. Insert exactly one new row with the same series_id.
		// **The generated instance is always state=scheduled.**
		// Never next: a weekly:tue,fri bin day would sit in next actions forever.
		ins, err := tx.ExecContext(ctx,
			`INSERT INTO tasks(title, note, state, project_id, context_id, area_id,
			                   scheduled_on, energy, time_estimate, priority,
			                   recurrence, series_id, recurrence_ends_on, sort_order)
			 VALUES (?, ?, 'scheduled', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cur.Title, cur.Note, cur.ProjectID, cur.ContextID, cur.AreaID,
			FormatDate(nextOn), nullIfEmpty(cur.Energy), cur.TimeEstimate, cur.Priority,
			cur.Recurrence, seriesID, nullIfEmpty(cur.RecurrenceEndsOn), cur.SortOrder)
		if err != nil {
			return err
		}
		nextID, err := ins.LastInsertId()
		if err != nil {
			return err
		}

		// Fill in series_id on the starting task itself when it had none, so the
		// series can be followed
		if cur.SeriesID == nil {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET series_id = ? WHERE id = ?`, seriesID, cur.ID); err != nil {
				return err
			}
		}

		if res.Completed, err = scanTask(tx.QueryRowContext(ctx,
			`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id)); err != nil {
			return err
		}
		res.Next, err = scanTask(tx.QueryRowContext(ctx,
			`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, nextID))
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// EndSeries ends the series itself: the procedure **set recurrence to NULL
// first, then complete or drop** as a single operation (DESIGN 2.6).
// The UI must let the user choose between "this one" and "the whole series".
func (s *Service) EndSeries(ctx context.Context, id int64) (*Task, error) {
	var out *Task
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET recurrence = NULL, version = version + 1, updated_at = datetime('now')
			  WHERE id = ?`, id); err != nil {
			return err
		}
		var err error
		out, err = scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SeriesList lists the recurring series.
// **The Weekly Review screen must include it.** Taking stock of series that are
// only running out of inertia matters in GTD (DESIGN 2.6).
func (s *Service) SeriesList(ctx context.Context) ([]Series, error) {
	rows, err := s.db.QueryContext(ctx,
		`WITH sids AS (
		   SELECT DISTINCT COALESCE(series_id, id) AS sid FROM tasks WHERE recurrence IS NOT NULL
		 )
		 SELECT s.sid,
		   (SELECT title FROM tasks WHERE COALESCE(series_id, id) = s.sid
		     ORDER BY id DESC LIMIT 1),
		   (SELECT COALESCE(recurrence,'') FROM tasks WHERE COALESCE(series_id, id) = s.sid
		     AND recurrence IS NOT NULL ORDER BY id DESC LIMIT 1),
		   (SELECT id FROM tasks WHERE COALESCE(series_id, id) = s.sid
		     AND state NOT IN ('done','dropped','filed') ORDER BY id DESC LIMIT 1),
		   (SELECT COALESCE(scheduled_on,'') FROM tasks WHERE COALESCE(series_id, id) = s.sid
		     AND state NOT IN ('done','dropped','filed') ORDER BY id DESC LIMIT 1),
		   (SELECT count(*) FROM tasks WHERE COALESCE(series_id, id) = s.sid AND state = 'done'),
		   (SELECT COALESCE(recurrence_ends_on,'') FROM tasks WHERE COALESCE(series_id, id) = s.sid
		     ORDER BY id DESC LIMIT 1),
		   (SELECT COALESCE(date(max(completed_at)),'') FROM tasks
		     WHERE COALESCE(series_id, id) = s.sid AND state = 'done')
		 FROM sids s
		 ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Series{}
	for rows.Next() {
		var s Series
		var openID sql.NullInt64
		if err := rows.Scan(&s.SeriesID, &s.Title, &s.Recurrence, &openID, &s.NextOn,
			&s.DoneCount, &s.EndsOn, &s.LastDoneOn); err != nil {
			return nil, err
		}
		if openID.Valid {
			v := openID.Int64
			s.OpenTaskID = &v
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
