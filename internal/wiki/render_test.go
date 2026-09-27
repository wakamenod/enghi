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

// Every other code block renders exactly as goldmark's stock renderer does.
func TestRenderNonMermaidCodeUnchanged(t *testing.T) {
	stock := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	r := wiki.NewRenderer(noResolve)
	cases := map[string]string{
		"plain fence":    "```\nplain <b> & text\n```",
		"go fence":       "```go\nfunc main() { _ = a < b && c }\n```",
		"tilde fence":    "~~~ python extra\nprint('x')\n~~~",
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
