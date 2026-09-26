package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/wakamenod/enghi/internal/gtd"
)

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func optID(q string) *int64 {
	if q == "" {
		return nil
	}
	n, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func (s *Server) gtdErr(w http.ResponseWriter, err error) {
	var vc *gtd.VersionConflictError
	var lvc *gtd.LogVersionConflictError
	switch {
	case errors.Is(err, gtd.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not found")
	case errors.As(err, &vc):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "version_conflict",
			"message": "this item was updated elsewhere",
			"current": vc.Current,
		})
	case errors.As(err, &lvc):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "version_conflict",
			"message": "this log entry was updated elsewhere",
			"current": lvc.Current,
		})
	default:
		writeErr(w, http.StatusBadRequest, "request_failed", err.Error())
	}
}

// ---------------------------------------------------------------- Task

// apiListTasks is GET /api/tasks?state=&context=&project=&area=&due_before=
// state=next_actions selects with the view condition of 2.6.
func (s *Server) apiListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tasks, err := s.gtd.QueryTasks(ctxOf(r), gtd.TaskQuery{
		State:     q.Get("state"),
		ContextID: optID(q.Get("context")),
		ProjectID: optID(q.Get("project")),
		AreaID:    optID(q.Get("area")),
		DueBefore: q.Get("due_before"),
		Limit:     atoiDefault(q.Get("limit"), 200),
	})
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// apiLists is GET /api/lists: the count of every list on the GTD top page, in
// one call. Every field is always present; showing Areas is the client's choice.
func (s *Server) apiLists(w http.ResponseWriter, r *http.Request) {
	c, err := s.gtd.ListCounts(ctxOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// apiCreateTask is capture. **{title} alone must be enough** (DESIGN 4.2).
func (s *Server) apiCreateTask(w http.ResponseWriter, r *http.Request) {
	var in gtd.CaptureInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	t, err := s.gtd.Capture(ctxOf(r), in)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) apiGetTask(w http.ResponseWriter, r *http.Request) {
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
	links, _ := s.gtd.LinkedPages(ctxOf(r), "task", id)
	writeJSON(w, http.StatusOK, map[string]any{"task": t, "links": links})
}

// apiPatchTask is a partial update. **State transitions go through here too**
// (DESIGN 4.2).
func (s *Server) apiPatchTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var p gtd.TaskPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	t, err := s.gtd.Patch(ctxOf(r), id, p)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) apiDeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if err := s.gtd.Delete(ctxOf(r), id); err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiCompleteTask completes or skips. For a recurring task it generates and
// returns the next instance (DESIGN 2.6).
func (s *Server) apiCompleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in struct {
		Skip bool `json:"skip"`
		// EndSeries ends the whole series. The UI must offer "this one" as the
		// other choice.
		EndSeries bool `json:"end_series"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	if in.EndSeries {
		if _, err := s.gtd.EndSeries(ctxOf(r), id); err != nil {
			s.gtdErr(w, err)
			return
		}
	}
	res, err := s.gtd.Complete(ctxOf(r), id, in.Skip)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, res)
}

// apiFileAsReference is the reference path of clarify: create a wiki page, set
// the original task to filed and connect them through links (DESIGN 8-13).
func (s *Server) apiFileAsReference(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in gtd.FileAsReferenceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	task, page, err := s.gtd.FileAsReference(ctxOf(r), s.pages, id, in)
	if err != nil {
		if s.writeConflict(w, r, err) {
			return
		}
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "page": page})
}

// ---------------------------------------------------------------- Project

func (s *Server) apiListProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.gtd.Projects(ctxOf(r), r.URL.Query().Get("status"))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

// apiStalledProjects returns the active projects with no next action
// (DESIGN 2.4).
func (s *Server) apiStalledProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.gtd.StalledProjects(ctxOf(r))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

func (s *Server) apiGetProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	p, err := s.gtd.Project(ctxOf(r), id)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	tasks, _ := s.gtd.TasksOfProject(ctxOf(r), id)
	links, _ := s.gtd.LinkedPages(ctxOf(r), "project", id)
	writeJSON(w, http.StatusOK, map[string]any{"project": p, "tasks": tasks, "links": links})
}

func (s *Server) apiCreateProject(w http.ResponseWriter, r *http.Request) {
	var in gtd.ProjectInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := s.gtd.CreateProject(ctxOf(r), in)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "project"})
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) apiPatchProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in gtd.ProjectInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := s.gtd.PatchProject(ctxOf(r), id, in)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "project"})
	writeJSON(w, http.StatusOK, p)
}

// ---------------------------------------------------------------- Area / Context

func (s *Server) apiListAreas(w http.ResponseWriter, r *http.Request) {
	as, err := s.gtd.Areas(ctxOf(r))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"areas": as})
}

func (s *Server) apiCreateArea(w http.ResponseWriter, r *http.Request) {
	var in gtd.AreaInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a, err := s.gtd.CreateArea(ctxOf(r), in)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) apiPatchArea(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in gtd.AreaInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a, err := s.gtd.PatchArea(ctxOf(r), id, in)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) apiListContexts(w http.ResponseWriter, r *http.Request) {
	cs, err := s.gtd.Contexts(ctxOf(r))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contexts": cs})
}

func (s *Server) apiCreateContext(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	c, err := s.gtd.CreateContext(ctxOf(r), in.Name)
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// ---------------------------------------------------------------- Review

func (s *Server) apiReview(w http.ResponseWriter, r *http.Request) {
	d, err := s.reviewData(ctxOf(r), s.langOf(r))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// apiSeries lists the recurring series, for taking stock (DESIGN 2.6).
func (s *Server) apiSeries(w http.ResponseWriter, r *http.Request) {
	list, err := s.gtd.SeriesList(ctxOf(r))
	if err != nil {
		s.gtdErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": list})
}
