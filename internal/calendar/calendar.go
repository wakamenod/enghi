// Package calendar imports calendar events, read-only: from a macOS Shortcuts
// shortcut the server runs (sync.go), or from anything that PUTs them to
// /api/calendar/events. enghi never writes to a calendar.
//
// **Events come as JSON objects in one shape**, one per line from the
// shortcut and as an array from the API:
//
//	{"title":"…","start":"2026-09-26T15:00:00+09:00","end":"…","calendar":"Work","location":"…"}
//
// There is no ICS parsing on purpose: the calendar app has already expanded
// recurrences and resolved time zones by the time an event reaches us.
package calendar

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// The window the shortcut covers: from yesterday through the next 14 days, by
// start date. A sync replaces exactly this window, so **these must match the
// shortcut** (WINDOW_PAST_DAYS / WINDOW_FUTURE_DAYS in
// packaging/shortcuts/build.py). Yesterday is included so an edit or a
// deletion near today is picked up.
const (
	WindowPastDays   = 1
	WindowFutureDays = 14
)

// SourceShortcuts is the source the shortcut's events are stored under.
const SourceShortcuts = "shortcuts"

// Window is [yesterday 00:00, today+15 00:00) in local time.
func Window(now time.Time) (from, to time.Time) {
	today := midnight(now)
	return today.AddDate(0, 0, -WindowPastDays), today.AddDate(0, 0, WindowFutureDays+1)
}

// Input is one event as it arrives.
type Input struct {
	Title    string `json:"title"`
	Start    string `json:"start"` // ISO 8601 with an offset, or YYYY-MM-DD for an all-day event
	End      string `json:"end"`   // likewise; a date is the last day, inclusive
	Calendar string `json:"calendar"`
	Location string `json:"location"`
}

// Event is a stored event. Start and End are local time; End is exclusive, so
// an all-day event ends at the midnight after its last day.
type Event struct {
	ID       int64     `json:"id"`
	Source   string    `json:"source"`
	Calendar string    `json:"calendar"`
	Title    string    `json:"title"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	AllDay   bool      `json:"all_day"`
	Location string    `json:"location"`
	Key      string    `json:"-"`
	// TaskID is the task made from this event, when there is one.
	TaskID int64 `json:"task_id,omitempty"`
}

const dateLayout = "2006-01-02"

// Date is the local date the event starts on.
func (e *Event) Date() string { return e.Start.Format(dateLayout) }

// TimeRange is "15:00–16:00", or "15:00" when the event ends on another day
// or where it starts; empty for an all-day event.
func (e *Event) TimeRange() string {
	if e.AllDay {
		return ""
	}
	s := e.Start.Format("15:04")
	if e.End.After(e.Start) && sameDay(e.Start, e.End.Add(-time.Nanosecond)) {
		s += "–" + e.End.Format("15:04")
	}
	return s
}

// TaskTitle is the title of the task made from the event: the time range,
// then the event's title. All-day events get no time.
func (e *Event) TaskTitle() string {
	title := strings.TrimSpace(e.Title)
	if title == "" {
		title = "(untitled)"
	}
	if r := e.TimeRange(); r != "" {
		return r + " " + title
	}
	return title
}

// Normalize checks an input and derives the stored form.
//
// **All-day is derived from the times**, not taken from a flag: Shortcuts
// writes its all-day flag in the user's language. An all-day event starts at
// local midnight and ends at 23:59:59 on its last day (what Calendar reports)
// or at a later midnight; either way it is stored ending at the midnight after
// its last day.
func Normalize(in Input) (Event, error) {
	e := Event{Title: strings.TrimSpace(in.Title), Calendar: strings.TrimSpace(in.Calendar),
		Location: strings.TrimSpace(in.Location)}
	start, startDate, err := parseTime(in.Start)
	if err != nil {
		return e, fmt.Errorf("start: %w", err)
	}
	var end time.Time
	endDate := false
	if strings.TrimSpace(in.End) == "" {
		end, endDate = start, startDate
	} else if end, endDate, err = parseTime(in.End); err != nil {
		return e, fmt.Errorf("end: %w", err)
	}
	if endDate {
		end = end.AddDate(0, 0, 1) // the last day, inclusive
	}
	if end.Before(start) {
		return e, errors.New("end is before start")
	}
	e.Start, e.End = start, end
	if start.Equal(midnight(start)) {
		switch {
		case startDate || end.Equal(midnight(end)) && end.After(start):
			e.AllDay = true
		case end.Hour() == 23 && end.Minute() == 59 && end.Second() == 59:
			e.AllDay = true
			e.End = midnight(end).AddDate(0, 0, 1)
		}
	}
	e.Key = eventKey(e.Calendar, e.Title, e.Start)
	return e, nil
}

// parseTime reads ISO 8601 with an offset, or a bare date as local midnight.
func parseTime(s string) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return t, false, errors.New("missing")
	}
	if t, err = time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local), false, nil
	}
	if t, err = time.ParseInLocation(dateLayout, s, time.Local); err == nil {
		return t, true, nil
	}
	return t, false, fmt.Errorf("%q is neither ISO 8601 with an offset nor YYYY-MM-DD", s)
}

// eventKey identifies an event across syncs. Shortcuts exposes no stable
// event ID, so it is the calendar, the title and the start: a moved or renamed
// event is a new event.
func eventKey(calendar, title string, start time.Time) string {
	h := sha1.Sum([]byte(calendar + "|" + title + "|" + utc(start)))
	return hex.EncodeToString(h[:])
}

// LineError is a line of the shortcut's output that could not be used.
type LineError struct {
	Line int
	Err  error
}

func (e LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

// ParseLines reads JSON Lines. **A bad line is skipped, not fatal**: one odd
// event must not empty the whole calendar. Blank lines are nothing.
func ParseLines(r io.Reader) ([]Event, []LineError) {
	var out []Event
	var bad []LineError
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var in Input
		if err := json.Unmarshal(line, &in); err != nil {
			bad = append(bad, LineError{n, err})
			continue
		}
		e, err := Normalize(in)
		if err != nil {
			bad = append(bad, LineError{n, err})
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		bad = append(bad, LineError{n + 1, err})
	}
	return out, bad
}

func midnight(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func sameDay(a, b time.Time) bool {
	a, b = a.In(time.Local), b.In(time.Local)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
