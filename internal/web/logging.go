package web

import (
	"bufio"
	"log"
	"net"
	"net/http"
)

// statusRecorder は書かれたステータスを覚えておく。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush は streaming のために透過させる。
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack は WebSocket の Upgrade に必要。
// **包んだ時点で元の ResponseWriter が持っていた機能は失われる。**
// 透過させないと /api/events が張れなくなる(テストで検出した)。
func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

// logErrors は 400 以上の応答だけを記録する。
//
// **成功したリクエストは記録しない。**常駐するものなので、
// 普段は静かにしておき、うまくいかなかったものだけを残す。
// 「開いたはずのページが出ない」ときに、何を要求したのかが分からないと切り分けられない。
func logErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status >= 400 {
			ua := r.Header.Get("User-Agent")
			if len(ua) > 60 {
				ua = ua[:60] + "…"
			}
			log.Printf("%d %s %s (host=%s ua=%q)",
				rec.status, r.Method, r.URL.RequestURI(), r.Host, ua)
		}
	})
}
