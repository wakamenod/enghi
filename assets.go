// Package enghi はテンプレートと静的ファイルの埋め込みだけを持つ。
// embed は親ディレクトリを辿れないため、DESIGN.md のディレクトリ構成
// (web/templates, web/static)を保ったままリポジトリ直下に置いている。
package enghi

import "embed"

// 【注意】ディレクトリ指定の embed は `_` や `.` で始まるファイルを除外する。
// 部分テンプレートに _ 接頭辞を付けると静かに埋め込まれず、実行時に
// 「no such template」で落ちるので、その命名をしないこと。
//
//go:embed web/templates
var TemplatesFS embed.FS

//go:embed web/static
var StaticFS embed.FS
