package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/wakamenod/enghi/internal/gtd"
	"github.com/wakamenod/enghi/internal/wiki"
)

// ---------------------------------------------------------------- screens

type gtdTopData struct {
	InboxCount int
	NextCount  int
	WaitingCnt int
	SchedCount int
	SomedayCnt int
	Projects   []*gtd.Project
	Stalled    []*gtd.Project
	Contexts   []*gtd.Context
	Areas      []*gtd.Area
}

func (s *Server) viewGTDTop(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	d := gtdTopData{}
	inbox, err := s.gtd.Inbox(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.InboxCount = len(inbox)
	if next, err := s.gtd.NextActions(ctx, nil); err == nil {
		d.NextCount = len(next)
	}
	if waiting, err := s.gtd.Waiting(ctx); err == nil {
		d.WaitingCnt = len(waiting)
	}
	if sch, err := s.gtd.Scheduled(ctx); err == nil {
		d.SchedCount = len(sch)
	}
	if sm, err := s.gtd.Someday(ctx); err == nil {
		d.SomedayCnt = len(sm)
	}
	d.Projects, _ = s.gtd.Projects(ctx, "active")
	d.Stalled, _ = s.gtd.StalledProjects(ctx)
	d.Contexts, _ = s.gtd.Contexts(ctx)
	d.Areas, _ = s.gtd.Areas(ctx)
	s.render(w, r, "gtd.html", viewData{Title: s.tr(r, "gtd.title"), Nav: "gtd", Data: d})
}

type taskListData struct {
	Heading  string
	Note     string
	Tasks    []*gtd.Task
	Contexts []*gtd.Context
	Current  *gtd.Context
	Projects []*gtd.Project
	Kind     string // inbox / next / waiting / scheduled / someday
}

func (s *Server) viewInbox(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.gtd.Inbox(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "tasks.html", viewData{Title: s.tr(r, "gtd.inbox"), Nav: "gtd",
		Data: taskListData{Heading: s.tr(r, "gtd.inbox"), Kind: "inbox", Tasks: tasks,
			Note: s.tr(r, "gtd.inbox_note")}})
}

func (s *Server) viewNextActions(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	contexts, _ := s.gtd.Contexts(ctx)
	var cur *gtd.Context
	var ctxID *int64
	if name := r.URL.Query().Get("context"); name != "" && s.settings(r).Contexts {
		if c, err := s.gtd.ContextByName(ctx, name); err == nil {
			cur, ctxID = c, &c.ID
		} else if id, err := strconv.ParseInt(name, 10, 64); err == nil {
			ctxID = &id
			for _, c := range contexts {
				if c.ID == id {
					cur = c
				}
			}
		}
	}
	tasks, err := s.gtd.NextActions(ctx, ctxID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	heading := s.tr(r, "gtd.next")
	if cur != nil {
		heading += " — " + cur.Name
	}
	s.render(w, r, "tasks.html", viewData{Title: heading, Nav: "gtd",
		Data: taskListData{Heading: heading, Kind: "next", Tasks: tasks,
			Contexts: contexts, Current: cur,
			Note: s.tr(r, "gtd.next_note")}})
}

func (s *Server) viewWaiting(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.gtd.Waiting(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "tasks.html", viewData{Title: s.tr(r, "gtd.waiting"), Nav: "gtd",
		Data: taskListData{Heading: s.tr(r, "gtd.waiting"), Kind: "waiting", Tasks: tasks,
			Note: s.tr(r, "gtd.waiting_note")}})
}

func (s *Server) viewScheduled(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.gtd.Scheduled(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "tasks.html", viewData{Title: s.tr(r, "gtd.scheduled"), Nav: "gtd",
		Data: taskListData{Heading: s.tr(r, "gtd.scheduled_title"), Kind: "scheduled", Tasks: tasks,
			Note: s.tr(r, "gtd.scheduled_note")}})
}

func (s *Server) viewSomeday(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.gtd.Someday(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "tasks.html", viewData{Title: s.tr(r, "gtd.someday"), Nav: "gtd",
		Data: taskListData{Heading: s.tr(r, "gtd.someday"), Kind: "someday", Tasks: tasks}})
}

type projectsData struct {
	Projects []*gtd.Project
	Stalled  map[int64]bool
	Areas    []*gtd.Area
	Status   string
}

func (s *Server) viewProjects(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	status := r.URL.Query().Get("status")
	ps, err := s.gtd.Projects(ctx, status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stalled, _ := s.gtd.StalledProjects(ctx)
	m := map[int64]bool{}
	for _, p := range stalled {
		m[p.ID] = true
	}
	areas, _ := s.gtd.Areas(ctx)
	s.render(w, r, "projects.html", viewData{Title: s.tr(r, "gtd.projects"), Nav: "gtd",
		Data: projectsData{Projects: ps, Stalled: m, Areas: areas, Status: status}})
}

type projectData struct {
	Project  *gtd.Project
	Tasks    []*gtd.Task
	Areas    []*gtd.Area
	Contexts []*gtd.Context
	Links    []wiki.Link
	NoteHTML template.HTML
	Stalled  bool
}

func (s *Server) viewProject(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.gtd.Project(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := projectData{Project: p, Stalled: p.Stalled()}
	d.Tasks, _ = s.gtd.TasksOfProject(ctx, id)
	d.Areas, _ = s.gtd.Areas(ctx)
	d.Contexts, _ = s.gtd.Contexts(ctx)
	d.Links, _ = s.gtd.LinkedPages(ctx, "project", id)
	// The wiki page embedded through note_page_id (DESIGN 8-19)
	if p.NotePageID != nil {
		if page, err := s.pages.BySlug(ctx, p.NotePageSlug); err == nil {
			if html, err := s.renderBody(r, page.Body); err == nil {
				d.NoteHTML = html
			}
		}
	}
	s.render(w, r, "project.html", viewData{Title: p.Title, Nav: "gtd", Data: d})
}

type areasData struct {
	Areas []*gtd.Area
}

func (s *Server) viewAreas(w http.ResponseWriter, r *http.Request) {
	if !s.settings(r).Areas {
		s.featureOff(w, r)
		return
	}
	areas, err := s.gtd.Areas(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "areas.html", viewData{Title: s.tr(r, "area.title"), Nav: "gtd",
		Data: areasData{Areas: areas}})
}

type areaData struct {
	Area     *gtd.Area
	Projects []*gtd.Project
	Tasks    []*gtd.Task
	NoteHTML template.HTML
}

func (s *Server) viewArea(w http.ResponseWriter, r *http.Request) {
	if !s.settings(r).Areas {
		s.featureOff(w, r)
		return
	}
	ctx := ctxOf(r)
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.gtd.Area(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := areaData{Area: a}
	d.Projects, _ = s.gtd.ProjectsOfArea(ctx, id)
	d.Tasks, _ = s.gtd.TasksOfArea(ctx, id)
	if a.NotePageID != nil {
		if page, err := s.pages.BySlug(ctx, a.NotePageSlug); err == nil {
			if html, err := s.renderBody(r, page.Body); err == nil {
				d.NoteHTML = html
			}
		}
	}
	s.render(w, r, "area.html", viewData{Title: a.Name, Nav: "gtd", Data: d})
}

func (s *Server) viewReview(w http.ResponseWriter, r *http.Request) {
	d, err := s.reviewData(ctxOf(r), s.langOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, r, "review.html", viewData{Title: s.tr(r, "review.title"), Nav: "gtd", Data: d})
}

type clarifyData struct {
	Task     *gtd.Task
	Projects []*gtd.Project
	Contexts []*gtd.Context
	Areas    []*gtd.Area
}

// viewClarify opens one inbox item to decide "action or reference"
// (DESIGN 8-13).
func (s *Server) viewClarify(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := s.gtd.Task(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := clarifyData{Task: t}
	d.Projects, _ = s.gtd.Projects(ctx, "active")
	d.Contexts, _ = s.gtd.Contexts(ctx)
	d.Areas, _ = s.gtd.Areas(ctx)
	s.render(w, r, "clarify.html", viewData{Title: s.tr(r, "clarify.title", t.Title), Nav: "gtd", Data: d})
}

// ---------------------------------------------------------------- form posts

func redirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	dest := r.FormValue("return_to")
	if dest == "" || !strings.HasPrefix(dest, "/") {
		dest = r.Referer()
	}
	if dest == "" || !strings.HasPrefix(dest, "/") {
		dest = fallback
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) uiCapture(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.gtd.Capture(ctxOf(r), gtd.CaptureInput{
		Title: r.FormValue("title"), Note: r.FormValue("note")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	redirectBack(w, r, "/gtd/inbox")
}

func (s *Server) uiPatchTask(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := gtd.TaskPatch{}
	if v := r.FormValue("title"); v != "" {
		p.Title = &v
	}
	if r.Form.Has("note") {
		v := r.FormValue("note")
		p.Note = &v
	}
	if v := r.FormValue("state"); v != "" {
		p.State = &v
	}
	if r.Form.Has("project_id") {
		if v := r.FormValue("project_id"); v == "" {
			p.ClearProject = true
		} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			p.ProjectID = &n
		}
	}
	if r.Form.Has("context_id") {
		if v := r.FormValue("context_id"); v == "" {
			p.ClearContext = true
		} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			p.ContextID = &n
		}
	}
	if r.Form.Has("area_id") {
		if v := r.FormValue("area_id"); v == "" {
			p.ClearArea = true
		} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			p.AreaID = &n
		}
	}
	for _, f := range []struct {
		name string
		dst  **string
	}{
		{"scheduled_on", &p.ScheduledOn}, {"deadline_on", &p.DeadlineOn},
		{"waiting_for", &p.WaitingFor}, {"recurrence", &p.Recurrence},
		{"recurrence_ends_on", &p.RecurrenceEndsOn}, {"energy", &p.Energy},
	} {
		if r.Form.Has(f.name) {
			v := r.FormValue(f.name)
			*f.dst = &v
		}
	}
	if v := r.FormValue("time_estimate"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.TimeEstimate = &n
		}
	}
	if _, err := s.gtd.Patch(ctxOf(r), id, p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	redirectBack(w, r, "/gtd")
}

func (s *Server) uiCompleteTask(w http.ResponseWriter, r *http.Request) {
	s.completeOrSkip(w, r, false)
}

func (s *Server) uiSkipTask(w http.ResponseWriter, r *http.Request) {
	s.completeOrSkip(w, r, true)
}

func (s *Server) completeOrSkip(w http.ResponseWriter, r *http.Request, skip bool) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.gtd.Complete(ctxOf(r), id, skip); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	redirectBack(w, r, "/gtd/next")
}

// uiEndSeries ends the whole series.
func (s *Server) uiEndSeries(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.gtd.EndSeries(ctxOf(r), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/review")
}

func (s *Server) uiFileAsReference(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, page, err := s.gtd.FileAsReference(ctxOf(r), s.pages, id, gtd.FileAsReferenceInput{
		Title: r.FormValue("title"), Body: r.FormValue("body"),
		Tags: splitTags(r.FormValue("tags"))})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.hub.Broadcast(Event{Type: "updated", Kind: "task"})
	http.Redirect(w, r, "/wiki/"+page.Slug, http.StatusSeeOther)
}

func (s *Server) uiDeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.gtd.Delete(ctxOf(r), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/inbox")
}

func (s *Server) uiCreateProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in := gtd.ProjectInput{
		Title:    r.FormValue("title"),
		Outcome:  r.FormValue("outcome"),
		Status:   r.FormValue("status"),
		ReviewOn: r.FormValue("review_on"),
	}
	if v := r.FormValue("area_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.AreaID = &n
		}
	}
	p, err := s.gtd.CreateProject(ctxOf(r), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/gtd/project/"+strconv.FormatInt(p.ID, 10), http.StatusSeeOther)
}

func (s *Server) uiPatchProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in := gtd.ProjectInput{
		Title:    r.FormValue("title"),
		Outcome:  r.FormValue("outcome"),
		Status:   r.FormValue("status"),
		ReviewOn: r.FormValue("review_on"),
	}
	if r.Form.Has("area_id") {
		if v := r.FormValue("area_id"); v == "" {
			in.ClearArea = true
		} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.AreaID = &n
		}
	}
	if r.Form.Has("note_page") {
		if v := strings.TrimSpace(r.FormValue("note_page")); v == "" {
			in.ClearNote = true
		} else if page, err := s.pages.ByTitle(ctxOf(r), v); err == nil {
			in.NotePageID = &page.ID
		} else if page, err := s.pages.BySlug(ctxOf(r), v); err == nil {
			in.NotePageID = &page.ID
		}
	}
	if _, err := s.gtd.PatchProject(ctxOf(r), id, in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/project/"+strconv.FormatInt(id, 10))
}

func (s *Server) uiCreateArea(w http.ResponseWriter, r *http.Request) {
	if !s.settings(r).Areas {
		s.featureOff(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.gtd.CreateArea(ctxOf(r), gtd.AreaInput{
		Name: r.FormValue("name"), Description: r.FormValue("description")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/areas")
}

func (s *Server) uiPatchArea(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in := gtd.AreaInput{Name: r.FormValue("name"), Description: r.FormValue("description")}
	if r.Form.Has("note_page") {
		if v := strings.TrimSpace(r.FormValue("note_page")); v == "" {
			in.ClearNote = true
		} else if page, err := s.pages.ByTitle(ctxOf(r), v); err == nil {
			in.NotePageID = &page.ID
		}
	}
	if _, err := s.gtd.PatchArea(ctxOf(r), id, in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/area/"+strconv.FormatInt(id, 10))
}

func (s *Server) uiCreateContext(w http.ResponseWriter, r *http.Request) {
	if !s.settings(r).Contexts {
		s.featureOff(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.gtd.CreateContext(ctxOf(r), r.FormValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd")
}

func (s *Server) uiReviewCheck(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.gtd.SetChecklistItem(ctxOf(r), id,
		r.FormValue("key"), r.FormValue("checked") == "1"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectBack(w, r, "/gtd/review")
}

func (s *Server) uiReviewComplete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.gtd.CompleteReview(ctxOf(r), id, r.FormValue("note")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/gtd", http.StatusSeeOther)
}
