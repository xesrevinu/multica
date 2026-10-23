package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type batchClaimReceiptResponse struct {
	Tasks []struct {
		ID                  string   `json:"id"`
		RuntimeID           string   `json:"runtime_id"`
		AuthToken           string   `json:"auth_token"`
		DeliveredCommentIDs []string `json:"delivered_comment_ids"`
	} `json:"tasks"`
}

// TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing pins the MUL-4257 review
// fix: max_tasks=0 is a valid "no free slots" poll that must claim nothing —
// it must NOT be coerced to 1 (which would dispatch a task the daemon can't run
// and strand it until stale reclaim).
func TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch max0 rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch max0 agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("max_tasks=0 claimed %d tasks, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued", status)
	}
}

// TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest pins that a negative
// max_tasks is rejected rather than silently coerced.
func TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	rt := createClaimReclaimRuntime(t, context.Background(), "Batch neg rt")
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, -1)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative max_tasks, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimTasksByRuntime_SkipsInvalidRuntimeID pins the MUL-4257 review fix:
// a malformed runtime_id must be skipped (non-panicking parse), not turned into
// a 500 — and a valid runtime in the same request is still claimed.

// seedCommentBackedQueuedTask inserts a queued task triggered by a real comment
// on its issue, returning (taskID, commentID).
func seedCommentBackedQueuedTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string) (string, string) {
	t.Helper()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please handle this')
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority)
		VALUES ($1, $2, $3, $4, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID, commentID).Scan(&taskID); err != nil {
		t.Fatalf("seed comment-backed task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID, commentID
}

func assertCommentDelivered(t *testing.T, ctx context.Context, taskID, commentID string) {
	t.Helper()
	var member bool
	if err := testPool.QueryRow(ctx, `
		SELECT $1 = ANY(delivered_comment_ids) FROM agent_task_queue WHERE id = $2
	`, commentID, taskID).Scan(&member); err != nil {
		t.Fatalf("read delivered_comment_ids: %v", err)
	}
	if !member {
		t.Fatalf("trigger comment %s not persisted in task %s delivered_comment_ids", commentID, taskID)
	}
}

// TestClaimTasksByRuntime_PersistsCommentDeliveryReceipt pins the MUL-4257
// must-fix: the batch path routes through FinalizeTaskClaim, so a comment-backed
// task claimed via batch persists the delivered_comment_ids receipt AND returns
// it in the response.

// TestClaimTasksByRuntime_StaleReclaimRecordsDeliveryReceipt pins that a
// comment-backed task recovered via the batch reclaim path (dispatched, never
// started, past the recovery window) is re-finalized so its delivery receipt is
// recorded on the replacement claim.

// TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon pins the MUL-4257
// review must-fix: a daemon must not batch-claim a task routed to a runtime
// bound to a DIFFERENT daemon, even in the same workspace. The runtime is
// skipped and its task stays queued for the owning machine.
func TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Runtime bound to a different daemon in the same (handler-test) workspace.
	var rt string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'other-daemon-machine', 'Other daemon RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&rt); err != nil {
		t.Fatalf("create other-daemon runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rt) })

	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Other daemon agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	// postBatchClaim sends daemon_id = batchClaimTestDaemonID ("batch-claim-review"),
	// which differs from the runtime's "other-daemon-machine".
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("daemon-A claimed %d tasks from a runtime owned by daemon-B, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued for the owning daemon", status)
	}
}

// TestClaimTasksByRuntime_RequiresDaemonID pins that the batch claim rejects a
// request with no daemon_id rather than falling back to workspace-only scoping.
func TestClaimTasksByRuntime_RequiresDaemonID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/claim",
		map[string]any{"runtime_ids": []string{}, "max_tasks": 5},
		testWorkspaceID, batchClaimTestDaemonID)
	testHandler.ClaimTasksByRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when daemon_id is missing, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimTasksByRuntime_RepairsStaleCommentPlan pins the MUL-4257 review
// must-fix: when a claimed task's trigger comment was deleted (only coalesced
// survivors remain), the batch path must NOT finalize+dispatch it (which would
// silently drop the surviving comment). Instead it cancels the stale task,
// omits it from the batch, and replays the surviving comment as a fresh plan.
