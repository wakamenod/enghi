package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CaptureInput is POST /api/tasks. **{title} alone must be enough**
// (DESIGN 4.2).
type CaptureInput struct {
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
	State string `json:"state,omitempty"` // inbox when omitted
}

// Capture puts one item in the inbox. Being able to throw a line in from
// anywhere is the premise of GTD.
func (s *Service) Capture(ctx context.Context, in CaptureInput) (*Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, errors.New("the title is empty")
	}
	state := in.State
	if state == "" {
		state = StateInbox
	}
	if !validStates[state] {
		return nil, fmt.Errorf("invalid state: %q", state)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks(title, note, state) VALUES (?, ?, ?)`, title, in.Note, state)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Task(ctx, id)
}

// TaskPatch is PATCH /api/tasks/:id. nil fields are left alone.
type TaskPatch struct {
	Title            *string `json:"title,omitempty"`
	Note             *string `json:"note,omitempty"`
	State            *string `json:"state,omitempty"`
	ProjectID        *int64  `json:"project_id,omitempty"`
	ContextID        *int64  `json:"context_id,omitempty"`
	AreaID           *int64  `json:"area_id,omitempty"`
	ScheduledOn      *string `json:"scheduled_on,omitempty"`
	DeadlineOn       *string `json:"deadline_on,omitempty"`
	WaitingFor       *string `json:"waiting_for,omitempty"`
	DelegatedAt      *string `json:"delegated_at,omitempty"`
	Energy           *string `json:"energy,omitempty"`
	TimeEstimate     *int    `json:"time_estimate,omitempty"`
	Priority         *int    `json:"priority,omitempty"`
	Recurrence       *string `json:"recurrence,omitempty"`
	RecurrenceEndsOn *string `json:"recurrence_ends_on,omitempty"`
	SortOrder        *int    `json:"sort_order,omitempty"`
	// ClearProject and friends are flags standing in for an explicit null.
	ClearProject bool `json:"clear_project,omitempty"`
	ClearContext bool `json:"clear_context,omitempty"`
	ClearArea    bool `json:"clear_area,omitempty"`
	Version      int  `json:"version"`
}

// setBuilder builds the SET clause of an UPDATE, keeping the column-to-value
// mapping in one place. Maintaining set and args by hand in parallel always
// drifts apart eventually.
type setBuilder struct {
	parts []string
	args  []any
}

// Set appends "col = ?" together with its value.
func (b *setBuilder) Set(col string, v any) {
	b.parts = append(b.parts, col+" = ?")
	b.args = append(b.args, v)
}

// Raw appends an expression with no value, such as col = NULL or
// col = datetime('now').
func (b *setBuilder) Raw(expr string) { b.parts = append(b.parts, expr) }

// Date is a date column where an empty string means NULL.
func (b *setBuilder) Date(col, val string) error {
	if val == "" {
		b.Raw(col + " = NULL")
		return nil
	}
	if _, err := ParseDate(val); err != nil {
		return fmt.Errorf("%s must be YYYY-MM-DD: %q", col, val)
	}
	b.Set(col, val)
	return nil
}

func (b *setBuilder) Empty() bool { return len(b.parts) == 0 }

// Patch applies a partial update to a task. State transitions go through here
// as well.
func (s *Service) Patch(ctx context.Context, id int64, p TaskPatch) (*Task, error) {
	var out *Task
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
		if err != nil {
			return err
		}
		// version may be omitted: a one-off state change from the UI does not need
		// it. When it is sent, it is always checked.
		if p.Version != 0 && p.Version != cur.Version {
			return &VersionConflictError{Current: cur}
		}

		b := &setBuilder{}

		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return errors.New("the title is empty")
			}
			b.Set("title", strings.TrimSpace(*p.Title))
		}
		if p.Note != nil {
			b.Set("note", *p.Note)
		}
		if p.State != nil {
			if !validStates[*p.State] {
				return fmt.Errorf("invalid state: %q", *p.State)
			}
			b.Set("state", *p.State)
			switch *p.State {
			case StateDone, StateDropped:
				if cur.CompletedAt == "" {
					b.Raw("completed_at = datetime('now')")
				}
			case StateWaiting:
				// Without a delegation date, use today; the days-elapsed warning
				// needs it
				if cur.DelegatedAt == "" && p.DelegatedAt == nil {
					b.Raw("delegated_at = " + sqlToday)
				}
			}
		}
		if p.ProjectID != nil {
			b.Set("project_id", *p.ProjectID)
		} else if p.ClearProject {
			b.Raw("project_id = NULL")
		}
		if p.ContextID != nil {
			b.Set("context_id", *p.ContextID)
		} else if p.ClearContext {
			b.Raw("context_id = NULL")
		}
		if p.AreaID != nil {
			b.Set("area_id", *p.AreaID)
		} else if p.ClearArea {
			b.Raw("area_id = NULL")
		}
		for _, f := range []struct {
			col string
			val *string
		}{
			{"scheduled_on", p.ScheduledOn},
			{"deadline_on", p.DeadlineOn},
			{"delegated_at", p.DelegatedAt},
			{"recurrence_ends_on", p.RecurrenceEndsOn},
		} {
			if f.val == nil {
				continue
			}
			if err := b.Date(f.col, *f.val); err != nil {
				return err
			}
		}
		if p.WaitingFor != nil {
			b.Set("waiting_for", *p.WaitingFor)
		}
		if p.Energy != nil {
			switch *p.Energy {
			case "":
				b.Raw("energy = NULL")
			case "low", "mid", "high":
				b.Set("energy", *p.Energy)
			default:
				return fmt.Errorf("invalid energy: %q", *p.Energy)
			}
		}
		if p.TimeEstimate != nil {
			b.Set("time_estimate", *p.TimeEstimate)
		}
		if p.Priority != nil {
			b.Set("priority", *p.Priority)
		}
		if p.Recurrence != nil {
			if *p.Recurrence == "" {
				// **To end a series, set recurrence to NULL first, then complete or
				// drop the task**
				b.Raw("recurrence = NULL")
			} else {
				if _, err := ParseRecurrence(*p.Recurrence); err != nil {
					return err
				}
				b.Set("recurrence", *p.Recurrence)
			}
		}
		if p.SortOrder != nil {
			b.Set("sort_order", *p.SortOrder)
		}
		if b.Empty() {
			out = cur
			return nil
		}

		b.Raw("version = version + 1")
		b.Raw("updated_at = datetime('now')")
		args := append(b.args, id)
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET `+strings.Join(b.parts, ", ")+` WHERE id = ?`, args...); err != nil {
			return err
		}
		if err := validateTask(ctx, tx, id); err != nil {
			return err
		}
		out, err = scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` `+taskFrom+` WHERE t.id = ?`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// validateTask checks that the state and the columns agree.
func validateTask(ctx context.Context, tx *sql.Tx, id int64) error {
	var state, sched string
	if err := tx.QueryRowContext(ctx,
		`SELECT state, COALESCE(scheduled_on,'') FROM tasks WHERE id = ?`, id).Scan(&state, &sched); err != nil {
		return err
	}
	if state == StateScheduled && sched == "" {
		return errors.New("state='scheduled' requires scheduled_on")
	}
	return nil
}

// Delete removes a task. Link lifecycle is managed here, not by the database
// (DESIGN 2.1).
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL WHERE dst_kind = 'task' AND dst_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM links WHERE src_kind = 'task' AND src_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
