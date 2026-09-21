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

// staticAsset は埋め込み済みの静的ファイル1件。
type staticAsset struct {
	body []byte
	etag string // 中身のハッシュ。"..." で囲んだ ETag 形式
	ver  string // ?v= に載せる短いハッシュ
}

// staticAssets は起動時に1度だけ作る。埋め込み済みなので読み直す意味はない。
//
// **embed.FS の ModTime はゼロである。** そのため素の http.FileServer では
// Last-Modified が出ず、ブラウザはヒューリスティックなキャッシュ判断をする。
// 「CSS を直したのに画面が変わらない」が起きるので、内容のハッシュを ETag と
// ?v= の両方に使い、URL が変わったときだけ取り直させる。
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

// assetURL は静的ファイルの URL に内容のハッシュを付ける。テンプレートから
// {{asset "/static/app.css"}} として使う。未知のパスはそのまま返す。
func assetURL(p string) string {
	a, ok := staticAssets[p]
	if !ok {
		return p
	}
	return p + "?v=" + a.ver
}

// serveStatic は埋め込み済みの静的ファイルを返す。?v= が付く前提で
// 長期キャッシュを指示する(内容が変われば URL が変わる)。
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
		// ?v= なしで直接叩かれた場合。毎回確かめさせる(ETag で 304 になる)
		w.Header().Set("Cache-Control", "no-cache")
	}
	// ModTime はゼロのまま渡す。ServeContent は ETag で 304 を返す。
	http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(a.body))
}
