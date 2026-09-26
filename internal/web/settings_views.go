package web

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/wakamenod/enghi/internal/settings"
	"github.com/wakamenod/enghi/internal/skill"
	"github.com/wakamenod/enghi/internal/store"
)

// The settings screen.
//
// **Contexts and areas are off by default.** They are optional tools within GTD,
// and until there is a reason to filter by them they only add choices. Whoever
// needs them turns them on here, and they then appear both in the app and in
// the guide.

type settingsData struct {
	Set     settings.Settings
	Backups []store.Backup
	Export  string // where exports are written
	Dir     string // where backups are written

	SkillPath string     // where install-skill writes the Claude Code skill
	Skill     skill.Info // what is there now
	Local     bool       // the request came from this machine (fromThisMachine)

	Calendar calendarPanel
}

func (s *Server) viewSettings(w http.ResponseWriter, r *http.Request) {
	d := settingsData{Set: s.settings(r), Export: s.cfg.ExportDir, Dir: s.cfg.BackupDir,
		SkillPath: s.skillDest(), Local: fromThisMachine(r), Calendar: s.calendarPanel(ctxOf(r))}
	d.Backups, _ = store.Backups(s.cfg.BackupDir)
	var err error
	if d.Skill, err = skill.Status(d.SkillPath, s.baseURL()); err != nil {
		log.Printf("skill status: %v", err)
	}
	s.render(w, r, "settings.html", viewData{Title: s.tr(r, "settings.title"), Nav: "settings", Data: d})
}

// uiUpdateSettings receives the settings form.
// **An unchecked checkbox is not submitted at all**, so the handler writes every
// known key, treating "absent" as off.
func (s *Server) uiUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, s.tr(r, "err.bad_request"), http.StatusBadRequest)
		return
	}
	for _, key := range settings.Keys {
		if err := s.set.Set(ctxOf(r), key, r.PostForm.Get(key) == "1"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// uiBackup is the manual backup from the settings screen.
func (s *Server) uiBackup(w http.ResponseWriter, r *http.Request) {
	if _, err := store.RunBackup(ctxOf(r), s.db, s.files, s.cfg.BackupDir, s.cfg.BackupKeep); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) skillDest() string { return filepath.Join(s.cfg.SkillsDir, "enghi") }

// baseURL is this server's address. cfg.Port already carries the serve --port
// override, so the skill points at the server that wrote it.
func (s *Server) baseURL() string {
	return "http://" + net.JoinHostPort(s.cfg.Host, fmt.Sprint(s.cfg.Port))
}

// uiInstallSkill does what `enghi install-skill` does, from the settings
// screen. **Only from this machine**: it writes into the home directory, which
// is not something a device coming in through allowed_hosts should reach.
func (s *Server) uiInstallSkill(w http.ResponseWriter, r *http.Request) {
	if !fromThisMachine(r) {
		http.Error(w, s.tr(r, "settings.skill_local_only"), http.StatusForbidden)
		return
	}
	if _, err := skill.Write(s.skillDest(), s.baseURL()); err != nil {
		http.Error(w, s.tr(r, "settings.skill_failed", err.Error()), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// featureOff answers a request for a screen belonging to a feature that is off.
// **It is a 404**: for someone who has not turned the feature on, the screen
// does not exist. It still points at the settings, because without knowing they
// exist there is no way back.
func (s *Server) featureOff(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, r, "featureoff.html", viewData{
		Title: s.tr(r, "settings.feature_off_title"), Nav: "settings"})
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.set.Load(ctxOf(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, set)
}
