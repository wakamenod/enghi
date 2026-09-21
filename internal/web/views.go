package web

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/wakamenod/enghi/internal/export"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// viewData は全画面の共通部分。
type viewData struct {
	Title string
	Nav   string // ハイライトするナビ項目
	Query string
	Flash string
	Err   string
	Data  any
}

func (s *Server) viewDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.dashboardData(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "dashboard.html", viewData{Title: "ダッシュボード", Nav: "dashboard", Data: d})
}

type pageListData struct {
	Pages []*wiki.Page
	Sort  string
	Total int
	Tags  []wiki.TagCount
}

func (s *Server) viewPageList(w http.ResponseWriter, r *http.Request) {
	sort := r.URL.Query().Get("sort")
	pages, err := s.pages.List(ctxOf(r), sort, 200, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	total, _ := s.pages.CountPages(ctxOf(r))
	tags, _ := s.pages.Tags(ctxOf(r))
	s.render(w, "pages.html", viewData{Title: "記事一覧", Nav: "wiki",
		Data: pageListData{Pages: pages, Sort: sort, Total: total, Tags: tags}})
}

type pageViewData struct {
	Page      *wiki.Page
	HTML      template.HTML
	Links     []wiki.Link
	Backlinks []wiki.Backlink
	Aliases   []wiki.Alias
}

func (s *Server) viewPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(ctxOf(r), slug)
	if err != nil {
		if errors.Is(err, wiki.ErrNotFound) {
			// タイトル(別名を含む)でも引いてみる。Emacs から [[...]] で飛んできた場合に効く。
			if p2, err2 := s.pages.ByTitle(ctxOf(r), slug); err2 == nil {
				http.Redirect(w, r, "/wiki/"+p2.Slug, http.StatusSeeOther)
				return
			}
			s.renderNotFound(w, slug)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	html, err := s.renderBody(r, p.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	links, _ := s.pages.Links(ctxOf(r), p.ID)
	backlinks, _ := s.pages.Backlinks(ctxOf(r), p.ID)
	aliases, _ := s.pages.Aliases(ctxOf(r), p.ID)
	s.render(w, "page.html", viewData{Title: p.Title, Nav: "wiki",
		Data: pageViewData{Page: p, HTML: html, Links: links, Backlinks: backlinks, Aliases: aliases}})
}

func (s *Server) renderBody(r *http.Request, body string) (template.HTML, error) {
	rd := wiki.NewRenderer(s.pages.ResolverFor(ctxOf(r)))
	out, err := rd.Render(body)
	if err != nil {
		return "", err
	}
	return template.HTML(out), nil
}

func (s *Server) renderNotFound(w http.ResponseWriter, slug string) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, "notfound.html", viewData{Title: "見つかりません", Nav: "wiki", Data: slug})
}

type editData struct {
	Page    *wiki.Page
	IsNew   bool
	TagText string
}

func (s *Server) viewPageNew(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	p := &wiki.Page{Title: title, Version: 0}
	s.render(w, "edit.html", viewData{Title: "新規作成", Nav: "wiki",
		Data: editData{Page: p, IsNew: true}})
}

func (s *Server) viewPageEdit(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r.PathValue("slug"))
		return
	}
	s.render(w, "edit.html", viewData{Title: p.Title + " を編集", Nav: "wiki",
		Data: editData{Page: p, TagText: strings.Join(p.Tags, ", ")}})
}

type historyData struct {
	Page      *wiki.Page
	Revisions []wiki.Revision
	Aliases   []wiki.Alias
}

func (s *Server) viewPageHistory(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r.PathValue("slug"))
		return
	}
	revs, _ := s.pages.Revisions(ctxOf(r), p.ID)
	aliases, _ := s.pages.Aliases(ctxOf(r), p.ID)
	s.render(w, "history.html", viewData{Title: p.Title + " の履歴", Nav: "wiki",
		Data: historyData{Page: p, Revisions: revs, Aliases: aliases}})
}

type tagData struct {
	Tag   string
	Pages []*wiki.Page
}

func (s *Server) viewTag(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	pages, err := s.pages.ByTag(ctxOf(r), name, 200, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "tag.html", viewData{Title: "タグ: " + name, Nav: "wiki",
		Data: tagData{Tag: name, Pages: pages}})
}

func (s *Server) viewTagList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.pages.Tags(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "tags.html", viewData{Title: "タグ一覧", Nav: "wiki", Data: tags})
}

type searchData struct {
	Query   string
	Results []search.Result
}

func (s *Server) viewSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	var results []search.Result
	if q != "" {
		var err error
		results, err = s.search.Search(ctxOf(r), q, nil, 50, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.render(w, "search.html", viewData{Title: "検索", Nav: "search", Query: q,
		Data: searchData{Query: q, Results: results}})
}

// uiSearchFragment は htmx 用。打鍵ごとに呼ばれるので軽く保つ(DESIGN 6)。
func (s *Server) uiSearchFragment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	results, err := s.search.Search(ctxOf(r), q, nil, 10, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "search_suggest.html", searchData{Query: q, Results: results})
}

// ---------------------------------------------------------------- form 送信

func (s *Server) uiCreatePage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in := wiki.CreateInput{
		Title: r.FormValue("title"),
		Body:  r.FormValue("body"),
		Tags:  splitTags(r.FormValue("tags")),
	}
	p, err := s.pages.Create(ctxOf(r), in)
	if err != nil {
		s.renderEditError(w, err, &wiki.Page{Title: in.Title, Body: in.Body, Tags: in.Tags}, true)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}

func (s *Server) uiUpdatePage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in := wiki.UpdateInput{
		Title:   r.FormValue("title"),
		Body:    r.FormValue("body"),
		Tags:    splitTags(r.FormValue("tags")),
		Version: atoiDefault(r.FormValue("version"), 0),
	}
	p, err := s.pages.Update(ctxOf(r), r.PathValue("slug"), in)
	if err != nil {
		cur := &wiki.Page{Slug: r.PathValue("slug"), Title: in.Title, Body: in.Body,
			Tags: in.Tags, Version: in.Version}
		s.renderEditError(w, err, cur, false)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}

// renderEditError は編集画面に戻す。**入力は絶対に捨てない**(DESIGN 4.2)。
func (s *Server) renderEditError(w http.ResponseWriter, err error, cur *wiki.Page, isNew bool) {
	msg := err.Error()
	var vc *wiki.VersionConflictError
	var tc *wiki.TitleConflictError
	switch {
	case errors.As(err, &vc):
		msg = "このページは他の経路で更新されています(現行の版: " +
			itoa(vc.Current.Version) + ")。内容を確認してから保存し直してください。" +
			"入力はこの画面に保持されています"
	case errors.As(err, &tc):
		msg = "同名(大小を区別しない)のページが既に存在します: " + tc.Conflicting.Title +
			" (/wiki/" + tc.Conflicting.Slug + ")"
	}
	w.WriteHeader(http.StatusConflict)
	s.render(w, "edit.html", viewData{Title: "保存できませんでした", Nav: "wiki", Err: msg,
		Data: editData{Page: cur, IsNew: isNew, TagText: strings.Join(cur.Tags, ", ")}})
}

func (s *Server) uiDeletePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := s.pages.Delete(ctxOf(r), slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: slug})
	http.Redirect(w, r, "/wiki", http.StatusSeeOther)
}

func (s *Server) uiAddAlias(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r.PathValue("slug"))
		return
	}
	if err := s.pages.AddAlias(ctxOf(r), p.ID, r.FormValue("alias")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/wiki/"+p.Slug+"/history", http.StatusSeeOther)
}

func (s *Server) uiDeleteAlias(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r.PathValue("slug"))
		return
	}
	if err := s.pages.DeleteAlias(ctxOf(r), p.ID, r.FormValue("alias")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/wiki/"+p.Slug+"/history", http.StatusSeeOther)
}

// uiRewriteReferences は参照元の本文を一括置換する。
// **ユーザが明示的に実行する操作であり、自動では絶対にやらない**(DESIGN 2.5)。
func (s *Server) uiRewriteReferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r.PathValue("slug"))
		return
	}
	n, err := s.pages.RewriteReferences(ctxOf(r), r.FormValue("from"), p.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	http.Redirect(w, r, "/wiki/"+p.Slug+"/history?rewrote="+itoa(n), http.StatusSeeOther)
}

func (s *Server) uiExport(w http.ResponseWriter, r *http.Request) {
	res, err := export.Run(ctxOf(r), s.db, s.files, s.cfg.ExportDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = res
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) uiPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := s.renderBody(r, r.FormValue("body"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func splitTags(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' || r == '\n' })
	out := make([]string, 0, len(f))
	for _, t := range f {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var _ = store.Doctor
