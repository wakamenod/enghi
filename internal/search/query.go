// Package search は横断検索を実装する。
// **DESIGN.md 3 節は 3 万件の実データによる計測に基づく確定仕様である。推測で簡略化しないこと。**
package search

import (
	"strings"
	"unicode"
)

// Phrase はクエリを FTS5 のフレーズリテラルにする(DESIGN 3.3)。
//  1. クエリ内の " を "" に置換する
//  2. 全体を " で囲む
//
// これをしないと C++ や a"b で FTS5 の構文エラーになり 500 を返す。
// 結果として常にフレーズ検索になるが、trigram では部分一致と等価なのでそれが望ましい。
//
// **クエリを空白や句読点で分割して AND で結ぶ前処理をしてはいけない**(DESIGN 3.2)。
// 日本語クエリには空白がないため空振りし、英数字クエリでは意図せず精度を落とす。
func Phrase(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

// LikeEscape は LIKE のワイルドカードを無効化する。ESCAPE '\' と併用すること(DESIGN 3.6)。
func LikeEscape(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

// RuneLen はクエリ長(文字数)。バイト数ではないので注意。日本語の2文字は 6 バイトある。
func RuneLen(q string) int { return len([]rune(q)) }

// IsASCII2 は「ASCII 2 文字のクエリ」かどうか。
// Go / UI / DB / AI のような技術メモで日常的なクエリが該当する(DESIGN 3.4)。
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

// WordBoundaryMatch は s の中に q が語境界で現れるかを判定する。
// **ASCII 2 文字クエリにのみ適用すること**(DESIGN 3.4)。
// LIKE '%go%' は algorithm や going に当たるため、アプリ側で落とす必要がある。
// 日本語 2 文字クエリに適用してはいけない(語境界の概念がないため全部落ちる)。
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
		return false // 文字列の端は語境界
	}
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		b >= 0x80 // マルチバイト文字の隣接は語の一部とみなす
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

// Truncations は 0 件時のフォールバック用に、クエリを後ろから切り詰めた候補を返す。
// 「オフィスの移転について」→「オフィスの移転」→「オフィス」。**2 段までとする**(DESIGN 3.4)。
func Truncations(q string) []string {
	rs := []rune(q)
	var out []string
	for i := 0; i < 2; i++ {
		// 3 分の 2 ずつに切り詰める。3 文字を下回ったら打ち切り(trigram で引けなくなるため)
		n := len(rs) * 2 / 3
		if n < 3 || n >= len(rs) {
			break
		}
		rs = rs[:n]
		out = append(out, strings.TrimSpace(string(rs)))
	}
	return out
}
