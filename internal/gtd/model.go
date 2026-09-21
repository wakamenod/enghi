package gtd

import (
	"errors"
	"fmt"
)

// Area は責任範囲(20,000ft)。**完了しない**(DESIGN 2.2)。
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

// Project は GTD の中核。「1年以内に完了でき、2つ以上の行動ステップを要する、望ましい結果」。
type Project struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Outcome は**完了状態の記述**。GTD の作法として必須。任意入力だが UI で促す。
	Outcome      string `json:"outcome"`
	Status       string `json:"status"` // active / someday / done / dropped
	AreaID       *int64 `json:"area_id,omitempty"`
	AreaName     string `json:"area_name,omitempty"`
	NotePageID   *int64 `json:"note_page_id,omitempty"` // Project Support Material
	NotePageSlug string `json:"note_page_slug,omitempty"`
	// ReviewOn は再検討日。**someday にしたプロジェクトが二度と浮上しないのを防ぐ tickler**。
	// これが無いと Someday は事実上のゴミ箱になる(DESIGN 2.2)。
	ReviewOn    string `json:"review_on,omitempty"`
	SortOrder   int    `json:"sort_order"`
	Version     int    `json:"version"`
	CompletedAt string `json:"completed_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	// 集計(一覧表示用)
	OpenTasks int `json:"open_tasks"`
	NextCount int `json:"next_count"`
}

// Stalled は「Next Action が1つも無いアクティブなプロジェクト」か。
// **これがシステムの価値の半分を担う**(DESIGN 2.4)。
func (p *Project) Stalled() bool { return p.Status == "active" && p.NextCount == 0 }

// Context は @電話 @オフィス などの実行文脈。
type Context struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Archived  bool   `json:"archived"`
	Count     int    `json:"count"` // Next Action の件数(一覧用)。0 も意味を持つので omitempty にしない
}

// Task は行動。**Next Action リストに載るのは「今すぐ物理的に実行できる単一行動」だけ**。
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

	// WaitingDays は state='waiting' のとき delegated_at からの経過日数。
	WaitingDays int `json:"waiting_days"`
}

// Recurring は定期タスクかどうか。
func (t *Task) Recurring() bool { return t.Recurrence != "" }

// Series は定期タスク系列1本(Weekly Review の棚卸し用。DESIGN 2.6)。
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

// Review は週次レビュー1回分。
type Review struct {
	ID          int64           `json:"id"`
	StartedAt   string          `json:"started_at"`
	CompletedAt string          `json:"completed_at,omitempty"`
	Checklist   map[string]bool `json:"checklist"`
	Note        string          `json:"note"`
}

// ChecklistKeys は標準チェックリスト。**実装者が項目を思いつきで決めないこと**(DESIGN 2.3)。
// review_projects と review_someday がこのシステムの存在理由に直結する2項目。
var ChecklistKeys = []struct {
	Key   string
	Label string
	Data  string // 同じ画面に出すデータの説明
}{
	{"collect_loose_papers", "散らばった紙を集める", ""},
	{"inbox_zero", "Inbox を空にする", "Inbox 一覧(その場で clarify できる)"},
	{"empty_head", "頭の中を空にする", "クイックキャプチャ欄"},
	{"review_next_actions", "Next Actions を見直す", "コンテキスト別 Next 一覧"},
	{"review_past_calendar", "先週のカレンダーを振り返る", "先週完了したタスク"},
	{"review_upcoming_calendar", "今後の予定を確認する", "今後2週間の予定/締切"},
	{"review_waiting_for", "Waiting For を見直す", "委譲からの経過日数付き一覧"},
	{"review_projects", "プロジェクトリストを見直す", "停滞プロジェクトの検出結果"},
	{"review_someday", "Someday/Maybe を見直す", "再検討日が到来した someday"},
	{"review_recurring", "定期タスクを棚卸しする", "定期タスク系列の一覧"},
}

// 状態。
const (
	StateInbox     = "inbox"
	StateNext      = "next"
	StateLater     = "later"
	StateWaiting   = "waiting"
	StateScheduled = "scheduled"
	StateSomeday   = "someday"
	// StateFiled は「Inbox の項目が行動ではなく参照資料と判断され、Wiki ページになった」状態。
	// GTD 的には完了でも破棄でもないため独立した状態として持つ(DESIGN 2.2)。
	StateFiled   = "filed"
	StateDone    = "done"
	StateDropped = "dropped"
)

var validStates = map[string]bool{
	StateInbox: true, StateNext: true, StateLater: true, StateWaiting: true,
	StateScheduled: true, StateSomeday: true, StateFiled: true,
	StateDone: true, StateDropped: true,
}

// ErrNotFound は対象が無いとき。
var ErrNotFound = errors.New("not found")

// VersionConflictError は楽観ロックの版不一致(pages と同じ扱い)。
type VersionConflictError struct {
	Current *Task `json:"current"`
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: 現行の版は %d", e.Current.Version)
}
