// Package wiki はページ、タイトル名前空間、タグ、リンクを扱う。
package wiki

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// Slugify は URL 用の slug を作る。**必ず小文字に正規化する**(DESIGN 2.1)。
// APFS は大小を区別しないため、Foo と foo を別ページにするとエクスポート時に衝突する。
func Slugify(s string) string {
	// **正規化を先に行う。**macOS は「ビ」を「ヒ」+ 濁点で渡してくることがあり、
	// 揃えないと見た目が同じ別の slug ができる(textnorm を参照)。
	s = textnorm.NFC(strings.ToLower(strings.TrimSpace(s)))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || unicode.IsSpace(r) || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		case unicode.IsControl(r):
			// 捨てる
		case r == '.' && b.Len() == 0:
			// 先頭のドットは捨てる(隠しファイルになるため)
		case strings.ContainsRune(`"'<>:|?*#%&{}$!@+`+"`", r):
			// ファイル名と URL で面倒になる文字は捨てる
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}
	out := strings.Trim(b.String(), "-.")
	// ".." が残らないようにする(エクスポート時のパストラバーサル防止)
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	if out == "" {
		out = "page"
	}
	return out
}

var (
	fencedRe   = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	inlineRe   = regexp.MustCompile("`[^`\n]*`")
	wikilinkRe = regexp.MustCompile(`\[\[([^\[\]|\n]+)(?:\|([^\[\]\n]*))?\]\]`)
)

// Wikilink は本文中の [[...]] 1件。
type Wikilink struct {
	Title string // [[ と ]] の間に書かれたタイトル(生の文字列)
	Label string // [[Title|Label]] の Label。無ければ空
}

// ParseLinks は本文から [[...]] を抽出する。コードブロックとインラインコードは除外する。
// 同じタイトルの重複は1件にまとめる(links の UNIQUE 制約と対応させるため)。
func ParseLinks(body string) []Wikilink {
	// コード部分は同じ長さの空白に置換して位置を保つ
	blank := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return '\n'
			}
			return ' '
		}, s)
	}
	cleaned := fencedRe.ReplaceAllStringFunc(body, blank)
	cleaned = inlineRe.ReplaceAllStringFunc(cleaned, blank)

	var out []Wikilink
	seen := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(cleaned, -1) {
		title := textnorm.NFC(strings.TrimSpace(m[1]))
		if title == "" {
			continue
		}
		// タイトルは NOCASE なので重複判定も大小を無視する
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Wikilink{Title: textnorm.NFC(title), Label: strings.TrimSpace(m[2])})
	}
	return out
}

// Bigrams は文字列を bigram に分割する。titles_fts への格納と、
// 2 文字クエリの検索の両方で **同じ関数を通すこと**(DESIGN 3.4)。
//
//	"オフィス移転" → "オフ フィ ィス ス移 移転"
//
// 空白で区切られた語の境界は跨がない。1 文字の語はその文字自体を1トークンとする。
func Bigrams(s string) string {
	var toks []string
	for _, field := range strings.Fields(textnorm.NFC(strings.ToLower(s))) {
		rs := []rune(field)
		if len(rs) == 1 {
			toks = append(toks, string(rs))
			continue
		}
		for i := 0; i+1 < len(rs); i++ {
			toks = append(toks, string(rs[i:i+2]))
		}
	}
	return strings.Join(toks, " ")
}
