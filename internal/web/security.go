package web

import (
	"fmt"
	"net/http"
	"strings"

	filestore "github.com/wakamenod/enghi/internal/files"
)

// secure は DESIGN 4.4 の3層をすべて適用する。
// **「127.0.0.1 に bind したから安全」は誤りである。**
// 利用者が普段ブラウザで開いている任意の Web ページの JavaScript が
// http://127.0.0.1:<port>/api/... を叩けるため、以下がすべて必要になる。
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Host ヘッダの検証。
		//    **これが本丸であり、DNS rebinding に対する唯一有効な防御である。**
		//    rebinding が成立するとブラウザから見て同一オリジンになるため Origin 検査では防げない。
		if !s.allowedHost(r.Host) {
			forbid(w, s.tr(r, "err.forbidden_host", r.Host))
			return
		}

		// 2. Origin / Sec-Fetch-Site の検証。クロスオリジン呼び出しを弾く。
		//    **Sec-Fetch-Site は curl や Emacs の url-retrieve では送られてこない。**
		//    判定は「ヘッダが存在する場合に same-origin 以外なら 403」とすること。
		//    「存在しなければ拒否」にすると Emacs 層が動かなくなる。
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

		// 3. Content-Type の強制。simple request として preflight 無しに通る経路を塞ぐ。
		//    simple request になりうるのは GET / HEAD / POST だけなので、強制が本当に
		//    効いているのは POST である。PUT / PATCH / DELETE はメソッド自体が
		//    必ず preflight を起こすため、**本文の無い呼び出しにまで Content-Type を
		//    要求すると、防御を足さずに Emacs 層と curl を壊すだけになる。**
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

// formAllowed は「ブラウザの UI から来た form 送信」に限って
// application/x-www-form-urlencoded を許す経路。
// Sec-Fetch-Site: same-origin が付いていることを条件とするため、
// 外部ページからの simple request では通らない。
// needsJSONContentType は Content-Type を強制すべき呼び出しかを判定する。
func (s *Server) needsJSONContentType(r *http.Request) bool {
	if !isWrite(r.Method) || s.formAllowed(r) {
		return false
	}
	if r.Method == http.MethodPost {
		// ファイルのアップロードは生のバイト列を受け取る。
		// 許すのは files.MediaTypeAllowed に載っている種別だけで、
		// これらは CORS の simple request にならないため preflight を避けられない。
		if r.URL.Path == "/api/files" && filestore.MediaTypeAllowed(mediaType(r.Header.Get("Content-Type"))) {
			return false
		}
		return true // simple request になりうるのは POST。常に強制する
	}
	// PUT / PATCH / DELETE は本文を伴うときだけ検査する
	return r.ContentLength != 0 || r.Header.Get("Content-Type") != ""
}

func (s *Server) formAllowed(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		return false
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func (s *Server) allowedHost(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	h = strings.Trim(h, "[]")
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
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
