package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)


func TestWorkspaceWakeupSummariesScopeAndBounds(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup summary")
	agent := dbfx.Agent(t, "summary target", testRuntimeID)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	var first db.IssueWakeup
	for i := 0; i < 5; i++ {
		in := service.WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "PRIVATE PROMPT"}
		if i == 0 {
			in.Kind = "at"
			in.EventTypes = nil
			in.AfterSeconds = 3600
		}
		w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), "", in)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = w
		}
	}
	read := func(req *http.Request, want int) []db.ListWorkspaceWakeupSummaryRowsRow {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.ListWorkspaceWakeupSummaries(rec, req)
		if rec.Code != want {
			t.Fatalf("summary %d: %s", rec.Code, rec.Body.String())
		}
		if want != 200 {
			return nil
		}
		if strings.Contains(rec.Body.String(), "PRIVATE PROMPT") || strings.Contains(rec.Body.String(), "instruction") {
			t.Fatal("prompt leaked to summary")
		}
		var rows []db.ListWorkspaceWakeupSummaryRowsRow
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read(newRequest("GET", "/api/issue-wakeup-summaries", nil), 200)
	if len(rows) != 3 || rows[0].ID != first.ID || rows[0].ActiveCount != 5 || int64(rows[0].EventCount.Float64) != 4 {
		t.Fatalf("wrong bounded summary: %+v", rows)
	}
	outsider := dbfx.User(t, "summary outsider", "summary-outsider@multica.test")
	req := newRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", outsider)
	read(req, 404)
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	if got := read(req, 200); len(got) != 3 || got[0].ActiveCount != 5 {
		t.Fatal("shared issue rules disappeared from summary")
	}
	// Rule visibility follows the shared issue; all read surfaces consistently
	// redact private source-agent and source-run references.
	sourceRun := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed"})
	dbfx.Exec(t, "UPDATE issue_wakeup SET filter_agent_id=$2,filter_task_id=$3 WHERE id=$1", first.ID, agent, sourceRun)
	for _, row := range read(req, 200) {
		if row.FilterAgentName.Valid || ifaceString(row.FilterTaskID) != "" {
			t.Fatal("private source exposed in summary")
		}
	}
	detail := httptest.NewRecorder()
	testHandler.ListIssueWakeups(detail, withURLParam(req, "id", issue))
	var details []db.ListIssueWakeupsRow
	if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &details) != nil || len(details) != 5 {
		t.Fatalf("detail response: %d %s", detail.Code, detail.Body.String())
	}
	for _, row := range details {
		if row.FilterAgentName.Valid || ifaceString(row.FilterAgentID) != "" || ifaceString(row.FilterTaskID) != "" {
			t.Fatal("private source exposed in detail")
		}
		if row.Instruction != "PRIVATE PROMPT" {
			t.Fatal("shared issue instruction missing")
		}
	}
	other := dbfx.Workspace(t, "other summary", "other-summary")
	dbfx.Member(t, other, testUserID, "owner")
	req = newRequest("GET", "/", nil)
	req.Header.Set("X-Workspace-ID", other)
	if got := read(req, 200); len(got) != 0 {
		t.Fatal("cross-workspace summary exposed")
	}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), first.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	rows = read(newRequest("GET", "/", nil), 200)
	if len(rows) != 3 || rows[0].ActiveCount != 4 || int64(rows[0].EventCount.Float64) != 4 {
		t.Fatalf("disabled entry retained: %+v", rows)
	}
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", issue)
	if got := read(newRequest("GET", "/", nil), 200); len(got) != 0 {
		t.Fatal("closed issue summary retained")
	}
}

func TestWakeupDeferredRunSnapshotPreservesOrigin(t *testing.T) {
	issue := dbfx.Issue(t, "deferred wakeup snapshot")
	agent := dbfx.Agent(t, "deferred wakeup", testRuntimeID)
	for _, contextJSON := range []string{`{}`, `{"wakeup_id":"01900000-0000-7000-8000-000000000001"}`} {
		dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "deferred", "runtime_id": testRuntimeID, "context": contextJSON})
	}
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceAgentTaskSnapshot(rec, newRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var tasks []AgentTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	var found int
	for _, task := range tasks {
		if task.IssueID == issue {
			found++
			if task.WakeupID != "01900000-0000-7000-8000-000000000001" || task.Status != "deferred" {
				t.Fatalf("bad wakeup origin: %+v", task)
			}
		}
	}
	if found != 1 {
		t.Fatalf("wrong deferred snapshot count: %d", found)
	}
}




func TestIssueWakeupInstructionAPI(t *testing.T) {
	issue := dbfx.Issue(t, "edit prompt api")
	agent := dbfx.Agent(t, "edit prompt", testRuntimeID)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	rule, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), "", service.WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "old"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		body map[string]any
		want int
	}{
		{uuidToString(rule.ID), map[string]any{"instruction": "new", "expected_instruction": "old", "revision": rule.Revision}, 204},
		{uuidToString(rule.ID), map[string]any{"instruction": "overwritten", "expected_instruction": "old", "revision": rule.Revision}, 409},
		{uuidToString(rule.ID), map[string]any{"instruction": " ", "expected_instruction": "new", "revision": rule.Revision}, 400},
		{uuidToString(rule.ID), map[string]any{"instruction": "new", "expected_instruction": "new", "revision": rule.Revision, "enabled": true}, 400},
		{"not-a-uuid", map[string]any{"instruction": "new", "expected_instruction": "new", "revision": rule.Revision}, 400},
	} {
		req := withURLParams(newRequest("PATCH", "/", tc.body), "id", issue, "wakeupID", tc.id)
		testutil.Call(t, testHandler.EditIssueWakeupInstruction, req).Want(tc.want)
	}
}
