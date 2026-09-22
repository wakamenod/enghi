// Package i18n localizes the screens and the messages.
package i18n

import (
	"fmt"
	"net/http"
	"strings"
)

// Lang is a supported language.
type Lang string

const (
	JA Lang = "ja"
	EN Lang = "en"
)

// Default is the language used when none could be determined.
const Default = JA

// All is every supported language, in display order.
var All = []Lang{JA, EN}

// Name is the display name of a language, written in that language.
var Name = map[Lang]string{JA: "日本語", EN: "English"}

// CookieName is the cookie that remembers the choice.
const CookieName = "enghi-lang"

// Valid reports whether the language is supported.
func Valid(l string) bool {
	for _, x := range All {
		if string(x) == l {
			return true
		}
	}
	return false
}

// FromRequest decides the language from a request: an explicit choice in the
// cookie wins, and otherwise Accept-Language is consulted.
func FromRequest(r *http.Request) Lang {
	if c, err := r.Cookie(CookieName); err == nil && Valid(c.Value) {
		return Lang(c.Value)
	}
	return FromAcceptLanguage(r.Header.Get("Accept-Language"))
}

// FromAcceptLanguage decides the language from the Accept-Language header. It
// honours quality values (q=), and on a tie takes whichever came first.
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
		// Pick up region-tagged forms such as ja-JP too
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

// T returns the message for a key, filling in args with fmt.Sprintf.
//
// **A missing key returns the key itself.** Seeing what is missing is easier to
// fix than a blank spot on the screen.
func T(lang Lang, key string, args ...any) string {
	msg, ok := lookup(lang, key)
	if !ok {
		// Fall back to the default language when this one has no entry
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

// Keys returns every registered key, for checks.
func Keys() []string {
	out := make([]string, 0, len(catalog[Default]))
	for k := range catalog[Default] {
		out = append(out, k)
	}
	return out
}

// catalog holds the messages per language, defined in ja.go and en.go.
var catalog = map[Lang]map[string]string{}

func register(lang Lang, m map[string]string) { catalog[lang] = m }
