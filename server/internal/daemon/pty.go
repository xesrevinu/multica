package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	ptyWriteWait  = 10 * time.Second
	ptyPongWait   = 60 * time.Second
	ptyPingPeriod = 54 * time.Second
	ptyReadLimit  = 1 << 20
	ptyMaxBackoff = 30 * time.Second
	ptyReplayCap  = 128 * 1024
)

var errPTYUnsupported = errors.New("pty is not supported on this platform")

type ptyOutbound struct {
	messageType int
	data        []byte
}

type ptySession struct {
	mu     sync.Mutex
	file   io.ReadWriteCloser
	resize func(cols, rows uint16) error
	kill   func()
	replay []byte
}

func (s *ptySession) write(p []byte) error {
	s.mu.Lock()
	file := s.file
	s.mu.Unlock()
	if file == nil {
		return io.ErrClosedPipe
	}
	_, err := file.Write(p)
	return err
}

func (s *ptySession) setSize(cols, rows uint16) error {
	s.mu.Lock()
	resize := s.resize
	s.mu.Unlock()
	if resize == nil {
		return nil
	}
	return resize(cols, rows)
}

func (s *ptySession) appendReplay(p []byte) {
	s.mu.Lock()
	s.replay = append(s.replay, p...)
	if len(s.replay) > ptyReplayCap {
		s.replay = append([]byte(nil), s.replay[len(s.replay)-ptyReplayCap:]...)
	}
	s.mu.Unlock()
}

func (s *ptySession) snapshotReplay() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, len(s.replay))
	copy(out, s.replay)
	return out
}

func (s *ptySession) close() {
	s.mu.Lock()
	file := s.file
	kill := s.kill
	s.file = nil
	s.resize = nil
	s.kill = nil
	s.mu.Unlock()
	if kill != nil {
		kill()
	}
	if file != nil {
		_ = file.Close()
	}
}

func (d *Daemon) ptyLoop(ctx context.Context) {
	backoff := time.Second
	for {
		connectedFor, err := d.runPTYConnection(ctx)
		if ctx.Err() != nil {
			return
		}
		if connectedFor >= 10*time.Second {
			backoff = time.Second
		}
		if err != nil {
			d.logger.Debug("pty websocket unavailable", "error", err, "retry_in", backoff)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < ptyMaxBackoff {
			backoff *= 2
			if backoff > ptyMaxBackoff {
				backoff = ptyMaxBackoff
			}
		}
	}
}

func (d *Daemon) runPTYConnection(ctx context.Context) (time.Duration, error) {
	wsURL, err := ptyWebSocketURL(d.cfg.ServerBaseURL)
	if err != nil {
		return 0, err
	}
	if d.cfg.DaemonID != "" {
		u, parseErr := url.Parse(wsURL)
		if parseErr == nil {
			q := u.Query()
			q.Set("daemon_id", d.cfg.DaemonID)
			u.RawQuery = q.Encode()
			wsURL = u.String()
		}
	}

	headers := http.Header{}
	if token := d.client.Token(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	if d.client.platform != "" {
		headers.Set("X-Client-Platform", d.client.platform)
	}
	if d.client.version != "" {
		headers.Set("X-Client-Version", d.client.version)
	}
	if d.client.os != "" {
		headers.Set("X-Client-OS", d.client.os)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return 0, err
	}
	connectedAt := time.Now()
	defer conn.Close()

	d.logger.Info("pty websocket connected")
	d.servePTY(ctx, conn)
	return time.Since(connectedAt), nil
}

func ptyWebSocketURL(baseURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid daemon server URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("daemon server URL must use http, https, ws, or wss")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/daemon/pty/ws"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (d *Daemon) servePTY(ctx context.Context, conn *websocket.Conn) {
	writes := make(chan ptyOutbound, 64)
	var sessionMu sync.Mutex
	sessions := make(map[string]*ptySession)

	closeOne := func(id string) {
		sessionMu.Lock()
		s := sessions[id]
		delete(sessions, id)
		sessionMu.Unlock()
		if s != nil {
			s.close()
		}
	}
	closeAll := func() {
		sessionMu.Lock()
		open := sessions
		sessions = make(map[string]*ptySession)
		sessionMu.Unlock()
		for _, s := range open {
			s.close()
		}
	}
	lookup := func(id string) *ptySession {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		return sessions[id]
	}
	defer closeAll()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(ptyPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case item, ok := <-writes:
				conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
				if !ok {
					_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(item.messageType, item.data); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var sendMu sync.Mutex
	sendClosed := false
	closeWrites := func() {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendClosed {
			return
		}
		sendClosed = true
		close(writes)
	}
	send := func(item ptyOutbound) {
		sendMu.Lock()
		defer sendMu.Unlock()
		if sendClosed {
			return
		}
		select {
		case writes <- item:
		default:
			d.logger.Debug("pty websocket write buffer full")
		}
	}
	sendJSON := func(msg protocol.PTYControl) {
		raw, err := json.Marshal(msg)
		if err != nil {
			return
		}
		send(ptyOutbound{messageType: websocket.TextMessage, data: raw})
	}

	conn.SetReadLimit(ptyReadLimit)
	conn.SetReadDeadline(time.Now().Add(ptyPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(ptyPongWait))
		return nil
	})

	for {
		if ctx.Err() != nil {
			closeWrites()
			<-writerDone
			return
		}
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			closeWrites()
			<-writerDone
			return
		}
		conn.SetReadDeadline(time.Now().Add(ptyPongWait))

		switch messageType {
		case websocket.BinaryMessage:
			uid, payload, ok := protocol.DecodePTYFrame(data)
			if !ok {
				continue
			}
			s := lookup(uid.String())
			if s == nil {
				sessionMu.Lock()
				for key, candidate := range sessions {
					if protocol.ParsePTYID(key) == uid {
						s = candidate
						break
					}
				}
				sessionMu.Unlock()
			}
			if s == nil {
				continue
			}
			if err := s.write(payload); err != nil {
				d.logger.Debug("pty stdin write failed", "error", err)
			}
		case websocket.TextMessage:
			var msg protocol.PTYControl
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			id := msg.ID
			if id == "" {
				id = "default"
			}
			switch msg.Type {
			case protocol.PTYTypeOpen:
				if existing := lookup(id); existing != nil {
					if msg.Cols > 0 && msg.Rows > 0 {
						_ = existing.setSize(msg.Cols, msg.Rows)
					}
					sendJSON(protocol.PTYControl{Type: protocol.PTYTypeOpened, ID: id})
					if snap := existing.snapshotReplay(); len(snap) > 0 {
						send(ptyOutbound{
							messageType: websocket.BinaryMessage,
							data:        protocol.EncodePTYFrame(protocol.ParsePTYID(id), snap),
						})
					}
					continue
				}
				opened, err := startPTY(msg)
				if err != nil {
					sendJSON(protocol.PTYControl{Type: protocol.PTYTypeError, ID: id, Error: err.Error()})
					continue
				}
				sessionMu.Lock()
				sessions[id] = opened
				sessionMu.Unlock()
				sendJSON(protocol.PTYControl{Type: protocol.PTYTypeOpened, ID: id})
				go d.copyPTYOutput(id, opened, send, sendJSON)
			case protocol.PTYTypeResize:
				s := lookup(id)
				if s == nil {
					continue
				}
				if err := s.setSize(msg.Cols, msg.Rows); err != nil {
					d.logger.Debug("pty resize failed", "error", err)
				}
			case protocol.PTYTypeClose:
				closeOne(id)
			}
		}
	}
}

func (d *Daemon) copyPTYOutput(id string, session *ptySession, send func(ptyOutbound), sendJSON func(protocol.PTYControl)) {
	uid := protocol.ParsePTYID(id)
	buf := make([]byte, 32*1024)
	for {
		session.mu.Lock()
		file := session.file
		session.mu.Unlock()
		if file == nil {
			return
		}
		n, err := file.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			session.appendReplay(chunk)
			send(ptyOutbound{messageType: websocket.BinaryMessage, data: protocol.EncodePTYFrame(uid, chunk)})
		}
		if err != nil {
			code := 0
			sendJSON(protocol.PTYControl{Type: protocol.PTYTypeExit, ID: id, Code: &code})
			session.close()
			return
		}
	}
}

func defaultPTYArgv() []string {
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return []string{"/bin/zsh", "-l"}
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return []string{shell, "-l"}
	}
	return []string{"/bin/sh", "-l"}
}

func resolvePTYCwd(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			if home = os.Getenv("HOME"); home == "" {
				return "", fmt.Errorf("cwd not set and $HOME is empty")
			}
		}
		return home, nil
	}
	return cwd, nil
}
