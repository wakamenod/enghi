package web

import (
	"bufio"
	"log"
	"net"
	"net/http"
)

// statusRecorder remembers the status that was written.
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

// Flush is passed through for streaming.
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack is needed for a WebSocket upgrade.
// **Wrapping a ResponseWriter drops whatever the original could do.**
// Without passing this through, /api/events cannot be established - a test
// caught it.
func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

// logErrors records only responses of 400 and above.
//
// **Successful requests are not logged.** This runs all day, so it stays quiet
// and keeps only what went wrong. When "the page I opened did not come up",
// knowing what was actually requested is what makes it diagnosable.
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
