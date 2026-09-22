package gtd

import (
	"errors"
	"fmt"
)

// Area is an area of responsibility (20,000ft). **It never completes**
// (DESIGN 2.2).
type Area struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	NotePageID   *int64 `json:"note_page_id,omitempty"`
	NotePageSlug string `json:"note_page_slug,omitempty"`
	SortOrder    int    `json:"sort_order"`
	Archived     bool   `json:"archived"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// Project is the core of GTD: a desired outcome that can be finished within a
// year and takes more than one action step.
type Project struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Outcome **describes the finished state**. GTD treats it as required; the
	// field is optional but the UI asks for it.
	Outcome      string `json:"outcome"`
	Status       string `json:"status"` // active / someday / done / dropped
	AreaID       *int64 `json:"area_id,omitempty"`
	AreaName     string `json:"area_name,omitempty"`
	NotePageID   *int64 `json:"note_page_id,omitempty"` // Project Support Material
	NotePageSlug string `json:"note_page_slug,omitempty"`
	// ReviewOn is the review date: **the tickler that keeps a project parked in
	// someday from never resurfacing**. Without it, someday is effectively a bin
	// (DESIGN 2.2).
	ReviewOn    string `json:"review_on,omitempty"`
	SortOrder   int    `json:"sort_order"`
	Version     int    `json:"version"`
	CompletedAt string `json:"completed_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	// Aggregates, for list views
	OpenTasks int `json:"open_tasks"`
	NextCount int `json:"next_count"`
}

// Stalled reports whether this is an active project with no next action.
// **This carries half the value of the system** (DESIGN 2.4).
func (p *Project) Stalled() bool { return p.Status == "active" && p.NextCount == 0 }

// Context is where an action can be done: @phone, @office and so on.
type Context struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Archived  bool   `json:"archived"`
	Count     int    `json:"count"` // next actions in this context; 0 is meaningful, so no omitempty
}

// Task is an action. **Only "a single action you can physically do right now"
// belongs on the next-action list.**
type Task struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
	State string `json:"state"`

	ProjectID    *int64 `json:"project_id,omitempty"`
	ProjectTitle string `json:"project_title,omitempty"`
	ContextID    *int64 `json:"context_id,omitempty"`
	ContextName  string `json:"context_name,omitempty"`
	AreaID       *int64 `json:"area_id,omitempty"`
	AreaName     string `json:"area_name,omitempty"`

	ScheduledOn string `json:"scheduled_on,omitempty"`
	DeadlineOn  string `json:"deadline_on,omitempty"`
	WaitingFor  string `json:"waiting_for,omitempty"`
	DelegatedAt string `json:"delegated_at,omitempty"`

	Energy       string `json:"energy,omitempty"`
	TimeEstimate *int   `json:"time_estimate,omitempty"`
	Priority     int    `json:"priority"`

	Recurrence       string `json:"recurrence,omitempty"`
	SeriesID         *int64 `json:"series_id,omitempty"`
	RecurrenceEndsOn string `json:"recurrence_ends_on,omitempty"`

	SortOrder   int    `json:"sort_order"`
	Version     int    `json:"version"`
	CompletedAt string `json:"completed_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	// WaitingDays is the days since delegated_at when state='waiting'.
	WaitingDays int `json:"waiting_days"`
}

// Recurring reports whether this is a recurring task.
func (t *Task) Recurring() bool { return t.Recurrence != "" }

// Series is one recurring series, for the Weekly Review inventory (DESIGN 2.6).
type Series struct {
	SeriesID   int64  `json:"series_id"`
	Title      string `json:"title"`
	Recurrence string `json:"recurrence"`
	OpenTaskID *int64 `json:"open_task_id,omitempty"`
	NextOn     string `json:"next_on,omitempty"`
	DoneCount  int    `json:"done_count"`
	EndsOn     string `json:"recurrence_ends_on,omitempty"`
	LastDoneOn string `json:"last_done_on,omitempty"`
}

// Review is a single weekly review.
type Review struct {
	ID          int64           `json:"id"`
	StartedAt   string          `json:"started_at"`
	CompletedAt string          `json:"completed_at,omitempty"`
	Checklist   map[string]bool `json:"checklist"`
	Note        string          `json:"note"`
}

// ChecklistKeys is the standard checklist (DESIGN 2.3).
// **Implementers do not get to invent these items.**
// review_projects and review_someday are the two tied directly to why this
// system exists.
//
// The labels live in i18n under `checklist.<key>` and `checklist.data.<key>`.
// Putting text here would make the language impossible to switch.
var ChecklistKeys = []struct {
	Key string
}{
	{"collect_loose_papers"},
	{"inbox_zero"},
	{"empty_head"},
	{"review_next_actions"},
	{"review_past_calendar"},
	{"review_upcoming_calendar"},
	{"review_waiting_for"},
	{"review_projects"},
	{"review_someday"},
	{"review_recurring"},
}

// States.
const (
	StateInbox     = "inbox"
	StateNext      = "next"
	StateLater     = "later"
	StateWaiting   = "waiting"
	StateScheduled = "scheduled"
	StateSomeday   = "someday"
	// StateFiled means an inbox item was judged to be reference material rather
	// than an action and became a wiki page. In GTD terms that is neither done
	// nor dropped, hence a state of its own (DESIGN 2.2).
	StateFiled   = "filed"
	StateDone    = "done"
	StateDropped = "dropped"
)

var validStates = map[string]bool{
	StateInbox: true, StateNext: true, StateLater: true, StateWaiting: true,
	StateScheduled: true, StateSomeday: true, StateFiled: true,
	StateDone: true, StateDropped: true,
}

// ErrNotFound is returned when the target does not exist.
var ErrNotFound = errors.New("not found")

// VersionConflictError is an optimistic-lock mismatch, as with pages.
type VersionConflictError struct {
	Current *Task `json:"current"`
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: the current version is %d", e.Current.Version)
}
