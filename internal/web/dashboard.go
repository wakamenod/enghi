package web

import (
	"context"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Dashboard は / と GET /api/dashboard が返すもの(DESIGN 5)。
// 上段が GTD、下段が Wiki。**GTD を使っていなければ上段は自然に空になる。**
type Dashboard struct {
	GTD  GTDSummary  `json:"gtd"`
	Wiki WikiSummary `json:"wiki"`
}

// GTDSummary は上段(DESIGN 5)。**GTD を使っていなければ自然に空になる。**
type GTDSummary struct {
	// 1. Inbox 件数(0 でないときだけ強調する)
	InboxCount int `json:"inbox_count"`
	// 2. 今日の Next Actions(deadline_on <= today または scheduled_on <= today)
	Today []*gtd.Task `json:"today"`
	// 3. コンテキスト別の Next Action 件数
	Contexts []*gtd.Context `json:"contexts"`
	// 4. **停滞プロジェクト(Next Action が無いもの)** — DESIGN 2.4
	StalledProjects []*gtd.Project `json:"stalled_projects"`
	// 5. Waiting For のうち委譲から一定日数が経過したもの(既定 7 日)
	WaitingOverdue []*gtd.Task `json:"waiting_overdue"`
	// 6. 再検討日が到来した Someday プロジェクト
	SomedayDueReview []*gtd.Project `json:"someday_due_review"`

	Enabled bool `json:"enabled"` // GTD のデータが1件でもあるか
}

// WikiSummary は下段。
type WikiSummary struct {
	TotalPages   int               `json:"total_pages"`
	RecentUpdate []*wiki.Page      `json:"recently_updated"`
	RecentCreate []*wiki.Page      `json:"recently_created"`
	Unresolved   []wiki.Unresolved `json:"unresolved_links"`
	Tags         []wiki.TagCount   `json:"tags"`
}

// dashboardData は必要な集計をまとめて取る。
func (s *Server) dashboardData(ctx context.Context) (*Dashboard, error) {
	d := &Dashboard{}

	total, err := s.pages.CountPages(ctx)
	if err != nil {
		return nil, err
	}
	d.Wiki.TotalPages = total

	// 7. 最近更新した記事 20 件。
	// **ダッシュボードの「最近の変更」はこれ1本で出す。**新規作成も updated_at が
	// 動くのでここに入る。新規かどうかは version == 1 で見分ける(dashboard.html)。
	if d.Wiki.RecentUpdate, err = s.pages.List(ctx, "updated", 20, 0); err != nil {
		return nil, err
	}
	// 8. 最近作成した記事。画面では 7 に併合したが、API の利用者のために残す。
	if d.Wiki.RecentCreate, err = s.pages.RecentlyCreated(ctx, 10); err != nil {
		return nil, err
	}
	// 9. 未解決リンク — 書くべきものの示唆になる
	if d.Wiki.Unresolved, err = s.pages.UnresolvedLinks(ctx, 15); err != nil {
		return nil, err
	}
	if d.Wiki.Tags, err = s.pages.Tags(ctx); err != nil {
		return nil, err
	}

	// 上段 — GTD。**空でも崩れないレイアウトにすること**(DESIGN 5)。
	var gtdRows int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM tasks) + (SELECT count(*) FROM projects) + (SELECT count(*) FROM areas)`).
		Scan(&gtdRows); err != nil {
		return nil, err
	}
	d.GTD.Enabled = gtdRows > 0

	inbox, err := s.gtd.Inbox(ctx)
	if err != nil {
		return nil, err
	}
	d.GTD.InboxCount = len(inbox)

	if d.GTD.Today, err = s.gtd.Today(ctx); err != nil {
		return nil, err
	}
	if d.GTD.Contexts, err = s.gtd.Contexts(ctx); err != nil {
		return nil, err
	}
	if d.GTD.StalledProjects, err = s.gtd.StalledProjects(ctx); err != nil {
		return nil, err
	}
	if d.GTD.WaitingOverdue, err = s.gtd.WaitingOverdue(ctx, 7); err != nil {
		return nil, err
	}
	if d.GTD.SomedayDueReview, err = s.gtd.SomedayDueReview(ctx); err != nil {
		return nil, err
	}
	return d, nil
}
