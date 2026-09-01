package handler

import (
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func (h *Handler) DaemonPTYWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.PTYHub == nil {
		writeError(w, http.StatusServiceUnavailable, "pty websocket unavailable")
		return
	}
	// Local daemons authenticate with a user PAT, which does not put daemon_id
	// in the auth context. Accept the query param the daemon also sends.
	daemonID := middleware.DaemonIDFromContext(r.Context())
	if daemonID == "" {
		daemonID = strings.TrimSpace(r.URL.Query().Get("daemon_id"))
	}
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
