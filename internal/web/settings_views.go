package web

import (
	"net/http"

	"github.com/wakamenod/enghi/internal/settings"
	"github.com/wakamenod/enghi/internal/store"
)

// 設定画面。
//
// **Context と Area は既定で off。** GTD の中では任意の道具であり、
// 絞り込む必要が無いうちは選択肢が増えるだけになる。必要になった人が
// ここで on にすると、画面にも使い方ガイドにも現れる。

type settingsData struct {
	Set     settings.Settings
	Backups []store.Backup
	Export  string // エクスポート先
	Dir     string // バックアップ先
}

func (s *Server) viewSettings(w http.ResponseWriter, r *http.Request) {
	d := settingsData{Set: s.settings(r), Export: s.cfg.ExportDir, Dir: s.cfg.BackupDir}
	d.Backups, _ = store.Backups(s.cfg.BackupDir)
	s.render(w, r, "settings.html", viewData{Title: s.tr(r, "settings.title"), Nav: "settings", Data: d})
}

// uiUpdateSettings は設定画面のフォームを受ける。
// **チェックボックスは off のとき送られてこない。** 出ている項目を hidden で
// 明示し、その集合について on/off を書き込む(送られてこない = off)。
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

// uiBackup は設定画面からの手動バックアップ。
func (s *Server) uiBackup(w http.ResponseWriter, r *http.Request) {
	if _, err := store.RunBackup(ctxOf(r), s.db, s.files, s.cfg.BackupDir, s.cfg.BackupKeep); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// featureOff は、設定で off にしている機能の画面に来たときの応答。
// **404 にする。** その機能を on にしていない人にとって、この画面は存在しない。
// ただし設定への入口は示す(設定の存在を知らないと戻れないため)。
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
