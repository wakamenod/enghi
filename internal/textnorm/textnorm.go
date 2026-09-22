// Package textnorm normalizes text.
//
// **macOS hands us Japanese in NFD (decomposed form) through many paths.**
// There, "ビ" is stored as "ヒ" plus a combining dakuten — two code points that
// look identical on screen but differ as bytes. In a wiki where titles are the
// lookup key, leaving this alone produces "the page exists but cannot be found"
// and "two pages with the same name".
//
// Normalizing to NFC on both write and search absorbs the difference between
// input paths.
package textnorm

import "golang.org/x/text/unicode/norm"

// NFC normalizes a string to composed form.
// Already-NFC strings are returned as is (norm makes that check cheap).
func NFC(s string) string {
	if norm.NFC.IsNormalString(s) {
		return s
	}
	return norm.NFC.String(s)
}

// IsNFC reports whether the string is already normalized. Used by doctor.
func IsNFC(s string) bool { return norm.NFC.IsNormalString(s) }

// Slice normalizes a whole slice of strings.
func Slice(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = NFC(s)
	}
	return out
}
