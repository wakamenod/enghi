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

// 使い方ガイド。本文は docs/guide/<lang>/<topic>.md、画面からは
// /guide/<topic>#<anchor> で個別の節を指している。
//
// **このリンクは放っておくと静かに切れる。** 見出しを書き換えても、
// 訳を片方だけ直しても、コンパイルは通るしテンプレートも壊れない。
// ここで機械的に検査する。

var (
	guideH2 = regexp.MustCompile(`(?m)^#{2,3} +(.+?)\s*$`)
	// 画面から張られたガイドへのリンク(テンプレート内の /guide/... と help 部分テンプレート)
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
		t.Fatalf("ガイドを読めない: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("ガイドが1つも埋め込まれていない")
	}
	return out
}

// 見出しには必ず {#id} を書くこと。
// **自動生成の ID に頼らない。** 日本語見出しから作られる ID は英語版とずれるため、
// 画面から張ったアンカーが言語を切り替えた途端に切れる。
func TestGuideHeadingsHaveExplicitAnchors(t *testing.T) {
	for name, src := range guideFiles(t) {
		for _, m := range guideH2.FindAllStringSubmatch(src, -1) {
			if !anchorRe.MatchString(m[1]) {
				t.Errorf("%s: 見出しに {#id} が無い: %q", name, m[1])
			}
		}
	}
}

// 言語をまたいで、同じ topic は同じアンカーを持つこと。
// **片方だけ節を足すと、その言語でだけリンクが切れる。**
func TestGuideAnchorsMatchAcrossLanguages(t *testing.T) {
	byTopic := map[string]map[string][]string{} // topic -> lang -> anchors
	for name, src := range guideFiles(t) {
		lang, topic, ok := strings.Cut(name, "/")
		if !ok {
			t.Fatalf("想定外のパス: %s", name)
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
				t.Errorf("%s: アンカーが %s と %s で違う\n%s: %v\n%s: %v",
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

// 画面から張ったリンクの飛び先が実在すること。
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
			t.Errorf("%s: ガイドに %q という topic が無い", where, topic)
			return
		}
		if !anchors[topic][anchor] {
			t.Errorf("%s: /guide/%s に #%s という節が無い", where, topic, anchor)
		}
	}

	// テンプレートから
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
	// ガイド本文どうしの相互リンクも同じ検査にかける
	for name, src := range files {
		for _, m := range guideLinkRe.FindAllStringSubmatch(src, -1) {
			check(name, m[1], m[2])
		}
	}
}

// 描画したガイドに、見出しの id が実際に載っていること。
// **`{#id}` の解釈を切ると、ID が消えるのではなく本文に文字列として出る。**
func TestGuideRendersHeadingIDs(t *testing.T) {
	h := newServer(t)
	w := do(h, req("GET", "/guide/enghi", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /guide/enghi → %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="states"`) {
		t.Error(`見出しの id が出ていない(WithHeadingAttribute が効いていない)`)
	}
	if strings.Contains(body, "{#") {
		t.Error("アンカー記法が本文にそのまま出ている")
	}
	if !strings.Contains(body, "</table>") {
		t.Error("表が描画されていない(GFM が効いていない)")
	}
}

// 言語を切り替えても同じ節に飛べること(英語版が無ければ日本語にフォールバックする)。
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
				t.Errorf("GET /guide/%s (%s): 本文が描画されていない", topic, lang)
			}
		}
	}
}

func TestGuideUnknownTopicIs404(t *testing.T) {
	h := newServer(t)
	if got := do(h, req("GET", "/guide/nonexistent", "")).Code; got != http.StatusNotFound {
		t.Errorf("GET /guide/nonexistent → %d, want 404", got)
	}
	// topic は埋め込み FS のパスに入るので、脱出できないことを確かめる。
	// (ServeMux がパスを正規化して弾くため 404 とは限らない。200 で何かを返さないことが要件)
	for _, p := range []string{"/guide/..%2fetc%2fpasswd", "/guide/gtd%2f..%2f..%2fschema.sql"} {
		w := do(h, req("GET", p, ""))
		if w.Code == http.StatusOK {
			t.Errorf("GET %s → 200。埋め込み FS から外へ出られている", p)
		}
	}
}
