package daemonpty

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestHubRelaysTextAndBinary(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hub.Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		switch r.URL.Path {
		case "/daemon":
			hub.ServeDaemon(r.URL.Query().Get("id"), conn)
		case "/browser":
			hub.ServeBrowser(r.URL.Query().Get("id"), r.URL.Query().Get("pty"), conn)
		default:
			conn.Close()
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	daemon, _, err := websocket.DefaultDialer.Dial(wsBase+"/daemon?id=d1", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemon.Close()

	// Give the hub a moment to register the daemon before the browser attaches.
	time.Sleep(20 * time.Millisecond)

	browser, _, err := websocket.DefaultDialer.Dial(wsBase+"/browser?id=d1&pty=p1", nil)
	if err != nil {
		t.Fatalf("dial browser: %v", err)
	}
	defer browser.Close()

	open := protocol.PTYControl{Type: protocol.PTYTypeOpen, Cols: 80, Rows: 24, Argv: []string{"zsh", "-l"}}
	raw, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := browser.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("browser write open: %v", err)
	}

	daemon.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err := daemon.ReadMessage()
	if err != nil {
		t.Fatalf("daemon read open: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Fatalf("daemon got message type %d, want text", msgType)
	}
	var got protocol.PTYControl
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != protocol.PTYTypeOpen || got.Cols != 80 || got.Rows != 24 || got.ID != "p1" {
		t.Fatalf("got %+v", got)
	}

	frame := protocol.EncodePTYFrame(protocol.ParsePTYID("p1"), []byte("hello-pty"))
	if err := daemon.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("daemon write bytes: %v", err)
	}
	browser.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err = browser.ReadMessage()
	if err != nil {
		t.Fatalf("browser read bytes: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Fatalf("browser got message type %d, want binary", msgType)
	}
	if string(payload) != "hello-pty" {
		t.Fatalf("browser got %q", payload)
	}
}

func TestHubIsolatesTwoBrowsers(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hub.Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		switch r.URL.Path {
		case "/daemon":
			hub.ServeDaemon(r.URL.Query().Get("id"), conn)
		case "/browser":
			hub.ServeBrowser(r.URL.Query().Get("id"), r.URL.Query().Get("pty"), conn)
		default:
			conn.Close()
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	daemon, _, err := websocket.DefaultDialer.Dial(wsBase+"/daemon?id=d1", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemon.Close()
	time.Sleep(20 * time.Millisecond)

	a, _, err := websocket.DefaultDialer.Dial(wsBase+"/browser?id=d1&pty=pane-a", nil)
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.Close()
	b, _, err := websocket.DefaultDialer.Dial(wsBase+"/browser?id=d1&pty=pane-b", nil)
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.Close()

	if err := a.WriteMessage(websocket.BinaryMessage, []byte("from-a")); err != nil {
		t.Fatalf("a write: %v", err)
	}
	daemon.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err := daemon.ReadMessage()
	if err != nil {
		t.Fatalf("daemon read a: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Fatalf("want binary, got %d", msgType)
	}
	id, rest, ok := protocol.DecodePTYFrame(payload)
	if !ok || id != protocol.ParsePTYID("pane-a") || string(rest) != "from-a" {
		t.Fatalf("daemon got id=%s payload=%q", id, rest)
	}

	if err := daemon.WriteMessage(websocket.BinaryMessage, protocol.EncodePTYFrame(protocol.ParsePTYID("pane-b"), []byte("only-b"))); err != nil {
		t.Fatalf("daemon write b: %v", err)
	}
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err = b.ReadMessage()
	if err != nil {
		t.Fatalf("b read: %v", err)
	}
	if msgType != websocket.BinaryMessage || string(payload) != "only-b" {
		t.Fatalf("b got type=%d payload=%q", msgType, payload)
	}
	a.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, extra, err := a.ReadMessage(); err == nil {
		t.Fatalf("pane a received leaked bytes %q", extra)
	}
}

func TestHubBrowserWithoutDaemon(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.ServeBrowser("missing", "p1", conn)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Fatalf("got type %d", msgType)
	}
	var got protocol.PTYControl
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != protocol.PTYTypeError || got.Error == "" {
		t.Fatalf("got %+v", got)
	}
}
