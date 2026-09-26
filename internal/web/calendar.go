package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	enghi "github.com/wakamenod/enghi"
	"github.com/wakamenod/enghi/internal/calendar"
	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/settings"
)

// Calendar events: the settings panel, the timeline on the dashboard and the
// day page, "make a task", and /api/calendar/*. What is stored and how a sync
// works is internal/calendar.

// UseShortcuts puts the Shortcuts runner behind the sync; main calls it. It
// returns the Syncer, whose Daemon the caller starts. **Tests never call it**,
// or a stub runner, so they never reach /usr/bin/shortcuts.
func (s *Server) UseShortcuts(r calendar.Runner, supported bool) *calendar.Syncer {
	s.sync = calendar.NewSyncer(s.cal, s.set, r, s.cfg.CalendarShortcut, s.cfg.CalendarSyncInterval, supported)
	s.sync.OnChange = func() { s.hub.Broadcast(Event{Type: "updated", Kind: "calendar"}) }
	return s.sync
}

// ---------------------------------------------------------------- timeline

// timelineEvent is an event as the timeline shows it.
type timelineEvent struct {
	*calendar.Event
	Time string // "15:00–16:00"; empty for an all-day event
	Past bool   // ended; dimmed
	// Unix milliseconds, for app.js to move the now marker without a reload
	StartMS, EndMS int64
}

// timeline is one day's events: all-day ones first, then the timed ones in
// order. On today, the now marker goes before Timed[NowAt]: after every event
// that has started.
type timeline struct {
	Date    string
	IsToday bool
	AllDay  []timelineEvent
	Timed   []timelineEvent
	NowAt   int
	// Shown is whether the section appears at all: the sync is on, or some
	// events arrived anyway (the PUT API works without it)
	Shown bool
}

func buildTimeline(day, now time.Time, evs []*calendar.Event, on bool) *timeline {
	y, m, d := now.Date()
	t := &timeline{Date: day.Format(gtd.DateLayout), Shown: on || len(evs) > 0}
	dy, dm, dd := day.Date()
	t.IsToday = y == dy && m == dm && d == dd
	for _, e := range evs {
		te := timelineEvent{Event: e, Time: e.TimeRange(),
			StartMS: e.Start.UnixMilli(), EndMS: e.End.UnixMilli()}
		if e.AllDay {
			t.AllDay = append(t.AllDay, te)
			continue
		}
		// Only today dims what is over; a past day is all over
		te.Past = t.IsToday && !e.End.After(now)
		if t.IsToday && !e.Start.After(now) {
			t.NowAt = len(t.Timed) + 1
		}
		t.Timed = append(t.Timed, te)
	}
	return t
}

// hiddenCalendars are the calendars the settings leave off the screens.
func (s *Server) hiddenCalendars(ctx context.Context) []string {
	set, err := s.set.Load(ctx)
	if err != nil {
		log.Printf("settings: %v", err)
	}
	return set.CalendarHidden
}

// dayTimeline is the timeline for a local day.
func (s *Server) dayTimeline(ctx context.Context, day time.Time) (*timeline, []*calendar.Event, error) {
	set, _ := s.set.Load(ctx)
	evs, err := s.cal.Day(ctx, day, set.CalendarHidden)
	if err != nil {
		return nil, nil, err
	}
	return buildTimeline(day, time.Now(), evs, set.Calendar), evs, nil
}

// ---------------------------------------------------------------- settings

type calendarChoice struct {
	Name   string
	Hidden bool
}

// calendarPanel is the Calendar panel of the settings screen.
type calendarPanel struct {
	Status    calendar.Status
	Installed bool
	Calendars []calendarChoice
	// Status's times, as "01/02 15:04"
	LastRun, LastSuccess string
}

func (s *Server) calendarPanel(ctx context.Context) calendarPanel {
	p := calendarPanel{Status: s.sync.Status(ctx)}
	p.LastRun, p.LastSuccess = localStamp(p.Status.LastRun), localStamp(p.Status.LastSuccess)
	if p.Status.Supported {
		var err error
		if p.Installed, err = s.sync.Installed(ctx); err != nil {
			log.Printf("calendar: shortcuts list: %v", err)
		}
	}
	names, err := s.cal.Calendars(ctx)
	if err != nil {
		log.Printf("calendar: %v", err)
	}
	hidden := s.hiddenCalendars(ctx)
	for _, n := range names {
		p.Calendars = append(p.Calendars, calendarChoice{Name: n, Hidden: slices.Contains(hidden, n)})
	}
	return p
}

// localStamp is an RFC 3339 time as "01/02 15:04"; empty stays empty.
func localStamp(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format("01/02 15:04")
}

// uiCalendarSettings receives the Calendar panel: the switch, and a checkbox
// per calendar, checked to show it. **Hidden = listed but unchecked**; a
// calendar that was hidden and is no longer listed stays hidden.
func (s *Server) uiCalendarSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, s.tr(r, "err.bad_request"), http.StatusBadRequest)
		return
	}
	ctx := ctxOf(r)
	set, _ := s.set.Load(ctx)
	on := r.PostForm.Get("enabled") == "1"
	if err := s.set.Set(ctx, settings.KeyCalendar, on); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	listed, shown := r.PostForm["known"], r.PostForm["show"]
	hidden := []string{}
	for _, n := range set.CalendarHidden {
		if !slices.Contains(listed, n) {
			hidden = append(hidden, n)
		}
	}
	for _, n := range listed {
		if !slices.Contains(shown, n) && !slices.Contains(hidden, n) {
			hidden = append(hidden, n)
		}
	}
	if err := s.set.SetCalendarHidden(ctx, hidden); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "calendar"})
	// Turning it on syncs right away rather than at the next tick
	if on && !set.Calendar && s.sync.Supported() {
		go func() {
			if err := s.sync.Sync(context.Background()); err != nil {
				log.Printf("warning: calendar sync failed: %v", err)
			}
		}()
	}
	http.Redirect(w, r, "/settings#calendar", http.StatusSeeOther)
}

// uiCalendarSync is "Sync now". The run takes a fraction of a second, so it
// is done before answering and the screen shows the result.
func (s *Server) uiCalendarSync(w http.ResponseWriter, r *http.Request) {
	if err := s.sync.Sync(ctxOf(r)); err != nil {
		log.Printf("warning: calendar sync failed: %v", err)
	}
	http.Redirect(w, r, "/settings#calendar", http.StatusSeeOther)
}

// uiCalendarInstall hands the shortcut built into this binary to Shortcuts,
// which asks, on this Mac, whether to add it. **Only from this machine**, like
// install-skill: the dialog opens where the server runs.
func (s *Server) uiCalendarInstall(w http.ResponseWriter, r *http.Request) {
	if !fromThisMachine(r) {
		http.Error(w, s.tr(r, "cal.local_only"), http.StatusForbidden)
		return
	}
	if err := s.sync.Import(ctxOf(r), enghi.ShortcutFile); err != nil {
		http.Error(w, s.tr(r, "cal.install_failed", err.Error()), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings#calendar", http.StatusSeeOther)
}

// ---------------------------------------------------------------- make a task

// uiEventTask is the "make a task" button.
func (s *Server) uiEventTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, _, err := s.cal.MakeTask(ctxOf(r), id, s.gtd); err != nil {
		if errors.Is(err, calendar.ErrNotFound) {
			http.Error(w, s.tr(r, "cal.event_gone"), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	redirectBack(w, r, "/")
}

// apiEventTask is POST /api/calendar/events/{id}/task: 201 with the new task,
// or 200 with the one made earlier.
func (s *Server) apiEventTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	t, created, err := s.cal.MakeTask(ctxOf(r), id, s.gtd)
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", s.tr(r, "cal.event_gone"))
		return
	case err != nil:
		s.gtdErr(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
		s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	}
	writeJSON(w, code, t)
}

// ---------------------------------------------------------------- API

// dateRange reads from/to (YYYY-MM-DD, both inclusive) as [from 00:00, the day
// after to 00:00) local. Both default to today; to defaults to from.
func dateRange(r *http.Request) (from, to time.Time, err error) {
	q := r.URL.Query()
	n := time.Now()
	from = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
	if v := q.Get("from"); v != "" {
		if from, err = gtd.ParseDay(v); err != nil {
			return from, to, fmt.Errorf("from must be YYYY-MM-DD: %q", v)
		}
	}
	to = from
	if v := q.Get("to"); v != "" {
		if to, err = gtd.ParseDay(v); err != nil {
			return from, to, fmt.Errorf("to must be YYYY-MM-DD: %q", v)
		}
	}
	to = to.AddDate(0, 0, 1)
	if !to.After(from) {
		return from, to, errors.New("to is before from")
	}
	return from, to, nil
}

// apiCalendarEvents is GET /api/calendar/events?from=&to=: the events
// overlapping those days, hidden calendars left out.
func (s *Server) apiCalendarEvents(w http.ResponseWriter, r *http.Request) {
	from, to, err := dateRange(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_date", err.Error())
		return
	}
	evs, err := s.cal.Events(ctxOf(r), from, to, s.hiddenCalendars(ctxOf(r)))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(gtd.DateLayout), "to": to.AddDate(0, 0, -1).Format(gtd.DateLayout),
		"events": evs})
}

// apiPutCalendarEvents is PUT /api/calendar/events?source=&from=&to= with a
// JSON array of events, in the shortcut's shape. The source's events starting
// in those days are replaced by the array, as a sync does. **Every item must
// be valid**, or nothing is stored: a caller can fix and resend.
func (s *Server) apiPutCalendarEvents(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "source is required")
		return
	}
	if r.URL.Query().Get("from") == "" || r.URL.Query().Get("to") == "" {
		writeErr(w, http.StatusBadRequest, "bad_date", "from and to are required")
		return
	}
	from, to, err := dateRange(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_date", err.Error())
		return
	}
	var in []calendar.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "the body must be a JSON array of events: "+err.Error())
		return
	}
	evs := make([]calendar.Event, 0, len(in))
	for i, x := range in {
		e, err := calendar.Normalize(x)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_event", fmt.Sprintf("event %d: %v", i, err))
			return
		}
		evs = append(evs, e)
	}
	changed, err := s.cal.Replace(ctxOf(r), source, from, to, evs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if changed {
		s.hub.Broadcast(Event{Type: "updated", Kind: "calendar"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stored": len(evs), "changed": changed})
}

// calendarStatusJSON is the sync status, with whether the shortcut is there.
func (s *Server) calendarStatusJSON(ctx context.Context) map[string]any {
	st := s.sync.Status(ctx)
	installed, _ := s.sync.Installed(ctx)
	return map[string]any{"status": st, "installed": installed}
}

// apiCalendarStatus is GET /api/calendar/status.
func (s *Server) apiCalendarStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.calendarStatusJSON(ctxOf(r)))
}

// apiCalendarSync is POST /api/calendar/sync: sync now, whether or not the
// periodic sync is on, and answer the status.
func (s *Server) apiCalendarSync(w http.ResponseWriter, r *http.Request) {
	if err := s.sync.Sync(ctxOf(r)); err != nil {
		out := s.calendarStatusJSON(ctxOf(r))
		out["error"], out["message"] = "sync_failed", err.Error()
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	writeJSON(w, http.StatusOK, s.calendarStatusJSON(ctxOf(r)))
}
