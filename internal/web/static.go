package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	enghi "github.com/wakamenod/enghi"
)

// staticAsset is one embedded static file.
type staticAsset struct {
	body []byte
	etag string // hash of the content, in ETag form with quotes
	ver  string // the short hash carried in ?v=
}

// staticAssets is built once at start-up. The files are embedded, so there is
// nothing to re-read.
//
// **ModTime in an embed.FS is zero.** A plain http.FileServer therefore emits
// no Last-Modified and the browser falls back to heuristic caching, producing
// "I fixed the CSS but the page looks the same". So the content hash goes into
// both the ETag and ?v=, and the file is re-fetched only when the URL changes.
var staticAssets = loadStaticAssets()

func loadStaticAssets() map[string]staticAsset {
	out := map[string]staticAsset{}
	sub, err := fs.Sub(enghi.StaticFS, "web/static")
	if err != nil {
		panic(err)
	}
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		out["/static/"+p] = staticAsset{body: b, etag: `"` + h[:16] + `"`, ver: h[:8]}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return out
}

// assetURL appends the content hash to a static file's URL. Templates use it as
// {{asset "/static/app.css"}}. An unknown path is returned unchanged.
func assetURL(p string) string {
	a, ok := staticAssets[p]
	if !ok {
		return p
	}
	return p + "?v=" + a.ver
}

// serveStatic serves the embedded static files. It asks for long-lived caching
// on the assumption that ?v= is present: when the content changes, so does the
// URL.
func serveStatic(w http.ResponseWriter, r *http.Request) {
	a, ok := staticAssets[path.Clean(r.URL.Path)]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(r.URL.Path))); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("ETag", a.etag)
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// Requested directly without ?v=: revalidate every time (the ETag makes
		// it a 304)
		w.Header().Set("Cache-Control", "no-cache")
	}
	// Pass the zero ModTime through; ServeContent answers 304 from the ETag.
	http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(a.body))
}
