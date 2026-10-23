package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type supplementFixture struct {
	runtimeID string
	agentID   string
	issueID   string
	taskID    string
	triggerID string
}






func newSupplementFixture(t *testing.T, provider, status string, negotiated bool) supplementFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "supplement-"+provider, testutil.Cols{"provider": provider})
	agentID := dbfx.Agent(t, "Supplement "+provider, runtimeID)
	issueID := dbfx.Issue(t, "supplement "+provider)
	triggerID := dbfx.Comment(t, issueID, "original objective")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":           issueID,
		"runtime_id":         runtimeID,
		"trigger_comment_id": triggerID,
		"status":             status,
		"started_at":         testutil.Raw("CASE WHEN '" + status + "' = 'running' THEN now() ELSE NULL END"),
	})
	if negotiated {
		dbfx.Exec(t, `
			INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability)
			VALUES ($1, $2, $3, $4)
		`, taskID, testWorkspaceID, issueID, protocol.DaemonCapabilityTaskSupplementV1)
		dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id = $1`, taskID)
	}
	dbfx.Cleanup(t, `DELETE FROM task_supplement WHERE task_id = $1`, taskID)
	return supplementFixture{runtimeID: runtimeID, agentID: agentID, issueID: issueID, taskID: taskID, triggerID: triggerID}
}

func supplementRequest(t *testing.T, fixture supplementFixture, requestID, content string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+fixture.issueID+"/tasks/"+fixture.taskID+"/supplements", map[string]any{
		"client_request_id": requestID,
		"content":           content,
	})
	return testutil.Call(t, testHandler.CreateTaskSupplement,
		withURLParams(req, "id", fixture.issueID, "taskId", fixture.taskID),
	)
}








func assertTaskSupplementTerminalRaceCreatesNothing(t *testing.T, terminalStatus string) {
	t.Helper()
	fixture := newSupplementFixture(t, "codex", "running", true)
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(context.Background(), `SELECT id FROM agent_task_queue WHERE id = $1 FOR UPDATE`, fixture.taskID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, createErr := testHandler.Queries.CreateTaskSupplement(context.Background(), db.CreateTaskSupplementParams{
			TaskID: parseUUID(fixture.taskID), IssueID: nullString(parseUUID(fixture.issueID)), WorkspaceID: parseUUID(testWorkspaceID),
			AuthorID: parseUUID(testUserID), Content: "must not orphan",
			ClientRequestID: parseUUID("0199a4e8-22ce-7b01-bba5-444444444444"),
		})
		result <- createErr
	}()
	if _, err := tx.ExecContext(context.Background(), `UPDATE agent_task_queue SET status = $2, completed_at = now() WHERE id = $1`, fixture.taskID, terminalStatus); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("race create error = %v, want no rows", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("race create remained blocked")
	}
	var comments, supplements int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content = 'must not orphan'`, fixture.issueID).Scan(&comments)
	dbfx.QueryRow(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, fixture.taskID).Scan(&supplements)
	if comments != 0 || supplements != 0 {
		t.Fatalf("terminal race left comments=%d supplements=%d", comments, supplements)
	}
}




