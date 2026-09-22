package files

import (
	"context"
	"database/sql"
	"regexp"
)

// refRe picks /files/<hash> out of a body.
var refRe = regexp.MustCompile(`/files/([0-9a-f]{64})`)

// Referenced returns the set of hashes referenced anywhere in the main
// database.
//
// References only ever live inside bodies, so this simply scans them all.
// **There is no separate reference table.** One would duplicate what the bodies
// already say, and the moment a single save path forgets to update it, the two
// drift apart silently.
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

// Unused returns the hashes nothing references.
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

// Missing returns hashes referenced by a body with no file behind them - a
// broken link.
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

// Prune deletes unreferenced files and returns how many, and how many bytes,
// were removed.
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
