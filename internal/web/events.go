package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Event is a message delivered over /api/events.
type Event struct {
	Type string `json:"type"`           // navigate / updated
	Path string `json:"path,omitempty"` // for navigate
	Kind string `json:"kind,omitempty"` // for updated
	Slug string `json:"slug,omitempty"`
}

// Hub broadcasts to the connected clients.
// The focus channel of DESIGN 4.3: pick something in Emacs and the browser tab
// that is already open follows along.
//
// **WebSocket, not SSE** - the option DESIGN 4.3 lists first.
// SSE is an HTTP request that never finishes, so the browser considers the page
// to be still loading and Safari spins the tab spinner forever (measured: with
// readyState=complete, one unfinished request remains). After the upgrade a
// WebSocket is no longer an ordinary request, so the problem cannot arise.
type Hub struct {
	mu      sync.Mutex
	clients map[chan Event]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[chan Event]struct{}{}} }

func (h *Hub) add() chan Event {
	ch := make(chan Event, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) remove(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Broadcast delivers to every client, skipping - not dropping - the ones that
// are backed up.
func (h *Hub) Broadcast(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- e:
		default: // give up on a client whose buffer is full; never stall the server
		}
	}
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// originPatterns lists the origins allowed for the WebSocket.
// **This is a separate check from the secure middleware, so forgetting to add
// allowed_hosts here leaves "the page opens but live updates silently never
// connect".** app.js keeps retrying with exponential backoff, so no error is
// shown either.
func (s *Server) originPatterns() []string {
	out := []string{"127.0.0.1:*", "localhost:*", "[::1]:*"}
	for _, h := range s.cfg.NormalizedAllowedHosts() {
		out = append(out, h, h+":*")
	}
	return out
}

// handleEvents is the WebSocket endpoint.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// The secure middleware has already validated Origin, but the library's own
	// default - requiring Host and Origin to match - is left in place as well.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.originPatterns(),
	})
	if err != nil {
		return // Accept has already written the response
	}
	defer conn.CloseNow()

	ch := s.hub.add()
	defer s.hub.remove(ch)

	ctx := r.Context()

	// The read side. The client sends nothing, but **without reading, neither
	// close frames nor pings are processed.** When the read ends the connection
	// is gone, so the write side stops.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	// Check periodically that it is still alive, releasing half-open connections
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(wctx, conn, e)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// handleFocus is POST /api/focus {path}. It broadcasts a navigate to every
// connected client, moving the browser tab.
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if in.Path == "" || in.Path[0] != '/' {
		writeErr(w, http.StatusBadRequest, "bad_request", s.tr(r, "err.path_required"))
		return
	}
	s.hub.Broadcast(Event{Type: "navigate", Path: in.Path})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "clients": s.hub.Count()})
}

// handleStatus reports the server state without side effects. Without a way to
// check whether the focus channel is connected, "focus does not arrive for some
// reason" cannot be diagnosed.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"event_clients": s.hub.Count(),
		"db":            s.db.Path,
		"export_dir":    s.cfg.ExportDir,
	})
}
