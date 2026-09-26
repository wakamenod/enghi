package web

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/i18n"
)

// The per-day work record: /gtd/day, /api/day and /api/days. What a day holds
// is decided in gtd.Day; this file shows it, as a page, JSON and Markdown.
//
// **The sections are listed top to bottom in one place each** (dayPageData,
// dayJSON, dayMarkdown). Calendar events for the day are meant to go above the
// groups later; adding them is a new field and a new block, not a reshape.

// dayWeekdayKeys are the i18n keys for weekday names, Sunday first as
// time.Weekday counts.
var dayWeekdayKeys = [7]string{
	"repeat.wd.sun", "repeat.wd.mon", "repeat.wd.tue", "repeat.wd.wed",
	"repeat.wd.thu", "repeat.wd.fri", "repeat.wd.sat",
}

func weekdayLabel(lang i18n.Lang, t time.Time) string {
	return i18n.T(lang, dayWeekdayKeys[t.Weekday()])
}

// dayFromRequest reads the day from the path or the date parameter; today when
// neither is given. ok=false means it was given and is not a YYYY-MM-DD date.
func dayFromRequest(r *http.Request) (day time.Time, ok bool) {
	s := r.PathValue("date")
	if s == "" {
		s = r.URL.Query().Get("date")
	}
	if s == "" {
		n := time.Now()
		return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local), true
	}
	d, err := gtd.ParseDay(s)
	return d, err == nil
}

// isoLocal is a stored UTC timestamp as ISO 8601 with the local offset. The
// day's own times are for people reading a report, so they are local.
func isoLocal(ts string) string {
	t := gtd.LocalTime(ts)
	if t.IsZero() {
		return ts
	}
	return t.Format(time.RFC3339)
}

// clock is a stored timestamp as local HH:MM; with the date in front when it
// is not on `day`.
func clock(ts string, day time.Time) string {
	t := gtd.LocalTime(ts)
	if t.IsZero() {
		return ts
	}
	if t.Year() != day.Year() || t.YearDay() != day.YearDay() {
		return t.Format("01/02 15:04")
	}
	return t.Format("15:04")
}

// localZoneName is the IANA name of the local zone when it can be found:
// $TZ, or the target of /etc/localtime. time.Local itself only says "Local".
func localZoneName() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		return tz
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			return target[i+len("zoneinfo/"):]
		}
		return filepath.Base(target)
	}
	name, _ := time.Now().Zone()
	return name
}

// ---------------------------------------------------------------- page

type dayLogRow struct {
	*gtd.TaskLog
	Time string // HH:MM; an earlier entry has its date in front
	HTML template.HTML
}

type dayRow struct {
	*gtd.DayTask
	Time  string // completion time for done/dropped
	Since string // start of the open work, for working
	Logs  []dayLogRow
	// Earlier are a done task's entries from before the day. They start folded
	// when they are long, so the day's own record stays in view.
	Earlier     []dayLogRow
	EarlierOpen bool
}

// earlierFoldRunes is how much earlier text is shown unfolded.
const earlierFoldRunes = 400

// stamp is a stored timestamp as local "YYYY-MM-DD HH:MM", for an entry from
// another day.
func stamp(ts string) string {
	t := gtd.LocalTime(ts)
	if t.IsZero() {
		return ts
	}
	return t.Format("2006-01-02 15:04")
}

type calDay struct {
	Date     string
	Day      int
	InMonth  bool
	Active   bool // anything happened
	Selected bool
	Today    bool
	Title    string // counts, as a tooltip
}

type calendarData struct {
	Month     string // YYYY-MM
	PrevMonth string
	NextMonth string
	Weekdays  []string
	Weeks     [][]calDay
}

type dayPageData struct {
	Date     string
	Weekday  string
	IsToday  bool
	Prev     string
	Next     string
	Today    string
	Empty    bool
	Markdown string
	Calendar calendarData

	// In the order they are shown. Calendar events will go before Done.
	Done    []dayRow
	Working []dayRow
	Worked  []dayRow
	Dropped []dayRow
}

func (s *Server) dayRows(r *http.Request, day time.Time, ts []*gtd.DayTask) []dayRow {
	out := make([]dayRow, 0, len(ts))
	for _, t := range ts {
		row := dayRow{DayTask: t}
		if t.CompletedAt != "" {
			row.Time = clock(t.CompletedAt, day)
		}
		if t.Since != "" {
			row.Since = clock(t.Since, day)
		}
		for _, l := range t.Logs {
			row.Logs = append(row.Logs, s.dayLogRow(r, l, clock(l.CreatedAt, day)))
		}
		n := 0
		for _, l := range t.EarlierLogs {
			row.Earlier = append(row.Earlier, s.dayLogRow(r, l, stamp(l.CreatedAt)))
			n += utf8.RuneCountInString(l.Body)
		}
		row.EarlierOpen = n <= earlierFoldRunes
		out = append(out, row)
	}
	return out
}

func (s *Server) dayLogRow(r *http.Request, l *gtd.TaskLog, when string) dayLogRow {
	v := dayLogRow{TaskLog: l, Time: when}
	if l.Body != "" {
		if html, err := s.renderBody(r, l.Body); err == nil {
			v.HTML = html
		} else {
			v.HTML = template.HTML("<pre>" + template.HTMLEscapeString(l.Body) + "</pre>")
		}
	}
	return v
}

func (s *Server) viewDay(w http.ResponseWriter, r *http.Request) {
	day, ok := dayFromRequest(r)
	if !ok {
		http.Error(w, s.tr(r, "day.bad_date"), http.StatusBadRequest)
		return
	}
	ctx := ctxOf(r)
	rec, err := s.gtd.Day(ctx, day)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lang := s.langOf(r)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	d := dayPageData{
		Date:     rec.Date,
		Weekday:  weekdayLabel(lang, day),
		IsToday:  day.Equal(today),
		Prev:     day.AddDate(0, 0, -1).Format(gtd.DateLayout),
		Next:     day.AddDate(0, 0, 1).Format(gtd.DateLayout),
		Today:    today.Format(gtd.DateLayout),
		Empty:    rec.Empty(),
		Markdown: dayMarkdown(lang, day, rec),
		Done:     s.dayRows(r, day, rec.Done),
		Working:  s.dayRows(r, day, rec.Working),
		Worked:   s.dayRows(r, day, rec.Worked),
		Dropped:  s.dayRows(r, day, rec.Dropped),
	}

	// The calendar shows the day's month unless ?month= picks another
	month := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
	if m, err := time.ParseInLocation("2006-01", r.URL.Query().Get("month"), time.Local); err == nil {
		month = m
	}
	act, err := s.gtd.MonthActivity(ctx, month)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.Calendar = buildCalendar(lang, month, day, today, act)

	s.render(w, r, "day.html", viewData{
		Title: s.tr(r, "day.title") + " " + rec.Date, Nav: "gtd", Data: d})
}

// buildCalendar lays out a month in weeks, Sunday first, padded with the
// neighbouring months' days.
func buildCalendar(lang i18n.Lang, month, selected, today time.Time, act map[string]*gtd.DayActivity) calendarData {
	c := calendarData{
		Month:     month.Format("2006-01"),
		PrevMonth: month.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth: month.AddDate(0, 1, 0).Format("2006-01"),
	}
	for _, k := range dayWeekdayKeys {
		c.Weekdays = append(c.Weekdays, i18n.T(lang, k))
	}
	d := month.AddDate(0, 0, -int(month.Weekday()))
	for {
		week := make([]calDay, 7)
		for i := range week {
			date := d.Format(gtd.DateLayout)
			cd := calDay{Date: date, Day: d.Day(), InMonth: d.Month() == month.Month(),
				Selected: d.Equal(selected), Today: d.Equal(today)}
			if a := act[date]; a != nil {
				cd.Active = true
				cd.Title = i18n.T(lang, "day.activity", a.Done, a.Logs)
			}
			week[i] = cd
			d = d.AddDate(0, 0, 1)
		}
		c.Weeks = append(c.Weeks, week)
		if d.Month() != month.Month() {
			break
		}
	}
	return c
}

// ---------------------------------------------------------------- API

type dayLogJSON struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`               // raw Markdown
	MovedTo   string `json:"moved_to,omitempty"` // on an automatic pause
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// dayTaskJSON is one task of a group. logs are the entries written that day;
// earlier_logs, present on done items only, are the last few written before
// it (gtd.EarlierLogCount), oldest first.
type dayTaskJSON struct {
	Task        *gtd.Task     `json:"task"`
	CompletedAt string        `json:"completed_at,omitempty"`
	Since       string        `json:"since,omitempty"`
	Logs        []dayLogJSON  `json:"logs"`
	EarlierLogs *[]dayLogJSON `json:"earlier_logs,omitempty"`
}

// dayJSON is GET /api/day. The day's own times (completed_at, since and each
// log entry's) are ISO 8601 in local time; the embedded task keeps the stored
// UTC form, as everywhere else in the API.
type dayJSON struct {
	Date      string `json:"date"`
	Weekday   string `json:"weekday"`
	Timezone  string `json:"timezone"`
	UTCOffset string `json:"utc_offset"`

	// In the order they are shown. Calendar events will go before done.
	Done    []dayTaskJSON `json:"done"`
	Working []dayTaskJSON `json:"working"`
	Worked  []dayTaskJSON `json:"worked"`
	Dropped []dayTaskJSON `json:"dropped"`
}

func dayLogsJSON(ls []*gtd.TaskLog) []dayLogJSON {
	out := make([]dayLogJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, dayLogJSON{ID: l.ID, Kind: l.Kind, Body: l.Body, MovedTo: l.MovedTo,
			CreatedAt: isoLocal(l.CreatedAt), UpdatedAt: isoLocal(l.UpdatedAt)})
	}
	return out
}

// dayTasksJSON is a group; earlier says whether it carries earlier_logs.
func dayTasksJSON(ts []*gtd.DayTask, earlier bool) []dayTaskJSON {
	out := make([]dayTaskJSON, 0, len(ts))
	for _, t := range ts {
		j := dayTaskJSON{Task: t.Task, Logs: dayLogsJSON(t.Logs)}
		if t.CompletedAt != "" {
			j.CompletedAt = isoLocal(t.CompletedAt)
		}
		if t.Since != "" {
			j.Since = isoLocal(t.Since)
		}
		if earlier {
			e := dayLogsJSON(t.EarlierLogs)
			j.EarlierLogs = &e
		}
		out = append(out, j)
	}
	return out
}

// apiDay is GET /api/day?date=YYYY-MM-DD (today by default); format=markdown
// answers the same text the page's copy button copies.
func (s *Server) apiDay(w http.ResponseWriter, r *http.Request) {
	day, ok := dayFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_date", s.tr(r, "day.bad_date"))
		return
	}
	rec, err := s.gtd.Day(ctxOf(r), day)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	switch r.URL.Query().Get("format") {
	case "", "json":
	case "markdown", "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(dayMarkdown(s.langOf(r), day, rec)))
		return
	default:
		writeErr(w, http.StatusBadRequest, "bad_request", "format is json or markdown")
		return
	}
	writeJSON(w, http.StatusOK, dayJSON{
		Date:      rec.Date,
		Weekday:   day.Weekday().String()[:3],
		Timezone:  localZoneName(),
		UTCOffset: day.Format("-07:00"),
		Done:      dayTasksJSON(rec.Done, true),
		Working:   dayTasksJSON(rec.Working, false),
		Worked:    dayTasksJSON(rec.Worked, false),
		Dropped:   dayTasksJSON(rec.Dropped, false),
	})
}

// apiDays is GET /api/days?month=YYYY-MM (this month by default): what
// happened on each local day, for the calendar's dots. Days with nothing are
// absent.
func (s *Server) apiDays(w http.ResponseWriter, r *http.Request) {
	month := time.Now()
	if m := r.URL.Query().Get("month"); m != "" {
		t, err := time.ParseInLocation("2006-01", m, time.Local)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_date", s.tr(r, "day.bad_month"))
			return
		}
		month = t
	}
	act, err := s.gtd.MonthActivity(ctxOf(r), month)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"month": month.Format("2006-01"), "days": act})
}

// ---------------------------------------------------------------- Markdown

// dayMarkdown is the day as plain Markdown, for pasting elsewhere: a heading
// per group, a bullet per task, the day's entries indented under it. A done
// task's entries from before the day follow as a labelled sub-list, each with
// its date. **Keep the shape stable**; people paste it into reports and tools
// read it.
func dayMarkdown(lang i18n.Lang, day time.Time, rec *gtd.DayRecord) string {
	tr := func(key string, args ...any) string { return i18n.T(lang, key, args...) }
	var b strings.Builder
	// entry writes one log entry as a list item at indent: the time, the mark
	// if it is one, then the body, set off by a dash after a mark
	entry := func(indent, when string, l *gtd.TaskLog) {
		b.WriteString(indent + "- " + when)
		sep := " "
		switch {
		case l.Kind == gtd.LogStart:
			b.WriteString(" " + tr("day.md.start"))
			sep = " — "
		case l.Kind == gtd.LogPause && l.MovedTo != "":
			b.WriteString(" " + tr("day.md.pause_moved", tr("move.choice."+l.MovedTo)))
			sep = " — "
		case l.Kind == gtd.LogPause:
			b.WriteString(" " + tr("day.md.pause"))
			sep = " — "
		}
		for i, ln := range strings.Split(l.Body, "\n") {
			switch {
			case i == 0 && ln == "":
			case i == 0:
				b.WriteString(sep + ln)
			case strings.TrimSpace(ln) == "":
				b.WriteString("\n")
			default:
				b.WriteString("\n" + indent + "  " + ln)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("# " + tr("day.title") + " " + rec.Date + " (" + weekdayLabel(lang, day) + ")\n")
	if rec.Empty() {
		b.WriteString("\n" + tr("day.empty") + "\n")
		return b.String()
	}
	// line gets the task's name, its project after a dash when it has one
	group := func(heading string, ts []*gtd.DayTask, line func(t *gtd.DayTask, name string) string) {
		if len(ts) == 0 {
			return
		}
		b.WriteString("\n## " + heading + "\n\n")
		for _, t := range ts {
			name := t.Task.Title
			if t.Task.ProjectTitle != "" {
				name += " — " + t.Task.ProjectTitle
			}
			b.WriteString("- " + line(t, name) + "\n")
			for _, l := range t.Logs {
				entry("  ", clock(l.CreatedAt, day), l)
			}
			if len(t.EarlierLogs) > 0 {
				b.WriteString("  - " + tr("day.earlier") + "\n")
				for _, l := range t.EarlierLogs {
					entry("    ", stamp(l.CreatedAt), l)
				}
			}
		}
	}
	withTime := func(t *gtd.DayTask, name string) string { return clock(t.CompletedAt, day) + " " + name }
	group(tr("day.done"), rec.Done, withTime)
	group(tr("day.working"), rec.Working, func(t *gtd.DayTask, name string) string {
		return name + " (" + tr("day.since", clock(t.Since, day)) + ")"
	})
	group(tr("day.worked"), rec.Worked, func(_ *gtd.DayTask, name string) string { return name })
	group(tr("day.dropped"), rec.Dropped, withTime)
	return b.String()
}
