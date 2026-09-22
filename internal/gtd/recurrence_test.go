package gtd_test

import (
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/gtd"
)

func d(s string) time.Time {
	t, err := time.Parse(gtd.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func next(t *testing.T, rule, scheduled, completed, today string) string {
	t.Helper()
	r, err := gtd.ParseRecurrence(rule)
	if err != nil {
		t.Fatalf("ParseRecurrence(%q): %v", rule, err)
	}
	var s time.Time
	if scheduled != "" {
		s = d(scheduled)
	}
	return gtd.FormatDate(r.Next(s, d(completed), d(today)))
}

// The difference between +1w and .+1w matters a lot in practice (DESIGN 2.6):
// bin day is fixed to a weekday, washing the sheets is two weeks after you last
// did it.
func TestFixedIntervalVsFromCompletion(t *testing.T) {
	// Scheduled for 4/1, completed on 4/5
	// +1w counts from the **scheduled date** -> 4/8
	if got := next(t, "+1w", "2025-04-01", "2025-04-05", "2025-04-05"); got != "2025-04-08" {
		t.Errorf("+1w = %s, want 2025-04-08 (from the scheduled date)", got)
	}
	// .+1w counts from the **completion date** -> 4/12
	if got := next(t, ".+1w", "2025-04-01", "2025-04-05", "2025-04-05"); got != "2025-04-12" {
		t.Errorf(".+1w = %s, want 2025-04-12 (from the completion date)", got)
	}
}

// ++ exists to move on without piling up: it catches long neglect up to now.
func TestCatchupAdvancesPastToday(t *testing.T) {
	// Scheduled for 1/1, left for half a year, handled on 7/10
	got := next(t, "++1w", "2025-01-01", "2025-07-10", "2025-07-10")
	if got != "2025-07-16" {
		t.Errorf("++1w = %s, want 2025-07-16 (the first Wednesday after today)", got)
	}
	// Plain +1w advances once only, staying in the past
	if got := next(t, "+1w", "2025-01-01", "2025-07-10", "2025-07-10"); got != "2025-01-08" {
		t.Errorf("+1w = %s, want 2025-01-08 (advance once only)", got)
	}
}

// A date that does not exist is clamped to the last day of that month.
func TestMonthEndClamping(t *testing.T) {
	cases := []struct{ rule, sched, want string }{
		// 1/31 plus a month = 2/28, not 3/3
		{"+1m", "2025-01-31", "2025-02-28"},
		// 2/29 in a leap year
		{"+1m", "2024-01-31", "2024-02-29"},
		// 3/31 plus a month = 4/30
		{"+1m", "2025-03-31", "2025-04-30"},
		// 2/29 plus a year = 2/28 in a common year
		{"+1y", "2024-02-29", "2025-02-28"},
		// Crossing December
		{"+1m", "2025-12-31", "2026-01-31"},
		{"+2m", "2025-12-31", "2026-02-28"},
	}
	for _, c := range cases {
		if got := next(t, c.rule, c.sched, c.sched, c.sched); got != c.want {
			t.Errorf("%s from %s = %s, want %s", c.rule, c.sched, got, c.want)
		}
	}
}

// Bin day, weekly:tue,fri.
func TestWeekly(t *testing.T) {
	// 2025-04-01 is a Tuesday; completing it gives Friday next
	if got := next(t, "weekly:tue,fri", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2025-04-04" {
		t.Errorf("completed on Tuesday -> %s, want 2025-04-04 (Friday)", got)
	}
	// Completing on Friday gives Tuesday next
	if got := next(t, "weekly:tue,fri", "2025-04-04", "2025-04-04", "2025-04-04"); got != "2025-04-08" {
		t.Errorf("completed on Friday -> %s, want 2025-04-08 (Tuesday)", got)
	}
	// A single weekday
	if got := next(t, "weekly:mon", "2025-04-07", "2025-04-07", "2025-04-07"); got != "2025-04-14" {
		t.Errorf("weekly:mon = %s, want 2025-04-14", got)
	}
	// Handled late, it still never returns a date in the past
	if got := next(t, "weekly:tue", "2025-04-01", "2025-04-10", "2025-04-10"); got != "2025-04-15" {
		t.Errorf("handled late -> %s, want 2025-04-15 (after today)", got)
	}
}

// Expenses on monthly:25, and month end with monthly:last.
func TestMonthly(t *testing.T) {
	if got := next(t, "monthly:25", "2025-04-25", "2025-04-25", "2025-04-25"); got != "2025-05-25" {
		t.Errorf("monthly:25 = %s, want 2025-05-25", got)
	}
	// Handled early in the month gives the 25th of the same month
	if got := next(t, "monthly:25", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2025-04-25" {
		t.Errorf("monthly:25 = %s, want 2025-04-25", got)
	}
	// Asking for the 31st across February clamps to the end of the month
	if got := next(t, "monthly:31", "2025-01-31", "2025-01-31", "2025-01-31"); got != "2025-02-28" {
		t.Errorf("monthly:31 = %s, want 2025-02-28", got)
	}
	// Month end
	if got := next(t, "monthly:last", "2025-01-31", "2025-01-31", "2025-01-31"); got != "2025-02-28" {
		t.Errorf("monthly:last = %s, want 2025-02-28", got)
	}
	if got := next(t, "monthly:last", "2024-01-31", "2024-01-31", "2024-01-31"); got != "2024-02-29" {
		t.Errorf("monthly:last in a leap year = %s, want 2024-02-29", got)
	}
	// End of December -> end of January
	if got := next(t, "monthly:last", "2025-12-31", "2025-12-31", "2025-12-31"); got != "2026-01-31" {
		t.Errorf("monthly:last = %s, want 2026-01-31", got)
	}
}

func TestYearly(t *testing.T) {
	if got := next(t, "yearly:04-01", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2026-04-01" {
		t.Errorf("yearly:04-01 = %s, want 2026-04-01", got)
	}
	// Handled at the start of the year gives 4/1 of that year
	if got := next(t, "yearly:04-01", "2025-01-10", "2025-01-10", "2025-01-10"); got != "2025-04-01" {
		t.Errorf("yearly:04-01 = %s, want 2025-04-01", got)
	}
	// 2/29 clamps to 2/28 in a common year
	if got := next(t, "yearly:02-29", "2024-02-29", "2024-02-29", "2024-02-29"); got != "2025-02-28" {
		t.Errorf("yearly:02-29 = %s, want 2025-02-28", got)
	}
}

// Tasks without a scheduled date - marked done straight from the inbox, say -
// must not break this.
func TestNoScheduledDate(t *testing.T) {
	if got := next(t, "+3d", "", "2025-04-05", "2025-04-05"); got != "2025-04-08" {
		t.Errorf("no scheduled date, +3d = %s, want 2025-04-08", got)
	}
	if got := next(t, "weekly:mon", "", "2025-04-01", "2025-04-01"); got != "2025-04-07" {
		t.Errorf("no scheduled date, weekly:mon = %s, want 2025-04-07", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "1w", "+w", "+0d", "+1x", "weekly:", "weekly:xxx",
		"monthly:0", "monthly:32", "monthly:abc", "yearly:13-01", "yearly:0401", "なにか"} {
		if _, err := gtd.ParseRecurrence(bad); err == nil {
			t.Errorf("ParseRecurrence(%q) did not fail", bad)
		}
	}
	for _, good := range []string{"+1d", "+2w", "+1m", "+1y", "++1w", ".+3d",
		"weekly:mon,thu", "WEEKLY:MON", "monthly:25", "monthly:last", "yearly:04-01"} {
		if _, err := gtd.ParseRecurrence(good); err != nil {
			t.Errorf("ParseRecurrence(%q): %v", good, err)
		}
	}
}
