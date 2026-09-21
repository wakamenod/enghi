// Package i18n は画面とメッセージの多言語化を担う。
package i18n

import (
	"fmt"
	"net/http"
	"strings"
)

// Lang は対応している言語。
type Lang string

const (
	JA Lang = "ja"
	EN Lang = "en"
)

// Default は判定できなかったときの言語。
const Default = JA

// All は対応している言語(表示順)。
var All = []Lang{JA, EN}

// Name は言語の表示名(その言語自身で書く)。
var Name = map[Lang]string{JA: "日本語", EN: "English"}

// CookieName は選択を覚えておく cookie の名前。
const CookieName = "enghi-lang"

// Valid は対応している言語かどうか。
func Valid(l string) bool {
	for _, x := range All {
		if string(x) == l {
			return true
		}
	}
	return false
}

// FromRequest はリクエストから言語を決める。
// 明示的な選択(cookie)を最優先し、無ければ Accept-Language を見る。
func FromRequest(r *http.Request) Lang {
	if c, err := r.Cookie(CookieName); err == nil && Valid(c.Value) {
		return Lang(c.Value)
	}
	return FromAcceptLanguage(r.Header.Get("Accept-Language"))
}

// FromAcceptLanguage は Accept-Language ヘッダから言語を決める。
// 品質値(q=)の大小は見るが、同値なら先に書かれたものを採る。
func FromAcceptLanguage(header string) Lang {
	best, bestQ := Default, -1.0
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			if _, err := fmt.Sscanf(strings.TrimSpace(part[i+1:]), "q=%f", &q); err != nil {
				q = 1.0
			}
		}
		// ja-JP のような地域付きも拾う
		base := strings.ToLower(tag)
		if i := strings.Index(base, "-"); i >= 0 {
			base = base[:i]
		}
		if !Valid(base) {
			continue
		}
		if q > bestQ {
			best, bestQ = Lang(base), q
		}
	}
	if bestQ < 0 {
		return Default
	}
	return best
}

// T は key に対応する文言を返す。args があれば fmt.Sprintf で埋める。
//
// **key が無い場合は key そのものを返す。**画面が空欄になるより、
// 何が足りないかが見えるほうが直しやすい。
func T(lang Lang, key string, args ...any) string {
	msg, ok := lookup(lang, key)
	if !ok {
		// 対応する言語に無ければ既定の言語で探す
		if msg, ok = lookup(Default, key); !ok {
			return key
		}
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

func lookup(lang Lang, key string) (string, bool) {
	if m, ok := catalog[lang]; ok {
		if s, ok := m[key]; ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// Keys は登録されている key をすべて返す(検査用)。
func Keys() []string {
	out := make([]string, 0, len(catalog[Default]))
	for k := range catalog[Default] {
		out = append(out, k)
	}
	return out
}

// catalog は言語ごとの文言。ja.go / en.go で定義する。
var catalog = map[Lang]map[string]string{}

func register(lang Lang, m map[string]string) { catalog[lang] = m }
