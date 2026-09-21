package files

import (
	"context"
	"database/sql"
	"regexp"
)

// refRe は本文中の /files/<hash> を拾う。
var refRe = regexp.MustCompile(`/files/([0-9a-f]{64})`)

// Referenced は本体 DB のどこかから参照されている hash の集合を返す。
//
// 参照は本文の中にしか無いので、素直に全走査する。
// **参照表を別に持たない。**持つと本文と二重管理になり、
// 保存経路のどれか1つで更新を忘れた瞬間に静かにずれる。
func Referenced(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	out := map[string]bool{}
	sources := []string{
		`SELECT body FROM pages`,
		`SELECT note FROM tasks`,
		`SELECT outcome FROM projects`,
		`SELECT description FROM areas`,
		`SELECT body FROM page_revisions`,
	}
	for _, q := range sources {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				rows.Close()
				return nil, err
			}
			for _, m := range refRe.FindAllStringSubmatch(text, -1) {
				out[m[1]] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Unused はどこからも参照されていない hash を返す。
func (s *Store) Unused(ctx context.Context, db *sql.DB) ([]string, error) {
	refs, err := Referenced(ctx, db)
	if err != nil {
		return nil, err
	}
	all, err := s.Hashes(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, h := range all {
		if !refs[h] {
			out = append(out, h)
		}
	}
	return out, nil
}

// Missing は本文から参照されているのに実体が無い hash を返す(リンク切れ)。
func (s *Store) Missing(ctx context.Context, db *sql.DB) ([]string, error) {
	refs, err := Referenced(ctx, db)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	all, err := s.Hashes(ctx)
	if err != nil {
		return nil, err
	}
	for _, h := range all {
		have[h] = true
	}
	var out []string
	for h := range refs {
		if !have[h] {
			out = append(out, h)
		}
	}
	return out, nil
}

// Prune は未参照のファイルを消して、消した数と減ったバイト数を返す。
func (s *Store) Prune(ctx context.Context, db *sql.DB) (int, int64, error) {
	unused, err := s.Unused(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	var freed int64
	for _, h := range unused {
		if meta, err := s.Meta(ctx, h); err == nil {
			freed += meta.Bytes
		}
		if err := s.Delete(ctx, h); err != nil {
			return 0, freed, err
		}
	}
	return len(unused), freed, nil
}
