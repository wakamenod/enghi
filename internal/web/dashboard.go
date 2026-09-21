package web

import (
	"context"

	"github.com/wakamenod/enghi/internal/wiki"
)

// Dashboard は / と GET /api/dashboard が返すもの(DESIGN 5)。
// 上段が GTD、下段が Wiki。**GTD を使っていなければ上段は自然に空になる。**
type Dashboard struct {
	GTD  GTDSummary  `json:"gtd"`
	Wiki WikiSummary `json:"wiki"`
}

// GTDSummary は上段。第2段階で埋まる。第1段階では空のまま返る。
type GTDSummary struct {
	InboxCount       int   `json:"inbox_count"`
	TodayCount       int   `json:"today_count"`
	StalledProjects  []any `json:"stalled_projects"`
	WaitingOverdue   []any `json:"waiting_overdue"`
	SomedayDueReview []any `json:"someday_due_review"`
	ContextCounts    []any `json:"context_counts"`
	Enabled          bool  `json:"enabled"` // GTD のデータが1件でもあるか
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

	// 7. 最近更新した記事 20 件
	if d.Wiki.RecentUpdate, err = s.pages.List(ctx, "updated", 20, 0); err != nil {
		return nil, err
	}
	// 8. 最近作成した記事(更新と分ける)
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

	// 上段は第2段階。ここでは「GTD のデータが存在するか」だけを見る。
	// 空でも崩れないレイアウトにすること(DESIGN 5)。
	var gtdRows int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM tasks) + (SELECT count(*) FROM projects) + (SELECT count(*) FROM areas)`).
		Scan(&gtdRows); err != nil {
		return nil, err
	}
	d.GTD.Enabled = gtdRows > 0
	d.GTD.StalledProjects = []any{}
	d.GTD.WaitingOverdue = []any{}
	d.GTD.SomedayDueReview = []any{}
	d.GTD.ContextCounts = []any{}
	return d, nil
}
