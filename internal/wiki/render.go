package wiki

import (
	"bytes"
	"context"
	"database/sql"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Resolver resolves [[title]] to a slug. A false second return value means an
// unresolved link.
type Resolver func(title string) (slug string, ok bool)

// wikilinkParser is the goldmark extension that reads [[Title]] and
// [[Title|Label]] as links. Inline code and code blocks are consumed by
// goldmark first, so their contents never reach this parser.
type wikilinkParser struct{ resolve Resolver }

func (p *wikilinkParser) Trigger() []byte { return []byte{'['} }

func (p *wikilinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	end := bytes.Index(line, []byte("]]"))
	if end < 0 {
		return nil
	}
	inner := string(line[2:end])
	if strings.ContainsAny(inner, "[]") {
		return nil
	}
	title, label := strings.TrimSpace(inner), ""
	if i := strings.Index(inner, "|"); i >= 0 {
		title = strings.TrimSpace(inner[:i])
		label = strings.TrimSpace(inner[i+1:])
	}
	if title == "" {
		return nil
	}
	if label == "" {
		// Display exactly what was written in [[...]]; never substitute the
		// current title (DESIGN 2.5).
		label = title
	}
	block.Advance(end + 2)
	_ = seg

	link := ast.NewLink()
	if slug, ok := p.resolve(title); ok {
		link.Destination = []byte("/wiki/" + slug)
	} else {
		// An unresolved link. Clicking it goes to the new-page screen.
		link.Destination = []byte("/wiki/new?title=" + queryEscape(title))
		link.SetAttributeString("class", []byte("wikilink-new"))
		link.Title = []byte("a page that does not exist yet")
	}
	link.AppendChild(link, ast.NewString([]byte(label)))
	return link
}

func queryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// Renderer turns Markdown into HTML.
type Renderer struct{ md goldmark.Markdown }

// NewRenderer builds a renderer with GFM and heading IDs.
//
// **A single newline is rendered as a line break** (`html.WithHardWraps`).
// By default CommonMark turns a newline inside a paragraph into a space, which
// in Japanese text shows up as a visible gap mid-sentence. The two-trailing-
// spaces notation is invisible and gets eaten by editors that strip trailing
// whitespace, so it is not an option.
//
// The cost is that **exported Markdown loses those breaks in other renderers**
// (paragraphs join into one line). Export writes the source out verbatim, so
// the difference is only in how it is displayed.
func NewRenderer(resolve Resolver) *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithInlineParsers(util.Prioritized(&wikilinkParser{resolve: resolve}, 150)),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
		),
	)
	return &Renderer{md: md}
}

// Render turns Markdown source into HTML.
func (r *Renderer) Render(src string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ResolverFor returns a Resolver backed by this Service's page_titles.
func (s *Service) ResolverFor(ctx context.Context) Resolver {
	cache := map[string]string{}
	return func(title string) (string, bool) {
		if slug, ok := cache[strings.ToLower(title)]; ok {
			return slug, slug != ""
		}
		var slug string
		err := s.db.QueryRowContext(ctx,
			`SELECT p.slug FROM pages p JOIN page_titles t ON t.page_id = p.id WHERE t.title = ?`,
			title).Scan(&slug)
		if err == sql.ErrNoRows {
			cache[strings.ToLower(title)] = ""
			return "", false
		} else if err != nil {
			return "", false
		}
		cache[strings.ToLower(title)] = slug
		return slug, true
	}
}
