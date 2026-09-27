package wiki_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/wakamenod/enghi/internal/wiki"
)

func noResolve(string) (string, bool) { return "", false }

// A ```mermaid block becomes a figure with the diagram slot and the source
// folded below it. The <details> is open on the server; only the JS closes it
// once the diagram is drawn.
func TestRenderMermaid(t *testing.T) {
	r := wiki.NewRenderer(noResolve)
	got, err := r.Render("前\n\n```mermaid\ngraph TD\n  A[<script>alert(1)</script>] --> B[\"a & b\"]\n```\n\n後")
	if err != nil {
		t.Fatal(err)
	}
	want := `<figure class="mermaid">
<div class="mermaid-diagram"></div>
<details class="mermaid-source" open>
<summary>Mermaid</summary>
<pre><code class="language-mermaid">graph TD
  A[&lt;script&gt;alert(1)&lt;/script&gt;] --&gt; B[&quot;a &amp; b&quot;]
</code></pre>
</details>
</figure>
`
	if !strings.Contains(got, want) {
		t.Fatalf("mermaid block not rendered as expected:\n%s", got)
	}
	if strings.Contains(got, "<script>") {
		t.Errorf("the source was not escaped:\n%s", got)
	}
	if !strings.Contains(got, "<p>前</p>") || !strings.Contains(got, "<p>後</p>") {
		t.Errorf("the surrounding paragraphs were lost:\n%s", got)
	}
}

// A code block with no language, or one chroma does not know, renders exactly
// as goldmark's stock renderer does.
func TestRenderPlainCodeUnchanged(t *testing.T) {
	stock := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	r := wiki.NewRenderer(noResolve)
	cases := map[string]string{
		"plain fence":    "```\nplain <b> & text\n```",
		"unknown lang":   "```nosuchlang\nfunc main() { _ = a < b && c }\n```",
		"tilde fence":    "~~~ nosuchlang extra\nprint('x')\n~~~",
		"mermaid-ish":    "```mermaidjs\ngraph TD\n```",
		"Mermaid case":   "```Mermaid\ngraph TD\n```",
		"indented block": "para\n\n    indented <code>\n    second line",
		"empty fence":    "```\n```",
	}
	for name, src := range cases {
		var want bytes.Buffer
		if err := stock.Convert([]byte(src), &want); err != nil {
			t.Fatal(err)
		}
		got, err := r.Render(src)
		if err != nil {
			t.Fatal(err)
		}
		if got != want.String() {
			t.Errorf("%s: output changed\n got: %q\nwant: %q", name, got, want.String())
		}
	}
}

// A block in a known language gets class-only token spans (the colours are in
// app.css) inside the stock <pre><code class="language-xxx"> wrapper.
func TestRenderHighlight(t *testing.T) {
	r := wiki.NewRenderer(noResolve)
	cases := []struct{ src, want string }{
		{"```sql\nSELECT * FROM t WHERE a < 1;\n```",
			`<pre class="chroma"><code class="language-sql"><span class="k">SELECT</span>`},
		{"```json\n{\"a\": true}\n```",
			`<pre class="chroma"><code class="language-json"><span class="p">{</span><span class="nt">&#34;a&#34;</span>`},
		{"~~~ go extra\nfunc main() {}\n~~~",
			`<pre class="chroma"><code class="language-go"><span class="kd">func</span>`},
	}
	for _, c := range cases {
		got, err := r.Render(c.src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, c.want) || strings.Contains(got, "style=") {
			t.Errorf("%q not highlighted as expected:\n got: %s\nwant: %s", c.src, got, c.want)
		}
	}
	for _, lang := range []string{"bash", "sh", "yaml", "js", "ts", "python", "diff"} {
		got, err := r.Render("```" + lang + "\nx = 1\n```")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, `<pre class="chroma"><code class="language-`+lang+`">`) {
			t.Errorf("%s: not highlighted: %s", lang, got)
		}
	}
	// Fence attributes (line numbers, highlighted lines) are ignored: the block
	// is plain highlighted code in the usual wrapper, with no table inside.
	got, err := r.Render("```go {linenos=table hl_lines=[1]}\nfunc a() {}\nfunc b() {}\n```")
	if err != nil {
		t.Fatal(err)
	}
	want := `<pre class="chroma"><code class="language-go"><span class="kd">func</span>`
	if !strings.HasPrefix(got, want) || strings.Contains(got, "<table") ||
		strings.Contains(got, "<div") || strings.Contains(got, `class="hl"`) {
		t.Errorf("fence attributes changed the markup:\n%s", got)
	}
	// The source is escaped whether or not it is highlighted.
	got, _ = r.Render("```html\n<script>alert(1)</script>\n```")
	if strings.Contains(got, "<script>") {
		t.Errorf("the source was not escaped:\n%s", got)
	}
}

// A table is wrapped in div.table-wrap, the box that scrolls sideways; the
// table inside renders exactly as goldmark's stock renderer does.
func TestRenderTableWrapped(t *testing.T) {
	stock := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	table := "| 名前 | 説明 |\n|:--|--:|\n| `a` | **b** <c> |\n"
	var tbl bytes.Buffer
	if err := stock.Convert([]byte(table), &tbl); err != nil {
		t.Fatal(err)
	}
	r := wiki.NewRenderer(noResolve)
	got, err := r.Render("前\n\n" + table + "\n後")
	if err != nil {
		t.Fatal(err)
	}
	want := "<p>前</p>\n<div class=\"table-wrap\">\n" + tbl.String() + "</div>\n<p>後</p>\n"
	if got != want {
		t.Errorf("table not wrapped as expected\n got: %q\nwant: %q", got, want)
	}
	if n := strings.Count(got, "table-wrap"); n != 1 {
		t.Errorf("want one wrapper, got %d:\n%s", n, got)
	}
}

// A paragraph of images alone is marked so the page can let it out of the
// measure; an image inside a sentence leaves the paragraph a plain <p>.
func TestRenderImageParagraphs(t *testing.T) {
	r := wiki.NewRenderer(noResolve)
	for src, want := range map[string]bool{
		"![a](a.png)":                        true,
		"![a](a.png)\n![b](b.png)":           true,
		"[![a](a.png)](https://example.com)": true,
		"Click the ![gear](gear.png) icon":   false,
		"![a](a.png) caption":                false,
		"[![a](a.png) text](https://x.test)": false,
		"plain text":                         false,
	} {
		got, err := r.Render(src)
		if err != nil {
			t.Fatal(err)
		}
		if marked := strings.HasPrefix(got, `<p class="images">`); marked != want {
			t.Errorf("%q: marked=%v, want %v\n%s", src, marked, want, got)
		}
	}
}
