package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/realtime"
)

const ptyBrowserAuthTimeout = 10 * time.Second

// BrowserPTYWebSocket upgrades a browser connection and relays it to the
// daemon PTY socket identified by daemon_id. Auth matches the realtime WS:
// cookie first, otherwise the first JSON frame is `{type:"auth",payload:{token}}`.
func (h *Handler) BrowserPTYWebSocket(w http.ResponseWriter, r *http.Request, mc realtime.MembershipChecker, pr realtime.PATResolver, resolveSlug realtime.SlugResolver) {
	if h.PTYHub == nil {
		writeError(w, http.StatusServiceUnavailable, "pty websocket unavailable")
		return
	}

	daemonID := strings.TrimSpace(r.URL.Query().Get("daemon_id"))
	if daemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id required")
		return
	}

	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		if slug := strings.TrimSpace(r.URL.Query().Get("workspace_slug")); slug != "" && resolveSlug != nil {
			resolved, err := resolveSlug(r.Context(), slug)
			if err != nil {
				http.Error(w, `{"error":"workspace not found"}`, http.StatusNotFound)
				return
			}
			workspaceID = resolved
		}
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id or workspace_slug required")
		return
	}

	userID := ""
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		uid, errMsg := ptyAuthenticateToken(cookie.Value, pr, r.Context())
		if errMsg != "" {
			status := http.StatusUnauthorized
			if errMsg == `{"error":"account disabled"}` {
				status = http.StatusForbidden
			}
			http.Error(w, errMsg, status)
			return
		}
		if mc != nil && !mc.IsMember(r.Context(), uid, workspaceID) {
			http.Error(w, `{"error":"not a member of this workspace"}`, http.StatusForbidden)
			return
		}
		userID = uid
	}

	conn, err := h.PTYHub.Upgrade(w, r)
	if err != nil {
		slog.Error("pty browser websocket upgrade failed", "error", err)
		return
	}

	if userID == "" {
		token, errMsg, closed := ptyFirstMessageAuth(conn)
		if closed {
			return
		}
		if errMsg != "" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(errMsg))
			conn.Close()
			return
		}
		uid, errMsg := ptyAuthenticateToken(token, pr, r.Context())
		if errMsg != "" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(errMsg))
			conn.Close()
			return
		}
		if mc != nil && !mc.IsMember(r.Context(), uid, workspaceID) {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"not a member of this workspace"}`))
			conn.Close()
			return
		}
		userID = uid
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth_ack"}`))
	}

	ptyID := strings.TrimSpace(r.URL.Query().Get("pty_id"))
	if ptyID == "" {
		ptyID = "default"
	}
	slog.Info("pty browser websocket connected",
		"user_id", userID,
		"workspace_id", workspaceID,
		"daemon_id", daemonID,
		"pty_id", ptyID,
	)
	h.PTYHub.ServeBrowser(daemonID, ptyID, conn)
}

func ptyAuthenticateToken(tokenStr string, pr realtime.PATResolver, ctx context.Context) (string, string) {
	if strings.HasPrefix(tokenStr, "mul_") {
		if pr == nil {
			return "", `{"error":"invalid token"}`
		}
		uid, ok := pr.ResolveToken(ctx, tokenStr)
		if !ok {
			return "", `{"error":"invalid token"}`
		}
		if auth.IsTemporarilyDisabledUserID(uid) {
			return "", `{"error":"account disabled"}`
		}
		return uid, ""
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return "", `{"error":"invalid token"}`
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", `{"error":"invalid claims"}`
	}
	uid, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(uid) == "" {
		return "", `{"error":"invalid claims"}`
	}
	email, _ := claims["email"].(string)
	if auth.IsTemporarilyDisabledUser(uid, email) {
		return "", `{"error":"account disabled"}`
	}
	return uid, ""
}

func ptyFirstMessageAuth(conn *websocket.Conn) (token, errMsg string, closed bool) {
	conn.SetReadDeadline(time.Now().Add(ptyBrowserAuthTimeout))
	defer conn.SetReadDeadline(time.Time{})

	_, raw, err := conn.ReadMessage()
	if err != nil {
		if errors.Is(err, websocket.ErrReadLimit) {
			conn.Close()
			return "", "", true
		}
		return "", `{"error":"auth timeout or read error"}`, false
	}

	var msg struct {
		Type    string `json:"type"`
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "auth" || msg.Payload.Token == "" {
		return "", `{"error":"expected auth message as first frame"}`, false
	}
	return msg.Payload.Token, "", false
}
