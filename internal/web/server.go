// Package web は HTTP ハンドラ、テンプレート、静的ファイルを担う。
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	enghi "github.com/wakamenod/enghi"
	"github.com/wakamenod/enghi/internal/config"
	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Server は HTTP 層。
type Server struct {
	cfg    config.Config
	db     *store.DB
	pages  *wiki.Service
	gtd    *gtd.Service
	search *search.Service
	hub    *Hub
	tmpl   *template.Template
	mux    *http.ServeMux
}

// New はサーバを組み立てる。
func New(cfg config.Config, db *store.DB) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:    cfg,
		db:     db,
		pages:  wiki.New(db, cfg.RevisionCompactMinutes),
		gtd:    gtd.New(db),
		search: search.New(db),
		hub:    NewHub(),
		tmpl:   tmpl,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"shortTime": shortTime,
		"join":      strings.Join,
		"add":       func(a, b int) int { return a + b },
		"kindLabel": kindLabel,
		"snippet":   search.SnippetHTML,
		"list":      func(vals ...string) []string { return vals },
		// eqID は *int64 と int64 を比べる。テンプレートの eq は型が違うと実行時エラーになる。
		"eqID": func(p *int64, id int64) bool { return p != nil && *p == id },
	}
	return template.New("").Funcs(funcs).ParseFS(enghi.TemplatesFS, "web/templates/*.html")
}

// Handler は 4.4 節のセキュリティ3層を通したハンドラを返す。
func (s *Server) Handler() http.Handler { return s.secure(s.mux) }

// Hub は focus チャネル。CLI からの通知にも使う。
func (s *Server) Hub() *Hub { return s.hub }

func (s *Server) routes() {
	m := s.mux

	// ---- 画面(すべてに安定した URL を割り当てる。DESIGN 4.1)
	m.HandleFunc("GET /{$}", s.viewDashboard)
	m.HandleFunc("GET /wiki", s.viewPageList)
	m.HandleFunc("GET /wiki/new", s.viewPageNew)
	m.HandleFunc("GET /wiki/{slug}", s.viewPage)
	m.HandleFunc("GET /wiki/{slug}/edit", s.viewPageEdit)
	m.HandleFunc("GET /wiki/{slug}/history", s.viewPageHistory)
	m.HandleFunc("GET /tags/{name}", s.viewTag)
	m.HandleFunc("GET /tags", s.viewTagList)
	m.HandleFunc("GET /search", s.viewSearch)
	m.HandleFunc("GET /gtd", s.viewGTDTop)
	m.HandleFunc("GET /gtd/inbox", s.viewInbox)
	m.HandleFunc("GET /gtd/next", s.viewNextActions)
	m.HandleFunc("GET /gtd/waiting", s.viewWaiting)
	m.HandleFunc("GET /gtd/scheduled", s.viewScheduled)
	m.HandleFunc("GET /gtd/someday", s.viewSomeday)
	m.HandleFunc("GET /gtd/projects", s.viewProjects)
	m.HandleFunc("GET /gtd/project/{id}", s.viewProject)
	m.HandleFunc("GET /gtd/areas", s.viewAreas)
	m.HandleFunc("GET /gtd/area/{id}", s.viewArea)
	m.HandleFunc("GET /gtd/review", s.viewReview)
	m.HandleFunc("GET /gtd/clarify/{id}", s.viewClarify)

	// ---- UI からの form 送信(htmx / 素の form)
	m.HandleFunc("POST /ui/pages", s.uiCreatePage)
	m.HandleFunc("POST /ui/pages/{slug}", s.uiUpdatePage)
	m.HandleFunc("POST /ui/pages/{slug}/delete", s.uiDeletePage)
	m.HandleFunc("POST /ui/pages/{slug}/aliases", s.uiAddAlias)
	m.HandleFunc("POST /ui/pages/{slug}/aliases/delete", s.uiDeleteAlias)
	m.HandleFunc("POST /ui/pages/{slug}/rewrite-references", s.uiRewriteReferences)
	m.HandleFunc("POST /ui/export", s.uiExport)
	m.HandleFunc("POST /ui/tasks", s.uiCapture)
	m.HandleFunc("POST /ui/tasks/{id}", s.uiPatchTask)
	m.HandleFunc("POST /ui/tasks/{id}/complete", s.uiCompleteTask)
	m.HandleFunc("POST /ui/tasks/{id}/skip", s.uiSkipTask)
	m.HandleFunc("POST /ui/tasks/{id}/end-series", s.uiEndSeries)
	m.HandleFunc("POST /ui/tasks/{id}/file", s.uiFileAsReference)
	m.HandleFunc("POST /ui/tasks/{id}/delete", s.uiDeleteTask)
	m.HandleFunc("POST /ui/projects", s.uiCreateProject)
	m.HandleFunc("POST /ui/projects/{id}", s.uiPatchProject)
	m.HandleFunc("POST /ui/areas", s.uiCreateArea)
	m.HandleFunc("POST /ui/areas/{id}", s.uiPatchArea)
	m.HandleFunc("POST /ui/contexts", s.uiCreateContext)
	m.HandleFunc("POST /ui/review/{id}/check", s.uiReviewCheck)
	m.HandleFunc("POST /ui/review/{id}/complete", s.uiReviewComplete)
	m.HandleFunc("GET /ui/search", s.uiSearchFragment) // 打鍵ごとのインクリメンタル検索
	m.HandleFunc("POST /ui/preview", s.uiPreview)      // 編集画面のプレビュー

	// ---- API(JSON。Emacs 層が依存するため独立に成立させる。DESIGN 4.2)
	m.HandleFunc("GET /api/search", s.apiSearch)
	m.HandleFunc("GET /api/pages", s.apiListPages)
	m.HandleFunc("POST /api/pages", s.apiCreatePage)
	m.HandleFunc("GET /api/pages/{slug}", s.apiGetPage)
	m.HandleFunc("PUT /api/pages/{slug}", s.apiUpdatePage)
	m.HandleFunc("DELETE /api/pages/{slug}", s.apiDeletePage)
	m.HandleFunc("GET /api/pages/{slug}/backlinks", s.apiBacklinks)
	m.HandleFunc("GET /api/pages/{slug}/revisions", s.apiRevisions)
	m.HandleFunc("GET /api/tags", s.apiTags)
	m.HandleFunc("GET /api/dashboard", s.apiDashboard)
	m.HandleFunc("GET /api/doctor", s.apiDoctor)
	m.HandleFunc("POST /api/focus", s.handleFocus)
	m.HandleFunc("GET /api/events", s.handleEvents)
	m.HandleFunc("GET /api/status", s.handleStatus)
	m.HandleFunc("POST /api/export", s.apiExport)
	m.HandleFunc("POST /api/backup", s.apiBackup)
	m.HandleFunc("GET /api/backups", s.apiBackups)

	m.HandleFunc("GET /api/tasks", s.apiListTasks)
	m.HandleFunc("POST /api/tasks", s.apiCreateTask)
	m.HandleFunc("GET /api/tasks/{id}", s.apiGetTask)
	m.HandleFunc("PATCH /api/tasks/{id}", s.apiPatchTask)
	m.HandleFunc("DELETE /api/tasks/{id}", s.apiDeleteTask)
	m.HandleFunc("POST /api/tasks/{id}/complete", s.apiCompleteTask)
	m.HandleFunc("POST /api/tasks/{id}/file", s.apiFileAsReference)
	m.HandleFunc("GET /api/projects", s.apiListProjects)
	m.HandleFunc("POST /api/projects", s.apiCreateProject)
	m.HandleFunc("GET /api/projects/stalled", s.apiStalledProjects)
	m.HandleFunc("GET /api/projects/{id}", s.apiGetProject)
	m.HandleFunc("PATCH /api/projects/{id}", s.apiPatchProject)
	m.HandleFunc("GET /api/areas", s.apiListAreas)
	m.HandleFunc("POST /api/areas", s.apiCreateArea)
	m.HandleFunc("PATCH /api/areas/{id}", s.apiPatchArea)
	m.HandleFunc("GET /api/contexts", s.apiListContexts)
	m.HandleFunc("POST /api/contexts", s.apiCreateContext)
	m.HandleFunc("GET /api/review", s.apiReview)
	m.HandleFunc("GET /api/series", s.apiSeries)

	// ---- 静的ファイル
	static, err := fs.Sub(enghi.StaticFS, "web/static")
	if err != nil {
		panic(err)
	}
	m.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
}

// ---------------------------------------------------------------- 共通

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": errCode, "message": msg})
}

// writeConflict は 409 を返す。
// **error は機械可読なコードで2種を区別すること**(DESIGN 4.2):
//   - version_conflict … 楽観ロックの版不一致。差分を提示してマージさせる。入力は捨てない
//   - title_conflict   … タイトル衝突。別のタイトルを入力させる。本文は保持したまま
func writeConflict(w http.ResponseWriter, err error) bool {
	switch e := err.(type) {
	case *wiki.VersionConflictError:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "version_conflict",
			"message": "このページは他の経路で更新されています。現行データと突き合わせてください",
			"current": e.Current,
		})
		return true
	case *wiki.TitleConflictError:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "title_conflict",
			"message": "同名(大小を区別しない)のページが既に存在します",
			// 衝突相手を必ず返す。無いと利用者は該当ページへ行けない(DESIGN 4.2)。
			"conflicting_page": map[string]any{
				"id": e.Conflicting.ID, "slug": e.Conflicting.Slug, "title": e.Conflicting.Title,
			},
		})
		return true
	}
	return false
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

func shortTime(s string) string {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return s
	}
	now := time.Now().UTC()
	if t.Year() == now.Year() {
		return t.Format("01/02 15:04")
	}
	return t.Format("2006/01/02")
}

func kindLabel(kind string) string {
	switch kind {
	case "page":
		return "記事"
	case "project":
		return "Project"
	case "task":
		return "Task"
	case "area":
		return "Area"
	}
	return kind
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return def
		}
	}
	return n
}

// ctxOf はハンドラ用のコンテキスト(将来のタイムアウト設定の口)。
func ctxOf(r *http.Request) context.Context { return r.Context() }

var _ = fmt.Sprintf
