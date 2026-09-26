package main

import "testing"

// A version from ldflags is shown as it is; only "dev" (go install) gains the
// commit from the build info.
func TestDisplayVersion(t *testing.T) {
	for _, c := range []struct{ v, rev, want string }{
		{"v0.3.3-2-g127f1d4-dirty", "abc", "v0.3.3-2-g127f1d4-dirty"},
		{"0.3.3", "", "0.3.3"},
		{"dev", "127f1d4abcde-dirty", "dev (127f1d4abcde-dirty)"},
		{"dev", "", "dev"},
	} {
		if got := displayVersion(c.v, c.rev); got != c.want {
			t.Errorf("displayVersion(%q, %q) = %q, want %q", c.v, c.rev, got, c.want)
		}
	}
}
