package calendar

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/store"
)

const tsLayout = "2006-01-02 15:04:05" // stored UTC, as datetime('now') writes

func utc(t time.Time) string { return t.UTC().Format(tsLayout) }

func parseUTC(s string) time.Time {
	t, err := time.ParseInLocation(tsLayout, s, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t.In(time.Local)
}

// ErrNotFound is an event id that does not exist (any more: a sync may have
// replaced it).
var ErrNotFound = errors.New("no such event")

// Service reads and writes the stored events.
type Service struct {
	db *store.DB
	// taskMu makes MakeTask one at a time, so a double click cannot make two
	// tasks from one event.
	taskMu sync.Mutex
}

func New(db *store.DB) *Service { return &Service{db: db} }

// Replace stores one source's events for a window: in one transaction, the
// source's events starting within [from, to) that are not in evs are deleted
// and evs upserted, so an event still there keeps its id. **Rows outside the
// window stay**, as history. It reports whether anything in the window
// changed, so an unchanged sync does not reload the screens.
func (s *Service) Replace(ctx context.Context, source string, from, to time.Time, evs []Event) (changed bool, err error) {
	if source == "" {
		return false, errors.New("source is empty")
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		before, err := fingerprint(ctx, tx, source, from, to)
		if err != nil {
			return err
		}
		keys := make([]any, 0, len(evs)+3)
		keys = append(keys, source, utc(from), utc(to))
		for _, e := range evs {
			keys = append(keys, e.Key)
		}
		q := `DELETE FROM calendar_events WHERE source = ? AND starts_at >= ? AND starts_at < ?`
		if len(evs) > 0 {
			q += ` AND event_key NOT IN (?` + strings.Repeat(",?", len(evs)-1) + `)`
		}
		if _, err := tx.ExecContext(ctx, q, keys...); err != nil {
			return err
		}
		for _, e := range evs {
			// The same event twice (the shortcut's two finds overlap on a day)
			// is one row
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO calendar_events(source, calendar, title, starts_at, ends_at, all_day, location, event_key)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(source, event_key) DO UPDATE SET
				  ends_at = excluded.ends_at, all_day = excluded.all_day,
				  location = excluded.location, synced_at = datetime('now')`,
				source, e.Calendar, e.Title, utc(e.Start), utc(e.End), e.AllDay, e.Location, e.Key); err != nil {
				return err
			}
		}
		after, err := fingerprint(ctx, tx, source, from, to)
		changed = before != after
		return err
	})
	return changed, err
}

// fingerprint sums up the window's rows, to tell whether a replace changed
// anything.
func fingerprint(ctx context.Context, tx *sql.Tx, source string, from, to time.Time) (string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT event_key, ends_at, all_day, location FROM calendar_events
		 WHERE source = ? AND starts_at >= ? AND starts_at < ? ORDER BY event_key`,
		source, utc(from), utc(to))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha1.New()
	for rows.Next() {
		var key, ends, loc string
		var allDay bool
		if err := rows.Scan(&key, &ends, &allDay, &loc); err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s|%s|%t|%s\n", key, ends, allDay, loc)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), rows.Err()
}

const eventCols = `e.id, e.source, e.calendar, e.title, e.starts_at, e.ends_at, e.all_day, e.location,
	e.event_key, COALESCE(et.task_id, 0)`

const eventFrom = `FROM calendar_events e LEFT JOIN event_tasks et ON et.event_key = e.event_key`

func scanEvent(row interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	var start, end string
	if err := row.Scan(&e.ID, &e.Source, &e.Calendar, &e.Title, &start, &end, &e.AllDay,
		&e.Location, &e.Key, &e.TaskID); err != nil {
		return nil, err
	}
	e.Start, e.End = parseUTC(start), parseUTC(end)
	return &e, nil
}

// Events are the events overlapping [from, to), all-day ones first, then by
// start. Events on a hidden calendar are left out.
func (s *Service) Events(ctx context.Context, from, to time.Time, hidden []string) ([]*Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` `+eventFrom+`
		 WHERE e.starts_at < ? AND (e.ends_at > ? OR e.starts_at >= ?)
		 ORDER BY e.all_day DESC, e.starts_at, e.ends_at, e.title`,
		utc(to), utc(from), utc(from))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(hidden, e.Calendar) {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// Day are the events on a local calendar day: those overlapping it, so a
// multi-day event is on each of its days.
func (s *Service) Day(ctx context.Context, day time.Time, hidden []string) ([]*Event, error) {
	start := midnight(day)
	return s.Events(ctx, start, start.AddDate(0, 0, 1), hidden)
}

// Event is one event by id.
func (s *Service) Event(ctx context.Context, id int64) (*Event, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` `+eventFrom+` WHERE e.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// Calendars are the names of the calendars seen in the stored events, for
// choosing which to hide.
func (s *Service) Calendars(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT calendar FROM calendar_events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, rows.Err()
}

// MakeTask turns an event into a task through the ordinary capture and patch:
// titled with the event's time (TaskTitle), scheduled on the event's local
// date, so it comes up in Next Actions that day. **A second call returns the
// task the first one made** (created=false).
func (s *Service) MakeTask(ctx context.Context, id int64, g *gtd.Service) (t *gtd.Task, created bool, err error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	e, err := s.Event(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if e.TaskID != 0 {
		t, err := g.Task(ctx, e.TaskID)
		return t, false, err
	}
	captured, err := g.Capture(ctx, gtd.CaptureInput{Title: e.TaskTitle()})
	if err != nil {
		return nil, false, err
	}
	state, on := gtd.StateScheduled, e.Date()
	if t, err = g.Patch(ctx, captured.ID, gtd.TaskPatch{State: &state, ScheduledOn: &on}); err == nil {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO event_tasks(event_key, task_id) VALUES (?, ?)
			 ON CONFLICT(event_key) DO UPDATE SET task_id = excluded.task_id`, e.Key, t.ID)
	}
	if err != nil {
		_ = g.Delete(ctx, captured.ID) // leave no half-made task in the inbox
		return nil, false, err
	}
	return t, true, nil
}
