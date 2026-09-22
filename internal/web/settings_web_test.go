package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// Context と Area は既定で off。
// **既定を変えたら、この2つの期待も一緒に変える必要がある。**
func TestFeaturesAreOffByDefault(t *testing.T) {
	h := newServer(t)
	mustJSON(t, h, "POST", "/api/areas", `{"name":"経理"}`)
	mustJSON(t, h, "POST", "/api/contexts", `{"name":"@電話"}`)

	// 画面ごと無い
	for _, p := range []string{"/gtd/areas", "/gtd/area/1"} {
		if got := do(h, req("GET", p, "")).Code; got != http.StatusNotFound {
			t.Errorf("GET %s → %d, want 404", p, got)
		}
	}
	// GTD トップに Context の枠と Areas の行が出ない
	body := do(h, req("GET", "/gtd", "")).Body.String()
	for _, s := range []string{"/ui/contexts", "/gtd/areas"} {
		if strings.Contains(body, s) {
			t.Errorf("/gtd に %q が出ている(off のはず)", s)
		}
	}
	// Clarify に Context / Area の選択が出ない
	mustJSON(t, h, "POST", "/api/tasks", `{"title":"何か"}`)
	body = do(h, req("GET", "/gtd/clarify/1", "")).Body.String()
	for _, s := range []string{`name="context_id"`, `name="area_id"`} {
		if strings.Contains(body, s) {
			t.Errorf("clarify に %q が出ている(off のはず)", s)
		}
	}
}

// ガイドも設定に連動する。**画面から消えた機能をガイドだけが説明し続けない。**
func TestGuideHidesSectionsOfDisabledFeatures(t *testing.T) {
	h := newServer(t)

	off := do(h, req("GET", "/guide/enghi", "")).Body.String()
	if strings.Contains(off, `id="contexts"`) || strings.Contains(off, `id="areas"`) {
		t.Error("off のときにガイドへ Contexts / Areas の節が出ている")
	}
	if !strings.Contains(off, `id="states"`) {
		t.Error("関係のない節まで落ちている")
	}

	enableFeatures(t, h)
	on := do(h, req("GET", "/guide/enghi", "")).Body.String()
	if !strings.Contains(on, `id="contexts"`) || !strings.Contains(on, `id="areas"`) {
		t.Error("on にしてもガイドに節が出てこない")
	}
	// 入門側(GTD そのものの説明)も同じ扱い
	intro := do(h, req("GET", "/guide/gtd", "")).Body.String()
	if !strings.Contains(intro, `id="context"`) {
		t.Error("on にしても入門に Contexts の節が出てこない")
	}
}

// on にすると画面が戻ってくること。
func TestFeaturesCanBeEnabled(t *testing.T) {
	h := newServer(t)
	enableFeatures(t, h)
	for _, p := range []string{"/gtd/areas", "/settings"} {
		if got := do(h, req("GET", p, "")).Code; got != http.StatusOK {
			t.Errorf("GET %s → %d, want 200", p, got)
		}
	}
	if !strings.Contains(do(h, req("GET", "/gtd", "")).Body.String(), "/gtd/areas") {
		t.Error("on にしても GTD トップに Areas が出てこない")
	}
}
