package web

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/wakamenod/enghi/internal/files"
)

// apiUploadFile receives an image or similar file.
//
// The body is raw bytes and the kind comes in the Content-Type.
// **Only the media types listed in `files.MediaTypeAllowed` are accepted.**
// None of them can be a CORS simple request, so there is no path for an
// external page to push one in without a preflight.
func (s *Server) apiUploadFile(w http.ResponseWriter, r *http.Request) {
	mediaType := mediaType(r.Header.Get("Content-Type"))
	if !files.MediaTypeAllowed(mediaType) {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			s.tr(r, "err.file_unsupported", mediaType))
		return
	}
	// Cut off anything beyond the limit instead of reading it
	data, err := io.ReadAll(io.LimitReader(r.Body, files.MaxBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(data) > files.MaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large",
			s.tr(r, "err.file_too_large", files.MaxBytes/(1<<20)))
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
	// Return the Markdown to paste into an article as well, rather than making
	// the client assemble it
	writeJSON(w, http.StatusCreated, map[string]any{
		"file":     f,
		"markdown": markdownFor(f, s.tr(r, "files.alt_default")),
	})
}

func markdownFor(f *files.File, fallbackAlt string) string {
	alt := f.OriginalName
	if alt == "" {
		alt = fallbackAlt
	}
	if f.MediaType == "application/pdf" {
		return "[" + alt + "](" + f.URL + ")"
	}
	return "![" + alt + "](" + f.URL + ")"
}

// serveFile serves a stored file.
//
// Storage is content-addressed, so different content means a different URL and
// the response can be cached forever.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	// URLs with an extension are accepted too (/files/<hash>.png)
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
	// These are files the user pasted in, so turn content sniffing off: even if
	// something could be read as HTML, it must not run.
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

// apiListFiles lists the stored files.
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

// apiDeleteFile deletes one file, even when a body still references it; that
// reference becomes a broken link.
func (s *Server) apiDeleteFile(w http.ResponseWriter, r *http.Request) {
	if err := s.files.Delete(ctxOf(r), r.PathValue("hash")); err != nil {
		if errors.Is(err, files.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", s.tr(r, "err.not_found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
