package web

import (
	"bytes"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	enghi "github.com/wakamenod/enghi"
	"github.com/wakamenod/enghi/internal/i18n"
	"github.com/wakamenod/enghi/internal/settings"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// The guide.
//
// The prose lives in docs/guide/<lang>/<topic>.md and is embedded in the binary.
// **It is never loaded into the database as wiki pages.** The user could edit or
// delete it, and every release would collide with those changes. The guide is
// part of the program.
//
// Rendering does not go through wiki.Renderer because that one resolves
// [[...]]; guide prose counted as unresolved page links would pollute the
// dashboard.

// guideTopics is the structure of the guide, in display order. The slug is the
// URL.
var guideTopics = []string{"gtd", "enghi"}

// guideMD is the renderer used for the guide alone.
// WithHeadingAttribute enables `## heading {#id}`.
// **Heading IDs are never left to auto-generation**: IDs derived from Japanese
// headings differ from the English ones, so an anchor linked from a screen
// breaks the moment the language is switched.
// WithUnsafe is needed for the diagrams (inline SVG). **The guide is not user
// input; it is our own document, embedded at build time.** Never add it to the
// renderer used for article bodies (wiki.Renderer).
var guideMD = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithHeadingAttribute()),
	goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
)

type guideSection struct {
	ID    string
	Title string
}

type guideTopic struct {
	Slug     string
	Title    string
	Sections []guideSection
	Current  bool
}

type guideData struct {
	Topics []guideTopic // the table of contents in the sidebar
	Topic  *guideTopic  // the topic being shown; nil on the index page
	HTML   template.HTML
}

var (
	guideH1Re = regexp.MustCompile(`(?m)^# +(.+?)\s*$`)
	guideH2Re = regexp.MustCompile(`(?m)^## +(.+?)\s*\{#([A-Za-z0-9_-]+)\}\s*$`)
	// The feature tag. A section for a feature that is off is dropped entirely.
	// **It never goes on the heading line.** The end of a heading is where `{#id}`
	// lives, and a comment there stops goldmark picking the ID up. It goes on the
	// line before:
	//
	//	<!--feature:contexts-->
	//	### Contexts {#context}
	guideFeatRe = regexp.MustCompile(`(?m)^<!--\s*feature:([a-z]+)\s*-->\n(#{1,6}) `)
	// Section boundaries (heading lines), used to decide what to drop.
	guideHeadRe = regexp.MustCompile(`(?m)^(#{1,6}) `)
)

// guideFilter removes the sections of features that are turned off.
//
// **Hiding a feature on the screens alone would leave the guide explaining
// something that is not there.** A section runs from the heading after the
// feature tag up to the next heading at the same level or higher.
func guideFilter(src string, set settings.Settings) string {
	on := map[string]bool{"contexts": set.Contexts, "areas": set.Areas}
	heads := guideHeadRe.FindAllStringSubmatchIndex(src, -1)
	var out strings.Builder
	prev := 0
	for _, m := range guideFeatRe.FindAllStringSubmatchIndex(src, -1) {
		if on[src[m[2]:m[3]]] {
			continue
		}
		level := len(src[m[4]:m[5]])
		end := len(src)
		for _, h := range heads {
			if h[0] > m[4] && len(src[h[2]:h[3]]) <= level {
				end = h[0]
				break
			}
		}
		if m[0] > prev {
			out.WriteString(src[prev:m[0]])
		}
		if end > prev {
			prev = end
		}
	}
	out.WriteString(src[prev:])
	return out.String()
}

// guideSource returns the guide in the requested language, falling back to the
// default language when there is no translation - the same policy as messages.
func guideSource(lang i18n.Lang, topic string) (string, bool) {
	if !isGuideTopic(topic) {
		return "", false
	}
	for _, l := range []i18n.Lang{lang, i18n.Default} {
		b, err := enghi.GuideFS.ReadFile("docs/guide/" + string(l) + "/" + topic + ".md")
		if err == nil {
			return string(b), true
		}
	}
	return "", false
}

func isGuideTopic(topic string) bool {
	for _, t := range guideTopics {
		if t == topic {
			return true
		}
	}
	return false
}

// guideOutline picks the title and the sections (H2) out of the source.
// Only sections with an explicit `{#id}` are picked up; guide_web_test requires
// an ID on every H2.
func guideOutline(slug, src string) guideTopic {
	t := guideTopic{Slug: slug, Title: slug}
	if m := guideH1Re.FindStringSubmatch(src); m != nil {
		t.Title = strings.TrimSpace(m[1])
	}
	for _, m := range guideH2Re.FindAllStringSubmatch(src, -1) {
		t.Sections = append(t.Sections, guideSection{ID: m[2], Title: strings.TrimSpace(m[1])})
	}
	return t
}

// guideOutlines builds the table of contents for every topic, for the sidebar.
func (s *Server) guideOutlines(lang i18n.Lang, current string, set settings.Settings) []guideTopic {
	out := make([]guideTopic, 0, len(guideTopics))
	for _, slug := range guideTopics {
		src, ok := guideSource(lang, slug)
		if !ok {
			continue
		}
		t := guideOutline(slug, guideFilter(src, set))
		t.Current = slug == current
		out = append(out, t)
	}
	return out
}

// viewGuideIndex serves /guide, showing nothing but the contents of each topic.
func (s *Server) viewGuideIndex(w http.ResponseWriter, r *http.Request) {
	lang := s.langOf(r)
	set := s.settings(r)
	s.render(w, r, "guide.html", viewData{
		Title: s.tr(r, "guide.title"), Nav: "guide",
		Data: guideData{Topics: s.guideOutlines(lang, "", set)},
	})
}

// viewGuide serves /guide/{topic}.
func (s *Server) viewGuide(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	lang := s.langOf(r)
	src, ok := guideSource(lang, topic)
	if !ok {
		http.Error(w, s.tr(r, "err.not_found"), http.StatusNotFound)
		return
	}
	set := s.settings(r)
	src = guideFilter(src, set)
	var buf bytes.Buffer
	if err := guideMD.Convert([]byte(src), &buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cur := guideOutline(topic, src)
	cur.Current = true
	s.render(w, r, "guide.html", viewData{
		Title: cur.Title, Nav: "guide",
		Data: guideData{Topics: s.guideOutlines(lang, topic, set), Topic: &cur,
			// Our own embedded document, so goldmark's output can be trusted as is.
			HTML: template.HTML(buf.String())},
	})
}
