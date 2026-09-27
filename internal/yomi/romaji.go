package yomi

import "strings"

// romaji is the conversion table, Hepburn and Kunrei both (shi/si, chi/ti,
// tsu/tu, fu/hu, ji/zi, sha/sya, ja/jya/zya ...), plus the x/l prefix for the
// small kana. っ from a doubled consonant and ん are rules in ToKana, not
// entries here.
var romaji = map[string]string{
	"a": "あ", "i": "い", "u": "う", "e": "え", "o": "お",

	"ka": "か", "ki": "き", "ku": "く", "ke": "け", "ko": "こ",
	"kya": "きゃ", "kyu": "きゅ", "kyo": "きょ",
	"ga": "が", "gi": "ぎ", "gu": "ぐ", "ge": "げ", "go": "ご",
	"gya": "ぎゃ", "gyu": "ぎゅ", "gyo": "ぎょ",

	"sa": "さ", "si": "し", "shi": "し", "su": "す", "se": "せ", "so": "そ",
	"sha": "しゃ", "sya": "しゃ", "shu": "しゅ", "syu": "しゅ", "sho": "しょ", "syo": "しょ",
	"she": "しぇ", "sye": "しぇ",
	"za": "ざ", "zi": "じ", "ji": "じ", "zu": "ず", "ze": "ぜ", "zo": "ぞ",
	"ja": "じゃ", "jya": "じゃ", "zya": "じゃ", "ju": "じゅ", "jyu": "じゅ", "zyu": "じゅ",
	"jo": "じょ", "jyo": "じょ", "zyo": "じょ", "je": "じぇ", "jye": "じぇ", "zye": "じぇ",

	"ta": "た", "ti": "ち", "chi": "ち", "tu": "つ", "tsu": "つ", "te": "て", "to": "と",
	"cha": "ちゃ", "cya": "ちゃ", "tya": "ちゃ", "chu": "ちゅ", "cyu": "ちゅ", "tyu": "ちゅ",
	"cho": "ちょ", "cyo": "ちょ", "tyo": "ちょ", "che": "ちぇ", "cye": "ちぇ", "tye": "ちぇ",
	"thi": "てぃ", "tsa": "つぁ",
	"da": "だ", "di": "ぢ", "du": "づ", "de": "で", "do": "ど",
	"dya": "ぢゃ", "dyu": "ぢゅ", "dyo": "ぢょ", "dhi": "でぃ",

	"na": "な", "ni": "に", "nu": "ぬ", "ne": "ね", "no": "の",
	"nya": "にゃ", "nyu": "にゅ", "nyo": "にょ",

	"ha": "は", "hi": "ひ", "hu": "ふ", "fu": "ふ", "he": "へ", "ho": "ほ",
	"hya": "ひゃ", "hyu": "ひゅ", "hyo": "ひょ",
	"fa": "ふぁ", "fi": "ふぃ", "fe": "ふぇ", "fo": "ふぉ",
	"ba": "ば", "bi": "び", "bu": "ぶ", "be": "べ", "bo": "ぼ",
	"bya": "びゃ", "byu": "びゅ", "byo": "びょ",
	"pa": "ぱ", "pi": "ぴ", "pu": "ぷ", "pe": "ぺ", "po": "ぽ",
	"pya": "ぴゃ", "pyu": "ぴゅ", "pyo": "ぴょ",
	"va": "ゔぁ", "vi": "ゔぃ", "vu": "ゔ", "ve": "ゔぇ", "vo": "ゔぉ",

	"ma": "ま", "mi": "み", "mu": "む", "me": "め", "mo": "も",
	"mya": "みゃ", "myu": "みゅ", "myo": "みょ",
	"ya": "や", "yu": "ゆ", "ye": "いぇ", "yo": "よ",
	"ra": "ら", "ri": "り", "ru": "る", "re": "れ", "ro": "ろ",
	"rya": "りゃ", "ryu": "りゅ", "ryo": "りょ",
	"wa": "わ", "wi": "うぃ", "we": "うぇ", "wo": "を",

	"xa": "ぁ", "xi": "ぃ", "xu": "ぅ", "xe": "ぇ", "xo": "ぉ",
	"la": "ぁ", "li": "ぃ", "lu": "ぅ", "le": "ぇ", "lo": "ぉ",
	"xya": "ゃ", "xyu": "ゅ", "xyo": "ょ", "lya": "ゃ", "lyu": "ゅ", "lyo": "ょ",
	"xtu": "っ", "xtsu": "っ", "ltu": "っ", "ltsu": "っ",
	"xwa": "ゎ", "lwa": "ゎ", "xka": "ゕ", "xke": "ゖ",

	"-": "ー",
}

// maxKey is the longest key in romaji; partial holds every proper prefix of a
// key, to tell an unfinished syllable at the end from input that is not
// romaji at all.
var maxKey, partial = func() (int, map[string]bool) {
	n, p := 0, map[string]bool{}
	for k := range romaji {
		n = max(n, len(k))
		for i := 1; i < len(k); i++ {
			p[k[:i]] = true
		}
	}
	return n, p
}()

// ToKana converts a romaji query to hiragana, as an IME would, for matching
// against the stored readings. ok is false when the query is not romaji
// (test, http, xyz) or leaves no kana; the reading path is then skipped.
//
//   - A doubled consonant is っ (kk -> っk, and tch -> っch).
//   - nn, n' and an n not before a vowel or y are ん; nn before a vowel or y
//     is ん plus the next syllable, so konnichiha is こんにちは.
//   - **What is still being typed at the end is dropped**: an unfinished
//     consonant (the s of kens) and a lone final n. kan would otherwise become
//     かん and stop matching かな while the user is on the way to kana.
//   - Digits pass through, as the readings keep them. 「ー」 is dropped, as
//     Fold drops it from the readings.
func ToKana(q string) (string, bool) {
	s := strings.ToLower(q)
	var b strings.Builder
	kana := false
	for i := 0; i < len(s); {
		c := s[i]
		if c >= '0' && c <= '9' {
			b.WriteByte(c)
			i++
			continue
		}
		if c == 'n' {
			// A lone n at the end is still being typed
			if i+1 == len(s) {
				break
			}
			switch next := s[i+1]; {
			case next == '\'':
				b.WriteString("ん")
				kana, i = true, i+2
				continue
			case next == 'n':
				b.WriteString("ん")
				kana = true
				if i+2 < len(s) && isVowelOrY(s[i+2]) {
					i++ // the second n starts the next syllable
				} else {
					i += 2
				}
				continue
			case !isVowelOrY(next):
				b.WriteString("ん")
				kana, i = true, i+1
				continue
			}
		}
		if matched := longest(s[i:]); matched != "" {
			b.WriteString(romaji[matched])
			kana = kana || matched != "-"
			i += len(matched)
			continue
		}
		// っ from a doubled consonant (not n, handled above)
		if i+1 < len(s) && isConsonant(c) && (s[i+1] == c || (c == 't' && s[i+1] == 'c')) {
			b.WriteString("っ")
			kana, i = true, i+1
			continue
		}
		// The unfinished tail is dropped; anything else is not romaji
		if partial[s[i:]] {
			break
		}
		return "", false
	}
	out := strings.ReplaceAll(b.String(), "ー", "")
	if !kana || out == "" {
		return "", false
	}
	return out, true
}

func longest(s string) string {
	for n := min(maxKey, len(s)); n > 0; n-- {
		if _, ok := romaji[s[:n]]; ok {
			return s[:n]
		}
	}
	return ""
}

func isVowelOrY(c byte) bool { return strings.IndexByte("aiueoy", c) >= 0 }

func isConsonant(c byte) bool { return c >= 'a' && c <= 'z' && strings.IndexByte("aiueon", c) < 0 }
