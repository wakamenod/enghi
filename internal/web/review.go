package web

import (
	"context"

	"github.com/wakamenod/enghi/internal/gtd"
)

// ReviewData は Weekly Review 画面に出すすべて。
//
// **ウィザードは作らない**(DESIGN 2.3)。チェックリスト付きの画面を1枚用意し、
// **その画面に判断材料をすべて並べる。**
// 別画面に移動しないと確認できない項目があると、レビューが続かなくなる。
type ReviewData struct {
	Review    *gtd.Review     `json:"review"`
	Checklist []ChecklistItem `json:"checklist"`

	// inbox_zero
	Inbox []*gtd.Task `json:"inbox"`
	// review_next_actions — コンテキスト別
	Contexts    []*gtd.Context `json:"contexts"`
	NextActions []*gtd.Task    `json:"next_actions"`
	// review_past_calendar
	CompletedLastWeek []*gtd.Task `json:"completed_last_week"`
	// review_upcoming_calendar
	Upcoming []*gtd.Task `json:"upcoming"`
	// review_waiting_for
	Waiting []*gtd.Task `json:"waiting"`
	// **review_projects — 停滞プロジェクト検出の結果(DESIGN 2.4)**
	Stalled []*gtd.Project `json:"stalled_projects"`
	// **review_someday — 再検討日が到来した someday プロジェクト**
	SomedayDue []*gtd.Project `json:"someday_due_review"`
	// review_recurring — 定期タスク系列の一覧(DESIGN 2.6)
	Series []gtd.Series `json:"series"`
}

// ChecklistItem は表示用のチェック項目。
type ChecklistItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Data    string `json:"data"`
	Checked bool   `json:"checked"`
}

func (s *Server) reviewData(ctx context.Context) (*ReviewData, error) {
	d := &ReviewData{}
	var err error

	if d.Review, err = s.gtd.CurrentReview(ctx); err != nil {
		return nil, err
	}
	for _, c := range gtd.ChecklistKeys {
		d.Checklist = append(d.Checklist, ChecklistItem{
			Key: c.Key, Label: c.Label, Data: c.Data, Checked: d.Review.Checklist[c.Key],
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
	lastWeek := gtd.FormatDate(today.AddDate(0, 0, -7))
	if d.CompletedLastWeek, err = s.gtd.CompletedBetween(ctx, lastWeek, gtd.FormatDate(today)); err != nil {
		return nil, err
	}
	// 今後2週間
	if d.Upcoming, err = s.gtd.UpcomingBetween(ctx,
		gtd.FormatDate(today), gtd.FormatDate(today.AddDate(0, 0, 14))); err != nil {
		return nil, err
	}
	if d.Waiting, err = s.gtd.Waiting(ctx); err != nil {
		return nil, err
	}
	// この2項目がこのシステムの存在理由に直結する。**他を削ってもこの2つは削らないこと。**
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
