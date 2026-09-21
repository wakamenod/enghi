package gtd

import (
	"context"
	"database/sql"
)

// CompleteResult は完了/skip の結果。Next があれば生成された次インスタンス。
type CompleteResult struct {
	Completed *Task `json:"completed"`
	Next      *Task `json:"next,omitempty"`
}

// Complete はタスクを完了(または skip)し、定期タスクなら次の1件を生成する。
//
// **スケジューラで先回りして複数件を materialize しない**(DESIGN 2.6)。
// やると、消化できなかった分が溜まって Next Actions が定期タスクで埋まり、GTD が機能しなくなる。
// **同じ系列で開いているインスタンスは常に高々1件**とする。
//
// skip=true は「今回は飛ばす」。専用の state は設けず、現インスタンスを dropped にして次を生成する。
func (s *Service) Complete(ctx context.Context, id int64, skip bool) (*CompleteResult, error) {
	res := &CompleteResult{}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanTask(tx.QueryRowContext(ctx,
			`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
		if err != nil {
			return err
		}

		// 1. 現インスタンスを done(skip なら dropped)にする
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

		// recurrence が無ければここで終わり
		if cur.Recurrence == "" {
			res.Completed, err = scanTask(tx.QueryRowContext(ctx,
				`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
			return err
		}

		rule, err := ParseRecurrence(cur.Recurrence)
		if err != nil {
			// 規則が壊れていても完了自体は通す(次は作らない)。
			// ここで失敗させると、壊れた規則のせいでタスクを閉じられなくなる。
			res.Completed, _ = scanTask(tx.QueryRowContext(ctx,
				`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
			return nil
		}

		// 2. 次回日付を計算する
		today := Today()
		scheduled, _ := ParseDate(cur.ScheduledOn)
		nextOn := rule.Next(scheduled, today, today)

		// 3. recurrence_ends_on を過ぎていれば生成しない
		if cur.RecurrenceEndsOn != "" {
			if ends, err := ParseDate(cur.RecurrenceEndsOn); err == nil && nextOn.After(ends) {
				res.Completed, err = scanTask(tx.QueryRowContext(ctx,
					`SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
				return err
			}
		}

		// 系列 id は最初のタスクの id。無ければ自分自身が系列の起点。
		seriesID := cur.ID
		if cur.SeriesID != nil {
			seriesID = *cur.SeriesID
		}

		// 同じ系列で開いているインスタンスが既にあるなら作らない(高々1件の不変条件)。
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

		// 4. 同じ series_id で新しい行を1件だけ挿入する。
		// **生成される次インスタンスの state は必ず scheduled とする。**
		// next で作ってはいけない。weekly:tue,fri のゴミ出しが常時 Next Actions に居座る。
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

		// 起点タスク自身に series_id が無かった場合はここで埋める(系列を辿れるように)
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

// EndSeries は系列そのものを終わらせる。
// **先に recurrence を NULL にしてから完了/破棄する**という手順を1操作にしたもの(DESIGN 2.6)。
// UI では「この回だけ」「この系列全体」を選ばせること。
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

// SeriesList は定期タスク系列の一覧。
// **Weekly Review 画面に必ず設けること。**惰性で回り続けている系列の棚卸しは GTD 上重要(DESIGN 2.6)。
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
