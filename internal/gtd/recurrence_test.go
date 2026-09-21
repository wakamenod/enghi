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

// +1w と .+1w の違いは実用上とても重要(DESIGN 2.6)。
// ゴミ出しは曜日が固定、シーツの洗濯はやった日から2週間後。
func TestFixedIntervalVsFromCompletion(t *testing.T) {
	// 4/1 の予定を 4/5 に完了した場合
	// +1w は **予定日** 基準 → 4/8
	if got := next(t, "+1w", "2025-04-01", "2025-04-05", "2025-04-05"); got != "2025-04-08" {
		t.Errorf("+1w = %s, want 2025-04-08(予定日基準)", got)
	}
	// .+1w は **完了日** 基準 → 4/12
	if got := next(t, ".+1w", "2025-04-01", "2025-04-05", "2025-04-05"); got != "2025-04-12" {
		t.Errorf(".+1w = %s, want 2025-04-12(完了日基準)", got)
	}
}

// ++ は「溜めずに次に進む」ためのもの。長期放置を現在まで一気に追いつかせる。
func TestCatchupAdvancesPastToday(t *testing.T) {
	// 1/1 の予定を半年放置して 7/10 に処理した
	got := next(t, "++1w", "2025-01-01", "2025-07-10", "2025-07-10")
	if got != "2025-07-16" {
		t.Errorf("++1w = %s, want 2025-07-16(今日より後の最初の水曜)", got)
	}
	// 素の +1w は1回分しか進めない(過去のまま)
	if got := next(t, "+1w", "2025-01-01", "2025-07-10", "2025-07-10"); got != "2025-01-08" {
		t.Errorf("+1w = %s, want 2025-01-08(1回分だけ進める)", got)
	}
}

// 存在しない日付はその月の最終日に丸める。
func TestMonthEndClamping(t *testing.T) {
	cases := []struct{ rule, sched, want string }{
		// 1/31 + 1ヶ月 = 2/28(3/3 に繰り上げない)
		{"+1m", "2025-01-31", "2025-02-28"},
		// 閏年は 2/29
		{"+1m", "2024-01-31", "2024-02-29"},
		// 3/31 + 1ヶ月 = 4/30
		{"+1m", "2025-03-31", "2025-04-30"},
		// 2/29 + 1年 = 2/28(平年)
		{"+1y", "2024-02-29", "2025-02-28"},
		// 12月をまたぐ
		{"+1m", "2025-12-31", "2026-01-31"},
		{"+2m", "2025-12-31", "2026-02-28"},
	}
	for _, c := range cases {
		if got := next(t, c.rule, c.sched, c.sched, c.sched); got != c.want {
			t.Errorf("%s from %s = %s, want %s", c.rule, c.sched, got, c.want)
		}
	}
}

// weekly:tue,fri のゴミ出し。
func TestWeekly(t *testing.T) {
	// 2025-04-01 は火曜。完了したら次は金曜
	if got := next(t, "weekly:tue,fri", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2025-04-04" {
		t.Errorf("火曜に完了 → %s, want 2025-04-04(金)", got)
	}
	// 金曜に完了したら次は火曜
	if got := next(t, "weekly:tue,fri", "2025-04-04", "2025-04-04", "2025-04-04"); got != "2025-04-08" {
		t.Errorf("金曜に完了 → %s, want 2025-04-08(火)", got)
	}
	// 単一曜日
	if got := next(t, "weekly:mon", "2025-04-07", "2025-04-07", "2025-04-07"); got != "2025-04-14" {
		t.Errorf("weekly:mon = %s, want 2025-04-14", got)
	}
	// 遅れて処理した場合も、過去の日付を返さない
	if got := next(t, "weekly:tue", "2025-04-01", "2025-04-10", "2025-04-10"); got != "2025-04-15" {
		t.Errorf("遅れて処理 → %s, want 2025-04-15(今日より後)", got)
	}
}

// monthly:25 の経費精算、monthly:last の月末。
func TestMonthly(t *testing.T) {
	if got := next(t, "monthly:25", "2025-04-25", "2025-04-25", "2025-04-25"); got != "2025-05-25" {
		t.Errorf("monthly:25 = %s, want 2025-05-25", got)
	}
	// 月初に処理したら当月の25日
	if got := next(t, "monthly:25", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2025-04-25" {
		t.Errorf("monthly:25 = %s, want 2025-04-25", got)
	}
	// 31日指定で2月をまたぐ → 月末に丸める
	if got := next(t, "monthly:31", "2025-01-31", "2025-01-31", "2025-01-31"); got != "2025-02-28" {
		t.Errorf("monthly:31 = %s, want 2025-02-28", got)
	}
	// 月末
	if got := next(t, "monthly:last", "2025-01-31", "2025-01-31", "2025-01-31"); got != "2025-02-28" {
		t.Errorf("monthly:last = %s, want 2025-02-28", got)
	}
	if got := next(t, "monthly:last", "2024-01-31", "2024-01-31", "2024-01-31"); got != "2024-02-29" {
		t.Errorf("monthly:last(閏年) = %s, want 2024-02-29", got)
	}
	// 12月末 → 1月末
	if got := next(t, "monthly:last", "2025-12-31", "2025-12-31", "2025-12-31"); got != "2026-01-31" {
		t.Errorf("monthly:last = %s, want 2026-01-31", got)
	}
}

func TestYearly(t *testing.T) {
	if got := next(t, "yearly:04-01", "2025-04-01", "2025-04-01", "2025-04-01"); got != "2026-04-01" {
		t.Errorf("yearly:04-01 = %s, want 2026-04-01", got)
	}
	// 年初に処理したら当年の4/1
	if got := next(t, "yearly:04-01", "2025-01-10", "2025-01-10", "2025-01-10"); got != "2025-04-01" {
		t.Errorf("yearly:04-01 = %s, want 2025-04-01", got)
	}
	// 2/29 指定は平年だと 2/28 に丸める
	if got := next(t, "yearly:02-29", "2024-02-29", "2024-02-29", "2024-02-29"); got != "2025-02-28" {
		t.Errorf("yearly:02-29 = %s, want 2025-02-28", got)
	}
}

// 予定日が無いタスク(inbox から直接 done にした等)でも壊れないこと。
func TestNoScheduledDate(t *testing.T) {
	if got := next(t, "+3d", "", "2025-04-05", "2025-04-05"); got != "2025-04-08" {
		t.Errorf("予定日なし +3d = %s, want 2025-04-08", got)
	}
	if got := next(t, "weekly:mon", "", "2025-04-01", "2025-04-01"); got != "2025-04-07" {
		t.Errorf("予定日なし weekly:mon = %s, want 2025-04-07", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "1w", "+w", "+0d", "+1x", "weekly:", "weekly:xxx",
		"monthly:0", "monthly:32", "monthly:abc", "yearly:13-01", "yearly:0401", "なにか"} {
		if _, err := gtd.ParseRecurrence(bad); err == nil {
			t.Errorf("ParseRecurrence(%q) がエラーにならない", bad)
		}
	}
	for _, good := range []string{"+1d", "+2w", "+1m", "+1y", "++1w", ".+3d",
		"weekly:mon,thu", "WEEKLY:MON", "monthly:25", "monthly:last", "yearly:04-01"} {
		if _, err := gtd.ParseRecurrence(good); err != nil {
			t.Errorf("ParseRecurrence(%q): %v", good, err)
		}
	}
}
