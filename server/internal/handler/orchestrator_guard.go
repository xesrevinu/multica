package handler

import (
	"fmt"
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

// orchestratorMetaGuard returns a refusal message when an agent's metadata
// write or delete on an orchestrated issue would forge or rewind engine
// state (see orchestrator.CheckAgentMetaWrite). Members and the engine pass.
func (h *Handler) orchestratorMetaGuard(r *http.Request, userID string, issue db.Issue, key string, value []byte, del bool) string {
	if h.Orchestrator == nil {
		return ""
	}
	if callerType, _ := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID)); callerType != "agent" {
		return ""
	}
	cur := map[string]string{}
	for k, v := range parseIssueMetadata(issue.Metadata) {
		cur[k] = fmt.Sprint(v)
	}
	return h.Orchestrator.CheckAgentMetaWrite(orchestrator.MetaWrite{
		ProjectID: uuidToString(issue.ProjectID), Status: issue.Status, Key: key, Value: string(value), Current: cur, Delete: del,
	})
}

// refuseOrchestratorLabel writes 403 orchestrator_owned and returns true when
// a label change on an orchestrated issue is refused (orchestrator.CheckLabel):
// an agent adding/removing orchestrator-pause or removing an acceptance gate,
// or the engine's member adding orchestrator-pause.
func (h *Handler) refuseOrchestratorLabel(w http.ResponseWriter, r *http.Request, userID string, issue db.Issue, label string, attach bool) bool {
	if h.Orchestrator == nil {
		return false
	}
	callerType, _ := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	if msg := h.Orchestrator.CheckLabel(orchestrator.LabelWrite{
		ProjectID: uuidToString(issue.ProjectID), CallerType: callerType, UserID: userID, Label: label, Attach: attach,
	}); msg != "" {
		writeErrorCode(w, http.StatusForbidden, orchestrator.ErrCodeOwned, msg)
		return true
	}
	return false
}

// orchestratorCreateLabelsGuard returns a refusal message when labels supplied
// at issue creation would be refused if attached afterwards (CheckLabel).
func (h *Handler) orchestratorCreateLabelsGuard(r *http.Request, callerType, userID string, workspaceID, projectID pgtype.UUID, labelIDs []pgtype.UUID) string {
	if h.Orchestrator == nil || !projectID.Valid || len(labelIDs) == 0 || h.Orchestrator.Team(uuidToString(projectID)) == nil {
		return ""
	}
	for _, id := range labelIDs {
		label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			continue // unknown labels are rejected by the create itself
		}
		if msg := h.Orchestrator.CheckLabel(orchestrator.LabelWrite{
			ProjectID: uuidToString(projectID), CallerType: callerType, UserID: userID, Label: label.Name, Attach: true,
		}); msg != "" {
			return msg
		}
	}
	return ""
}
