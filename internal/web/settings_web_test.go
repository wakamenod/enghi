package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/config"
)

// Contexts and areas are off by default.
// **Change that default and these two expectations must change with it.**
func TestFeaturesAreOffByDefault(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/areas", `{"name":"経理"}`)
	mustJSON(t, h, "POST", "/api/contexts", `{"name":"@電話"}`)

	// The screens are gone entirely
	for _, p := range []string{"/gtd/areas", "/gtd/area/1"} {
		if got := do(h, req("GET", p, "")).Code; got != http.StatusNotFound {
			t.Errorf("GET %s → %d, want 404", p, got)
		}
	}
	// The GTD screen shows neither the contexts panel nor the areas row
	body := do(h, req("GET", "/gtd", "")).Body.String()
	for _, s := range []string{"/ui/contexts", "/gtd/areas"} {
		if strings.Contains(body, s) {
			t.Errorf("/gtd shows %q although it should be off", s)
		}
	}
	// Clarify offers no context or area selector
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"何か"}`)
	body = do(h, req("GET", "/gtd/clarify/1", "")).Body.String()
	for _, s := range []string{`name="context_id"`, `name="area_id"`} {
		if strings.Contains(body, s) {
			t.Errorf("clarify shows %q although it should be off", s)
		}
	}
}

// The guide follows the settings too. **A feature gone from the screens must
// not live on in the guide.**
func TestGuideHidesSectionsOfDisabledFeatures(t *testing.T) {
	h := newServer(t)

	off := do(h, req("GET", "/guide/enghi", "")).Body.String()
	if strings.Contains(off, `id="contexts"`) || strings.Contains(off, `id="areas"`) {
		t.Error("the guide shows the contexts / areas sections while they are off")
	}
	if !strings.Contains(off, `id="states"`) {
		t.Error("unrelated sections were dropped as well")
	}

	enableFeatures(t, h)
	on := do(h, req("GET", "/guide/enghi", "")).Body.String()
	if !strings.Contains(on, `id="contexts"`) || !strings.Contains(on, `id="areas"`) {
		t.Error("the sections do not come back when turned on")
	}
	// The introduction, which explains GTD itself, behaves the same way
	intro := do(h, req("GET", "/guide/gtd", "")).Body.String()
	if !strings.Contains(intro, `id="context"`) {
		t.Error("the introduction does not show the contexts section when turned on")
	}
}

// Turning them on brings the screens back.
func TestFeaturesCanBeEnabled(t *testing.T) {
	h := newServer(t)
	enableFeatures(t, h)
	for _, p := range []string{"/gtd/areas", "/settings"} {
		if got := do(h, req("GET", p, "")).Code; got != http.StatusOK {
			t.Errorf("GET %s → %d, want 200", p, got)
		}
	}
	if !strings.Contains(do(h, req("GET", "/gtd", "")).Body.String(), "/gtd/areas") {
		t.Error("the GTD screen does not show areas when turned on")
	}
}

func installSkill(h http.Handler, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/ui/install-skill", nil)
	r.Host = host
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return do(h, r)
}

// The settings screen installs the Claude Code skill, pointed at this server's
// port, and then reports it as up to date.
func TestInstallSkillFromSettings(t *testing.T) {
	h, cfg := newServerWith(t, func(c *config.Config) { c.Port = 8123 })

	body := do(h, req("GET", "/settings", "")).Body.String()
	if !strings.Contains(body, `action="/ui/install-skill"`) {
		t.Fatal("the settings screen has no install button")
	}
	if got := installSkill(h, "127.0.0.1:8123").Code; got != http.StatusSeeOther {
		t.Fatalf("POST /ui/install-skill → %d, want 303", got)
	}
	b, err := os.ReadFile(filepath.Join(cfg.SkillsDir, "enghi", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "http://127.0.0.1:8123/api/") {
		t.Error("SKILL.md does not point at the server's port")
	}
	body = do(h, req("GET", "/settings", "")).Body.String()
	if strings.Contains(body, `action="/ui/install-skill"`) {
		t.Error("the install button is still offered for an up-to-date skill")
	}
}

// Behind a proxy (a name from allowed_hosts) the action is refused and the
// button is not shown: it writes into the home directory of the machine enghi
// runs on.
func TestInstallSkillIsLocalOnly(t *testing.T) {
	h, cfg := newServerWith(t, func(c *config.Config) { c.AllowedHosts = []string{"macbook.local"} })

	r := req("GET", "/settings", "")
	r.Host = "macbook.local"
	if body := do(h, r).Body.String(); strings.Contains(body, `action="/ui/install-skill"`) {
		t.Error("the install button is shown to a request through allowed_hosts")
	}
	if got := installSkill(h, "macbook.local").Code; got != http.StatusForbidden {
		t.Errorf("POST via allowed_hosts → %d, want 403", got)
	}
	// A proxy that rewrites Host to the upstream address still gives itself away
	r = httptest.NewRequest("POST", "/ui/install-skill", nil)
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Forwarded-For", "192.168.1.20")
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("POST with X-Forwarded-For → %d, want 403", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillsDir, "enghi")); !os.IsNotExist(err) {
		t.Errorf("something was written: %v", err)
	}
}
