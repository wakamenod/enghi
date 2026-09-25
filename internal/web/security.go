package web

import (
	"fmt"
	"net/http"
	"strings"

	filestore "github.com/wakamenod/enghi/internal/files"
)

// secure applies all three layers of DESIGN 4.4.
// **"It binds to 127.0.0.1, so it is safe" is wrong.** JavaScript on any page
// the user happens to have open can call http://127.0.0.1:<port>/api/..., which
// is why every one of the following is needed.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Validate the Host header.
		//    **This is the main defense, and the only effective one against DNS
		//    rebinding.** Once rebinding succeeds the browser considers it the
		//    same origin, so an Origin check cannot stop it.
		if !s.allowedHost(r.Host) {
			forbid(w, s.tr(r, "err.forbidden_host", r.Host))
			return
		}

		// 2. Validate Origin / Sec-Fetch-Site to reject cross-origin calls.
		//    **Neither curl nor Emacs's url-retrieve sends Sec-Fetch-Site.**
		//    The rule is "if the header is present and is not same-origin, 403".
		//    "Reject when absent" would break the Emacs layer.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			if site != "same-origin" && site != "none" {
				forbid(w, s.tr(r, "err.forbidden_site", site))
				return
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.allowedOrigin(origin) {
				forbid(w, s.tr(r, "err.forbidden_origin", origin))
				return
			}
		}

		// 3. Enforce Content-Type, closing the path where a simple request gets
		//    through without a preflight. Only GET / HEAD / POST can be simple
		//    requests, so in practice this bites on POST. PUT / PATCH / DELETE
		//    always trigger a preflight by virtue of the method, so **demanding a
		//    Content-Type even on a body-less call adds no protection and merely
		//    breaks the Emacs layer and curl.**
		if s.needsJSONContentType(r) {
			ct := r.Header.Get("Content-Type")
			if mediaType(ct) != "application/json" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnsupportedMediaType)
				fmt.Fprintf(w, `{"error":"unsupported_media_type","message":%q}`,
					s.tr(r, "err.unsupported_media"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// formAllowed lets application/x-www-form-urlencoded through, but only for a
// form submitted by the browser UI. It requires Sec-Fetch-Site: same-origin, so
// a simple request from an external page never qualifies.
// needsJSONContentType reports whether a call must carry a JSON Content-Type.
func (s *Server) needsJSONContentType(r *http.Request) bool {
	if !isWrite(r.Method) || s.formAllowed(r) {
		return false
	}
	if r.Method == http.MethodPost {
		// File uploads take raw bytes. Only the media types listed in
		// files.MediaTypeAllowed are accepted, and none of them can be a CORS
		// simple request, so a preflight is unavoidable for them.
		if r.URL.Path == "/api/files" && filestore.MediaTypeAllowed(mediaType(r.Header.Get("Content-Type"))) {
			return false
		}
		return true // POST is the one that can be a simple request; always enforce
	}
	// PUT / PATCH / DELETE are checked only when they carry a body
	return r.ContentLength != 0 || r.Header.Get("Content-Type") != ""
}

func (s *Server) formAllowed(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		return false
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// hostName lower-cases a Host header value and drops its port and brackets.
func hostName(host string) string {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.Trim(h, "[]")
}

func isLoopbackName(h string) bool {
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// fromThisMachine reports whether the request came in under a loopback name
// rather than one of allowed_hosts. Actions that write outside enghi's own
// data (install-skill writes into the home directory) are limited to it.
// **A proxy may rewrite Host to the upstream address**, so a request carrying
// forwarding headers does not count either.
func fromThisMachine(r *http.Request) bool {
	for _, k := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host"} {
		if r.Header.Get(k) != "" {
			return false
		}
	}
	return isLoopbackName(hostName(r.Host))
}

func (s *Server) allowedHost(host string) bool {
	h := hostName(host)
	if isLoopbackName(h) {
		return true
	}
	// Names added explicitly in the configuration, for running behind a proxy.
	// **It is empty by default, and then only the three loopback names above
	// are accepted.**
	for _, a := range s.cfg.NormalizedAllowedHosts() {
		if h == a {
			return true
		}
	}
	return false
}

func (s *Server) allowedOrigin(origin string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return s.allowedHost(o)
}

func isWrite(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func mediaType(ct string) string {
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

func forbid(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"error":"forbidden","message":%q}`, msg)
}
