package wiki

import (
	"bytes"
	"context"
	"database/sql"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Resolver は [[タイトル]] を slug に解決する。第2返値が false なら未解決リンク。
type Resolver func(title string) (slug string, ok bool)

// wikilinkParser は [[Title]] / [[Title|Label]] をリンクとして解釈する goldmark の拡張。
// インラインコードとコードブロックの中身は goldmark 側で先に消費されるため、ここには来ない。
type wikilinkParser struct{ resolve Resolver }

func (p *wikilinkParser) Trigger() []byte { return []byte{'['} }

func (p *wikilinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	end := bytes.Index(line, []byte("]]"))
	if end < 0 {
		return nil
	}
	inner := string(line[2:end])
	if strings.ContainsAny(inner, "[]") {
		return nil
	}
	title, label := strings.TrimSpace(inner), ""
	if i := strings.Index(inner, "|"); i >= 0 {
		title = strings.TrimSpace(inner[:i])
		label = strings.TrimSpace(inner[i+1:])
	}
	if title == "" {
		return nil
	}
	if label == "" {
		// 表示は [[...]] に書かれた文字列をそのまま出す。現タイトルに置換しない(DESIGN 2.5)。
		label = title
	}
	block.Advance(end + 2)
	_ = seg

	link := ast.NewLink()
	if slug, ok := p.resolve(title); ok {
		link.Destination = []byte("/wiki/" + slug)
	} else {
		// 未解決リンク。クリックすると新規作成画面へ行く。
		link.Destination = []byte("/wiki/new?title=" + queryEscape(title))
		link.SetAttributeString("class", []byte("wikilink-new"))
		link.Title = []byte("まだ存在しないページ")
	}
	link.AppendChild(link, ast.NewString([]byte(label)))
	return link
}

func queryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// Renderer は Markdown を HTML にする。
type Renderer struct{ md goldmark.Markdown }

// NewRenderer は GFM + 見出し ID 付きのレンダラを作る。
func NewRenderer(resolve Resolver) *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithInlineParsers(util.Prioritized(&wikilinkParser{resolve: resolve}, 150)),
		),
	)
	return &Renderer{md: md}
}

// Render は Markdown 原文を HTML にする。
func (r *Renderer) Render(src string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ResolverFor はこの Service の page_titles を引く Resolver を返す。
func (s *Service) ResolverFor(ctx context.Context) Resolver {
	cache := map[string]string{}
	return func(title string) (string, bool) {
		if slug, ok := cache[strings.ToLower(title)]; ok {
			return slug, slug != ""
		}
		var slug string
		err := s.db.QueryRowContext(ctx,
			`SELECT p.slug FROM pages p JOIN page_titles t ON t.page_id = p.id WHERE t.title = ?`,
			title).Scan(&slug)
		if err == sql.ErrNoRows {
			cache[strings.ToLower(title)] = ""
			return "", false
		} else if err != nil {
			return "", false
		}
		cache[strings.ToLower(title)] = slug
		return slug, true
	}
}
