package web_test

import (
	"net/http"
	"strings"
	"testing"
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
