package web_test

import (
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	enghi "github.com/wakamenod/enghi"
)

// The guide. The prose lives in docs/guide/<lang>/<topic>.md, and the screens
// point at individual sections with /guide/<topic>#<anchor>.
//
// **Those links break silently if nobody watches them.** Rewriting a heading, or
// fixing only one translation, still compiles and still renders. So they are
// checked mechanically here.

var (
	guideH2 = regexp.MustCompile(`(?m)^#{2,3} +(.+?)\s*$`)
	// Links into the guide from the screens: /guide/... in templates, and the
	// help partial
	guideLinkRe = regexp.MustCompile(`/guide/([a-z-]+)#([A-Za-z0-9_-]+)`)
	guideHelpRe = regexp.MustCompile(`{{template "help" "([a-z-]+)#([A-Za-z0-9_-]+)"}}`)
	anchorRe    = regexp.MustCompile(`\{#([A-Za-z0-9_-]+)\}\s*$`)
)

func guideFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(enghi.GuideFS, "docs/guide", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		b, err := fs.ReadFile(enghi.GuideFS, path)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(path, "docs/guide/")] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read the guide: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no guide file is embedded at all")
	}
	return out
}

// Every heading carries an explicit {#id}.
// **Never rely on generated IDs.** An ID derived from a Japanese heading differs
// from the English one, so an anchor linked from a screen breaks the moment the
// language is switched.
func TestGuideHeadingsHaveExplicitAnchors(t *testing.T) {
	for name, src := range guideFiles(t) {
		for _, m := range guideH2.FindAllStringSubmatch(src, -1) {
			if !anchorRe.MatchString(m[1]) {
				t.Errorf("%s: heading without {#id}: %q", name, m[1])
			}
		}
	}
}

// The same topic must carry the same anchors in every language.
// **Adding a section to one language alone breaks the links in the other.**
func TestGuideAnchorsMatchAcrossLanguages(t *testing.T) {
	byTopic := map[string]map[string][]string{} // topic -> lang -> anchors
	for name, src := range guideFiles(t) {
		lang, topic, ok := strings.Cut(name, "/")
		if !ok {
			t.Fatalf("unexpected path: %s", name)
		}
		topic = strings.TrimSuffix(topic, ".md")
		if byTopic[topic] == nil {
			byTopic[topic] = map[string][]string{}
		}
		byTopic[topic][lang] = guideAnchors(src)
	}
	for topic, langs := range byTopic {
		var base string
		for lang := range langs {
			if base == "" || lang < base {
				base = lang
			}
		}
		for lang, anchors := range langs {
			if lang == base {
				continue
			}
			if strings.Join(anchors, ",") != strings.Join(langs[base], ",") {
				t.Errorf("%s: anchors differ between %s and %s\n%s: %v\n%s: %v",
					topic, base, lang, base, langs[base], lang, anchors)
			}
		}
	}
}

func guideAnchors(src string) []string {
	var out []string
	for _, m := range guideH2.FindAllStringSubmatch(src, -1) {
		if a := anchorRe.FindStringSubmatch(m[1]); a != nil {
			out = append(out, a[1])
		}
	}
	sort.Strings(out)
	return out
}

// Every link from a screen must point at something that exists.
func TestGuideLinksFromScreensResolve(t *testing.T) {
	files := guideFiles(t)
	anchors := map[string]map[string]bool{} // topic -> anchor
	for name, src := range files {
		_, topic, _ := strings.Cut(name, "/")
		topic = strings.TrimSuffix(topic, ".md")
		if anchors[topic] == nil {
			anchors[topic] = map[string]bool{}
		}
		for _, a := range guideAnchors(src) {
			anchors[topic][a] = true
		}
	}

	check := func(where, topic, anchor string) {
		if anchors[topic] == nil {
			t.Errorf("%s: the guide has no topic %q", where, topic)
			return
		}
		if !anchors[topic][anchor] {
			t.Errorf("%s: /guide/%s has no section #%s", where, topic, anchor)
		}
	}

	// From the templates
	tpls, err := fs.Glob(enghi.TemplatesFS, "web/templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tpls {
		b, err := fs.ReadFile(enghi.TemplatesFS, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range guideLinkRe.FindAllStringSubmatch(string(b), -1) {
			check(p, m[1], m[2])
		}
		for _, m := range guideHelpRe.FindAllStringSubmatch(string(b), -1) {
			check(p, m[1], m[2])
		}
	}
	// Cross-links between guide pages go through the same check
	for name, src := range files {
		for _, m := range guideLinkRe.FindAllStringSubmatch(src, -1) {
			check(name, m[1], m[2])
		}
	}
}

// The rendered guide must actually carry the heading IDs.
// **Turning `{#id}` off does not drop the ID; it prints it as text in the
// body.**
func TestGuideRendersHeadingIDs(t *testing.T) {
	h := newServer(t)
	w := do(h, req("GET", "/guide/enghi", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /guide/enghi → %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="states"`) {
		t.Error(`no heading id in the output (WithHeadingAttribute is not in effect)`)
	}
	if strings.Contains(body, "{#") {
		t.Error("the anchor notation is printed in the body")
	}
	if !strings.Contains(body, "</table>") {
		t.Error("no table was rendered (GFM is not in effect)")
	}
}

// The same sections are reachable in either language, falling back to Japanese
// when there is no English text.
func TestGuideServesEachLanguage(t *testing.T) {
	h := newServer(t)
	for _, lang := range []string{"ja", "en"} {
		for _, topic := range []string{"gtd", "enghi"} {
			r := req("GET", "/guide/"+topic, "")
			r.Header.Set("Accept-Language", lang)
			w := do(h, r)
			if w.Code != http.StatusOK {
				t.Errorf("GET /guide/%s (%s) → %d", topic, lang, w.Code)
				continue
			}
			if !strings.Contains(w.Body.String(), `id="`) {
				t.Errorf("GET /guide/%s (%s): nothing was rendered", topic, lang)
			}
		}
	}
}

func TestGuideUnknownTopicIs404(t *testing.T) {
	h := newServer(t)
	if got := do(h, req("GET", "/guide/nonexistent", "")).Code; got != http.StatusNotFound {
		t.Errorf("GET /guide/nonexistent → %d, want 404", got)
	}
	// The topic goes into a path inside the embedded FS, so check that it cannot
	// escape. (ServeMux normalizes and rejects, so it is not necessarily a 404;
	// the requirement is that nothing is served with a 200.)
	for _, p := range []string{"/guide/..%2fetc%2fpasswd", "/guide/gtd%2f..%2f..%2fschema.sql"} {
		w := do(h, req("GET", p, ""))
		if w.Code == http.StatusOK {
			t.Errorf("GET %s answered 200: it escaped the embedded FS", p)
		}
	}
}

// The diagrams (inline SVG) must survive rendering.
// **goldmark drops raw HTML by default.** Remove WithUnsafe and the diagrams
// vanish silently while the prose still renders; neither the templates nor the
// type checker say a word. A mismatch in the number of diagrams between
// languages is just as hard to notice.
func TestGuideDiagramsSurviveRendering(t *testing.T) {
	h := newServer(t)
	for _, lang := range []string{"ja", "en"} {
		r := req("GET", "/guide/gtd", "")
		r.Header.Set("Accept-Language", lang)
		body := do(h, r).Body.String()
		if n := strings.Count(body, `<svg class="dg"`); n == 0 {
			t.Errorf("/guide/gtd (%s): no diagram was rendered (WithUnsafe is missing)", lang)
		}
	}

	counts := map[string]map[string]int{} // topic -> lang -> number of diagrams
	for name, src := range guideFiles(t) {
		lang, topic, _ := strings.Cut(name, "/")
		topic = strings.TrimSuffix(topic, ".md")
		if counts[topic] == nil {
			counts[topic] = map[string]int{}
		}
		counts[topic][lang] = strings.Count(src, `<svg class="dg"`)
	}
	for topic, langs := range counts {
		var want = -1
		for _, n := range langs {
			if want < 0 || n < want {
				want = n
			}
		}
		for lang, n := range langs {
			if n != want {
				t.Errorf("%s: the number of diagrams differs by language (%s=%d, others=%d)", topic, lang, n, want)
			}
		}
	}
}

// Emphasis must not be broken.
//
// **Without a space before and after, `**` next to Japanese text never closes
// and is printed literally.** (CommonMark rule: a closing delimiter run that is
// flanked on both sides cannot close.) It can happen with any wording change,
// so the rendered output is watched.
func TestGuideHasNoRawMarkdown(t *testing.T) {
	h := newServer(t)
	enableFeatures(t, h)
	for _, lang := range []string{"ja", "en"} {
		for _, topic := range []string{"gtd", "enghi"} {
			r := req("GET", "/guide/"+topic, "")
			r.Header.Set("Accept-Language", lang)
			body := do(h, r).Body.String()
			for _, bad := range []string{"**", "](/guide"} {
				if strings.Contains(body, bad) {
					t.Errorf("/guide/%s (%s): %q appears in the rendered output", topic, lang, bad)
				}
			}
		}
	}
}
