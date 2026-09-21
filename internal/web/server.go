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
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/i18n"
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
	files  *filestore.Store
	search *search.Service
	hub    *Hub
	// tmpl は言語ごとに1組。**テンプレートの関数は解析時に束縛されるので、
	// リクエストごとに差し替えられない。**言語の数だけ作っておく。
	tmpl map[i18n.Lang]*template.Template
	mux  *http.ServeMux
}

// New はサーバを組み立てる。
// files は画像などの保管庫(本体 DB とは別ファイル)。
func New(cfg config.Config, db *store.DB, files *filestore.Store) (*Server, error) {
	tmpl := map[i18n.Lang]*template.Template{}
	for _, lang := range i18n.All {
		t, err := parseTemplates(lang)
		if err != nil {
			return nil, err
		}
		tmpl[lang] = t
	}
	s := &Server{
		cfg:    cfg,
		db:     db,
		pages:  wiki.New(db, cfg.RevisionCompactMinutes),
		gtd:    gtd.New(db),
		files:  files,
		search: search.New(db),
		hub:    NewHub(),
		tmpl:   tmpl,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func parseTemplates(lang i18n.Lang) (*template.Template, error) {
	funcs := template.FuncMap{
		"shortTime": shortTime,
		"join":      strings.Join,
		"add":       func(a, b int) int { return a + b },
		"snippet":   search.SnippetHTML,
		"list":      func(vals ...string) []string { return vals },
		// eqID は *int64 と int64 を比べる。テンプレートの eq は型が違うと実行時エラーになる。
		"eqID": func(p *int64, id int64) bool { return p != nil && *p == id },
		// t は文言を引く。解析時に言語が決まる。
		"t":         func(key string, args ...any) string { return i18n.T(lang, key, args...) },
		"kindLabel": func(kind string) string { return i18n.T(lang, "kind."+kind) },
		"lang":      func() string { return string(lang) },
		"langs":     func() []i18n.Lang { return i18n.All },
		"langName":  func(l i18n.Lang) string { return i18n.Name[l] },
	}
	return template.New("").Funcs(funcs).ParseFS(enghi.TemplatesFS, "web/templates/*.html")
}

// Handler は 4.4 節のセキュリティ3層を通したハンドラを返す。
func (s *Server) Handler() http.Handler { return logErrors(s.secure(s.mux)) }

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
	m.HandleFunc("GET /ui/lang", s.handleSetLang)
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
	m.HandleFunc("POST /api/files", s.apiUploadFile)
	m.HandleFunc("GET /api/files", s.apiListFiles)
	m.HandleFunc("DELETE /api/files/{hash}", s.apiDeleteFile)
	m.HandleFunc("GET /files/{hash}", s.serveFile)

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
func (s *Server) writeConflict(w http.ResponseWriter, r *http.Request, err error) bool {
	switch e := err.(type) {
	case *wiki.VersionConflictError:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "version_conflict",
			"message": s.tr(r, "err.version_conflict"),
			"current": e.Current,
		})
		return true
	case *wiki.TitleConflictError:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "title_conflict",
			"message": s.tr(r, "err.title_conflict"),
			// 衝突相手を必ず返す。無いと利用者は該当ページへ行けない(DESIGN 4.2)。
			"conflicting_page": map[string]any{
				"id": e.Conflicting.ID, "slug": e.Conflicting.Slug, "title": e.Conflicting.Title,
			},
		})
		return true
	}
	return false
}

// langOf はこのリクエストで使う言語。
func (s *Server) langOf(r *http.Request) i18n.Lang { return i18n.FromRequest(r) }

// tr はハンドラから文言を引く。
func (s *Server) tr(r *http.Request, key string, args ...any) string {
	return i18n.T(s.langOf(r), key, args...)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	// 言語と現在地は全画面で要るので、ここでまとめて埋める。
	// 各ハンドラに書かせると必ずどこかで漏れる。
	if vd, ok := data.(viewData); ok {
		lang := s.langOf(r)
		vd.LangCode = string(lang)
		vd.Path = r.URL.RequestURI()
		vd.Strings = template.JS(jsStrings(lang))
		data = vd
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl[s.langOf(r)].ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl[s.langOf(r)].ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

// jsStrings は JS 側で使う文言を JSON にする。
// **JS の中に文言を直書きしない。**片方だけ翻訳が漏れる。
func jsStrings(lang i18n.Lang) string {
	keys := []string{
		"theme.auto", "theme.light", "theme.dark", "theme.toggle", "gtd.capture_short",
		"capture.title", "capture.hint", "capture.added", "capture.failed",
		"capture.uploading", "capture.upload_failed",
	}
	m := make(map[string]string, len(keys))
	for _, k := range keys {
		m[k] = i18n.T(lang, k)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// handleSetLang は言語の選択を cookie に覚えさせる。
// **サーバ側で描画するので、選択はブラウザではなくリクエストと一緒に来る必要がある。**
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	l := r.URL.Query().Get("set")
	if !i18n.Valid(l) {
		http.Error(w, "unknown language", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     i18n.CookieName,
		Value:    l,
		Path:     "/",
		MaxAge:   400 * 24 * 60 * 60,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
	dest := r.URL.Query().Get("return_to")
	if dest == "" || !strings.HasPrefix(dest, "/") {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
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
