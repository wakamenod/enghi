// Package gtd handles areas, projects, contexts, tasks and reviews.
package gtd

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateLayout is the format of date columns: 'YYYY-MM-DD'.
const DateLayout = "2006-01-02"

// ParseDate reads 'YYYY-MM-DD'. An empty string gives the zero value.
func ParseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(DateLayout, s)
}

// FormatDate renders 'YYYY-MM-DD'.
func FormatDate(t time.Time) string { return t.Format(DateLayout) }

// Today is the local date with the time dropped.
func Today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// Recurrence is a parsed org-mode repeater (DESIGN 2.6).
//
//	+1d +2w +1m +1y   fixed interval: previous scheduled date + interval, once
//	++1w              fixed interval, added repeatedly until it is past today
//	.+3d              from the completion date: completed + interval
//	weekly:mon,thu    the given weekdays, every week
//	monthly:25        the 25th of every month
//	monthly:last      the last day of every month
//	yearly:04-01      once a year
type Recurrence struct {
	Raw string

	Kind     string // interval / weekly / monthly / yearly
	Catchup  bool   // ++ : advance until it is past today
	FromDone bool   // .+ : count from the completion date

	N    int  // the number in the interval
	Unit byte // d / w / m / y
	Days []time.Weekday
	Dom  int  // the 25 of monthly:25
	Last bool // monthly:last
	Mon  int  // the 4 of yearly:04-01
	MDay int  // the 1 of yearly:04-01
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseRecurrence parses the string in the recurrence column.
func ParseRecurrence(s string) (*Recurrence, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil, fmt.Errorf("the recurrence rule is empty")
	}
	r := &Recurrence{Raw: s}

	switch {
	case strings.HasPrefix(s, "weekly:"):
		r.Kind = "weekly"
		for _, name := range strings.Split(strings.TrimPrefix(s, "weekly:"), ",") {
			wd, ok := weekdayNames[strings.TrimSpace(name)]
			if !ok {
				return nil, fmt.Errorf("cannot read the weekday: %q", name)
			}
			r.Days = append(r.Days, wd)
		}
		if len(r.Days) == 0 {
			return nil, fmt.Errorf("weekly: has no weekday")
		}
		return r, nil

	case strings.HasPrefix(s, "monthly:"):
		r.Kind = "monthly"
		arg := strings.TrimSpace(strings.TrimPrefix(s, "monthly:"))
		if arg == "last" {
			r.Last = true
			return r, nil
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > 31 {
			return nil, fmt.Errorf("cannot read the day of monthly:: %q", arg)
		}
		r.Dom = n
		return r, nil

	case strings.HasPrefix(s, "yearly:"):
		r.Kind = "yearly"
		arg := strings.TrimPrefix(s, "yearly:")
		parts := strings.Split(arg, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("yearly: must be MM-DD: %q", arg)
		}
		mon, err1 := strconv.Atoi(parts[0])
		day, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || mon < 1 || mon > 12 || day < 1 || day > 31 {
			return nil, fmt.Errorf("cannot read the month and day of yearly:: %q", arg)
		}
		r.Mon, r.MDay = mon, day
		return r, nil
	}

	// Interval forms: +1d / ++1w / .+3d
	rest := s
	switch {
	case strings.HasPrefix(rest, ".+"):
		r.FromDone = true
		rest = rest[2:]
	case strings.HasPrefix(rest, "++"):
		r.Catchup = true
		rest = rest[2:]
	case strings.HasPrefix(rest, "+"):
		rest = rest[1:]
	default:
		return nil, fmt.Errorf("cannot read the recurrence rule: %q", s)
	}
	if len(rest) < 2 {
		return nil, fmt.Errorf("cannot read the recurrence rule: %q", s)
	}
	unit := rest[len(rest)-1]
	if unit != 'd' && unit != 'w' && unit != 'm' && unit != 'y' {
		return nil, fmt.Errorf("the unit must be one of d/w/m/y: %q", s)
	}
	n, err := strconv.Atoi(rest[:len(rest)-1])
	if err != nil || n < 1 {
		return nil, fmt.Errorf("cannot read the interval: %q", s)
	}
	r.Kind, r.N, r.Unit = "interval", n, unit
	return r, nil
}

// Next computes the next scheduled date.
//
//	scheduled ... scheduled_on of the current instance (zero if absent)
//	completed ... the day it was completed (or skipped)
//	today     ... today
//
// **A date that does not exist (a month without a 31st, a leap day) is clamped
// to the last day of that month** (DESIGN 2.6).
func (r *Recurrence) Next(scheduled, completed, today time.Time) time.Time {
	switch r.Kind {
	case "interval":
		base := scheduled
		if r.FromDone || base.IsZero() {
			// .+ counts from the completion date, which also stands in when there
			// is no scheduled date
			base = completed
		}
		if base.IsZero() {
			base = today
		}
		next := addInterval(base, r.N, r.Unit)
		if r.Catchup {
			// **Add the interval repeatedly until it is past today.**
			// This is what catches a long-neglected fixed-date task up to now.
			for !next.After(today) {
				next = addInterval(next, r.N, r.Unit)
			}
		}
		return next

	case "weekly":
		base := laterOf(scheduled, today)
		for i := 1; i <= 7; i++ {
			d := base.AddDate(0, 0, i)
			for _, wd := range r.Days {
				if d.Weekday() == wd {
					return d
				}
			}
		}
		return base.AddDate(0, 0, 7) // unreachable, kept as a safety net

	case "monthly":
		base := laterOf(scheduled, today)
		// This month's candidate if it is after base, otherwise next month
		cand := monthlyCandidate(base.Year(), base.Month(), r)
		if !cand.After(base) {
			y, m := nextMonth(base.Year(), base.Month())
			cand = monthlyCandidate(y, m, r)
		}
		return cand

	case "yearly":
		base := laterOf(scheduled, today)
		cand := clampDay(base.Year(), time.Month(r.Mon), r.MDay)
		if !cand.After(base) {
			cand = clampDay(base.Year()+1, time.Month(r.Mon), r.MDay)
		}
		return cand
	}
	return today
}

func addInterval(t time.Time, n int, unit byte) time.Time {
	switch unit {
	case 'd':
		return t.AddDate(0, 0, n)
	case 'w':
		return t.AddDate(0, 0, 7*n)
	case 'm':
		return addMonthsClamped(t, n)
	case 'y':
		return addMonthsClamped(t, 12*n)
	}
	return t
}

// addMonthsClamped adds months. **AddDate cannot be used: it turns 1/31 plus a
// month into 3/3.** A date that does not exist is clamped to the last day of
// that month (1/31 plus a month = 2/28).
func addMonthsClamped(t time.Time, months int) time.Time {
	y := t.Year()
	m := int(t.Month()) - 1 + months
	y += m / 12
	m = m % 12
	if m < 0 {
		m += 12
		y--
	}
	return clampDay(y, time.Month(m+1), t.Day())
}

// clampDay builds year y, month m, day d, clamping d to the end of the month
// when that day does not exist.
func clampDay(y int, m time.Month, d int) time.Time {
	last := daysInMonth(y, m)
	if d > last {
		d = last
	}
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func monthlyCandidate(y int, m time.Month, r *Recurrence) time.Time {
	if r.Last {
		return time.Date(y, m, daysInMonth(y, m), 0, 0, 0, 0, time.UTC)
	}
	return clampDay(y, m, r.Dom)
}

func nextMonth(y int, m time.Month) (int, time.Month) {
	if m == time.December {
		return y + 1, time.January
	}
	return y, m + 1
}

func laterOf(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if a.After(b) {
		return a
	}
	return b
}
