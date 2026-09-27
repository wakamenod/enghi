// Package yomi turns titles into kana readings and romaji queries into kana,
// so that a title can be found by typing its reading in romaji (DESIGN 3.8).
//
// **The dictionary costs about 93 MB of heap and never goes away once loaded**
// (ipa.Dict caches it for the life of the process). Only the reading rebuild
// loads it, and the server runs that in a short-lived child process
// (internal/readings). ToKana needs no dictionary.
package yomi

import (
	"strings"
	"unicode"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"

	"github.com/wakamenod/enghi/internal/textnorm"
)

// Reader makes readings. Load it only where the memory is affordable.
type Reader struct{ t *tokenizer.Tokenizer }

// NewReader loads the IPA dictionary (about 270 ms).
func NewReader() (*Reader, error) {
	t, err := tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
	if err != nil {
		return nil, err
	}
	return &Reader{t: t}, nil
}

// Readings returns the reading (キョウハ, トウキョウ) and the pronunciation
// (キョーワ, トーキョー) of a title, both as hiragana with the long-vowel mark
// removed: 「東京は」 -> ("とうきょうは", "ときょわ").
//
// A token the dictionary does not know keeps its surface where that is
// readable as is: katakana becomes hiragana, hiragana stays, ASCII letters and
// digits are lower-cased. Anything else (an unknown kanji, symbols) is
// dropped.
func (r *Reader) Readings(title string) (reading, pronunciation string) {
	var rd, pr strings.Builder
	for _, tok := range r.t.Tokenize(title) {
		if tok.Class == tokenizer.DUMMY {
			continue
		}
		k, ok := tok.Reading()
		if !ok || k == "*" {
			k = tok.Surface
		}
		p, ok := tok.Pronunciation()
		if !ok || p == "*" {
			p = k
		}
		rd.WriteString(Fold(k))
		pr.WriteString(Fold(p))
	}
	return textnorm.NFC(rd.String()), textnorm.NFC(pr.String())
}

// Fold keeps what a romaji query can reach: katakana becomes hiragana, ASCII
// letters are lower-cased, digits stay, and everything else is dropped -
// including the long-vowel mark 「ー」, so that saba finds サーバー and the
// reading and the query agree however long vowels are spelled.
func Fold(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'ァ' && c <= 'ヶ':
			b.WriteRune(c - 'ァ' + 'ぁ')
		case c >= 'ぁ' && c <= 'ゖ':
			b.WriteRune(c)
		case c < unicode.MaxASCII && (unicode.IsLetter(c) || unicode.IsDigit(c)):
			b.WriteRune(unicode.ToLower(c))
		}
	}
	return b.String()
}
