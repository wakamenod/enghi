package web

import (
	"testing"
	"time"
)

// Stored timestamps are UTC; the screens show local time.
func TestShortTimeShowsLocalTime(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	time.Local = time.FixedZone("JST", 9*60*60)

	year := time.Now().Year()
	for _, c := range []struct{ in, want string }{
		// 21:07 UTC is 06:07 the next morning in Japan
		{time.Date(year, 9, 24, 21, 7, 0, 0, time.UTC).Format("2006-01-02 15:04:05"), "09/25 06:07"},
		{"2020-12-31 20:00:00", "2021/01/01"},
		{"not a time", "not a time"},
	} {
		if got := shortTime(c.in); got != c.want {
			t.Errorf("shortTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
