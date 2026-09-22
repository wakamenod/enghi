// Package settings は利用者が画面から切り替える設定を読み書きする。
//
// **行が無い = 既定値。** 既定値を DB に書かないので、既定を変えるときは
// コードを直すだけで済み、既存の DB を書き換える移行が要らない。
package settings

import (
	"context"
	"database/sql"

	"github.com/wakamenod/enghi/internal/store"
)

// 設定の key。値は "1" / "0" の文字列で持つ。
const (
	KeyContexts = "gtd.contexts"
	KeyAreas    = "gtd.areas"
)

// Settings は画面の描画に必要な設定をまとめたもの。
//
// **Context と Area は GTD の中では任意の道具である。** 場所や道具で絞り込む
// 必要が無いうちは、選択肢が増えるだけで邪魔になる。そのため既定は off とし、
// 必要になった人だけが設定画面で on にする。
type Settings struct {
	Contexts bool `json:"contexts"`
	Areas    bool `json:"areas"`
}

// Keys は画面から切り替えられる設定の一覧(表示順)。
var Keys = []string{KeyContexts, KeyAreas}

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// Load は現在の設定を返す。読めなかった場合も既定値で返す(画面を落とさない)。
func (s *Service) Load(ctx context.Context) (Settings, error) {
	out := Settings{} // 既定はすべて off
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

// Set は1つの設定を書き換える。
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
