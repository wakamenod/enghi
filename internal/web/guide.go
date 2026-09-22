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

// 使い方ガイド。
//
// 本文は docs/guide/<lang>/<topic>.md に置き、バイナリに埋め込む。
// **Wiki ページとして DB に投入しない。** 利用者が編集・削除できてしまい、
// 版を上げるたびに利用者の変更と衝突する。ガイドはプログラムの一部である。
//
// 描画に wiki.Renderer を使わないのは、あちらが [[...]] を解決するためである。
// ガイド本文がページの未解決リンクとして集計に混ざると、ダッシュボードが汚れる。

// guideTopics はガイドの構成(表示順)。slug がそのまま URL になる。
var guideTopics = []string{"gtd", "enghi"}

// guideMD はガイド専用のレンダラ。
// WithHeadingAttribute で `## 見出し {#id}` を有効にする。
// **見出し ID を自動生成に任せない。**日本語見出しから作られる ID は
// 英語版とずれるため、画面から張ったアンカーが言語を切り替えた途端に切れる。
// WithUnsafe は図(inline SVG)のために要る。**ガイド本文は利用者の入力ではなく、
// ビルド時にバイナリへ埋め込む自分の文書である。**記事本文のレンダラ(wiki.Renderer)
// には付けないこと。
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
	Topics []guideTopic // 目次(サイドバー)
	Topic  *guideTopic  // 本文を出している topic。目次ページでは nil
	HTML   template.HTML
}

var (
	guideH1Re = regexp.MustCompile(`(?m)^# +(.+?)\s*$`)
	guideH2Re = regexp.MustCompile(`(?m)^## +(.+?)\s*\{#([A-Za-z0-9_-]+)\}\s*$`)
	// 機能タグ。設定で off の機能の節は本文ごと落とす。
	// **見出しの行内には書かない。** 見出しの末尾は `{#id}` の位置であり、
	// そこに注釈を足すと goldmark が ID を拾えなくなる。直前の行に置く:
	//
	//	<!--feature:contexts-->
	//	### Contexts {#context}
	guideFeatRe = regexp.MustCompile(`(?m)^<!--\s*feature:([a-z]+)\s*-->\n(#{1,6}) `)
	// 節の切れ目(見出し行)。落とす範囲を決めるのに使う。
	guideHeadRe = regexp.MustCompile(`(?m)^(#{1,6}) `)
)

// guideFilter は、設定で off になっている機能の節を本文から取り除く。
//
// **出し分けを画面だけでやると、ガイドだけが「無い機能」を説明し続ける。**
// 節は、機能タグの次の見出しから、次に来る同位以上の見出しの手前までとする。
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

// guideSource は指定言語のガイド本文を返す。
// その言語の訳が無ければ既定の言語にフォールバックする(文言と同じ方針)。
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

// guideOutline は本文から表題と節(H2)を拾う。
// 節は `{#id}` を明示したものだけを拾う。guide_web_test が全 H2 に ID を要求する。
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

// guideOutlines は全 topic の目次を作る(サイドバー用)。
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

// viewGuideIndex は /guide。全 topic の目次だけを出す。
func (s *Server) viewGuideIndex(w http.ResponseWriter, r *http.Request) {
	lang := s.langOf(r)
	set := s.settings(r)
	s.render(w, r, "guide.html", viewData{
		Title: s.tr(r, "guide.title"), Nav: "guide",
		Data: guideData{Topics: s.guideOutlines(lang, "", set)},
	})
}

// viewGuide は /guide/{topic}。
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
			// 埋め込んだ自分の文書なので、goldmark の出力をそのまま信頼してよい。
			HTML: template.HTML(buf.String())},
	})
}
