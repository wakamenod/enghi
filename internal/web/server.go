// Package web owns the HTTP handlers, the templates and the static files.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	enghi "github.com/wakamenod/enghi"
	"github.com/wakamenod/enghi/internal/calendar"
	"github.com/wakamenod/enghi/internal/config"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/i18n"
	"github.com/wakamenod/enghi/internal/search"
	"github.com/wakamenod/enghi/internal/settings"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// Server is the HTTP layer.
type Server struct {
	cfg    config.Config
	db     *store.DB
	pages  *wiki.Service
	gtd    *gtd.Service
	files  *filestore.Store
	search *search.Service
	set    *settings.Service
	cal    *calendar.Service
	sync   *calendar.Syncer
	hub    *Hub
	// One template set per language. **Template functions are bound at parse
	// time and cannot be swapped per request**, so we build as many sets as
	// there are languages.
	tmpl map[i18n.Lang]*template.Template
	mux  *http.ServeMux
}

// New assembles the server. files is the store for images and the like, a
// separate file from the main database.
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
		set:    settings.New(db),
		cal:    calendar.New(db),
		hub:    NewHub(),
		tmpl:   tmpl,
		mux:    http.NewServeMux(),
	}
	// No runner until UseShortcuts: the sync does nothing, the rest works
	s.sync = calendar.NewSyncer(s.cal, s.set, nil, cfg.CalendarShortcut, cfg.CalendarSyncInterval, false)
	s.routes()
	return s, nil
}

func parseTemplates(lang i18n.Lang) (*template.Template, error) {
	funcs := template.FuncMap{
		"shortTime": shortTime,
		"base":      filepath.Base,
		"tilde":     config.Abbrev, // a path under the home directory as ~/...
		"join":      strings.Join,
		"add":       func(a, b int) int { return a + b },
		"snippet":   search.SnippetHTML,
		"list":      func(vals ...string) []string { return vals },
		// eqID compares *int64 with int64; the template eq fails at run time when
		// the types differ.
		"eqID": func(p *int64, id int64) bool { return p != nil && *p == id },
		// t looks a message up; the language is fixed at parse time.
		"t":         func(key string, args ...any) string { return i18n.T(lang, key, args...) },
		"kindLabel": func(kind string) string { return i18n.T(lang, "kind."+kind) },
		// markLabel is the head of a start/pause mark, as "⏸ paused 10:00".
		"markLabel": func(kind, movedTo, when string) string { return markLabel(lang, kind, movedTo, when) },
		// sinceClock / elapsed are a working task's start and how long ago it
		// was (dashboard.go)
		"sinceClock": func(since string) string { return sinceClock(since, time.Now()) },
		"elapsed":    func(since string) string { return elapsed(lang, since, time.Now()) },
		"lang":       func() string { return string(lang) },
		"langs":      func() []i18n.Lang { return i18n.All },
		"langName":   func(l i18n.Lang) string { return i18n.Name[l] },
		// asset appends the content hash to a static file URL (static.go)
		"asset": assetURL,
	}
	return template.New("").Funcs(funcs).ParseFS(enghi.TemplatesFS, "web/templates/*.html")
}

// Handler returns the handler wrapped in the three security layers of
// section 4.4.
func (s *Server) Handler() http.Handler { return logErrors(s.secure(s.mux)) }

// Hub is the focus channel, also used for notifications from the CLI.
func (s *Server) Hub() *Hub { return s.hub }

func (s *Server) routes() {
	m := s.mux

	// ---- screens; every one gets a stable URL (DESIGN 4.1)
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
	m.HandleFunc("GET /gtd/day", s.viewDay)
	m.HandleFunc("GET /gtd/day/{date}", s.viewDay)
	m.HandleFunc("GET /settings", s.viewSettings)
	m.HandleFunc("GET /guide", s.viewGuideIndex)
	m.HandleFunc("GET /guide/{topic}", s.viewGuide)

	// ---- form posts from the UI (htmx and plain forms)
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
	m.HandleFunc("POST /ui/tasks/{id}/logs", s.uiAddLog)
	m.HandleFunc("POST /ui/tasks/{id}/start", s.uiStartTask)
	m.HandleFunc("POST /ui/tasks/{id}/pause", s.uiPauseTask)
	m.HandleFunc("POST /ui/task-logs/{id}", s.uiEditLog)
	m.HandleFunc("POST /ui/task-logs/{id}/delete", s.uiDeleteLog)
	m.HandleFunc("POST /ui/projects", s.uiCreateProject)
	m.HandleFunc("POST /ui/projects/{id}", s.uiPatchProject)
	m.HandleFunc("POST /ui/areas", s.uiCreateArea)
	m.HandleFunc("POST /ui/areas/{id}", s.uiPatchArea)
	m.HandleFunc("POST /ui/contexts", s.uiCreateContext)
	m.HandleFunc("POST /ui/review/{id}/check", s.uiReviewCheck)
	m.HandleFunc("POST /ui/review/{id}/complete", s.uiReviewComplete)
	m.HandleFunc("POST /ui/settings", s.uiUpdateSettings)
	m.HandleFunc("POST /ui/backup", s.uiBackup)
	m.HandleFunc("POST /ui/install-skill", s.uiInstallSkill)
	m.HandleFunc("POST /ui/calendar", s.uiCalendarSettings)
	m.HandleFunc("POST /ui/calendar/sync", s.uiCalendarSync)
	m.HandleFunc("POST /ui/calendar/install", s.uiCalendarInstall)
	m.HandleFunc("POST /ui/calendar/events/{id}/task", s.uiEventTask)
	m.HandleFunc("GET /ui/lang", s.handleSetLang)
	m.HandleFunc("GET /ui/search", s.uiSearchFragment) // incremental search, per keystroke
	m.HandleFunc("POST /ui/preview", s.uiPreview)      // preview on the edit screen

	// ---- API (JSON). It stands on its own because the Emacs layer depends on
	// it (DESIGN 4.2)
	m.HandleFunc("GET /api/search", s.apiSearch)
	m.HandleFunc("GET /api/titles", s.apiTitles) // candidates for [[...]] completion
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
	m.HandleFunc("GET /api/settings", s.apiSettings)
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
	m.HandleFunc("GET /api/tasks/{id}/logs", s.apiListLogs)
	m.HandleFunc("POST /api/tasks/{id}/logs", s.apiAddLog)
	m.HandleFunc("PATCH /api/task-logs/{id}", s.apiEditLog)
	m.HandleFunc("DELETE /api/task-logs/{id}", s.apiDeleteLog)
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
	m.HandleFunc("GET /api/day", s.apiDay)
	m.HandleFunc("GET /api/days", s.apiDays)
	m.HandleFunc("GET /api/calendar/events", s.apiCalendarEvents)
	m.HandleFunc("PUT /api/calendar/events", s.apiPutCalendarEvents)
	m.HandleFunc("POST /api/calendar/events/{id}/task", s.apiEventTask)
	m.HandleFunc("GET /api/calendar/status", s.apiCalendarStatus)
	m.HandleFunc("POST /api/calendar/sync", s.apiCalendarSync)

	// ---- static files, with an ETag and ?v= from the content hash (static.go)
	m.HandleFunc("GET /static/", serveStatic)
}

// ---------------------------------------------------------------- shared

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

// writeConflict answers 409.
// **The error field distinguishes the two cases with a machine-readable code**
// (DESIGN 4.2):
//   - version_conflict ... optimistic-lock mismatch. Show the difference and let
//     the user merge; never throw the input away
//   - title_conflict   ... title collision. Ask for another title, keeping the
//     body as it is
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
			// Always return what it collided with; without it the user cannot get
			// to that page (DESIGN 4.2).
			"conflicting_page": map[string]any{
				"id": e.Conflicting.ID, "slug": e.Conflicting.Slug, "title": e.Conflicting.Title,
			},
		})
		return true
	}
	return false
}

// langOf is the language for this request.
func (s *Server) langOf(r *http.Request) i18n.Lang { return i18n.FromRequest(r) }

// sideNav lists the screens that show the tag column on the left.
var sideNav = map[string]bool{"dashboard": true, "wiki": true, "tags": true, "search": true}

// settings are the settings for this request.
// **A screen still renders when they cannot be read.** Failing to read a
// setting and failing to show a page are different problems.
func (s *Server) settings(r *http.Request) settings.Settings {
	set, err := s.set.Load(ctxOf(r))
	if err != nil {
		log.Printf("settings: %v", err)
	}
	return set
}

// tr looks a message up from a handler.
func (s *Server) tr(r *http.Request, key string, args ...any) string {
	return i18n.T(s.langOf(r), key, args...)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	// The language and the current location are needed on every screen, so they
	// are filled in here. Leaving it to each handler always misses one.
	if vd, ok := data.(viewData); ok {
		lang := s.langOf(r)
		vd.LangCode = string(lang)
		vd.Path = r.URL.RequestURI()
		vd.Strings = template.JS(jsStrings(lang))
		vd.Set = s.settings(r)
		// The tag column appears on article screens only, not on GTD or the guide
		// (the guide has a table of contents of its own on the left).
		if vd.Side = sideNav[vd.Nav]; vd.Side {
			vd.SideTags, _ = s.pages.Tags(ctxOf(r))
		}
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

// jsStrings renders the messages used from JS as JSON.
// **Never write messages inline in the JS**; one language ends up untranslated.
func jsStrings(lang i18n.Lang) string {
	keys := []string{
		"theme.auto", "theme.light", "theme.dark", "theme.toggle", "gtd.capture_short",
		"capture.title", "capture.hint", "capture.added", "capture.failed",
		"capture.uploading", "capture.upload_failed",
		"wikilink.alias", "wikilink.create", "wikilink.hint",
		"keys.ask_title",
		"move.title", "move.back", "move.submit", "move.details",
		"move.choice.next", "move.choice.later", "move.choice.waiting", "move.choice.scheduled",
		"move.choice.someday", "move.choice.done", "move.choice.dropped", "move.choice.filed",
		"move.choice.inbox",
		"move.project", "move.project_required", "move.context", "move.none",
		"move.waiting_for", "move.scheduled_on", "move.file_title", "move.file_tags",
		"move.confirm_drop", "move.hint",
		"undo.moved", "undo.dropped", "undo.action", "undo.dismiss", "undo.conflict", "undo.failed",
		"repeat", "repeat.none", "repeat.interval", "repeat.every", "repeat.weekly",
		"repeat.monthly", "repeat.yearly", "repeat.custom",
		"repeat.days", "repeat.weeks", "repeat.months", "repeat.years",
		"repeat.from_scheduled", "repeat.from_done", "repeat.last_day",
		"repeat.ends_on", "repeat.pick_day",
		"repeat.wd.sun", "repeat.wd.mon", "repeat.wd.tue", "repeat.wd.wed",
		"repeat.wd.thu", "repeat.wd.fri", "repeat.wd.sat",
		"day.copied", "day.copy_manual",
		"dur.m", "dur.hm", "dur.dh",
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

// handleSetLang remembers the language choice in a cookie.
// **Rendering happens on the server, so the choice has to arrive with the
// request, not stay in the browser.**
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

// shortTime renders a stored timestamp in local time.
// **Timestamps are stored in UTC** (datetime('now')), so showing them as they
// are puts everything 9 hours early in Japan, and on the previous day before
// 09:00.
func shortTime(s string) string {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return s
	}
	t = t.In(time.Local)
	if t.Year() == time.Now().Year() {
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

// ctxOf is the context for handlers, and the place to add a timeout later.
func ctxOf(r *http.Request) context.Context { return r.Context() }

var _ = fmt.Sprintf
