package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertCommentForScopeTest inserts a member comment on issueID in the given
// workspace and returns its id.
func insertCommentForScopeTest(t *testing.T, ctx context.Context, issueID, workspaceID, content string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, issueID, workspaceID, testUserID, content).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, id) })
	return id
}

// enqueueIssueTaskWithTrigger enqueues a real task for (issueID, agentID) via the
// production TaskService.EnqueueTaskForIssue path — the path that snapshots
// trigger_summary and resolves the originator from the triggering comment — so
// the MUL-4252 workspace scoping is exercised end-to-end rather than bypassed
// with a hand-written row. Returns the created task id.
func enqueueIssueTaskWithTrigger(t *testing.T, ctx context.Context, agentID, issueID, triggerCommentID string) string {
	t.Helper()
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           parseUUID(issueID),
		WorkspaceID:  parseUUID(testWorkspaceID),
		AssigneeType: sql.NullString{String: "agent", Valid: true},
		AssigneeID:   nullString(parseUUID(agentID)),
		CreatorType:  "member",
		CreatorID:    parseUUID(testUserID),
		Priority:     "none",
	}, parseUUID(triggerCommentID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	taskID := uuidToString(task.ID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// claimTriggerFieldsForTest claims the next task for runtimeID and returns the
// triggering comment content + summary the claim response carried (both empty
// when absent — omitempty on the wire) plus the raw response body.
func claimTriggerFieldsForTest(t *testing.T, runtimeID string) (taskID, triggerContent, triggerSummary, raw string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "comment-workspace-scope")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			ID                    string `json:"id"`
			TriggerCommentContent string `json:"trigger_comment_content"`
			TriggerSummary        string `json:"trigger_summary"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", "", w.Body.String()
	}
	return resp.Task.ID, resp.Task.TriggerCommentContent, resp.Task.TriggerSummary, w.Body.String()
}

// TestClaimDeliversSameWorkspaceTriggerComment is the positive path: a triggering
// comment in the task's own workspace must still be snapshotted into
// trigger_summary at enqueue and embedded (content + summary) in the claim
// response. The MUL-4252 workspace-scoped lookups must not regress delivery.

// TestClaimDoesNotLeakForeignWorkspaceTriggerCommentOrSummary is the MUL-4252
// guard, exercised through the real enqueue + claim path. A task whose
// trigger_comment_id points at a comment owned by a DIFFERENT workspace must
// leak neither the full body (claim-time fetch) nor the truncated summary
// (enqueue-time snapshot), and must not inherit an originator from it.
