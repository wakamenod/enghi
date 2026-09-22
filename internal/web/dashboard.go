package web

import (
	"context"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Dashboard is what / and GET /api/dashboard return (DESIGN 5).
// GTD on top, the wiki below. **Without GTD in use, the top half is simply
// empty.**
type Dashboard struct {
	GTD  GTDSummary  `json:"gtd"`
	Wiki WikiSummary `json:"wiki"`
}

// GTDSummary is the top half (DESIGN 5). **Without GTD in use it is simply
// empty.**
type GTDSummary struct {
	// 1. Inbox count, emphasized only when it is not zero
	InboxCount int `json:"inbox_count"`
	// 2. Today's next actions (deadline_on <= today or scheduled_on <= today)
	Today []*gtd.Task `json:"today"`
	// 3. Next-action counts per context
	Contexts []*gtd.Context `json:"contexts"`
	// 4. **Stalled projects, those without a next action** - DESIGN 2.4
	StalledProjects []*gtd.Project `json:"stalled_projects"`
	// 5. Waiting-for items delegated more than a few days ago (7 by default)
	WaitingOverdue []*gtd.Task `json:"waiting_overdue"`
	// 6. Someday projects whose review date has come
	SomedayDueReview []*gtd.Project `json:"someday_due_review"`

	Enabled bool `json:"enabled"` // whether there is any GTD data at all
}

// WikiSummary is the lower half.
type WikiSummary struct {
	TotalPages   int               `json:"total_pages"`
	RecentUpdate []*wiki.Page      `json:"recently_updated"`
	RecentCreate []*wiki.Page      `json:"recently_created"`
	Unresolved   []wiki.Unresolved `json:"unresolved_links"`
	Tags         []wiki.TagCount   `json:"tags"`
}

// dashboardData gathers every aggregate in one go.
func (s *Server) dashboardData(ctx context.Context) (*Dashboard, error) {
	d := &Dashboard{}

	total, err := s.pages.CountPages(ctx)
	if err != nil {
		return nil, err
	}
	d.Wiki.TotalPages = total

	// 7. The 20 most recently updated articles.
	// **"Recent changes" on the dashboard comes from this one query.** Creating
	// an article moves updated_at too, so new ones appear here as well; whether
	// something is new is decided by version == 1 (dashboard.html).
	if d.Wiki.RecentUpdate, err = s.pages.List(ctx, "updated", 20, 0); err != nil {
		return nil, err
	}
	// 8. Recently created articles. The screen folds these into 7, but the API
	// keeps them for its callers.
	if d.Wiki.RecentCreate, err = s.pages.RecentlyCreated(ctx, 10); err != nil {
		return nil, err
	}
	// 9. Unresolved links - a hint at what is worth writing
	if d.Wiki.Unresolved, err = s.pages.UnresolvedLinks(ctx, 15); err != nil {
		return nil, err
	}
	if d.Wiki.Tags, err = s.pages.Tags(ctx); err != nil {
		return nil, err
	}

	// The top half, GTD. **The layout must hold up when it is empty**
	// (DESIGN 5).
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
