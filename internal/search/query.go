// Package search implements search across everything.
// **Section 3 of DESIGN.md is a settled specification based on measurements over
// 30k real rows. Do not simplify it on a hunch.**
package search

import (
	"strings"
	"unicode"
)

// Phrase turns a query into an FTS5 phrase literal (DESIGN 3.3):
//  1. replace " with "" inside the query
//  2. wrap the whole thing in "
//
// Without this, C++ or a"b is an FTS5 syntax error and the request 500s.
// Everything therefore becomes a phrase search, which with trigram is the same
// as a substring match - exactly what we want.
//
// **Never pre-split the query on spaces or punctuation and AND the pieces
// together** (DESIGN 3.2). Japanese queries have no spaces, so it misses
// entirely, and for alphanumeric queries it silently lowers precision.
func Phrase(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

// LikeEscape neutralizes LIKE wildcards; pair it with ESCAPE '\\' (DESIGN 3.6).
func LikeEscape(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

// RuneLen is the query length in characters, not bytes: two Japanese
// characters are six bytes.
func RuneLen(q string) int { return len([]rune(q)) }

// IsASCII2 reports whether the query is two ASCII characters - Go, UI, DB, AI
// and the like, which are everyday queries in technical notes (DESIGN 3.4).
func IsASCII2(q string) bool {
	rs := []rune(q)
	if len(rs) != 2 {
		return false
	}
	for _, r := range rs {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// WordBoundaryMatch reports whether q appears in s at a word boundary.
// **Apply it to two-character ASCII queries only** (DESIGN 3.4).
// LIKE '%go%' matches algorithm and going, so those have to be dropped in the
// application. Never apply it to a two-character Japanese query: there is no
// concept of a word boundary there, so everything would be dropped.
func WordBoundaryMatch(s, q string) bool {
	ls, lq := strings.ToLower(s), strings.ToLower(q)
	for i := 0; ; {
		j := strings.Index(ls[i:], lq)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(lq)
		if !isWordByte(prevByte(ls, start)) && !isWordByte(nextByte(ls, end)) {
			return true
		}
		i = start + 1
		if i >= len(ls) {
			return false
		}
	}
}

func isWordByte(b byte, ok bool) bool {
	if !ok {
		return false // the ends of the string are word boundaries
	}
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		b >= 0x80 // an adjacent multi-byte character counts as part of the word
}

func prevByte(s string, i int) (byte, bool) {
	if i <= 0 {
		return 0, false
	}
	return s[i-1], true
}

func nextByte(s string, i int) (byte, bool) {
	if i >= len(s) {
		return 0, false
	}
	return s[i], true
}

// Truncations returns progressively shorter queries, trimmed from the end, as a
// fallback when there are no hits:
// 「オフィスの移転について」 -> 「オフィスの移転」 -> 「オフィス」.
// **Two steps at most** (DESIGN 3.4).
func Truncations(q string) []string {
	rs := []rune(q)
	var out []string
	for i := 0; i < 2; i++ {
		// Cut to two thirds each time, stopping below three characters (trigram
		// cannot match shorter than that)
		n := len(rs) * 2 / 3
		if n < 3 || n >= len(rs) {
			break
		}
		rs = rs[:n]
		out = append(out, strings.TrimSpace(string(rs)))
	}
	return out
}
