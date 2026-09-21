package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const projectCols = `p.id, p.title, p.outcome, p.status, p.area_id, p.note_page_id,
	COALESCE(p.review_on,''), p.sort_order, p.version, COALESCE(p.completed_at,''),
	p.created_at, p.updated_at, COALESCE(a.name,''), COALESCE(pg.slug,''),
	(SELECT count(*) FROM tasks t WHERE t.project_id = p.id
	   AND t.state NOT IN ('done','dropped','filed')),
	(SELECT count(*) FROM tasks t WHERE t.project_id = p.id
	   AND t.state IN ('next','waiting','scheduled'))`

const projectFrom = `FROM projects p
	LEFT JOIN areas a  ON a.id = p.area_id
	LEFT JOIN pages pg ON pg.id = p.note_page_id`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Title, &p.Outcome, &p.Status, &p.AreaID, &p.NotePageID,
		&p.ReviewOn, &p.SortOrder, &p.Version, &p.CompletedAt, &p.CreatedAt, &p.UpdatedAt,
		&p.AreaName, &p.NotePageSlug, &p.OpenTasks, &p.NextCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *Service) projects(ctx context.Context, where string, args ...any) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` `+projectFrom+` `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Project は1件。
func (s *Service) Project(ctx context.Context, id int64) (*Project, error) {
	return scanProject(s.db.QueryRowContext(ctx,
		`SELECT `+projectCols+` `+projectFrom+` WHERE p.id = ?`, id))
}

// Projects は status で絞り込める一覧。
func (s *Service) Projects(ctx context.Context, status string) ([]*Project, error) {
	if status == "" {
		return s.projects(ctx, `ORDER BY
			CASE p.status WHEN 'active' THEN 0 WHEN 'someday' THEN 1 ELSE 2 END,
			p.sort_order, p.id`)
	}
	return s.projects(ctx, `WHERE p.status = ? ORDER BY p.sort_order, p.id`, status)
}

// StalledProjects は **「Next Action が1つも無いアクティブなプロジェクト」**。
//
// **これがシステムの価値の半分を担う**(DESIGN 2.4)。
// ダッシュボードと Weekly Review 画面の両方に必ず出すこと。
func (s *Service) StalledProjects(ctx context.Context) ([]*Project, error) {
	return s.projects(ctx,
		`WHERE p.status = 'active'
		   AND NOT EXISTS (
		     SELECT 1 FROM tasks t
		      WHERE t.project_id = p.id AND t.state IN ('next','waiting','scheduled')
		   )
		 ORDER BY p.sort_order, p.id`)
}

// SomedayDueReview は再検討日が到来した Someday プロジェクト。
// **これが無いと Someday は事実上のゴミ箱になる**(DESIGN 2.2)。
func (s *Service) SomedayDueReview(ctx context.Context) ([]*Project, error) {
	return s.projects(ctx,
		`WHERE p.status = 'someday' AND p.review_on IS NOT NULL AND p.review_on <= date('now')
		 ORDER BY p.review_on`)
}

// ProjectInput は作成/更新の入力。
type ProjectInput struct {
	Title      string `json:"title"`
	Outcome    string `json:"outcome"`
	Status     string `json:"status"`
	AreaID     *int64 `json:"area_id,omitempty"`
	NotePageID *int64 `json:"note_page_id,omitempty"`
	ReviewOn   string `json:"review_on,omitempty"`
	SortOrder  *int   `json:"sort_order,omitempty"`
	Version    int    `json:"version,omitempty"`
	ClearArea  bool   `json:"clear_area,omitempty"`
	ClearNote  bool   `json:"clear_note_page,omitempty"`
}

var validProjectStatus = map[string]bool{"active": true, "someday": true, "done": true, "dropped": true}

// CreateProject はプロジェクトを作る。
func (s *Service) CreateProject(ctx context.Context, in ProjectInput) (*Project, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, errors.New("タイトルが空です")
	}
	status := in.Status
	if status == "" {
		status = "active"
	}
	if !validProjectStatus[status] {
		return nil, fmt.Errorf("status が不正です: %q", status)
	}
	if in.ReviewOn != "" {
		if _, err := ParseDate(in.ReviewOn); err != nil {
			return nil, fmt.Errorf("review_on は YYYY-MM-DD 形式です: %q", in.ReviewOn)
		}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(title, outcome, status, area_id, note_page_id, review_on)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		title, in.Outcome, status, in.AreaID, in.NotePageID, nullIfEmpty(in.ReviewOn))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Project(ctx, id)
}

// PatchProject は部分更新。
func (s *Service) PatchProject(ctx context.Context, id int64, in ProjectInput) (*Project, error) {
	var out *Project
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		cur, err := scanProject(tx.QueryRowContext(ctx,
			`SELECT `+projectCols+` `+projectFrom+` WHERE p.id = ?`, id))
		if err != nil {
			return err
		}
		b := &setBuilder{}
		if in.Title != "" {
			b.Set("title", strings.TrimSpace(in.Title))
		}
		if in.Outcome != "" {
			b.Set("outcome", in.Outcome)
		}
		if in.Status != "" {
			if !validProjectStatus[in.Status] {
				return fmt.Errorf("status が不正です: %q", in.Status)
			}
			b.Set("status", in.Status)
			if (in.Status == "done" || in.Status == "dropped") && cur.CompletedAt == "" {
				b.Raw("completed_at = datetime('now')")
			}
		}
		if in.AreaID != nil {
			b.Set("area_id", *in.AreaID)
		} else if in.ClearArea {
			b.Raw("area_id = NULL")
		}
		if in.NotePageID != nil {
			b.Set("note_page_id", *in.NotePageID)
		} else if in.ClearNote {
			b.Raw("note_page_id = NULL")
		}
		if in.ReviewOn != "" || in.Status == "active" {
			// active に戻したら再検討日は不要になる
			if in.ReviewOn != "" {
				if err := b.Date("review_on", in.ReviewOn); err != nil {
					return err
				}
			}
		}
		if in.SortOrder != nil {
			b.Set("sort_order", *in.SortOrder)
		}
		if b.Empty() {
			out = cur
			return nil
		}
		b.Raw("version = version + 1")
		b.Raw("updated_at = datetime('now')")
		args := append(b.args, id)
		if _, err := tx.ExecContext(ctx,
			`UPDATE projects SET `+strings.Join(b.parts, ", ")+` WHERE id = ?`, args...); err != nil {
			return err
		}
		out, err = scanProject(tx.QueryRowContext(ctx,
			`SELECT `+projectCols+` `+projectFrom+` WHERE p.id = ?`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteProject は消す。links のライフサイクルはアプリ側で管理する(DESIGN 2.1)。
func (s *Service) DeleteProject(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE links SET dst_id = NULL WHERE dst_kind = 'project' AND dst_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM links WHERE src_kind = 'project' AND src_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
