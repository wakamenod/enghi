// Package enghi はテンプレートと静的ファイルの埋め込みだけを持つ。
// embed は親ディレクトリを辿れないため、DESIGN.md のディレクトリ構成
// (web/templates, web/static)を保ったままリポジトリ直下に置いている。
package enghi

import "embed"

//go:embed web/templates
var TemplatesFS embed.FS

//go:embed web/static
var StaticFS embed.FS
