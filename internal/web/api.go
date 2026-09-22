package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wakamenod/enghi/internal/export"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

// apiSearch is GET /api/search?q=&kind=&limit=&offset=
// The UI and Emacs use the same endpoint. The default is 50 results
// (DESIGN 3.7).
func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	var kinds []string
	if k := r.URL.Query().Get("kind"); k != "" {
		kinds = strings.Split(k, ",")
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 50)
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)

	results, err := s.search.Search(ctxOf(r), q, kinds, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query": q, "limit": limit, "offset": offset,
		"count": len(results), "results": results,
	})
}

func (s *Server) apiListPages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pages, err := s.pages.List(ctxOf(r), q.Get("sort"),
		atoiDefault(q.Get("limit"), 50), atoiDefault(q.Get("offset"), 0))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	total, _ := s.pages.CountPages(ctxOf(r))
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages, "total": total})
}

func (s *Server) apiCreatePage(w http.ResponseWriter, r *http.Request) {
	var in wiki.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := s.pages.Create(ctxOf(r), in)
	if err != nil {
		if s.writeConflict(w, r, err) {
			return
		}
		writeErr(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	writeJSON(w, http.StatusCreated, p)
}

// apiGetPage returns {id, slug, title, body, tags, version, links, backlinks}
// (DESIGN 4.2).
func (s *Server) apiGetPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.apiNotFound(w, err)
		return
	}
	links, err := s.pages.Links(ctxOf(r), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	backlinks, err := s.pages.Backlinks(ctxOf(r), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	aliases, err := s.pages.Aliases(ctxOf(r), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": p.ID, "slug": p.Slug, "title": p.Title, "body": p.Body,
		"tags": p.Tags, "version": p.Version,
		"created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
		"links": links, "backlinks": backlinks, "aliases": aliases,
	})
}

func (s *Server) apiUpdatePage(w http.ResponseWriter, r *http.Request) {
	var in wiki.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if in.Version <= 0 {
		// The optimistic lock is mandatory. Allowing a write-back without a
		// version would make conflict detection on the Emacs side meaningless.
		writeErr(w, http.StatusBadRequest, "version_required", s.tr(r, "err.version_required"))
		return
	}
	p, err := s.pages.Update(ctxOf(r), r.PathValue("slug"), in)
	if err != nil {
		if s.writeConflict(w, r, err) {
			return
		}
		s.apiNotFound(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) apiDeletePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := s.pages.Delete(ctxOf(r), slug); err != nil {
		s.apiNotFound(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: slug})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiBacklinks(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.apiNotFound(w, err)
		return
	}
	backlinks, err := s.pages.Backlinks(ctxOf(r), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backlinks": backlinks})
}

func (s *Server) apiRevisions(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(ctxOf(r), r.PathValue("slug"))
	if err != nil {
		s.apiNotFound(w, err)
		return
	}
	revs, err := s.pages.Revisions(ctxOf(r), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

func (s *Server) apiTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.pages.Tags(ctxOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// apiDashboard returns everything the dashboard needs in one call.
// **Never fire N separate queries for it** (DESIGN 5).
func (s *Server) apiDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.dashboardData(ctxOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) apiDoctor(w http.ResponseWriter, r *http.Request) {
	problems, err := store.Doctor(ctxOf(r), s.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "doctor_failed", err.Error())
		return
	}
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(problems) == 0, "problems": out})
}

// apiExport does not take the destination from the request.
// **It is fixed to the value in the configuration file: being able to trigger a
// write to an arbitrary path from outside is dangerous** (DESIGN 4.4).
func (s *Server) apiExport(w http.ResponseWriter, r *http.Request) {
	res, err := export.Run(ctxOf(r), s.db, s.files, s.cfg.ExportDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "export_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// apiBackup backs the database up.
// **The destination does not come from the request**; it is fixed to the
// configuration file, as with export.
func (s *Server) apiBackup(w http.ResponseWriter, r *http.Request) {
	b, err := store.RunBackup(ctxOf(r), s.db, s.files, s.cfg.BackupDir, s.cfg.BackupKeep)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// apiBackups returns the existing backups, newest first.
func (s *Server) apiBackups(w http.ResponseWriter, r *http.Request) {
	list, err := store.Backups(s.cfg.BackupDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dir": s.cfg.BackupDir, "keep": s.cfg.BackupKeep,
		"enabled": s.cfg.BackupOn(), "backups": list,
	})
}

func (s *Server) apiNotFound(w http.ResponseWriter, err error) {
	if errors.Is(err, wiki.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
}

// apiTitles is GET /api/titles?q=&limit=
// Candidates for [[...]] completion. **Only titles and aliases are searched**,
// never bodies, so that everything offered is guaranteed to resolve inside
// [[ ]] (DESIGN 2.5).
func (s *Server) apiTitles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	titles, err := s.pages.SuggestTitles(ctxOf(r), q.Get("q"), atoiDefault(q.Get("limit"), 10))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"titles": titles})
}
