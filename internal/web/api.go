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

// apiSearch は GET /api/search?q=&kind=&limit=&offset=
// UI からも Emacs からも同じエンドポイントを使う。既定 50 件(DESIGN 3.7)。
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
		if writeConflict(w, err) {
			return
		}
		writeErr(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "page", Slug: p.Slug})
	writeJSON(w, http.StatusCreated, p)
}

// apiGetPage は {id, slug, title, body, tags, version, links, backlinks} を返す(DESIGN 4.2)。
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
		// 楽観ロックは必須。version 無しの書き戻しを許すと Emacs 側の競合検出が意味を失う。
		writeErr(w, http.StatusBadRequest, "version_required", "version は必須です")
		return
	}
	p, err := s.pages.Update(ctxOf(r), r.PathValue("slug"), in)
	if err != nil {
		if writeConflict(w, err) {
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

// apiDashboard はダッシュボードに必要な集計を1発で返す。
// **個別に N 本クエリを投げないこと**(DESIGN 5)。
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

// apiExport は出力先をリクエストから受け取らない。
// **設定ファイルの値に固定する。任意パスへの書き出しを外部から起動できる状態は危険**(DESIGN 4.4)。
func (s *Server) apiExport(w http.ResponseWriter, r *http.Request) {
	res, err := export.Run(ctxOf(r), s.db, s.files, s.cfg.ExportDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "export_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// apiBackup は DB のバックアップを取る。
// **出力先はリクエストから受け取らない。**設定ファイルの値に固定する(export と同じ)。
func (s *Server) apiBackup(w http.ResponseWriter, r *http.Request) {
	b, err := store.RunBackup(ctxOf(r), s.db, s.files, s.cfg.BackupDir, s.cfg.BackupKeep)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// apiBackups は現存するバックアップを新しい順に返す。
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
		writeErr(w, http.StatusNotFound, "not_found", "ページが見つかりません")
		return
	}
	writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
}
