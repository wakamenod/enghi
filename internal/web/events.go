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

// Event は /api/events で配信するメッセージ。
type Event struct {
	Type string `json:"type"`           // navigate / updated
	Path string `json:"path,omitempty"` // navigate のとき
	Kind string `json:"kind,omitempty"` // updated のとき
	Slug string `json:"slug,omitempty"`
}

// Hub は接続中のクライアントに配信する。
// DESIGN 4.3 の focus チャネル: Emacs で選択 → 開きっぱなしのブラウザが追従する。
//
// **SSE ではなく WebSocket を使う**(DESIGN 4.3 が第一に挙げている方式)。
// SSE は「終わらない HTTP リクエスト」なので、ブラウザは常に読み込み中とみなし、
// Safari ではタブのスピナーが回り続ける(実測確認済み: readyState=complete でも
// 未完了リクエストが 1 本残る)。WebSocket は Upgrade 後に通常のリクエストではなくなるため、
// この問題が構造的に発生しない。
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

// Broadcast は全クライアントに配信する。詰まっているクライアントは落とさず読み飛ばす。
func (h *Hub) Broadcast(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- e:
		default: // バッファが詰まっているクライアントは諦める(常駐を止めない)
		}
	}
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// originPatterns は WebSocket で許す Origin。
// **ここは secure ミドルウェアとは別の判定なので、allowed_hosts を足し忘れると
// 「画面は開けるのに live 更新だけ黙って繋がらない」状態になる。**
// app.js は指数バックオフで再接続を試み続けるため、エラーも出ない。
func (s *Server) originPatterns() []string {
	out := []string{"127.0.0.1:*", "localhost:*", "[::1]:*"}
	for _, h := range s.cfg.NormalizedAllowedHosts() {
		out = append(out, h, h+":*")
	}
	return out
}

// handleEvents は WebSocket のエンドポイント。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Origin の検証は secure ミドルウェアが済ませているが、
	// ライブラリ側の既定(Host と Origin の一致を要求)もそのまま効かせる。
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.originPatterns(),
	})
	if err != nil {
		return // Accept が既にレスポンスを書いている
	}
	defer conn.CloseNow()

	ch := s.hub.add()
	defer s.hub.remove(ch)

	ctx := r.Context()

	// 読み側。クライアントからは何も送られてこないが、
	// **読まないと close フレームも ping も処理されない。**
	// 読みが終わったら接続が閉じたということなので、書き側を止める。
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	// 生きているかを定期的に確かめる。半開きの接続をここで解放する。
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

// handleFocus は POST /api/focus {path}。
// 接続中の全クライアントに navigate を配信し、ブラウザタブを遷移させる。
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if in.Path == "" || in.Path[0] != '/' {
		writeErr(w, http.StatusBadRequest, "bad_request", "path は / で始まる必要があります")
		return
	}
	s.hub.Broadcast(Event{Type: "navigate", Path: in.Path})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "clients": s.hub.Count()})
}

// handleStatus は副作用なしにサーバの状態を返す。
// focus チャネルが繋がっているかを確認する手段が無いと、
// 「なぜか focus が飛ばない」ときに切り分けができない。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"event_clients": s.hub.Count(),
		"db":            s.db.Path,
		"export_dir":    s.cfg.ExportDir,
	})
}
