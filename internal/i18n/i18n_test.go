package i18n_test

import (
	"regexp"
	"testing"

	"github.com/wakamenod/enghi/internal/i18n"
)

var verbRe = regexp.MustCompile(`%[-+ #0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z]`)

// 対応しているすべての言語が、同じ key を持っていること。
// **片方だけ足すと、もう片方の画面が key むき出しになる。**
func TestAllLanguagesHaveSameKeys(t *testing.T) {
	for _, key := range i18n.Keys() {
		for _, lang := range i18n.All {
			if got := i18n.T(lang, key); got == key {
				t.Errorf("%s に %q が無い", lang, key)
			}
		}
	}
}

// 書式(%s / %d)が言語をまたいで一致していること。
// **同じ引数を渡すので、種類と個数と順序が違うと表示が壊れる。**
func TestFormatVerbsMatchAcrossLanguages(t *testing.T) {
	for _, key := range i18n.Keys() {
		want := verbRe.FindAllString(i18n.T(i18n.JA, key), -1)
		for _, lang := range i18n.All {
			if lang == i18n.JA {
				continue
			}
			got := verbRe.FindAllString(i18n.T(lang, key), -1)
			if len(got) != len(want) {
				t.Errorf("%q: 書式の数が違う ja=%v %s=%v", key, want, lang, got)
				continue
			}
			for i := range want {
				if want[i] != got[i] {
					t.Errorf("%q: 書式が違う ja=%v %s=%v", key, want, lang, got)
					break
				}
			}
		}
	}
}

func TestAcceptLanguage(t *testing.T) {
	cases := map[string]i18n.Lang{
		"":                     i18n.JA,
		"en":                   i18n.EN,
		"en-US,en;q=0.9":       i18n.EN,
		"ja,en-US;q=0.9":       i18n.JA,
		"en-US;q=0.9,ja;q=0.8": i18n.EN,
		"ja-JP":                i18n.JA,
		"fr-FR,de;q=0.9":       i18n.JA, // 対応外なので既定
		"fr-FR,en;q=0.5":       i18n.EN,
	}
	for header, want := range cases {
		if got := i18n.FromAcceptLanguage(header); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %s, want %s", header, got, want)
		}
	}
}

// 未登録の key は key そのものを返すこと(画面が空欄になるより分かりやすい)。
func TestMissingKeyReturnsKey(t *testing.T) {
	if got := i18n.T(i18n.JA, "これは.存在しない"); got != "これは.存在しない" {
		t.Errorf("T = %q", got)
	}
}
