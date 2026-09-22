package i18n_test

import (
	"regexp"
	"testing"

	"github.com/wakamenod/enghi/internal/i18n"
)

var verbRe = regexp.MustCompile(`%[-+ #0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z]`)

// Every supported language must carry the same keys.
// **Adding one to a single language leaves the key showing on the other's
// screens.**
func TestAllLanguagesHaveSameKeys(t *testing.T) {
	for _, key := range i18n.Keys() {
		for _, lang := range i18n.All {
			if got := i18n.T(lang, key); got == key {
				t.Errorf("%s に %q が無い", lang, key)
			}
		}
	}
}

// The format verbs (%s / %d) must match across languages.
// **The same arguments are passed to both, so a different kind, count or order
// breaks the output.**
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

// An unregistered key returns the key itself, which is clearer than a blank
// spot on the screen.
func TestMissingKeyReturnsKey(t *testing.T) {
	if got := i18n.T(i18n.JA, "これは.存在しない"); got != "これは.存在しない" {
		t.Errorf("T = %q", got)
	}
}
