package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func claimWorkerReplyRun(t *testing.T, runtimeID string) *AgentTaskResponse {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
	return response.Task
}

func completeWorkerReplyRun(t *testing.T, taskID string) {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": "Processed the inputs delivered to this run"}, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK)
}

// A worker progress comment wakes the leader; its final reply must also reach a
// leader run even if the first wake was claimed before the reply arrived.

// Unplanned replies must be in the completing run's thread and timestamp window:
// otherwise a passing negative case could merely be the SQL scope excluding it.

// If completion commits while registration waits for the task row lock, the
// caller must see a miss and enqueue fresh, not attach an obligation to a run
// whose completion snapshot can no longer include it.

// Intercept only the new obligation write; all routing, comment persistence,
// completion, and successor enqueue still use their real database paths.
type workerReplyRegistrationDB struct {
	db.DBTX
	before func()
	err    error
	calls  int
}

func (d *workerReplyRegistrationDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "-- name: RegisterPlannedCommentForActiveTask :one") {
		d.calls++
		if d.before != nil {
			d.before()
		}
		if d.err != nil {
			return errSQLRow(d.err)
		}
	}
	return d.DBTX.QueryRowContext(ctx, query, args...)
}
