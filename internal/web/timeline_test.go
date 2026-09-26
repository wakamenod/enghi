package web

import (
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/calendar"
)

// The now marker goes after every event that has started; ended ones are
// dimmed, and only on today.
func TestTimelineNowMarker(t *testing.T) {
	day := time.Date(2026, 9, 26, 0, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	evs := []*calendar.Event{
		{Title: "holiday", Start: day, End: day.AddDate(0, 0, 1), AllDay: true},
		{Title: "ended", Start: at(9, 0), End: at(10, 0)},
		{Title: "running", Start: at(14, 30), End: at(15, 30)},
		{Title: "later", Start: at(16, 0), End: at(17, 0)},
	}
	tl := buildTimeline(day, at(15, 0), evs, true)
	if !tl.IsToday || len(tl.AllDay) != 1 || len(tl.Timed) != 3 || tl.NowAt != 2 {
		t.Fatalf("today: %+v", tl)
	}
	if !tl.Timed[0].Past || tl.Timed[1].Past || tl.Timed[2].Past {
		t.Error("dimming is wrong")
	}
	if tl.Timed[0].Time != "09:00–10:00" || tl.AllDay[0].Time != "" {
		t.Errorf("times: %q %q", tl.Timed[0].Time, tl.AllDay[0].Time)
	}
	if tl := buildTimeline(day, at(8, 0), evs, true); tl.NowAt != 0 {
		t.Errorf("before everything: NowAt = %d", tl.NowAt)
	}
	// Another day: nothing dimmed
	tl = buildTimeline(day, day.AddDate(0, 0, 3), evs, true)
	if tl.IsToday || tl.Timed[0].Past {
		t.Error("a past day is dimmed")
	}
	if tl := buildTimeline(day, at(15, 0), nil, false); tl.Shown {
		t.Error("shown while off and empty")
	}
}
