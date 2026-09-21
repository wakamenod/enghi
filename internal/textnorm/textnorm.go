// Package textnorm は文字列の正規化を担う。
//
// **macOS は日本語を NFD(分解形)で渡してくる経路が多い。**
// 「ビ」が「ヒ」+ 濁点の2文字になっている状態で、見た目は同じだが
// バイト列としては別物になる。タイトルで引く Wiki では、これを揃えないと
// 「同じ名前なのに見つからない」「同じ名前のページが2つ作れてしまう」が起きる。
//
// 保存も検索も NFC に揃えることで、入力経路の違いを吸収する。
package textnorm

import "golang.org/x/text/unicode/norm"

// NFC は文字列を合成形に正規化する。
// 既に NFC なら何もしない(その判定は norm 側が安く済ませる)。
func NFC(s string) string {
	if norm.NFC.IsNormalString(s) {
		return s
	}
	return norm.NFC.String(s)
}

// IsNFC は正規化済みかどうか。doctor の検査に使う。
func IsNFC(s string) bool { return norm.NFC.IsNormalString(s) }

// Slice は文字列の並びをまとめて正規化する。
func Slice(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = NFC(s)
	}
	return out
}
