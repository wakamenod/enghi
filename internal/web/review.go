package web

import (
	"context"
	"time"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/i18n"
)

// ReviewData is everything the Weekly Review screen shows.
//
// **No wizard** (DESIGN 2.3). One screen with a checklist, and **everything
// needed to decide laid out on it.** An item that can only be checked by
// navigating away is what makes the review stop happening.
type ReviewData struct {
	Review    *gtd.Review     `json:"review"`
	Checklist []ChecklistItem `json:"checklist"`

	// inbox_zero
	Inbox []*gtd.Task `json:"inbox"`
	// review_next_actions - grouped by context
	Contexts    []*gtd.Context `json:"contexts"`
	NextActions []*gtd.Task    `json:"next_actions"`
	// review_past_calendar
	CompletedLastWeek []*gtd.Task `json:"completed_last_week"`
	// The same week, a link to each day's work record. Screen only.
	PastDays []reviewDay `json:"-"`
	// review_upcoming_calendar
	Upcoming []*gtd.Task `json:"upcoming"`
	// review_waiting_for
	Waiting []*gtd.Task `json:"waiting"`
	// **review_projects - the stalled-project detection (DESIGN 2.4)**
	Stalled []*gtd.Project `json:"stalled_projects"`
	// **review_someday - someday projects whose review date has come**
	SomedayDue []*gtd.Project `json:"someday_due_review"`
	// review_recurring - the list of recurring series (DESIGN 2.6)
	Series []gtd.Series `json:"series"`
}

// reviewDay is one day of the past week, linking to /gtd/day/{date}.
type reviewDay struct {
	Date  string
	Label string
}

// ChecklistItem is a checklist item as displayed.
type ChecklistItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Data    string `json:"data"`
	Checked bool   `json:"checked"`
}

func (s *Server) reviewData(ctx context.Context, lang i18n.Lang) (*ReviewData, error) {
	d := &ReviewData{}
	var err error

	if d.Review, err = s.gtd.CurrentReview(ctx); err != nil {
		return nil, err
	}
	// Labels come from i18n. **No Japanese lives on the gtd side.**
	for _, c := range gtd.ChecklistKeys {
		d.Checklist = append(d.Checklist, ChecklistItem{
			Key:     c.Key,
			Label:   i18n.T(lang, "checklist."+c.Key),
			Data:    i18n.T(lang, "checklist.data."+c.Key),
			Checked: d.Review.Checklist[c.Key],
		})
	}

	if d.Inbox, err = s.gtd.Inbox(ctx); err != nil {
		return nil, err
	}
	if d.Contexts, err = s.gtd.Contexts(ctx); err != nil {
		return nil, err
	}
	if d.NextActions, err = s.gtd.NextActions(ctx, nil); err != nil {
		return nil, err
	}

	today := gtd.Today()
	past := reviewPastDays(today)
	if d.CompletedLastWeek, err = s.gtd.CompletedBetween(ctx,
		gtd.FormatDate(past[0]), gtd.FormatDate(past[len(past)-1])); err != nil {
		return nil, err
	}
	for _, day := range past {
		d.PastDays = append(d.PastDays, reviewDay{Date: gtd.FormatDate(day),
			Label: day.Format("01/02") + " " + i18n.T(lang, dayWeekdayKeys[day.Weekday()])})
	}
	// The next two weeks
	if d.Upcoming, err = s.gtd.UpcomingBetween(ctx,
		gtd.FormatDate(today), gtd.FormatDate(today.AddDate(0, 0, 14))); err != nil {
		return nil, err
	}
	if d.Waiting, err = s.gtd.Waiting(ctx); err != nil {
		return nil, err
	}
	// These two are tied directly to why this system exists. **Cut anything
	// else, but never these.**
	if d.Stalled, err = s.gtd.StalledProjects(ctx); err != nil {
		return nil, err
	}
	if d.SomedayDue, err = s.gtd.SomedayDueReview(ctx); err != nil {
		return nil, err
	}
	if d.Series, err = s.gtd.SeriesList(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

// reviewPastDays are the days the look back covers, oldest first: the seven
// days before today. **The completions listed and the links to each day's
// record both come from here**, so they always cover the same days. Today is
// not in it; it is still going, and the day page and dashboard show it.
func reviewPastDays(today time.Time) []time.Time {
	out := make([]time.Time, 0, 7)
	for i := 7; i >= 1; i-- {
		out = append(out, today.AddDate(0, 0, -i))
	}
	return out
}
