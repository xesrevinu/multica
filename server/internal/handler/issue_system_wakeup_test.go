package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

)

func createStagedChild(t *testing.T, parentID string, stage int, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "staged child " + time.Now().Format(time.RFC3339Nano), "status": status,
		"parent_issue_id": parentID, "stage": stage,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create staged child: %d %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupChildDoneIssue(child.ID) })
	return child
}

func listSystemWakeupsFor(t *testing.T, issueID string) []systemWakeupResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListIssueSystemWakeups(w, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/system-wakeups", nil), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("list system wakeups: %d %s", w.Code, w.Body.String())
	}
	var rules []systemWakeupResponse
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func putChildDoneRule(t *testing.T, issueID string, body map[string]any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParams(newRequest("PUT", "/api/issues/"+issueID+"/system-wakeups/child_done", body), "id", issueID, "rule", "child_done")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	testHandler.UpdateIssueSystemWakeup(w, req)
	return w
}



// Turning the rule off for one issue stops it there; an instruction set on the
// issue replaces the default in the run the assignee reads.

// The workspace default applies to every rule nobody changed on its issue;
// owners and admins set it.
