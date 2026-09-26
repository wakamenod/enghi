package web

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/i18n"
)

// The work log on a task. Entries are Markdown, rendered with the same
// renderer as articles, so [[links]], code and pasted images all work.
// Backlinks from entries are not stored in links yet: task rows there already
// mean "filed as reference", and entries resolve their links at render time.

// logView is one entry on the Clarify screen.
type logView struct {
	*gtd.TaskLog
	HTML template.HTML
}

// logViews renders a task's log for the screen. An entry that fails to render
// shows as plain text rather than taking the screen down.
func (s *Server) logViews(r *http.Request, taskID int64) []logView {
	logs, err := s.gtd.Logs(ctxOf(r), taskID)
	if err != nil {
		return nil
	}
	out := make([]logView, 0, len(logs))
	for _, l := range logs {
		v := logView{TaskLog: l}
		if l.Body != "" {
			if html, err := s.renderBody(r, l.Body); err == nil {
				v.HTML = html
			} else {
				v.HTML = template.HTML("<pre>" + template.HTMLEscapeString(l.Body) + "</pre>")
			}
		}
		out = append(out, v)
	}
	return out
}

// markLabel is the head of a start or pause mark: "▶ started 10:00", or for
// the pause a move out of Next wrote, "⏸ paused 10:00 (moved to Someday)".
func markLabel(lang i18n.Lang, kind, movedTo, when string) string {
	switch {
	case kind == gtd.LogStart:
		return i18n.T(lang, "log.started", when)
	case movedTo != "":
		return i18n.T(lang, "log.paused_moved", when, i18n.T(lang, "move.choice."+movedTo))
	default:
		return i18n.T(lang, "log.paused", when)
	}
}

// ---------------------------------------------------------------- form posts

// logRedirect lands on the entry just written. An explicit return_to wins, so
// the start/pause key in a list stays on the list; the Referer is not used,
// since it would lose the #log- anchor.
func logRedirect(w http.ResponseWriter, r *http.Request, taskID, logID int64) {
	dest := r.FormValue("return_to")
	if dest == "" || !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
		dest = "/gtd/clarify/" + strconv.FormatInt(taskID, 10) + "#log"
		if logID != 0 {
			dest += "-" + strconv.FormatInt(logID, 10)
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) uiAddLog(w http.ResponseWriter, r *http.Request) {
	s.uiWriteLog(w, r, gtd.LogNote)
}

func (s *Server) uiStartTask(w http.ResponseWriter, r *http.Request) {
	s.uiWriteLog(w, r, gtd.LogStart)
}

func (s *Server) uiPauseTask(w http.ResponseWriter, r *http.Request) {
	s.uiWriteLog(w, r, gtd.LogPause)
}

func (s *Server) uiWriteLog(w http.ResponseWriter, r *http.Request, kind string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	l, created, err := s.gtd.AddLog(ctxOf(r), id, kind, r.FormValue("body"))
	if err != nil {
		if errors.Is(err, gtd.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var logID int64
	if l != nil {
		logID = l.ID
	}
	if created {
		s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	}
	logRedirect(w, r, id, logID)
}

func (s *Server) uiEditLog(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	version, _ := strconv.Atoi(r.FormValue("version"))
	l, err := s.gtd.EditLog(ctxOf(r), id, r.FormValue("body"), version)
	if err != nil {
		var vc *gtd.LogVersionConflictError
		switch {
		case errors.Is(err, gtd.ErrNotFound):
			http.NotFound(w, r)
		case errors.As(err, &vc):
			http.Error(w, s.tr(r, "log.version_conflict"), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	logRedirect(w, r, l.TaskID, l.ID)
}

func (s *Server) uiDeleteLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	l, err := s.gtd.DeleteLog(ctxOf(r), id)
	if err != nil {
		if errors.Is(err, gtd.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	logRedirect(w, r, l.TaskID, 0)
}

// ---------------------------------------------------------------- API

// apiListLogs is GET /api/tasks/{id}/logs: the log, oldest first, and whether
// the task is being worked on.
func (s *Server) apiListLogs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	t, err := s.gtd.Task(ctxOf(r), id)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	logs, err := s.gtd.Logs(ctxOf(r), id)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "working": t.Working, "logs": logs})
}

// apiAddLog is POST /api/tasks/{id}/logs {kind?, body}; kind defaults to note.
// A start or pause that changes nothing answers 200 with created=false and the
// latest mark (null if there is none); a new entry answers 201.
func (s *Server) apiAddLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in struct {
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	l, created, err := s.gtd.AddLog(ctxOf(r), id, in.Kind, in.Body)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	t, err := s.gtd.Task(ctxOf(r), id)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
		s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	}
	writeJSON(w, code, map[string]any{"log": l, "created": created, "working": t.Working})
}

// apiEditLog is PATCH /api/task-logs/{id} {body, version}. **version is
// required**, as for pages: without it conflict detection means nothing.
func (s *Server) apiEditLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in struct {
		Body    string `json:"body"`
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if in.Version <= 0 {
		writeErr(w, http.StatusBadRequest, "version_required", s.tr(r, "err.version_required"))
		return
	}
	l, err := s.gtd.EditLog(ctxOf(r), id, in.Body, in.Version)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) apiDeleteLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := s.gtd.DeleteLog(ctxOf(r), id); err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
