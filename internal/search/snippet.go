package search

import (
	"encoding/json"
	"html"
	"html/template"
	"strings"
)

// Highlight ranges in a snippet are carried as control characters, not HTML.
// snippet() returns the body itself, so embedding <mark> directly would open a
// path where a < or & in the body is interpreted as HTML.
const (
	markStart = "\x01"
	markEnd   = "\x02"
)

// SnippetHTML is for templates: escape the text first, then turn only the
// highlight markers back into HTML.
func SnippetHTML(s string) template.HTML {
	escaped := html.EscapeString(s)
	escaped = strings.ReplaceAll(escaped, html.EscapeString(markStart), "<mark>")
	escaped = strings.ReplaceAll(escaped, html.EscapeString(markEnd), "</mark>")
	// EscapeString passes control characters through, so replace bare markers too
	escaped = strings.ReplaceAll(escaped, markStart, "<mark>")
	escaped = strings.ReplaceAll(escaped, markEnd, "</mark>")
	return template.HTML(escaped)
}

// SnippetPlain is the plain text with the markers removed. The JSON API
// returns this.
func SnippetPlain(s string) string {
	return strings.NewReplacer(markStart, "", markEnd, "").Replace(s)
}

// MarshalJSON returns the snippet without markers, for the Emacs layer.
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result // strip the methods to avoid infinite recursion
	a := alias(r)
	a.Snippet = SnippetPlain(a.Snippet)
	return json.Marshal(a)
}
