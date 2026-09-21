package gtd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------- Area

const areaCols = `a.id, a.name, a.description, a.note_page_id, COALESCE(pg.slug,''),
	a.sort_order, a.archived, a.created_at, a.updated_at`

const areaFrom = `FROM areas a LEFT JOIN pages pg ON pg.id = a.note_page_id`

func scanArea(row interface{ Scan(...any) error }) (*Area, error) {
	var a Area
	var archived int
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.NotePageID, &a.NotePageSlug,
		&a.SortOrder, &archived, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Archived = archived != 0
	return &a, nil
}

// Areas は責任範囲の一覧。
func (s *Service) Areas(ctx context.Context) ([]*Area, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+areaCols+` `+areaFrom+` WHERE a.archived = 0 ORDER BY a.sort_order, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Area{}
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Area は1件。
func (s *Service) Area(ctx context.Context, id int64) (*Area, error) {
	return scanArea(s.db.QueryRowContext(ctx, `SELECT `+areaCols+` `+areaFrom+` WHERE a.id = ?`, id))
}

// AreaInput は作成/更新の入力。
type AreaInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	NotePageID  *int64 `json:"note_page_id,omitempty"`
	SortOrder   *int   `json:"sort_order,omitempty"`
	Archived    *bool  `json:"archived,omitempty"`
	ClearNote   bool   `json:"clear_note_page,omitempty"`
}

// CreateArea は責任範囲を作る。**Area は完了しない**(DESIGN 2.2)。
func (s *Service) CreateArea(ctx context.Context, in AreaInput) (*Area, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("名前が空です")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO areas(name, description, note_page_id) VALUES (?, ?, ?)`,
		name, in.Description, in.NotePageID)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Area(ctx, id)
}

// PatchArea は部分更新。
func (s *Service) PatchArea(ctx context.Context, id int64, in AreaInput) (*Area, error) {
	b := &setBuilder{}
	if in.Name != "" {
		b.Set("name", strings.TrimSpace(in.Name))
	}
	if in.Description != "" {
		b.Set("description", in.Description)
	}
	if in.NotePageID != nil {
		b.Set("note_page_id", *in.NotePageID)
	} else if in.ClearNote {
		b.Raw("note_page_id = NULL")
	}
	if in.SortOrder != nil {
		b.Set("sort_order", *in.SortOrder)
	}
	if in.Archived != nil {
		v := 0
		if *in.Archived {
			v = 1
		}
		b.Set("archived", v)
	}
	if b.Empty() {
		return s.Area(ctx, id)
	}
	b.Raw("updated_at = datetime('now')")
	args := append(b.args, id)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE areas SET `+strings.Join(b.parts, ", ")+` WHERE id = ?`, args...); err != nil {
		return nil, err
	}
	return s.Area(ctx, id)
}

// ProjectsOfArea は Area 配下のプロジェクト。
func (s *Service) ProjectsOfArea(ctx context.Context, areaID int64) ([]*Project, error) {
	return s.projects(ctx, `WHERE p.area_id = ? ORDER BY
		CASE p.status WHEN 'active' THEN 0 WHEN 'someday' THEN 1 ELSE 2 END, p.sort_order`, areaID)
}

// TasksOfArea は Area に直接紐づくタスク(プロジェクト化するほどでない単発行動)。
func (s *Service) TasksOfArea(ctx context.Context, areaID int64) ([]*Task, error) {
	return s.tasks(ctx, `WHERE t.area_id = ? AND t.state NOT IN ('done','dropped')
		ORDER BY t.sort_order, t.id`, areaID)
}

// ---------------------------------------------------------------- Context

// Contexts は @電話 @オフィス などの一覧。Count は Next Action の件数。
func (s *Service) Contexts(ctx context.Context) ([]*Context, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.name, c.sort_order, c.archived,
		        (SELECT count(*) FROM tasks t WHERE t.context_id = c.id AND `+nextActionsWhere+`)
		   FROM contexts c WHERE c.archived = 0 ORDER BY c.sort_order, c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Context{}
	for rows.Next() {
		var c Context
		var archived int
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder, &archived, &c.Count); err != nil {
			return nil, err
		}
		c.Archived = archived != 0
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ContextByName は名前で引く。
func (s *Service) ContextByName(ctx context.Context, name string) (*Context, error) {
	var c Context
	var archived int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, sort_order, archived FROM contexts WHERE name = ?`, name).
		Scan(&c.ID, &c.Name, &c.SortOrder, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Archived = archived != 0
	return &c, nil
}

// CreateContext は実行文脈を作る。
func (s *Service) CreateContext(ctx context.Context, name string) (*Context, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("名前が空です")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO contexts(name) VALUES (?)`, name)
	if err != nil {
		return nil, fmt.Errorf("コンテキスト %q: %w", name, err)
	}
	id, _ := res.LastInsertId()
	var c Context
	var archived int
	if err := s.db.QueryRowContext(ctx,
		`SELECT id, name, sort_order, archived FROM contexts WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.SortOrder, &archived); err != nil {
		return nil, err
	}
	c.Archived = archived != 0
	return &c, nil
}
