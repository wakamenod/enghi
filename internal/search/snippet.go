package search

import (
	"encoding/json"
	"html"
	"html/template"
	"strings"
)

// スニペットの強調範囲は、HTML ではなく制御文字で印を付けて持ち回る。
// snippet() が返すのは本文そのものなので、<mark> を直接埋めると
// 本文中の < や & がそのまま HTML として解釈される経路ができてしまう。
const (
	markStart = "\x01"
	markEnd   = "\x02"
)

// SnippetHTML はテンプレート用。本文をエスケープしてから強調だけを HTML に戻す。
func SnippetHTML(s string) template.HTML {
	escaped := html.EscapeString(s)
	escaped = strings.ReplaceAll(escaped, html.EscapeString(markStart), "<mark>")
	escaped = strings.ReplaceAll(escaped, html.EscapeString(markEnd), "</mark>")
	// EscapeString は制御文字をそのまま通すので、素の印も置換しておく
	escaped = strings.ReplaceAll(escaped, markStart, "<mark>")
	escaped = strings.ReplaceAll(escaped, markEnd, "</mark>")
	return template.HTML(escaped)
}

// SnippetPlain は印を取り除いた素のテキスト。JSON API はこちらを返す。
func SnippetPlain(s string) string {
	return strings.NewReplacer(markStart, "", markEnd, "").Replace(s)
}

// MarshalJSON は Emacs 層向けに、印を含まない素のスニペットを返す。
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result // メソッドを外して無限再帰を避ける
	a := alias(r)
	a.Snippet = SnippetPlain(a.Snippet)
	return json.Marshal(a)
}
