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

// req は既定でローカルの正しいヘッダを付ける。
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

// 4.4-1: Host ヘッダの検証。**DNS rebinding に対する唯一有効な防御。**
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

// 4.4-2: Sec-Fetch-Site は**存在する場合にのみ**判定する。
// 「存在しなければ拒否」にすると Emacs 層(url-retrieve)や curl が動かなくなる。
func TestSecFetchSiteOnlyCheckedWhenPresent(t *testing.T) {
	h := newServer(t)

	// ヘッダ無し(curl / Emacs)は通る
	if got := do(h, req("GET", "/api/pages", "")).Code; got != http.StatusOK {
		t.Errorf("ヘッダ無し → %d, want 200(Emacs 層が動かなくなる)", got)
	}
	// same-origin は通る
	r := req("GET", "/api/pages", "")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if got := do(h, r).Code; got != http.StatusOK {
		t.Errorf("same-origin → %d, want 200", got)
	}
	// cross-site は弾く
	r = req("GET", "/api/pages", "")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("cross-site → %d, want 403", got)
	}
	// 外部オリジンは弾く
	r = req("GET", "/api/pages", "")
	r.Header.Set("Origin", "https://evil.example.com")
	if got := do(h, r).Code; got != http.StatusForbidden {
		t.Errorf("外部 Origin → %d, want 403", got)
	}
}

// 4.4-3: 書き込み系は application/json のみ。
// preflight を回避できる simple request(text/plain / form-urlencoded)経路を塞ぐ。
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

	// 本文の無い POST も塞ぐ(/api/export のような本文不要の経路が simple request で叩かれるため)
	r := httptest.NewRequest("POST", "/api/export", nil)
	r.Host = "127.0.0.1:7777"
	if got := do(h, r).Code; got != http.StatusUnsupportedMediaType {
		t.Errorf("本文の無い POST → %d, want 415", got)
	}
}

// 4.2: 409 は2つの異なる意味を持つ。**機械可読なコードで区別すること。**
func TestConflictCodesAreDistinguishable(t *testing.T) {
	h := newServer(t)
	mustCreate := func(title string) map[string]any {
		w := do(h, req("POST", "/api/pages", `{"title":"`+title+`","body":"x"}`))
		if w.Code != http.StatusCreated {
			t.Fatalf("作成に失敗 %d: %s", w.Code, w.Body.String())
		}
		var p map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := mustCreate("Emacs")
	b := mustCreate("Vim")

	// version_conflict — 入力は捨てず、現行データを返す
	w := do(h, req("PUT", "/api/pages/"+b["slug"].(string),
		`{"title":"Vim","body":"新しい本文","version":99}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("version 不一致 → %d, want 409", w.Code)
	}
	var vc map[string]any
	json.Unmarshal(w.Body.Bytes(), &vc)
	if vc["error"] != "version_conflict" {
		t.Errorf("error = %v, want version_conflict", vc["error"])
	}
	if vc["current"] == nil {
		t.Error("current(現行データ)が返っていない。マージできない")
	}

	// title_conflict — 衝突相手のページを返す
	w = do(h, req("PUT", "/api/pages/"+b["slug"].(string),
		`{"title":"Emacs","body":"x","version":1}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("タイトル衝突 → %d, want 409", w.Code)
	}
	var tc map[string]any
	json.Unmarshal(w.Body.Bytes(), &tc)
	if tc["error"] != "title_conflict" {
		t.Errorf("error = %v, want title_conflict", tc["error"])
	}
	cp, ok := tc["conflicting_page"].(map[string]any)
	if !ok || cp["slug"] != a["slug"] {
		t.Errorf("衝突相手のページが返っていない: %v", tc["conflicting_page"])
	}
}

// PUT は version 必須。無しを許すと Emacs 側の競合検出が意味を失う。
func TestUpdateRequiresVersion(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"記事","body":"x"}`))
	w := do(h, req("PUT", "/api/pages/記事", `{"title":"記事","body":"y"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("version 無し → %d, want 400", w.Code)
	}
}

// 4.3: POST /api/focus → 接続中のクライアントへ navigate を配信する。
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
		t.Fatalf("WebSocket に接続できない: %v", err)
	}
	defer conn.CloseNow()

	// 接続が登録されるまで待つ
	deadline := time.Now().Add(3 * time.Second)
	for srv.Hub().Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Hub().Count() != 1 {
		t.Fatalf("接続数 = %d, want 1", srv.Hub().Count())
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
		t.Fatalf("navigate が届かない: %v", err)
	}
	if got.Type != "navigate" || got.Path != "/wiki/foo" {
		t.Fatalf("受信した内容が違う: %+v", got)
	}

	// 切断したら解放されること(掴んだままの接続を残さない)
	conn.Close(websocket.StatusNormalClosure, "")
	deadline = time.Now().Add(3 * time.Second)
	for srv.Hub().Count() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.Hub().Count(); n != 0 {
		t.Fatalf("切断後も接続数が %d", n)
	}
}

// クロスオリジンからの WebSocket 接続は弾く。
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
		t.Fatal("外部オリジンからの WebSocket 接続が通ってしまった")
	}
}

// 4.4: POST /api/export は出力先を受け取らない。設定の値に固定される。
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
		t.Fatalf("リクエストの出力先が使われた: %v", res["dir"])
	}
}

func TestPagesAPIRoundTrip(t *testing.T) {
	h := newServer(t)
	w := do(h, req("POST", "/api/pages",
		`{"title":"Emacs","body":"[[Vim]] への言及","tags":["技術","メモ"]}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("作成 → %d: %s", w.Code, w.Body.String())
	}

	w = do(h, req("GET", "/api/pages/emacs", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("取得 → %d", w.Code)
	}
	var p map[string]any
	json.Unmarshal(w.Body.Bytes(), &p)
	for _, key := range []string{"id", "slug", "title", "body", "tags", "version", "links", "backlinks"} {
		if _, ok := p[key]; !ok {
			t.Errorf("レスポンスに %q が無い", key)
		}
	}
	links := p["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v", links)
	}
	if links[0].(map[string]any)["resolved"] != false {
		t.Error("[[Vim]] は未解決リンクのはず")
	}

	// 本文の無い DELETE に Content-Type を要求しない(Emacs 層と curl を壊さない)
	w = do(h, req("DELETE", "/api/pages/emacs", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("削除 → %d", w.Code)
	}
	if got := do(h, req("GET", "/api/pages/emacs", "")).Code; got != http.StatusNotFound {
		t.Fatalf("削除後の取得 → %d, want 404", got)
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

// wikilink のレンダリング: 解決済みはリンク、未解決は作成画面へ。
func TestWikilinkRendering(t *testing.T) {
	h := newServer(t)
	do(h, req("POST", "/api/pages", `{"title":"Vim","body":"エディタ"}`))
	do(h, req("POST", "/api/pages", `{"title":"Emacs","body":"[[Vim]] と [[まだ無い]] と [[Vim|別名表示]]"}`))

	w := do(h, req("GET", "/wiki/emacs", ""))
	body := w.Body.String()
	if !strings.Contains(body, `href="/wiki/vim"`) {
		t.Error("解決済みリンクが張られていない")
	}
	if !strings.Contains(body, `wikilink-new`) {
		t.Error("未解決リンクが区別されていない")
	}
	if !strings.Contains(body, "別名表示") {
		t.Error("[[Title|Label]] の Label が出ていない")
	}
}

// GET /api/titles は [[...]] の補完候補を返す。本文は見ない。
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
		t.Fatalf("本文ヒットは候補に入れない: %+v", res.Titles)
	}
	if res.Titles[0].Title != "Emacs" || res.Titles[0].Slug != "emacs" {
		t.Fatalf("候補: %+v", res.Titles[0])
	}
}

// 静的ファイルは内容のハッシュを ETag と ?v= に載せる。
// embed.FS の ModTime はゼロで Last-Modified が効かないため、これが無いと
// 「CSS を直したのに画面が変わらない」が起きる(static.go)。
func TestStaticAssetsAreFingerprinted(t *testing.T) {
	h := newServer(t)

	// テンプレートは ?v= 付きの URL を出すこと
	w := do(h, req("GET", "/wiki", ""))
	body := w.Body.String()
	m := regexp.MustCompile(`/static/app\.css\?v=([a-f0-9]{8})`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("テンプレートに ?v= 付きの app.css が無い")
	}

	w = do(h, req("GET", "/static/app.css?v="+m[1], ""))
	if w.Code != http.StatusOK {
		t.Fatalf("app.css → %d", w.Code)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag が無い")
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q", cc)
	}

	// 同じ ETag で問い合わせたら 304
	r := req("GET", "/static/app.css?v="+m[1], "")
	r.Header.Set("If-None-Match", etag)
	if w := do(h, r); w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match → %d, want 304", w.Code)
	}

	// 同梱したフォントも配れること(オフラインで動く前提)
	if w := do(h, req("GET", "/static/fonts/inter-latin-wght-normal.woff2", "")); w.Code != http.StatusOK {
		t.Fatalf("フォント → %d", w.Code)
	}
}

// allowed_hosts を設定すると、その名前の Host / Origin だけが追加で通る。
// **ワイルドカードは無い。設定しなければ従来どおりループバックのみ**(DESIGN 4.4)。
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
	cfg.AllowedHosts = []string{"Macbook.local"} // 大小は区別しない
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
		t.Errorf("許可した名前が通らない: %d", c)
	}
	if c := get("MACBOOK.local:443"); c != http.StatusOK {
		t.Errorf("大小とポートを無視すること: %d", c)
	}
	if c := get("127.0.0.1:7777"); c != http.StatusOK {
		t.Errorf("ループバックは従来どおり通ること: %d", c)
	}
	// 設定していない名前は今までどおり弾く
	for _, bad := range []string{"evil.com", "macbook.local.evil.com", "x.macbook.local"} {
		if c := get(bad); c != http.StatusForbidden {
			t.Errorf("%s が通ってしまった: %d", bad, c)
		}
	}
	// Origin も同じ基準で判定される(allowedOrigin が allowedHost を呼ぶ)
	r := httptest.NewRequest("GET", "/wiki", nil)
	r.Host = "macbook.local"
	r.Header.Set("Origin", "https://macbook.local")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("同じ名前の Origin が弾かれた: %d", w.Code)
	}
}

// 設定しなければ、これまでと何も変わらない。
func TestAllowedHostsEmptyByDefault(t *testing.T) {
	h := newServer(t)
	r := httptest.NewRequest("GET", "/wiki", nil)
	r.Host = "macbook.local"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("既定でループバック以外が通ってしまった: %d", w.Code)
	}
}
