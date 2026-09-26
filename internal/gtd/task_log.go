package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// TaskLog is one entry in a task's work log: what was tried, found and
// decided, or a start/pause mark.
//
// **CreatedAt never changes on edit.** It is the moment the entry was written,
// and what a per-day view groups by.
type TaskLog struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Kind      string `json:"kind"` // note / start / pause
	Body      string `json:"body"` // Markdown; may be empty for start/pause
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"` // UTC, as everywhere else
	UpdatedAt string `json:"updated_at"`
}

// Log kinds.
const (
	LogNote  = "note"
	LogStart = "start"
	LogPause = "pause"
)

// Day is the local calendar day the entry was written on. Timestamps are
// stored in UTC, so the date part of CreatedAt is the wrong day for part of
// every day anywhere but UTC.
func (l *TaskLog) Day() string {
	t, err := time.Parse("2006-01-02 15:04:05", l.CreatedAt)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format(DateLayout)
}

// LogVersionConflictError is an optimistic-lock mismatch on a log entry.
type LogVersionConflictError struct {
	Current *TaskLog `json:"current"`
}

func (e *LogVersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: the current version is %d", e.Current.Version)
}

// workingExpr derives "being worked on" for the task aliased t: its latest
// start/pause entry is a start, and it is still open.
// **Derived, never stored.** Completing or dropping a task clears it with no
// extra write, and the events alone are enough to reconstruct a past day. The
// subquery walks idx_task_logs_task backwards, so it costs one short index
// scan per row.
const workingExpr = `(t.state NOT IN ('done','dropped','filed') AND COALESCE((
	SELECT l.kind FROM task_logs l
	 WHERE l.task_id = t.id AND l.kind IN ('start','pause')
	 ORDER BY l.created_at DESC, l.id DESC LIMIT 1), '') = 'start')`

const logCols = `id, task_id, kind, body, version, created_at, updated_at`

func scanLog(row interface{ Scan(...any) error }) (*TaskLog, error) {
	var l TaskLog
	if err := row.Scan(&l.ID, &l.TaskID, &l.Kind, &l.Body, &l.Version, &l.CreatedAt, &l.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &l, nil
}

// normalizeLogBody is applied on every write: NFC, as for pages (macOS hands
// us NFD), LF line ends, and no surrounding blank lines. Leading spaces on the
// first line are kept; they can mean an indented code block.
func normalizeLogBody(s string) string {
	s = textnorm.NFC(strings.ReplaceAll(s, "\r\n", "\n"))
	return strings.TrimRight(strings.TrimLeft(s, "\n"), " \t\n")
}

// Logs returns a task's log, oldest first.
func (s *Service) Logs(ctx context.Context, taskID int64) ([]*TaskLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+logCols+` FROM task_logs WHERE task_id = ? ORDER BY created_at, id`, taskID)
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

// Log returns one entry.
func (s *Service) Log(ctx context.Context, id int64) (*TaskLog, error) {
	return scanLog(s.db.QueryRowContext(ctx, `SELECT `+logCols+` FROM task_logs WHERE id = ?`, id))
}

// AddLog appends an entry. A note needs a body; start and pause may carry an
// optional comment.
//
// **Start and pause only record a change.** Starting a task that is already
// working, or pausing one that is not, writes nothing and returns the latest
// mark with created=false - so a double press leaves no noise to explain on
// the day's page later. A comment sent with such a no-op is still kept, as a
// note, so no typed text is lost. A closed task (done/dropped/filed) cannot be
// started.
func (s *Service) AddLog(ctx context.Context, taskID int64, kind, body string) (entry *TaskLog, created bool, err error) {
	if kind == "" {
		kind = LogNote
	}
	switch kind {
	case LogNote, LogStart, LogPause:
	default:
		return nil, false, fmt.Errorf("invalid kind: %q", kind)
	}
	body = normalizeLogBody(body)
	if kind == LogNote && body == "" {
		return nil, false, errors.New("the log entry is empty")
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var state string
		var working bool
		if err := tx.QueryRowContext(ctx,
			`SELECT t.state, `+workingExpr+` FROM tasks t WHERE t.id = ?`, taskID).Scan(&state, &working); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if kind == LogStart && (state == StateDone || state == StateDropped || state == StateFiled) {
			return fmt.Errorf("cannot start a task that is %s", state)
		}
		if (kind == LogStart && working) || (kind == LogPause && !working) {
			if body != "" {
				kind = LogNote
			} else {
				latest, err := scanLog(tx.QueryRowContext(ctx,
					`SELECT `+logCols+` FROM task_logs
					  WHERE task_id = ? AND kind IN ('start','pause')
					  ORDER BY created_at DESC, id DESC LIMIT 1`, taskID))
				if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
				entry = latest // nil when a never-started task is paused
				return nil
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO task_logs(task_id, kind, body) VALUES (?, ?, ?)`, taskID, kind, body)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := touchTask(ctx, tx, taskID); err != nil {
			return err
		}
		created = true
		entry, err = scanLog(tx.QueryRowContext(ctx, `SELECT `+logCols+` FROM task_logs WHERE id = ?`, id))
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return entry, created, nil
}

// EditLog replaces an entry's body. **version is required**, as for pages: an
// edit made elsewhere in the meantime is a LogVersionConflictError, never
// silently overwritten. created_at stays; entries keep no revision history.
func (s *Service) EditLog(ctx context.Context, id int64, body string, version int) (*TaskLog, error) {
	body = normalizeLogBody(body)
	var out *TaskLog
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanLog(tx.QueryRowContext(ctx, `SELECT `+logCols+` FROM task_logs WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if version != cur.Version {
			return &LogVersionConflictError{Current: cur}
		}
		if cur.Kind == LogNote && body == "" {
			return errors.New("the log entry is empty")
		}
		if body == cur.Body {
			out = cur
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE task_logs SET body = ?, version = version + 1, updated_at = datetime('now')
			  WHERE id = ?`, body, id); err != nil {
			return err
		}
		if err := touchTask(ctx, tx, cur.TaskID); err != nil {
			return err
		}
		out, err = scanLog(tx.QueryRowContext(ctx, `SELECT `+logCols+` FROM task_logs WHERE id = ?`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteLog removes an entry and returns it, so the caller knows which task it
// belonged to. Deleting a start or pause changes whether the task is working,
// which is intended: it is how a mistaken press is taken back.
func (s *Service) DeleteLog(ctx context.Context, id int64) (*TaskLog, error) {
	var out *TaskLog
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanLog(tx.QueryRowContext(ctx, `SELECT `+logCols+` FROM task_logs WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_logs WHERE id = ?`, id); err != nil {
			return err
		}
		out = cur
		return touchTask(ctx, tx, cur.TaskID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// touchTask marks a task as changed by a log write. version is left alone: a
// log entry does not conflict with an edit of the task's own fields, and
// bumping it would turn a pending Undo into a spurious conflict.
func touchTask(ctx context.Context, tx *sql.Tx, taskID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at = datetime('now') WHERE id = ?`, taskID)
	return err
}
