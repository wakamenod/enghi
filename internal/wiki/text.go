// Package wiki handles pages, the title namespace, tags and links.
package wiki

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// Slugify builds the slug used in URLs. **It always lower-cases** (DESIGN 2.1).
// APFS is case-insensitive, so Foo and foo as separate pages collide on export.
func Slugify(s string) string {
	// **Normalize first.** macOS may hand us "ビ" as "ヒ" plus a combining
	// dakuten; without normalizing, two identical-looking slugs differ (see
	// textnorm).
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
			// drop it
		case r == '.' && b.Len() == 0:
			// drop a leading dot: it would make a hidden file
		case strings.ContainsRune(`"'<>:|?*#%&{}$!@+`+"`", r):
			// drop characters that are awkward in file names and URLs
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}
	out := strings.Trim(b.String(), "-.")
	// Make sure no ".." survives (path traversal on export)
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

// Wikilink is one [[...]] in a body.
type Wikilink struct {
	Title string // the title written between [[ and ]], raw
	Label string // the Label of [[Title|Label]]; empty when absent
}

// ParseLinks extracts [[...]] from a body, skipping code blocks and inline
// code. Duplicates of the same title collapse into one, to match the UNIQUE
// constraint on links.
func ParseLinks(body string) []Wikilink {
	// Blank out code spans with spaces of the same length to keep offsets
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
		// Titles are NOCASE, so duplicate detection ignores case too
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Wikilink{Title: textnorm.NFC(title), Label: strings.TrimSpace(m[2])})
	}
	return out
}

// Bigrams splits a string into bigrams. Storing into titles_fts and searching
// with a two-character query **must go through this same function**
// (DESIGN 3.4).
//
//	"オフィス移転" → "オフ フィ ィス ス移 移転"
//
// Bigrams never cross a whitespace-separated word boundary. A one-character
// word becomes a single token of that character.
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
