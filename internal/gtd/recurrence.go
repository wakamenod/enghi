// Package gtd は area / project / context / task / review を扱う。
package gtd

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateLayout は日付列の形式。'YYYY-MM-DD'。
const DateLayout = "2006-01-02"

// ParseDate は 'YYYY-MM-DD' を読む。空文字は零値。
func ParseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(DateLayout, s)
}

// FormatDate は 'YYYY-MM-DD' にする。
func FormatDate(t time.Time) string { return t.Format(DateLayout) }

// Today はローカルの今日(時刻を落としたもの)。
func Today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// Recurrence は org-mode のリピータ記法を解釈したもの(DESIGN 2.6)。
//
//	+1d +2w +1m +1y   固定間隔。前回の予定日 + 間隔。1回分だけ進める
//	++1w              固定間隔。前回の予定日 + 間隔を、今日より後になるまで繰り返し加算
//	.+3d              完了日基準。完了した日 + 間隔
//	weekly:mon,thu    毎週の指定曜日(複数可)
//	monthly:25        毎月25日
//	monthly:last      毎月末
//	yearly:04-01      毎年
type Recurrence struct {
	Raw string

	Kind     string // interval / weekly / monthly / yearly
	Catchup  bool   // ++ 。今日より後になるまで進める
	FromDone bool   // .+ 。完了日を基準にする

	N    int  // 間隔の数
	Unit byte // d / w / m / y
	Days []time.Weekday
	Dom  int  // monthly:25 の 25
	Last bool // monthly:last
	Mon  int  // yearly:04-01 の 4
	MDay int  // yearly:04-01 の 1
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseRecurrence は recurrence 列の文字列を解釈する。
func ParseRecurrence(s string) (*Recurrence, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil, fmt.Errorf("繰り返し規則が空です")
	}
	r := &Recurrence{Raw: s}

	switch {
	case strings.HasPrefix(s, "weekly:"):
		r.Kind = "weekly"
		for _, name := range strings.Split(strings.TrimPrefix(s, "weekly:"), ",") {
			wd, ok := weekdayNames[strings.TrimSpace(name)]
			if !ok {
				return nil, fmt.Errorf("曜日が読めません: %q", name)
			}
			r.Days = append(r.Days, wd)
		}
		if len(r.Days) == 0 {
			return nil, fmt.Errorf("weekly: に曜日がありません")
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
			return nil, fmt.Errorf("monthly: の日が読めません: %q", arg)
		}
		r.Dom = n
		return r, nil

	case strings.HasPrefix(s, "yearly:"):
		r.Kind = "yearly"
		arg := strings.TrimPrefix(s, "yearly:")
		parts := strings.Split(arg, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("yearly: は MM-DD の形式です: %q", arg)
		}
		mon, err1 := strconv.Atoi(parts[0])
		day, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || mon < 1 || mon > 12 || day < 1 || day > 31 {
			return nil, fmt.Errorf("yearly: の月日が読めません: %q", arg)
		}
		r.Mon, r.MDay = mon, day
		return r, nil
	}

	// 間隔系: +1d / ++1w / .+3d
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
		return nil, fmt.Errorf("繰り返し規則が読めません: %q", s)
	}
	if len(rest) < 2 {
		return nil, fmt.Errorf("繰り返し規則が読めません: %q", s)
	}
	unit := rest[len(rest)-1]
	if unit != 'd' && unit != 'w' && unit != 'm' && unit != 'y' {
		return nil, fmt.Errorf("単位は d/w/m/y のいずれかです: %q", s)
	}
	n, err := strconv.Atoi(rest[:len(rest)-1])
	if err != nil || n < 1 {
		return nil, fmt.Errorf("間隔が読めません: %q", s)
	}
	r.Kind, r.N, r.Unit = "interval", n, unit
	return r, nil
}

// Next は次回の予定日を計算する。
//
//	scheduled … 現インスタンスの scheduled_on(無ければ零値)
//	completed … 完了(または skip)した日
//	today     … 今日
//
// **存在しない日付(31日の無い月、閏日)はその月の最終日に丸める**(DESIGN 2.6)。
func (r *Recurrence) Next(scheduled, completed, today time.Time) time.Time {
	switch r.Kind {
	case "interval":
		base := scheduled
		if r.FromDone || base.IsZero() {
			// .+ は完了日基準。予定日が無い場合も完了日で代用する
			base = completed
		}
		if base.IsZero() {
			base = today
		}
		next := addInterval(base, r.N, r.Unit)
		if r.Catchup {
			// **今日より後になるまで繰り返し加算する。**
			// 長期放置した固定日タスクを現在まで一気に追いつかせるためのもの。
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
		return base.AddDate(0, 0, 7) // 到達しないが保険

	case "monthly":
		base := laterOf(scheduled, today)
		// 当月の候補が base より後ならそれ、さもなくば翌月
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

// addMonthsClamped は月を足す。**AddDate は 1/31 + 1ヶ月 を 3/3 に繰り上げてしまうので使えない。**
// 存在しない日付はその月の最終日に丸める(1/31 + 1ヶ月 = 2/28)。
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

// clampDay は y年m月d日を作る。d がその月に無ければ月末に丸める。
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
