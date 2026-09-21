//go:build !cgo

// このファイルは CGO_ENABLED=0 のときだけコンパイル対象になり、
// 必ずコンパイルエラーになる。
//
// go-sqlite3 は cgo 無しでも「実行すると必ず失敗するスタブ」としてコンパイルが
// 通るため、放っておくと壊れたバイナリが配布物に混ざる。
// なお Go はクロスコンパイル時に CGO_ENABLED を既定で 0 にするので、
// GOOS を跨いだビルドはここで止まる(各 OS の runner でビルドすること)。
package main

func init() {
	enghi_requires_cgo__set_CGO_ENABLED_1()
}
