package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wakamenod/enghi/internal/config"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/web"
)

func newServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.Default()
	cfg.ExportDir = filepath.Join(dir, "export")
	cfg.BackupDir = filepath.Join(dir, "backup")
	blobs, err := filestore.Open(filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

// req sets the correct local headers by default.
func req(method, path string, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Host = "127.0.0.1:7777"
	return r
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// 4.4-1: validating the Host header. **The only effective defense against DNS
// rebinding.**
func TestHostHeaderIsValidated(t *testing.T) {
	h := newServer(t)
	for _, host := range []string{"evil.example.com", "attacker.test:7777", "192.168.1.10:7777"} {
		r := req("GET", "/api/pages", "")
		r.Host = host
		if got := do(h, r).Code; got != http.StatusForbidden {
			t.Errorf("Host %q → %d, want 403", host, got)
		}
	}
	for _, host := range []string{"127.0.0.1:7777", "localhost:7777", "[::1]:7777"} {
		r := req("GET", "/api/pages", "")
		r.Host = host
		if got := do(h, r).Code; got != http.StatusOK {
			t.Errorf("Host %q → %d, want 200", host, got)
		}
	}
}

// 4.4-2: Sec-Fetch-Site is judged **only when present**.
// "Reject when absent" would break the Emacs layer (url-retrieve) and curl.
func TestSecFetchSiteOnlyCheckedWhenPresent(t *testing.T) {
	h := newServer(t)

	// No header at all (curl / Emacs) is accepted
	if got := do(h, req("GET", "/api/pages", "")).Code; got != http.StatusOK {
		t.Errorf("no header -> %d, want 200 (this would break the Emacs layer)", got)
	}
	// same-origin is accepted
	r := req("GET", "/api/pages", "")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if got := do(h, r).Code; got != http.StatusOK {
		t.Errorf("same-origin → %d, want 200", got)
	}
	// cross-site is rejected
	r = req("GET", "/api/pages", "")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("cross-site → %d, want 403", got)
	}
	// An external origin is rejected
	r = req("GET", "/api/pages", "")
	r.Header.Set("Origin", "https://evil.example.com")
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("external Origin -> %d, want 403", got)
	}
}

// 4.4-3: writes accept application/json only, closing the simple-request paths
// (text/plain, form-urlencoded) that avoid a preflight.
func TestWriteRequiresJSONContentType(t *testing.T) {
	h := newServer(t)
	body := `{"title":"攻撃","body":"x"}`

	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", ""} {
		r := httptest.NewRequest("POST", "/api/pages", strings.NewReader(body))
		r.Host = "127.0.0.1:7777"
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if got := do(h, r).Code; got != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q → %d, want 415", ct, got)
		}
	}
	if got := do(h, req("POST", "/api/pages", body)).Code; got != http.StatusCreated {
		t.Errorf("application/json → %d, want 201", got)
	}

	// A body-less POST is closed too: paths like /api/export need no body and
	// could otherwise be hit as a simple request
	r := httptest.NewRequest("POST", "/api/export", nil)
	r.Host = "127.0.0.1:7777"
	if got := do(h, r).Code; got != http.StatusUnsupportedMediaType {
		t.Errorf("body-less POST -> %d, want 415", got)
	}
}

// 4.2: a 409 carries two different meanings. **They are told apart by a
// machine-readable code.**
func TestConflictCodesAreDistinguishable(t *testing.T) {
	h := newServer(t)
	mustCreate := func(title string) map[string]any {
		w := do(h, req("POST", "/api/pages", `{"title":"`+title+`","body":"x"}`))
		if w.Code != http.StatusCreated {
			t.Fatalf("creation failed %d: %s", w.Code, w.Body.String())
		}
		var p map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := mustCreate("Emacs")
	b := mustCreate("Vim")

	// version_conflict - the input is kept and the current data returned
	w := do(h, req("PUT", "/api/pages/"+b["slug"].(string),
		`{"title":"Vim","body":"新しい本文","version":99}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("version mismatch -> %d, want 409", w.Code)
	}
	var vc map[string]any
	json.Unmarshal(w.Body.Bytes(), &vc)
	if vc["error"] != "version_conflict" {
		t.Errorf("error = %v, want version_conflict", vc["error"])
	}
	if vc["current"] == nil {
		t.Error("current data was not returned, so nothing can be merged")
	}

	// title_conflict - the colliding page is returned
	w = do(h, req("PUT", "/api/pages/"+b["slug"].(string),
		`{"title":"Emacs","body":"x","version":1}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("title collision -> %d, want 409", w.Code)
	}
	var tc map[string]any
	json.Unmarshal(w.Body.Bytes(), &tc)
	if tc["error"] != "title_conflict" {
		t.Errorf("error = %v, want title_conflict", tc["error"])
	}
	cp, ok := tc["conflicting_page"].(map[string]any)
	if !ok || cp["slug"] != a["slug"] {
		t.Errorf("the colliding page was not returned: %v", tc["conflicting_page"])
	}
}

// PUT requires a version. Allowing it to be omitted would make conflict
// detection on the Emacs side meaningless.
func TestUpdateRequiresVersion(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"記事","body":"x"}`))
	w := do(h, req("PUT", "/api/pages/記事", `{"title":"記事","body":"y"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("without a version -> %d, want 400", w.Code)
	}
}

// 4.3: POST /api/focus broadcasts a navigate to the connected clients.
func TestFocusChannelDeliversNavigate(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	blobs, err := filestore.Open(filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/events"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("cannot connect the WebSocket: %v", err)
	}
	defer conn.CloseNow()

	// Wait until the connection is registered
	deadline := time.Now().Add(3 * time.Second)
	for srv.Hub().Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Hub().Count() != 1 {
		t.Fatalf("connections = %d, want 1", srv.Hub().Count())
	}

	post, _ := http.NewRequest("POST", ts.URL+"/api/focus",
		bytes.NewReader([]byte(`{"path":"/wiki/foo"}`)))
	post.Header.Set("Content-Type", "application/json")
	pr, err := http.DefaultClient.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	pr.Body.Close()
	if pr.StatusCode != http.StatusOK {
		t.Fatalf("focus → %d", pr.StatusCode)
	}

	var got web.Event
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("the navigate never arrived: %v", err)
	}
	if got.Type != "navigate" || got.Path != "/wiki/foo" {
		t.Fatalf("wrong payload received: %+v", got)
	}

	// Disconnecting releases it; no connection is left held
	conn.Close(websocket.StatusNormalClosure, "")
	deadline = time.Now().Add(3 * time.Second)
	for srv.Hub().Count() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.Hub().Count(); n != 0 {
		t.Fatalf("still %d connection(s) after disconnect", n)
	}
}

// A WebSocket connection from another origin is rejected.
func TestWebSocketRejectsCrossOrigin(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	blobs, err := filestore.Open(filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	srv, err := web.New(config.Default(), db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/events"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example.com"}},
	})
	if err == nil {
		conn.CloseNow()
		t.Fatal("a WebSocket connection from an external origin was accepted")
	}
}

// 4.4: POST /api/export takes no destination; it is fixed to the configuration.
func TestExportIgnoresRequestPath(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"記事","body":"本文"}`))

	w := do(h, req("POST", "/api/export", `{"dir":"/tmp/attacker-controlled"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("export → %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if strings.Contains(res["dir"].(string), "attacker-controlled") {
		t.Fatalf("the destination from the request was used: %v", res["dir"])
	}
}

func TestPagesAPIRoundTrip(t *testing.T) {
	h := newServer(t)
	w := do(h, req("POST", "/api/pages",
		`{"title":"Emacs","body":"[[Vim]] への言及","tags":["技術","メモ"]}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create -> %d: %s", w.Code, w.Body.String())
	}

	w = do(h, req("GET", "/api/pages/emacs", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("fetch -> %d", w.Code)
	}
	var p map[string]any
	json.Unmarshal(w.Body.Bytes(), &p)
	for _, key := range []string{"id", "slug", "title", "body", "tags", "version", "links", "backlinks"} {
		if _, ok := p[key]; !ok {
			t.Errorf("the response has no %q", key)
		}
	}
	links := p["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v", links)
	}
	if links[0].(map[string]any)["resolved"] != false {
		t.Error("[[Vim]] should be an unresolved link")
	}

	// A body-less DELETE is not required to carry a Content-Type, so the Emacs
	// layer and curl keep working
	w = do(h, req("DELETE", "/api/pages/emacs", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("delete -> %d", w.Code)
	}
	if got := do(h, req("GET", "/api/pages/emacs", "")).Code; got != http.StatusNotFound {
		t.Fatalf("fetch after delete -> %d, want 404", got)
	}
}

func TestHTMLPagesRender(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"Emacs","body":"# 見出し\n\n[[Vim]] と本文"}`))

	for _, path := range []string{"/", "/wiki", "/wiki/emacs", "/wiki/emacs/edit",
		"/wiki/emacs/history", "/wiki/new", "/tags", "/search?q=Emacs", "/gtd"} {
		w := do(h, req("GET", path, ""))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s → %d", path, w.Code)
			continue
		}
		body, _ := io.ReadAll(w.Body)
		if !bytes.Contains(body, []byte("</html>")) {
			t.Errorf("GET %s: HTML が途中で切れている", path)
		}
	}
}

// Rendering wikilinks: resolved ones become links, unresolved ones point at
// the create screen.
func TestWikilinkRendering(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"Vim","body":"エディタ"}`))
	do(h, req("POST", "/api/pages", `{"title":"Emacs","body":"[[Vim]] と [[まだ無い]] と [[Vim|別名表示]]"}`))

	w := do(h, req("GET", "/wiki/emacs", ""))
	body := w.Body.String()
	if !strings.Contains(body, `href="/wiki/vim"`) {
		t.Error("no link was rendered for a resolved wikilink")
	}
	if !strings.Contains(body, `wikilink-new`) {
		t.Error("unresolved links are not distinguished")
	}
	if !strings.Contains(body, "別名表示") {
		t.Error("the Label of [[Title|Label]] is missing")
	}
}

// GET /api/titles returns candidates for [[...]] completion, never touching
// bodies.
func TestTitlesAPI(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"Emacs","body":"本文"}`))
	do(h, req("POST", "/api/pages", `{"title":"別の記事","body":"Emacs のことを書いた本文"}`))

	w := do(h, req("GET", "/api/titles?q=emacs", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("titles → %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Titles []struct {
			Title, Slug, Canonical string
			IsAlias                bool `json:"is_alias"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Titles) != 1 {
		t.Fatalf("body hits must not be offered: %+v", res.Titles)
	}
	if res.Titles[0].Title != "Emacs" || res.Titles[0].Slug != "emacs" {
		t.Fatalf("candidate: %+v", res.Titles[0])
	}
}

// Static files carry the content hash in the ETag and in ?v=.
// ModTime in an embed.FS is zero so Last-Modified does nothing, and without this
// you get "I fixed the CSS but the page looks the same" (static.go).
func TestStaticAssetsAreFingerprinted(t *testing.T) {
	h := newServer(t)

	// Templates must emit the URL with ?v=
	w := do(h, req("GET", "/wiki", ""))
	body := w.Body.String()
	m := regexp.MustCompile(`/static/app\.css\?v=([a-f0-9]{8})`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no app.css with ?v= in the template output")
	}

	w = do(h, req("GET", "/static/app.css?v="+m[1], ""))
	if w.Code != http.StatusOK {
		t.Fatalf("app.css → %d", w.Code)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q", cc)
	}

	// The same ETag must answer 304
	r := req("GET", "/static/app.css?v="+m[1], "")
	r.Header.Set("If-None-Match", etag)
	if w := do(h, r); w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match → %d, want 304", w.Code)
	}

	// The bundled fonts must be served too, since this works offline
	if w := do(h, req("GET", "/static/fonts/inter-latin-wght-normal.woff2", "")); w.Code != http.StatusOK {
		t.Fatalf("font -> %d", w.Code)
	}
}

// Setting allowed_hosts additionally accepts that exact Host / Origin.
// **There is no wildcard, and without the setting it stays loopback-only**
// (DESIGN 4.4).
func TestAllowedHosts(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	blobs, err := filestore.Open(filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })

	cfg := config.Default()
	cfg.ExportDir, cfg.BackupDir = filepath.Join(dir, "e"), filepath.Join(dir, "b")
	cfg.AllowedHosts = []string{"Macbook.local"} // case-insensitive
	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	get := func(host string) int {
		r := httptest.NewRequest("GET", "/wiki", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := get("macbook.local"); c != http.StatusOK {
		t.Errorf("an allowed name was rejected: %d", c)
	}
	if c := get("MACBOOK.local:443"); c != http.StatusOK {
		t.Errorf("case and port must be ignored: %d", c)
	}
	if c := get("127.0.0.1:7777"); c != http.StatusOK {
		t.Errorf("loopback must keep working: %d", c)
	}
	// A name that was not configured is rejected as before
	for _, bad := range []string{"evil.com", "macbook.local.evil.com", "x.macbook.local"} {
		if c := get(bad); c != http.StatusForbidden {
			t.Errorf("%s was accepted: %d", bad, c)
		}
	}
	// Origin is judged by the same rule (allowedOrigin calls allowedHost)
	r := httptest.NewRequest("GET", "/wiki", nil)
	r.Host = "macbook.local"
	r.Header.Set("Origin", "https://macbook.local")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("an Origin with the same name was rejected: %d", w.Code)
	}
}

// Without the setting, nothing changes at all.
func TestAllowedHostsEmptyByDefault(t *testing.T) {
	h := newServer(t)
	r := httptest.NewRequest("GET", "/wiki", nil)
	r.Host = "macbook.local"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("something other than loopback was accepted by default: %d", w.Code)
	}
}
