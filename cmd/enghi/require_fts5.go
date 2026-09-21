//go:build !sqlite_fts5

// このファイルは build tag sqlite_fts5 が無いときだけコンパイル対象になり、
// 必ずコンパイルエラーになる。
//
// タグを付け忘れても go-sqlite3 のビルド自体は通ってしまい、
// 失敗が起動時(store.verifyFTS)まで遅れる。検索は最重要の非機能要件(DESIGN 3)で
// あり、FTS5 の無いバイナリは配っても意味が無いので、ここで手前に一枚置く。
package main

func init() {
	enghi_requires_build_tag_sqlite_fts5__use_make_build()
}
