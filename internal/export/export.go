// Package export は全件を Markdown に書き出す。
// DB 単一正本の唯一の代償を消すための機能であり、第1段階から持つ(DESIGN 7)。
package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/wakamenod/enghi/internal/store"
)

// Result は書き出しの結果。
type Result struct {
	Dir   string `json:"dir"`
	Pages int    `json:"pages"`
	Files int    `json:"files"`
}

// Sanitize はファイル名に使える形に slug を直す。
// **/ 、.. 、制御文字、先頭のドットを除去してからファイル名にすること**(DESIGN 7)。
func Sanitize(slug string) string {
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r == '/' || r == '\\' || r == os.PathSeparator:
			b.WriteRune('-')
		case unicode.IsControl(r):
		case r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	out = strings.TrimLeft(out, ". ")
	out = strings.TrimRight(out, ". ")
	if out == "" {
		out = "untitled"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// Run は dir へ全件を書き出す。
// **dir はリクエストから受け取らないこと**(設定ファイルの値に固定する。DESIGN 4.4)。
func Run(ctx context.Context, db *store.DB, dir string) (*Result, error) {
	res := &Result{Dir: dir}
	wikiDir := filepath.Join(dir, "wiki")
	gtdDir := filepath.Join(dir, "gtd")
	for _, d := range []string{wikiDir, gtdDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, slug, title, body, created_at, updated_at FROM pages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	used := map[string]int{}
	for rows.Next() {
		var id int64
		var slug, title, body, created, updated string
		if err := rows.Scan(&id, &slug, &title, &body, &created, &updated); err != nil {
			return nil, err
		}
		tags, err := tagsOf(ctx, db, id)
		if err != nil {
			return nil, err
		}
		aliases, err := aliasesOf(ctx, db, id)
		if err != nil {
			return nil, err
		}
		name := Sanitize(slug)
		// slug は小文字に正規化して保存しているので APFS 上での大小衝突は起きないが、
		// サニタイズの結果として衝突しうるので番号を付ける。
		key := strings.ToLower(name)
		if n := used[key]; n > 0 {
			name = fmt.Sprintf("%s-%d", name, n+1)
		}
		used[key]++

		var b strings.Builder
		b.WriteString("---\n")
		fmt.Fprintf(&b, "title: %s\n", yamlString(title))
		fmt.Fprintf(&b, "slug: %s\n", yamlString(slug))
		b.WriteString("tags:")
		if len(tags) == 0 {
			b.WriteString(" []\n")
		} else {
			b.WriteString("\n")
			for _, t := range tags {
				fmt.Fprintf(&b, "  - %s\n", yamlString(t))
			}
		}
		if len(aliases) > 0 {
			b.WriteString("aliases:\n")
			for _, a := range aliases {
				fmt.Fprintf(&b, "  - %s\n", yamlString(a))
			}
		}
		fmt.Fprintf(&b, "created: %s\n", created)
		fmt.Fprintf(&b, "updated: %s\n", updated)
		b.WriteString("---\n\n")
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		if err := os.WriteFile(filepath.Join(wikiDir, name+".md"), []byte(b.String()), 0o600); err != nil {
			return nil, err
		}
		res.Pages++
		res.Files++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// GTD 側。第1段階では空でもファイルは作る(エクスポートの形を固定しておくため)。
	for name, fn := range map[string]func(context.Context, *store.DB) (string, error){
		"projects.md": exportProjects,
		"tasks.md":    exportTasks,
		"areas.md":    exportAreas,
	} {
		content, err := fn(ctx, db)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(gtdDir, name), []byte(content), 0o600); err != nil {
			return nil, err
		}
		res.Files++
	}
	return res, nil
}

func tagsOf(ctx context.Context, db *store.DB, pageID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.name FROM tags t JOIN page_tags pt ON pt.tag_id = t.id
		  WHERE pt.page_id = ? ORDER BY t.name`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func aliasesOf(ctx context.Context, db *store.DB, pageID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT title FROM page_titles WHERE page_id = ? AND is_canonical = 0 ORDER BY created_at`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func exportProjects(ctx context.Context, db *store.DB) (string, error) {
	var b strings.Builder
	b.WriteString("# Projects\n\n")
	rows, err := db.QueryContext(ctx,
		`SELECT p.id, p.title, p.outcome, p.status, COALESCE(a.name, ''), COALESCE(p.review_on, '')
		   FROM projects p LEFT JOIN areas a ON a.id = p.area_id
		  ORDER BY p.status, p.sort_order, p.id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, outcome, status, area, review string
		if err := rows.Scan(&id, &title, &outcome, &status, &area, &review); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		fmt.Fprintf(&b, "- status: %s\n", status)
		if outcome != "" {
			fmt.Fprintf(&b, "- outcome: %s\n", outcome)
		}
		if area != "" {
			fmt.Fprintf(&b, "- area: %s\n", area)
		}
		if review != "" {
			fmt.Fprintf(&b, "- review_on: %s\n", review)
		}
		b.WriteString("\n")
		if err := writeTasksOfProject(ctx, db, &b, id); err != nil {
			return "", err
		}
	}
	return b.String(), rows.Err()
}

func writeTasksOfProject(ctx context.Context, db *store.DB, b *strings.Builder, projectID int64) error {
	rows, err := db.QueryContext(ctx,
		`SELECT title, state, COALESCE(scheduled_on,''), COALESCE(deadline_on,'')
		   FROM tasks WHERE project_id = ? ORDER BY sort_order, id`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var title, state, sched, dead string
		if err := rows.Scan(&title, &state, &sched, &dead); err != nil {
			return err
		}
		mark := " "
		if state == "done" {
			mark = "x"
		}
		fmt.Fprintf(b, "- [%s] %s (%s)", mark, title, state)
		if sched != "" {
			fmt.Fprintf(b, " scheduled:%s", sched)
		}
		if dead != "" {
			fmt.Fprintf(b, " deadline:%s", dead)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return rows.Err()
}

func exportTasks(ctx context.Context, db *store.DB) (string, error) {
	var b strings.Builder
	b.WriteString("# Tasks\n\n")
	rows, err := db.QueryContext(ctx,
		`SELECT t.title, t.state, COALESCE(c.name,''), COALESCE(t.scheduled_on,''),
		        COALESCE(t.deadline_on,''), COALESCE(t.waiting_for,''), COALESCE(t.recurrence,'')
		   FROM tasks t LEFT JOIN contexts c ON c.id = t.context_id
		  ORDER BY t.state, t.sort_order, t.id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var title, state, ctxName, sched, dead, waiting, rec string
		if err := rows.Scan(&title, &state, &ctxName, &sched, &dead, &waiting, &rec); err != nil {
			return "", err
		}
		mark := " "
		if state == "done" {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s (%s)", mark, title, state)
		for _, kv := range [][2]string{
			{"context", ctxName}, {"scheduled", sched}, {"deadline", dead},
			{"waiting_for", waiting}, {"recurrence", rec},
		} {
			if kv[1] != "" {
				fmt.Fprintf(&b, " %s:%s", kv[0], kv[1])
			}
		}
		b.WriteString("\n")
	}
	return b.String(), rows.Err()
}

func exportAreas(ctx context.Context, db *store.DB) (string, error) {
	var b strings.Builder
	b.WriteString("# Areas of Responsibility\n\n")
	rows, err := db.QueryContext(ctx,
		`SELECT name, description FROM areas ORDER BY sort_order, id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var name, desc string
		if err := rows.Scan(&name, &desc); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "## %s\n\n", name)
		if desc != "" {
			fmt.Fprintf(&b, "%s\n\n", desc)
		}
	}
	return b.String(), rows.Err()
}

// yamlString は YAML frontmatter の値として安全な形にする。
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#{}[],&*?|-<>=!%@`\"'\n") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
	}
	return s
}
