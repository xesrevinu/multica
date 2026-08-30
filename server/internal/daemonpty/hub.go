package daemonpty

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 54 * time.Second
	readLimit  = 1 << 20
	sendBuf    = 256
)

type frame struct {
	messageType int
	data        []byte
}

// Hub pairs one browser WebSocket with one daemon PTY WebSocket per daemon_id.
// Control frames (Text JSON) and PTY bytes (Binary) are relayed as-is.
type Hub struct {
	upgrader websocket.Upgrader

	mu      sync.Mutex
	daemons map[string]*endpoint
}

type endpoint struct {
	id   string
	role string
	conn *websocket.Conn
	send chan frame

	mu      sync.Mutex
	peer    *endpoint
	closing bool
}

func NewHub() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			// Intranet POC: browsers and daemons both connect with credentials
			// on the handshake or first frame. Origin is not a security boundary here.
			CheckOrigin: func(*http.Request) bool { return true },
		},
		daemons: make(map[string]*endpoint),
	}
}

func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return h.upgrader.Upgrade(w, r, nil)
}

// ServeDaemon registers the daemon's PTY socket under daemonID and relays
// until either side disconnects. A reconnect replaces the previous socket.
func (h *Hub) ServeDaemon(daemonID string, conn *websocket.Conn) {
	if daemonID == "" {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon_id required",
		}))
		conn.Close()
		return
	}
	ep := h.attachDaemon(daemonID, conn)
	h.run(ep)
}

// ServeBrowser attaches a browser socket to the daemon's PTY socket. If the
// daemon is not connected, the browser is told so and the socket is closed.
// A second browser steals the session from the first.
func (h *Hub) ServeBrowser(daemonID string, conn *websocket.Conn) {
	if daemonID == "" {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon_id required",
		}))
		conn.Close()
		return
	}
	ep, ok := h.attachBrowser(daemonID, conn)
	if !ok {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon is not connected",
		}))
		conn.Close()
		return
	}
	h.run(ep)
}

func (h *Hub) attachDaemon(daemonID string, conn *websocket.Conn) *endpoint {
	ep := newEndpoint(daemonID, "daemon", conn)
	h.mu.Lock()
	prev := h.daemons[daemonID]
	h.daemons[daemonID] = ep
	h.mu.Unlock()
	if prev != nil {
		prev.closeWithError("daemon replaced")
	}
	return ep
}

func (h *Hub) attachBrowser(daemonID string, conn *websocket.Conn) (*endpoint, bool) {
	browser := newEndpoint(daemonID, "browser", conn)
	h.mu.Lock()
	daemon := h.daemons[daemonID]
	h.mu.Unlock()
	if daemon == nil {
		return nil, false
	}
	if !daemon.pair(browser) {
		return nil, false
	}
	return browser, true
}

func (h *Hub) dropDaemon(ep *endpoint) {
	h.mu.Lock()
	if current := h.daemons[ep.id]; current == ep {
		delete(h.daemons, ep.id)
	}
	h.mu.Unlock()
}

func newEndpoint(id, role string, conn *websocket.Conn) *endpoint {
	return &endpoint{
		id:   id,
		role: role,
		conn: conn,
		send: make(chan frame, sendBuf),
	}
}

func (ep *endpoint) pair(browser *endpoint) bool {
	ep.mu.Lock()
	if ep.closing {
		ep.mu.Unlock()
		return false
	}
	prev := ep.peer
	ep.peer = browser
	browser.mu.Lock()
	browser.peer = ep
	browser.mu.Unlock()
	ep.mu.Unlock()
	if prev != nil {
		prev.closeWithError("session taken over")
	}
	return true
}

func (ep *endpoint) peerSend(f frame) bool {
	ep.mu.Lock()
	peer := ep.peer
	ep.mu.Unlock()
	if peer == nil {
		return false
	}
	return peer.trySend(f)
}

func (ep *endpoint) trySend(f frame) bool {
	ep.mu.Lock()
	if ep.closing {
		ep.mu.Unlock()
		return false
	}
	ch := ep.send
	ep.mu.Unlock()
	select {
	case ch <- f:
		return true
	default:
		return false
	}
}

func (ep *endpoint) closeWithError(msg string) {
	ep.trySend(frame{
		messageType: websocket.TextMessage,
		data: mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: msg,
		}),
	})
	ep.close()
}

func (ep *endpoint) close() {
	ep.mu.Lock()
	if ep.closing {
		ep.mu.Unlock()
		return
	}
	ep.closing = true
	peer := ep.peer
	ep.peer = nil
	ch := ep.send
	role := ep.role
	ep.mu.Unlock()
	close(ch)
	if peer == nil {
		return
	}
	peer.detach(ep)
	// A browser leaving must not tear down the daemon socket — the next
	// browser should be able to open a new PTY on the same machine.
	if role == "daemon" {
		peer.close()
	}
}

func (ep *endpoint) detach(from *endpoint) {
	ep.mu.Lock()
	if ep.peer == from {
		ep.peer = nil
	}
	ep.mu.Unlock()
}

func (h *Hub) run(ep *endpoint) {
	defer func() {
		if ep.role == "daemon" {
			h.dropDaemon(ep)
		} else {
			ep.peerSend(frame{
				messageType: websocket.TextMessage,
				data:        mustControl(protocol.PTYControl{Type: protocol.PTYTypeClose}),
			})
		}
		ep.close()
		ep.conn.Close()
	}()

	done := make(chan struct{})
	go func() {
		ep.writePump()
		close(done)
	}()
	ep.readPump()
	<-done
}

func (ep *endpoint) readPump() {
	ep.conn.SetReadLimit(readLimit)
	ep.conn.SetReadDeadline(time.Now().Add(pongWait))
	ep.conn.SetPongHandler(func(string) error {
		ep.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	ep.conn.SetPingHandler(func(appData string) error {
		ep.conn.SetReadDeadline(time.Now().Add(pongWait))
		return ep.conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
	})

	for {
		messageType, data, err := ep.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("pty websocket read error", "error", err, "daemon_id", ep.id)
			}
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		if !ep.peerSend(frame{messageType: messageType, data: data}) {
			slog.Debug("pty websocket peer send dropped", "daemon_id", ep.id)
		}
	}
}

func (ep *endpoint) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case f, ok := <-ep.send:
			ep.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = ep.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := ep.conn.WriteMessage(f.messageType, f.data); err != nil {
				return
			}
		case <-ticker.C:
			ep.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ep.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func mustControl(msg protocol.PTYControl) []byte {
	b, err := json.Marshal(msg)
	if err != nil {
		return []byte(`{"type":"pty.error","error":"marshal failed"}`)
	}
	return b
}
