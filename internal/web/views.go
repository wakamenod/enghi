package web

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/wakamenod/enghi/internal/export"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/settings"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// viewData is what every screen has in common.
type viewData struct {
	Title string
	Nav   string // which nav item to highlight
	Query string
	Flash string
	Err   string
	Data  any

	// render fills in the rest; leaving it to each handler always misses one
	LangCode string            // the current language
	Path     string            // where to return after switching language
	Strings  template.JS       // messages used from JS, as JSON
	Set      settings.Settings // settings that decide what a screen shows
	Side     bool              // whether to show the tag column, on article screens
	SideTags []wiki.TagCount   // its contents
}

func (s *Server) viewDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.dashboardData(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "dashboard.html", viewData{Title: s.tr(r, "dash.title"), Nav: "dashboard", Data: d})
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
	s.render(w, r, "pages.html", viewData{Title: s.tr(r, "page.list_title"), Nav: "wiki",
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
			// Try the title too, aliases included. This is what makes a [[...]]
			// jump from Emacs land.
			if p2, err2 := s.pages.ByTitle(ctxOf(r), slug); err2 == nil {
				http.Redirect(w, r, "/wiki/"+p2.Slug, http.StatusSeeOther)
				return
			}
			s.renderNotFound(w, r, slug)
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
	s.render(w, r, "page.html", viewData{Title: p.Title, Nav: "wiki",
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

func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request, slug string) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, r, "notfound.html", viewData{
		Title: s.tr(r, "page.not_found_title"), Nav: "wiki", Data: slug})
}

type editData struct {
	Page    *wiki.Page
	IsNew   bool
	TagText string
}

func (s *Server) viewPageNew(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	p := &wiki.Page{Title: title, Version: 0}
	s.render(w, r, "edit.html", viewData{Title: s.tr(r, "edit.new"), Nav: "wiki",
		Data: editData{Page: p, IsNew: true}})
}

func (s *Server) viewPageEdit(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r, r.PathValue("slug"))
		return
	}
	s.render(w, r, "edit.html", viewData{Title: s.tr(r, "edit.editing", p.Title), Nav: "wiki",
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
		s.renderNotFound(w, r, r.PathValue("slug"))
		return
	}
	revs, _ := s.pages.Revisions(ctxOf(r), p.ID)
	aliases, _ := s.pages.Aliases(ctxOf(r), p.ID)
	s.render(w, r, "history.html", viewData{Title: s.tr(r, "history.title", p.Title), Nav: "wiki",
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
	s.render(w, r, "tag.html", viewData{Title: s.tr(r, "tag.title", name), Nav: "tags",
		Data: tagData{Tag: name, Pages: pages}})
}

func (s *Server) viewTagList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.pages.Tags(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "tags.html", viewData{Title: s.tr(r, "tag.list_title"), Nav: "tags", Data: tags})
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
	s.render(w, r, "search.html", viewData{Title: s.tr(r, "search.title"), Nav: "search", Query: q,
		Data: searchData{Query: q, Results: results}})
}

// uiSearchFragment serves htmx. It is called on every keystroke, so it stays
// cheap (DESIGN 6).
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
	s.renderFragment(w, r, "search_suggest.html", searchData{Query: q, Results: results})
}

// ---------------------------------------------------------------- form posts

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
		s.renderEditError(w, r, err, &wiki.Page{Title: in.Title, Body: in.Body, Tags: in.Tags}, true)
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
		s.renderEditError(w, r, err, cur, false)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}

// renderEditError returns to the edit screen. **The input is never thrown
// away** (DESIGN 4.2).
func (s *Server) renderEditError(w http.ResponseWriter, r *http.Request, err error,
	cur *wiki.Page, isNew bool) {

	msg := err.Error()
	var vc *wiki.VersionConflictError
	var tc *wiki.TitleConflictError
	switch {
	case errors.As(err, &vc):
		msg = s.tr(r, "edit.version_conflict", vc.Current.Version)
	case errors.As(err, &tc):
		msg = s.tr(r, "edit.title_conflict",
			tc.Conflicting.Title+" (/wiki/"+tc.Conflicting.Slug+")")
	}
	w.WriteHeader(http.StatusConflict)
	s.render(w, r, "edit.html", viewData{
		Title: s.tr(r, "edit.save_failed"), Nav: "wiki", Err: msg,
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
		s.renderNotFound(w, r, r.PathValue("slug"))
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
		s.renderNotFound(w, r, r.PathValue("slug"))
		return
	}
	if err := s.pages.DeleteAlias(ctxOf(r), p.ID, r.FormValue("alias")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/wiki/"+p.Slug+"/history", http.StatusSeeOther)
}

// uiRewriteReferences rewrites the bodies of referring pages in bulk.
// **The user runs this explicitly; it never happens automatically**
// (DESIGN 2.5).
func (s *Server) uiRewriteReferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.renderNotFound(w, r, r.PathValue("slug"))
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
	redirectBack(w, r, "/settings")
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
