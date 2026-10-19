package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orchestrator"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// orchestratorGuard returns a refusal message when an orchestrated project's
// status/assignee write is not the orchestrator's own.
func (h *Handler) orchestratorGuard(r *http.Request, callerType, userID string, prev db.Issue, statusSet bool, status pgtype.Text, assigneeChange bool) string {
	if h.Orchestrator == nil {
		return ""
	}
	to := ""
	if statusSet && status.Valid && status.String != prev.Status {
		to = status.String
	}
	return h.Orchestrator.CheckWrite(orchestrator.GuardInput{
		ProjectID: uuidToString(prev.ProjectID), CallerType: callerType, UserID: userID,
		FromStatus: prev.Status, ToStatus: to, AssigneeChange: assigneeChange,
		Override: r.Header.Get("X-Orchestrator-Override") == "1",
	})
}
