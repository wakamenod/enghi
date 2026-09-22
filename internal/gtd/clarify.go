package gtd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/wakamenod/enghi/internal/wiki"
)

// FileAsReferenceInput is the input for "this inbox item was reference
// material, not an action".
type FileAsReferenceInput struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
}

// FileAsReference turns one inbox item into a wiki page (the clarify flow;
// DESIGN 8-13):
//
//  1. create the wiki page
//  2. set the original task to state='filed' - **never done, never dropped.**
//     In GTD terms it is neither, hence a state of its own (DESIGN 2.2)
//  3. link to the new page through links
//
// References go one way only, GTD -> wiki. No GTD-derived column is added to
// pages.
func (s *Service) FileAsReference(ctx context.Context, pages *wiki.Service, taskID int64,
	in FileAsReferenceInput) (*Task, *wiki.Page, error) {

	cur, err := s.Task(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = cur.Title
	}
	body := in.Body
	if strings.TrimSpace(body) == "" {
		body = cur.Note
	}

	// 1. Create the page (a single transaction on the wiki side)
	page, err := pages.Create(ctx, wiki.CreateInput{Title: title, Body: body, Tags: in.Tags})
	if err != nil {
		return nil, nil, err
	}

	// 2-3. Mark the task filed and link it
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET state = 'filed', version = version + 1, updated_at = datetime('now')
			  WHERE id = ?`, taskID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO links(src_kind, src_id, dst_kind, dst_id, dst_title)
			 VALUES ('task', ?, 'page', ?, ?)`, taskID, page.ID, page.Title)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	t, err := s.Task(ctx, taskID)
	return t, page, err
}

// LinkedPages are the wiki pages linked from a task, project or area.
func (s *Service) LinkedPages(ctx context.Context, kind string, id int64) ([]wiki.Link, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.dst_id, l.dst_title, COALESCE(p.slug,'')
		   FROM links l LEFT JOIN pages p ON p.id = l.dst_id
		  WHERE l.src_kind = ? AND l.src_id = ? AND l.dst_kind = 'page'
		  ORDER BY l.dst_title`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wiki.Link{}
	for rows.Next() {
		var l wiki.Link
		var dstID sql.NullInt64
		if err := rows.Scan(&dstID, &l.Title, &l.Slug); err != nil {
			return nil, err
		}
		l.Kind = "page"
		if dstID.Valid {
			v := dstID.Int64
			l.PageID, l.Resolved = &v, true
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Weekly Review

// CurrentReview returns the unfinished review, starting a new one if there is
// none.
func (s *Service) CurrentReview(ctx context.Context) (*Review, error) {
	r, err := s.latestOpenReview(ctx)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO reviews(checklist) VALUES ('{}')`)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Review(ctx, id)
}

func (s *Service) latestOpenReview(ctx context.Context) (*Review, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM reviews WHERE completed_at IS NULL ORDER BY id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.Review(ctx, id)
}

// Review returns one review.
func (s *Service) Review(ctx context.Context, id int64) (*Review, error) {
	var r Review
	var completed sql.NullString
	var checklist string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, started_at, completed_at, checklist, note FROM reviews WHERE id = ?`, id).
		Scan(&r.ID, &r.StartedAt, &completed, &checklist, &r.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CompletedAt = completed.String
	r.Checklist = map[string]bool{}
	_ = json.Unmarshal([]byte(checklist), &r.Checklist)
	return &r, nil
}

// SetChecklistItem toggles one checklist item. The keys must match the standard
// checklist in DESIGN 2.3.
func (s *Service) SetChecklistItem(ctx context.Context, reviewID int64, key string, val bool) (*Review, error) {
	known := false
	for _, c := range ChecklistKeys {
		if c.Key == key {
			known = true
			break
		}
	}
	if !known {
		return nil, errors.New("invalid checklist key: " + key)
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var raw string
		if err := tx.QueryRowContext(ctx,
			`SELECT checklist FROM reviews WHERE id = ?`, reviewID).Scan(&raw); err != nil {
			return err
		}
		m := map[string]bool{}
		_ = json.Unmarshal([]byte(raw), &m)
		m[key] = val
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE reviews SET checklist = ? WHERE id = ?`, string(b), reviewID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Review(ctx, reviewID)
}

// CompleteReview finishes a review.
func (s *Service) CompleteReview(ctx context.Context, reviewID int64, note string) (*Review, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE reviews SET completed_at = datetime('now'), note = ? WHERE id = ?`,
		note, reviewID); err != nil {
		return nil, err
	}
	return s.Review(ctx, reviewID)
}

// PastReviews are the reviews already finished.
func (s *Service) PastReviews(ctx context.Context, limit int) ([]*Review, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM reviews WHERE completed_at IS NOT NULL ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := []*Review{}
	for _, id := range ids {
		r, err := s.Review(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
