package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func startClaimFixture(t *testing.T, status string) (string, string, time.Time) {
	t.Helper()
	if testHandler == nil {
		t.Fatal("PostgreSQL is required for start claim tests")
	}
	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	issueID := dbfx.Issue(t, "start claim ownership")
	generation := time.Date(2026, 9, 17, 9, 0, 0, 123456000, time.UTC)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": testRuntimeID,
		"status": status, "dispatched_at": generation,
	})
	return taskID, testRuntimeID, generation
}

func startClaimRequest(taskID, runtimeID string, generation time.Time) *http.Request {
	return withURLParam(newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", map[string]string{
		"runtime_id": runtimeID, "dispatched_at": generation.Format(time.RFC3339Nano),
	}, testWorkspaceID, "start-claim-test"), "taskId", taskID)
}






