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
	"github.com/yuin/goldmark/renderer"
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

// mermaidRenderer draws ```mermaid fenced blocks as a diagram with its source
// folded underneath; every other fenced block goes to the highlighter
// (newCodeRenderer). The diagram itself is drawn in the browser (app.js,
// renderMermaid). The <details> is emitted open and the JS closes it only once
// the diagram is drawn, so without JS, or on a syntax error, the source stays
// visible and the block is never empty.
type mermaidRenderer struct{ fallback renderer.NodeRendererFunc }

func newMermaidRenderer() *mermaidRenderer {
	m := &mermaidRenderer{}
	// Borrow the fenced-code function from the highlighter
	newCodeRenderer().RegisterFuncs(registerFunc(func(k ast.NodeKind, f renderer.NodeRendererFunc) {
		if k == ast.KindFencedCodeBlock {
			m.fallback = f
		}
	}))
	return m
}

type registerFunc func(ast.NodeKind, renderer.NodeRendererFunc)

func (f registerFunc) Register(k ast.NodeKind, fn renderer.NodeRendererFunc) { f(k, fn) }

func (m *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, m.render)
}

func (m *mermaidRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.FencedCodeBlock)
	if string(n.Language(source)) != "mermaid" {
		return m.fallback(w, source, node, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<figure class=\"mermaid\">\n<div class=\"mermaid-diagram\"></div>\n" +
		"<details class=\"mermaid-source\" open>\n<summary>Mermaid</summary>\n" +
		"<pre><code class=\"language-mermaid\">")
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		html.DefaultWriter.RawWrite(w, line.Value(source))
	}
	_, _ = w.WriteString("</code></pre>\n</details>\n</figure>\n")
	return ast.WalkSkipChildren, nil
}

// imageParagraphs marks a paragraph that holds nothing but images (each maybe
// wrapped in a link) with class="images". The page's measure caps running text
// at about 74 characters; an image alone should be free of it, but an inline
// icon in a sentence must not lift the cap from the whole paragraph, and CSS
// cannot tell the two apart, since it does not see text nodes.
type imageParagraphs struct{}

func (imageParagraphs) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindParagraph {
			return ast.WalkContinue, nil
		}
		if onlyImages(n, source) {
			n.SetAttributeString("class", []byte("images"))
		}
		return ast.WalkSkipChildren, nil
	})
}

// onlyImages reports whether n has at least one image and otherwise only
// whitespace (the line breaks between images stacked one per line).
func onlyImages(n ast.Node, source []byte) bool {
	found := false
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.Kind() {
		case ast.KindImage:
			found = true
		case ast.KindLink:
			if c.ChildCount() != 1 || c.FirstChild().Kind() != ast.KindImage {
				return false
			}
			found = true
		case ast.KindText:
			if len(bytes.TrimSpace(c.(*ast.Text).Segment.Value(source))) > 0 {
				return false
			}
		default:
			return false
		}
	}
	return found
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
//
// A ```mermaid block becomes a diagram (see mermaidRenderer), any other fenced
// block is highlighted (see newCodeRenderer), and a paragraph of images alone
// is marked (see imageParagraphs).
func NewRenderer(resolve Resolver) *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithInlineParsers(util.Prioritized(&wikilinkParser{resolve: resolve}, 150)),
			parser.WithASTTransformers(util.Prioritized(imageParagraphs{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			renderer.WithNodeRenderers(util.Prioritized(newMermaidRenderer(), 100)),
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
