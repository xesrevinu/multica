package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const terminalWorkspaceStateMaxBytes = 1 << 20

type TerminalWorkspaceStateResponse struct {
	State     json.RawMessage `json:"state"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

func (h *Handler) GetTerminalWorkspaceState(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	row, err := h.Queries.GetTerminalWorkspaceState(r.Context(), db.GetTerminalWorkspaceStateParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, TerminalWorkspaceStateResponse{
				State: json.RawMessage(`{"sessions":[],"activeSessionId":null,"sidebarCollapsed":false}`),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load terminal state")
		return
	}
	writeJSON(w, http.StatusOK, TerminalWorkspaceStateResponse{
		State:     json.RawMessage(row.State),
		UpdatedAt: timestampToString(row.UpdatedAt),
	})
}

type PutTerminalWorkspaceStateRequest struct {
	State json.RawMessage `json:"state"`
}

func (h *Handler) PutTerminalWorkspaceState(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	var req PutTerminalWorkspaceStateRequest
	r.Body = http.MaxBytesReader(w, r.Body, terminalWorkspaceStateMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.State) == 0 || req.State[0] != '{' {
		writeError(w, http.StatusBadRequest, "state must be a JSON object")
		return
	}
	row, err := h.Queries.UpsertTerminalWorkspaceState(r.Context(), db.UpsertTerminalWorkspaceStateParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		State:       req.State,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save terminal state")
		return
	}
	writeJSON(w, http.StatusOK, TerminalWorkspaceStateResponse{
		State:     json.RawMessage(row.State),
		UpdatedAt: timestampToString(row.UpdatedAt),
	})
}
