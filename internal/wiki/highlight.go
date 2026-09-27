package wiki

import (
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// newCodeRenderer highlights fenced code blocks on the server with chroma.
// Tokens get class names only (span.k, span.s, ...); the colours live in
// app.css so that they follow the light/dark theme. A block whose language
// chroma does not know, or that has none, comes out as plain code in the same
// <pre><code class="language-xxx"> wrapper; the language is never guessed.
func newCodeRenderer() renderer.NodeRenderer {
	return highlighting.NewHTMLRenderer(
		highlighting.WithFormatOptions(
			chromahtml.WithClasses(true),
			chromahtml.PreventSurroundingPre(true),
		),
		highlighting.WithCodeBlockOptions(ignoreFenceAttributes),
		highlighting.WithWrapperRenderer(codeWrapper),
	)
}

// ignoreFenceAttributes undoes the Hugo-style fence attributes the highlighter
// reads (```go {linenos=table hl_lines=[1]}). Line numbers put a <div><table>
// inside our <pre><code>, which is invalid HTML and has no styles, so such a
// fence renders as ordinary highlighted code instead. These options come after
// the ones taken from the attributes, so they win.
func ignoreFenceAttributes(highlighting.CodeBlockContext) []chromahtml.Option {
	return []chromahtml.Option{
		chromahtml.WithLineNumbers(false),
		chromahtml.LineNumbersInTable(false),
		chromahtml.HighlightLines(nil),
	}
}

// codeWrapper keeps the markup of goldmark's stock renderer, with "chroma" on
// the <pre> when the block was highlighted.
func codeWrapper(w util.BufWriter, c highlighting.CodeBlockContext, entering bool) {
	if !entering {
		_, _ = w.WriteString("</code></pre>\n")
		return
	}
	if c.Highlighted() {
		_, _ = w.WriteString("<pre class=\"chroma\"><code")
	} else {
		_, _ = w.WriteString("<pre><code")
	}
	if lang, ok := c.Language(); ok {
		_, _ = w.WriteString(" class=\"language-")
		html.DefaultWriter.Write(w, lang)
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
}

// CodeHighlighting is the extension for a goldmark instance that has no
// ```mermaid handling (the guide). wiki.Renderer does not use it; its
// mermaidRenderer hands other blocks to newCodeRenderer itself.
func CodeHighlighting() goldmark.Extender { return codeHighlighting{} }

type codeHighlighting struct{}

func (codeHighlighting) Extend(m goldmark.Markdown) {
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(newCodeRenderer(), 100)))
}
