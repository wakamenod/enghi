// Package settings reads and writes the settings the user toggles on screen.
//
// **No row means the default.** Defaults are never written to the database, so
// changing one is a code change and needs no migration over existing data.
package settings

import (
	"context"
	"database/sql"

	"github.com/wakamenod/enghi/internal/store"
)

// The setting keys. Values are stored as the strings "1" and "0".
const (
	KeyContexts = "gtd.contexts"
	KeyAreas    = "gtd.areas"
)

// Settings gathers what the screens need in order to render.
//
// **Contexts and areas are optional tools within GTD.** Until there is a reason
// to filter by place or tool, they only add choices to wade through. So they
// are off by default, and whoever needs them turns them on in the settings.
type Settings struct {
	Contexts bool `json:"contexts"`
	Areas    bool `json:"areas"`
}

// Keys lists the settings that can be toggled on screen, in display order.
var Keys = []string{KeyContexts, KeyAreas}

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// Load returns the current settings. On a read error it still returns the
// defaults, so a screen never dies because of this.
func (s *Service) Load(ctx context.Context) (Settings, error) {
	out := Settings{} // everything is off by default
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
		}
	}
	return out, rows.Err()
}

// Set writes one setting.
func (s *Service) Set(ctx context.Context, key string, on bool) error {
	if !valid(key) {
		return sql.ErrNoRows
	}
	v := "0"
	if on {
		v = "1"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, v)
	return err
}

func valid(key string) bool {
	for _, k := range Keys {
		if k == key {
			return true
		}
	}
	return false
}
