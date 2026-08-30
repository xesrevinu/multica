package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func (h *Handler) DaemonPTYWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.PTYHub == nil {
		writeError(w, http.StatusServiceUnavailable, "pty websocket unavailable")
		return
	}
	daemonID := middleware.DaemonIDFromContext(r.Context())
	if daemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon identity required")
		return
	}
	conn, err := h.PTYHub.Upgrade(w, r)
	if err != nil {
		return
	}
	h.PTYHub.ServeDaemon(daemonID, conn)
}
