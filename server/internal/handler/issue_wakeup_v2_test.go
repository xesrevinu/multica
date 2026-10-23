package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// A status write outside the HTTP handlers still records the child's change
// in its own transaction; the sweep turns it into the parent's wake exactly
// once.

// The request path processes its own change; a later sweep finds nothing.

// A rule created while the workspace default is off stays off until the
// issue sets its own.

type workspaceWakeupRow struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	Source          string  `json:"source"`
	Rule            *string `json:"rule"`
	SystemStage     *int    `json:"system_stage"`
	SystemRemaining *int    `json:"system_remaining"`
	Revision        *int64  `json:"revision"`
	Enabled         bool    `json:"enabled"`
	PausedReason    *string `json:"paused_reason"`
	Runs7d          int     `json:"runs_7d"`
	CanManage       bool    `json:"can_manage"`
	Condition       any     `json:"condition"`
}

func listWorkspaceWakeupRows(t *testing.T, query string) ([]workspaceWakeupRow, map[string]int) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceWakeups(rec, newRequest("GET", "/api/issue-wakeups?"+query, nil))
	if rec.Code != 200 {
		t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		Items  []workspaceWakeupRow `json:"items"`
		Counts map[string]int       `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Items, out.Counts
}


func TestIssueWakeupManagementEndpoints(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup management")
	agent := dbfx.Agent(t, "management target", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), "", service.WakeupInput{AgentID: agent, Kind: "every", IntervalSeconds: 3600, Instruction: "check staging"})
	if err != nil {
		t.Fatal(err)
	}
	id := uuidToString(w.ID)
	call := func(handler http.HandlerFunc, method string, body any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := withURLParams(newRequest(method, "/", body), "id", issue, "wakeupID", id)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	if rec := call(testHandler.TriggerIssueWakeup, "POST", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("wake now: %d %s", rec.Code, rec.Body.String())
	}
	var run string
	dbfx.QueryRow(t, "SELECT id::text FROM agent_task_queue WHERE context->>'wakeup_id'=$1", id).Scan(&run)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", run)
	// A member cannot check in for the run.
	if rec := call(testHandler.CheckInIssueWakeup, "POST", map[string]any{"note": "fine"}, map[string]string{"X-Task-ID": run}); rec.Code != http.StatusForbidden {
		t.Fatalf("member check-in: %d %s", rec.Code, rec.Body.String())
	}
	agentHeaders := map[string]string{"X-Task-ID": run, "X-Agent-ID": agent, "X-Actor-Source": "task_token"}
	if rec := call(testHandler.CheckInIssueWakeup, "POST", map[string]any{"note": "Nothing changed"}, agentHeaders); rec.Code != http.StatusNoContent {
		t.Fatalf("agent check-in: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(testHandler.ListIssueWakeupRuns, "GET", nil, nil)
	var runs []wakeupRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil || rec.Code != 200 {
		t.Fatalf("runs %d: %s", rec.Code, rec.Body.String())
	}
	if len(runs) != 1 || runs[0].ID != run || runs[0].CheckinNote != "Nothing changed" || len(runs[0].Triggers) != 1 || runs[0].Triggers[0] != "wakeup.manual" {
		t.Fatalf("runs: %+v", runs)
	}
	if rec := call(testHandler.DeleteIssueWakeup, "DELETE", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(testHandler.ListIssueWakeupRuns, "GET", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("runs of a deleted rule: %d", rec.Code)
	}
}
