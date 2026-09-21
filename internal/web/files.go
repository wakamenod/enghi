package web

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/wakamenod/enghi/internal/files"
)

// apiUploadFile は画像などを受け取る。
//
// 本文は生のバイト列で、種別は Content-Type で渡す。
// **受け付けるのは `files.MediaTypeAllowed` に載っている種別だけ。**
// これらはいずれも CORS の simple request にはならないため、外部ページから
// preflight 無しに投げ込まれる経路は無い。
func (s *Server) apiUploadFile(w http.ResponseWriter, r *http.Request) {
	mediaType := mediaType(r.Header.Get("Content-Type"))
	if !files.MediaTypeAllowed(mediaType) {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"この種別は受け付けません: "+mediaType)
		return
	}
	// 上限を超えた分は読まずに切る
	data, err := io.ReadAll(io.LimitReader(r.Body, files.MaxBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(data) > files.MaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large",
			"ファイルが大きすぎます(上限 "+strconv.Itoa(files.MaxBytes/(1<<20))+" MB)")
		return
	}

	name := r.URL.Query().Get("name")
	f, err := s.files.Put(ctxOf(r), data, mediaType, name)
	if err != nil {
		switch {
		case errors.Is(err, files.ErrTooLarge):
			writeErr(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
		case errors.Is(err, files.ErrUnsupportedType):
			writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "upload_failed", err.Error())
		}
		return
	}
	// 記事に貼るための Markdown も返す(クライアント側で組み立てさせない)
	writeJSON(w, http.StatusCreated, map[string]any{
		"file":     f,
		"markdown": markdownFor(f),
	})
}

func markdownFor(f *files.File) string {
	alt := f.OriginalName
	if alt == "" {
		alt = "画像"
	}
	if f.MediaType == "application/pdf" {
		return "[" + alt + "](" + f.URL + ")"
	}
	return "![" + alt + "](" + f.URL + ")"
}

// serveFile は保管しているファイルを返す。
//
// 内容でアドレスしているので、中身が変われば URL も変わる。
// したがって恒久的にキャッシュしてよい。
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	// 拡張子付きの URL も受ける(/files/<hash>.png)
	if i := strings.IndexByte(hash, '.'); i > 0 {
		hash = hash[:i]
	}
	meta, err := s.files.Meta(ctxOf(r), hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	etag := `"` + meta.Hash + `"`
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	data, _, err := s.files.Get(ctxOf(r), hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", meta.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Bytes, 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// 保管しているのは利用者が貼ったファイルなので、種別の推測を止め、
	// 万一 HTML と解釈されうる内容でも実行されないようにしておく。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if meta.MediaType == "application/pdf" && meta.OriginalName != "" {
		w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+urlEscape(meta.OriginalName))
	}
	_, _ = w.Write(data)
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// apiListFiles は保管しているファイルの一覧。
func (s *Server) apiListFiles(w http.ResponseWriter, r *http.Request) {
	list, err := s.files.List(ctxOf(r), atoiDefault(r.URL.Query().Get("limit"), 100))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	count, bytes, _ := s.files.Stats(ctxOf(r))
	writeJSON(w, http.StatusOK, map[string]any{
		"files": list, "count": count, "bytes": bytes,
	})
}

// apiDeleteFile は1件消す。本文からの参照が残っていても消す(参照側は壊れたリンクになる)。
func (s *Server) apiDeleteFile(w http.ResponseWriter, r *http.Request) {
	if err := s.files.Delete(ctxOf(r), r.PathValue("hash")); err != nil {
		if errors.Is(err, files.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "ファイルが見つかりません")
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
