// Package export writes everything out as Markdown.
// It exists to remove the one cost of keeping the database as the single source
// of truth, and has been there since stage one (DESIGN 7).
package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/store"
)

// Result is the outcome of an export.
type Result struct {
	Dir    string `json:"dir"`
	Pages  int    `json:"pages"`
	Files  int    `json:"files"`
	Images int    `json:"images"`
}

// fileRefRe picks /files/<hash> out of a body.
var fileRefRe = regexp.MustCompile(`/files/([0-9a-f]{64})`)

// Sanitize turns a slug into something usable as a file name.
// **Strip /, .., control characters and a leading dot before using it as a file
// name** (DESIGN 7).
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

// Run writes everything into dir.
// **dir never comes from a request**; it is fixed to the value in the
// configuration file (DESIGN 4.4).
//
// When blobs is non-nil, images referenced from bodies are written into
// `files/` and `/files/<hash>` in the text is rewritten to a relative path, so
// that what was exported is self-contained Markdown.
func Run(ctx context.Context, db *store.DB, blobs *filestore.Store, dir string) (*Result, error) {
	res := &Result{Dir: dir}
	wikiDir := filepath.Join(dir, "wiki")
	gtdDir := filepath.Join(dir, "gtd")
	filesDir := filepath.Join(dir, "files")
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
	written := map[string]string{} // hash -> extension, so nothing is written twice
	// rewriteFiles writes out the images a body references and rewrites them as
	// paths relative to wiki/ or gtd/, which sit side by side.
	rewriteFiles := func(body string) (string, error) {
		if blobs == nil {
			return body, nil
		}
		var werr error
		body = fileRefRe.ReplaceAllStringFunc(body, func(m string) string {
			hash := fileRefRe.FindStringSubmatch(m)[1]
			ext, err := writeBlob(ctx, blobs, filesDir, hash, written)
			if err != nil {
				werr = err
				return m
			}
			return "../files/" + hash + ext
		})
		return body, werr
	}
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
		// Slugs are stored lower-cased, so case collisions cannot happen on APFS,
		// but sanitizing can still produce a collision; number those.
		key := strings.ToLower(name)
		if n := used[key]; n > 0 {
			name = fmt.Sprintf("%s-%d", name, n+1)
		}
		used[key]++

		// Write out the images referenced by the body and rewrite them as relative
		// paths
		if body, err = rewriteFiles(body); err != nil {
			return nil, err
		}

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

	// The GTD side. The files are created even when empty, so the shape of an
	// export stays fixed.
	for name, fn := range map[string]func(context.Context, *store.DB) (string, error){
		"projects.md": exportProjects,
		"tasks.md": func(ctx context.Context, db *store.DB) (string, error) {
			return exportTasks(ctx, db, rewriteFiles)
		},
		"areas.md": exportAreas,
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
	// Counted last: work log entries reference images too
	res.Images = len(written)
	res.Files += len(written)
	return res, nil
}

// writeBlob writes one image out and returns its extension, doing nothing if
// it was written already.
func writeBlob(ctx context.Context, blobs *filestore.Store, dir, hash string,
	written map[string]string) (string, error) {

	if ext, ok := written[hash]; ok {
		return ext, nil
	}
	data, meta, err := blobs.Get(ctx, hash)
	if err != nil {
		// A reference with no file behind it is skipped, leaving the body as is
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ext := filestore.Ext(meta.MediaType)
	if err := os.WriteFile(filepath.Join(dir, hash+ext), data, 0o600); err != nil {
		return "", err
	}
	written[hash] = ext
	return ext, nil
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

// exportTasks lists every task, each followed by its work log.
func exportTasks(ctx context.Context, db *store.DB, rewriteFiles func(string) (string, error)) (string, error) {
	logs, err := taskLogs(ctx, db)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Tasks\n\n")
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.state, COALESCE(c.name,''), COALESCE(t.scheduled_on,''),
		        COALESCE(t.deadline_on,''), COALESCE(t.waiting_for,''), COALESCE(t.recurrence,'')
		   FROM tasks t LEFT JOIN contexts c ON c.id = t.context_id
		  ORDER BY t.state, t.sort_order, t.id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, state, ctxName, sched, dead, waiting, rec string
		if err := rows.Scan(&id, &title, &state, &ctxName, &sched, &dead, &waiting, &rec); err != nil {
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
		for _, l := range logs[id] {
			body, err := rewriteFiles(l.body)
			if err != nil {
				return "", err
			}
			writeLogEntry(&b, l.kind, l.movedTo, l.createdAt, body)
		}
	}
	return b.String(), rows.Err()
}

type logEntry struct{ kind, movedTo, body, createdAt string }

// taskLogs reads every work log entry, oldest first, keyed by task.
func taskLogs(ctx context.Context, db *store.DB) (map[int64][]logEntry, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, kind, COALESCE(moved_to,''), body, created_at FROM task_logs ORDER BY task_id, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]logEntry{}
	for rows.Next() {
		var id int64
		var l logEntry
		if err := rows.Scan(&id, &l.kind, &l.movedTo, &l.body, &l.createdAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], l)
	}
	return out, rows.Err()
}

// writeLogEntry writes one entry as a nested list item under its task: the
// local time with its offset (stored times are UTC), then the Markdown body
// indented so that it stays inside the item, code blocks and all.
func writeLogEntry(b *strings.Builder, kind, movedTo, createdAt, body string) {
	when := createdAt
	if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
		when = t.In(time.Local).Format("2006-01-02 15:04 -07:00")
	}
	switch kind {
	case "start":
		when += " started"
	case "pause":
		when += " paused"
		if movedTo != "" {
			when += " (moved to " + movedTo + ")"
		}
	}
	fmt.Fprintf(b, "  - %s\n", when)
	if body == "" {
		return
	}
	b.WriteString("\n")
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("\n")
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

// yamlString makes a value safe to use in the YAML frontmatter.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#{}[],&*?|-<>=!%@`\"'\n") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
	}
	return s
}
