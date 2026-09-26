// Package settings reads and writes the settings the user toggles on screen.
//
// **No row means the default.** Defaults are never written to the database, so
// changing one is a code change and needs no migration over existing data.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/wakamenod/enghi/internal/store"
)

// The setting keys. Values are stored as the strings "1" and "0", except for
// KeyCalendarHidden, a JSON array of calendar names.
const (
	KeyContexts = "gtd.contexts"
	KeyAreas    = "gtd.areas"
	// KeyCalendar turns the calendar sync on. It has a panel of its own, so it
	// is not in Keys, which the features form writes all of.
	KeyCalendar       = "calendar.enabled"
	KeyCalendarHidden = "calendar.hidden"
)

// Settings gathers what the screens need in order to render.
//
// **Contexts and areas are optional tools within GTD.** Until there is a reason
// to filter by place or tool, they only add choices to wade through. So they
// are off by default, and whoever needs them turns them on in the settings.
type Settings struct {
	Contexts bool `json:"contexts"`
	Areas    bool `json:"areas"`
	Calendar bool `json:"calendar"`
	// CalendarHidden are the calendars whose events are left off the screens.
	// They are still synced, so showing one again needs no resync.
	CalendarHidden []string `json:"calendar_hidden"`
}

// Keys lists the settings that can be toggled on screen, in display order.
var Keys = []string{KeyContexts, KeyAreas}

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// Load returns the current settings. On a read error it still returns the
// defaults, so a screen never dies because of this.
func (s *Service) Load(ctx context.Context) (Settings, error) {
	out := Settings{CalendarHidden: []string{}} // everything is off by default
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		on := v == "1"
		switch k {
		case KeyContexts:
			out.Contexts = on
		case KeyAreas:
			out.Areas = on
		case KeyCalendar:
			out.Calendar = on
		case KeyCalendarHidden:
			// A value that does not parse shows every calendar, never an error
			_ = json.Unmarshal([]byte(v), &out.CalendarHidden)
		}
	}
	return out, rows.Err()
}

// Set writes one on/off setting.
func (s *Service) Set(ctx context.Context, key string, on bool) error {
	if !valid(key) {
		return sql.ErrNoRows
	}
	v := "0"
	if on {
		v = "1"
	}
	return s.put(ctx, key, v)
}

// SetCalendarHidden replaces the list of hidden calendars.
func (s *Service) SetCalendarHidden(ctx context.Context, names []string) error {
	if names == nil {
		names = []string{}
	}
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return s.put(ctx, KeyCalendarHidden, string(b))
}

func (s *Service) put(ctx context.Context, key, v string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, v)
	return err
}

func valid(key string) bool {
	return key == KeyCalendar || slices.Contains(Keys, key)
}
