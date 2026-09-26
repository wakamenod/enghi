package web

import (
	"testing"
	"time"

	"github.com/wakamenod/enghi/internal/i18n"
)

// A working task's start and how long ago it was, as the dashboard shows them.
func TestSinceClockAndElapsed(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	time.Local = time.FixedZone("JST", 9*60*60)

	now := time.Date(2026, 9, 26, 12, 47, 30, 0, time.Local)
	for _, c := range []struct {
		since, clock, en, ja string
	}{
		{"2026-09-26T10:42:00+09:00", "10:42", "2h 5m", "2時間5分"},
		{"2026-09-26T12:47:00+09:00", "12:47", "0m", "0分"},
		// 15:30 UTC the day before is 00:30 today in Japan
		{"2026-09-25T15:30:00Z", "00:30", "12h 17m", "12時間17分"},
		// 14:59 UTC is 23:59 the day before
		{"2026-09-25T14:59:00Z", "09/25 23:59", "12h 48m", "12時間48分"},
		{"2026-09-24T10:42:00+09:00", "09/24 10:42", "2d 2h", "2日2時間"},
		// A clock that has gone back shows no negative time
		{"2026-09-26T12:50:00+09:00", "12:50", "0m", "0分"},
	} {
		if got := sinceClock(c.since, now); got != c.clock {
			t.Errorf("sinceClock(%s) = %q, want %q", c.since, got, c.clock)
		}
		if got := elapsed(i18n.EN, c.since, now); got != c.en {
			t.Errorf("elapsed(en, %s) = %q, want %q", c.since, got, c.en)
		}
		if got := elapsed(i18n.JA, c.since, now); got != c.ja {
			t.Errorf("elapsed(ja, %s) = %q, want %q", c.since, got, c.ja)
		}
	}
}
