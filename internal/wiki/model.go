package wiki

import (
	"errors"
	"fmt"
)

// Page is one article. It carries no GTD-derived field (DESIGN 0).
type Page struct {
	ID        int64    `json:"id"`
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Version   int      `json:"version"`
	Archived  bool     `json:"archived"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Tags      []string `json:"tags"`
}

// Link is the resolution of one [[...]] in a body.
type Link struct {
	Title    string `json:"title"`           // the raw string written in [[...]]
	Label    string `json:"label,omitempty"` // the Label part of [[Title|Label]]
	Kind     string `json:"kind"`            // page / project / task / area
	PageID   *int64 `json:"page_id,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Resolved bool   `json:"resolved"`
}

// Backlink is "something pointing at this page", of any kind (DESIGN 2.1).
type Backlink struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug,omitempty"`
	// DstTitle is the spelling the referrer wrote, which matters when the page
	// is referenced by an alias.
	DstTitle string `json:"dst_title"`
}

// Revision is a snapshot of a body and title.
type Revision struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
}

// Alias is a page_titles row with is_canonical = 0.
type Alias struct {
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// TagCount is used for tag listings.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Unresolved aggregates unresolved links, i.e. pages that do not exist yet.
type Unresolved struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

// ErrNotFound is returned when the page does not exist.
var ErrNotFound = errors.New("not found")

// VersionConflictError is an optimistic-lock mismatch (HTTP 409 /
// error="version_conflict"). The client must not throw the input away: show the
// difference against the current data and let the user merge (DESIGN 4.2).
type VersionConflictError struct {
	Current *Page
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: the current version is %d", e.Current.Version)
}

// TitleConflictError is returned when the new title collides with another
// page's canonical title or alias (HTTP 409 / error="title_conflict"). It means
// something entirely different from version_conflict.
type TitleConflictError struct {
	Conflicting *Page
}

func (e *TitleConflictError) Error() string {
	return "a page with the same name (case-insensitive) already exists"
}
