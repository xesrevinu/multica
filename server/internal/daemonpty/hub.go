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

// Hub multiplexes many browser PTY sockets onto one daemon PTY socket.
// Browser frames stay raw; daemon-facing binary frames are prefixed with a
// 16-byte PTY id so sessions never share stdin/stdout.
type Hub struct {
	upgrader websocket.Upgrader

	mu      sync.Mutex
	daemons map[string]*daemonSlot
}

type daemonSlot struct {
	mu       sync.Mutex
	daemon   *endpoint
	browsers map[string][]*endpoint
}

type endpoint struct {
	daemonID string
	ptyID    string
	role     string
	conn     *websocket.Conn
	send     chan frame

	mu      sync.Mutex
	closing bool
}

func NewHub() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		daemons: make(map[string]*daemonSlot),
	}
}

func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return h.upgrader.Upgrade(w, r, nil)
}

func (h *Hub) ServeDaemon(daemonID string, conn *websocket.Conn) {
	if daemonID == "" {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon_id required",
		}))
		conn.Close()
		return
	}
	ep := newEndpoint(daemonID, "", "daemon", conn)
	slot := h.replaceDaemon(daemonID, ep)
	h.run(slot, ep)
}

func (h *Hub) ServeBrowser(daemonID, ptyID string, conn *websocket.Conn) {
	if daemonID == "" {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon_id required",
		}))
		conn.Close()
		return
	}
	if ptyID == "" {
		ptyID = "default"
	}
	ep := newEndpoint(daemonID, ptyID, "browser", conn)
	slot, ok := h.attachBrowser(daemonID, ptyID, ep)
	if !ok {
		_ = conn.WriteMessage(websocket.TextMessage, mustControl(protocol.PTYControl{
			Type:  protocol.PTYTypeError,
			Error: "daemon is not connected",
		}))
		conn.Close()
		return
	}
	h.run(slot, ep)
}

func (h *Hub) replaceDaemon(daemonID string, ep *endpoint) *daemonSlot {
	h.mu.Lock()
	slot := h.daemons[daemonID]
	if slot == nil {
		slot = &daemonSlot{browsers: make(map[string][]*endpoint)}
		h.daemons[daemonID] = slot
	}
	h.mu.Unlock()

	slot.mu.Lock()
	prev := slot.daemon
	slot.daemon = ep
	slot.mu.Unlock()
	if prev != nil {
		prev.closeWithError("daemon replaced")
	}
	return slot
}

func (h *Hub) attachBrowser(daemonID, ptyID string, ep *endpoint) (*daemonSlot, bool) {
	h.mu.Lock()
	slot := h.daemons[daemonID]
	h.mu.Unlock()
	if slot == nil {
		return nil, false
	}
	slot.mu.Lock()
	if slot.daemon == nil || slot.daemon.closing {
		slot.mu.Unlock()
		return nil, false
	}
	slot.browsers[ptyID] = append(slot.browsers[ptyID], ep)
	slot.mu.Unlock()
	return slot, true
}

func (h *Hub) drop(ep *endpoint) {
	h.mu.Lock()
	slot := h.daemons[ep.daemonID]
	h.mu.Unlock()
	if slot == nil {
		return
	}
	slot.mu.Lock()
	if ep.role == "daemon" {
		if slot.daemon == ep {
			slot.daemon = nil
		}
	} else {
		list := slot.browsers[ep.ptyID]
		kept := list[:0]
		for _, browser := range list {
			if browser != ep {
				kept = append(kept, browser)
			}
		}
		if len(kept) == 0 {
			delete(slot.browsers, ep.ptyID)
		} else {
			slot.browsers[ep.ptyID] = kept
		}
	}
	empty := slot.daemon == nil && len(slot.browsers) == 0
	slot.mu.Unlock()
	if empty {
		h.mu.Lock()
		if current := h.daemons[ep.daemonID]; current == slot {
			delete(h.daemons, ep.daemonID)
		}
		h.mu.Unlock()
	}
}

func newEndpoint(daemonID, ptyID, role string, conn *websocket.Conn) *endpoint {
	return &endpoint{
		daemonID: daemonID,
		ptyID:    ptyID,
		role:     role,
		conn:     conn,
		send:     make(chan frame, sendBuf),
	}
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
			ID:    ep.ptyID,
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
	ch := ep.send
	ep.mu.Unlock()
	close(ch)
}

func (h *Hub) run(slot *daemonSlot, ep *endpoint) {
	defer func() {
		if ep.role != "browser" {
			slot.mu.Lock()
			browsers := make([]*endpoint, 0)
			for _, group := range slot.browsers {
				browsers = append(browsers, group...)
			}
			slot.mu.Unlock()
			for _, browser := range browsers {
				browser.closeWithError("daemon disconnected")
			}
		}
		h.drop(ep)
		ep.close()
		ep.conn.Close()
	}()

	done := make(chan struct{})
	go func() {
		ep.writePump()
		close(done)
	}()
	if ep.role == "daemon" {
		h.readDaemon(slot, ep)
	} else {
		h.readBrowser(slot, ep)
	}
	<-done
}

func (h *Hub) readBrowser(slot *daemonSlot, ep *endpoint) {
	if !readLoop(ep, func(f frame) {
		if f.messageType == websocket.BinaryMessage {
			uid := protocol.ParsePTYID(ep.ptyID)
			f.data = protocol.EncodePTYFrame(uid, f.data)
		} else {
			f.data = stampControl(f.data, ep.ptyID)
		}
		h.forwardToDaemon(slot, f.data, f.messageType)
	}) {
		return
	}
}

func (h *Hub) readDaemon(slot *daemonSlot, ep *endpoint) {
	readLoop(ep, func(f frame) {
		h.routeFromDaemon(slot, f)
	})
}

func (h *Hub) forwardToDaemon(slot *daemonSlot, data []byte, messageType int) {
	slot.mu.Lock()
	daemon := slot.daemon
	slot.mu.Unlock()
	if daemon == nil {
		return
	}
	if !daemon.trySend(frame{messageType: messageType, data: data}) {
		slog.Debug("pty websocket daemon send dropped", "daemon_id", daemon.daemonID)
	}
}

func (h *Hub) routeFromDaemon(slot *daemonSlot, f frame) {
	ptyID := ""
	payload := f.data
	if f.messageType == websocket.BinaryMessage {
		id, rest, ok := protocol.DecodePTYFrame(f.data)
		if !ok {
			return
		}
		ptyID = id.String()
		payload = rest
	} else {
		var msg protocol.PTYControl
		if json.Unmarshal(f.data, &msg) == nil {
			ptyID = msg.ID
		}
		if ptyID == "" {
			ptyID = "default"
		}
	}
	slot.mu.Lock()
	list := slot.browsers[ptyID]
	if len(list) == 0 {
		for key, candidate := range slot.browsers {
			if protocol.ParsePTYID(key) == protocol.ParsePTYID(ptyID) {
				list = candidate
				break
			}
		}
	}
	slot.mu.Unlock()
	for _, browser := range list {
		if !browser.trySend(frame{messageType: f.messageType, data: payload}) {
			slog.Debug("pty websocket browser send dropped", "pty_id", ptyID)
		}
	}
}

func readLoop(ep *endpoint, handle func(frame)) bool {
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
				slog.Debug("pty websocket read error", "error", err, "daemon_id", ep.daemonID)
			}
			return false
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		handle(frame{messageType: messageType, data: data})
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

func stampControl(data []byte, id string) []byte {
	var msg protocol.PTYControl
	if err := json.Unmarshal(data, &msg); err != nil {
		return data
	}
	if msg.ID == "" {
		msg.ID = id
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return data
	}
	return out
}

func mustControl(msg protocol.PTYControl) []byte {
	b, err := json.Marshal(msg)
	if err != nil {
		return []byte(`{"type":"pty.error","error":"marshal failed"}`)
	}
	return b
}
