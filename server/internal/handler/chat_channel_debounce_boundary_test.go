package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// appendChannelUserMessage writes an inbound channel message the way
// ChatSession.AppendUserMessage does: durable, unowned, and stamped with the
// immutable channel_ingested provenance. The 3s debounced flush is what later
// creates the task and seals this row into its input batch.
func appendChannelUserMessage(t *testing.T, ctx context.Context, sessionID, body string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
		VALUES ($1, 'user', $2, TRUE)
		RETURNING id
	`, sessionID, body).Scan(&id); err != nil {
		t.Fatalf("append channel user message %q: %v", body, err)
	}
	return id
}

// flushChannelChatRun is the debounced run trigger: it creates the task and
// seals the session's waiting input batch into it, exactly as Router's batcher
// does when the silence window expires.
func flushChannelChatRun(t *testing.T, ctx context.Context, sessionID string) db.AgentTaskQueue {
	t.Helper()
	sess, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	task, err := testHandler.TaskService.EnqueueChatTask(ctx, sess, parseUUID(testUserID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })
	return task
}

// claimResult is a claim attempt that does not assume success, so a failing
// claim reports the status and body the daemon actually received.
type claimResult struct {
	code int
	body string
	task *claimRuntimeGuardTask
}

func claimTaskRaw(t *testing.T, runtimeID, daemonID string) claimResult {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, daemonID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	out := claimResult{code: w.Code, body: w.Body.String()}
	if w.Code == 200 {
		var resp struct {
			Task *claimRuntimeGuardTask `json:"task"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode claim response: %v", err)
		}
		out.task = resp.Task
	}
	return out
}

// TestChannelChat_ReplyLandingInsideDebounceWindowStillAnswersTheNextMessage
// reproduces the lost turn seen on a self-hosted WeCom tenant, end to end.
//
// A message arrived while the previous agent turn was finishing:
//
//	18:31:17.7  chat_message "挺好的 正常了"  -> task_id NULL, waiting out the 3s window
//	18:31:20.2  the previous task completes   -> assistant row, NEWER than that message
//	18:31:20.7  the debounce fires            -> task bdc0947e, chat_input_task_id = itself
//	18:31:20.7  ERR chat claim: task-owned direct task has no user input; cancelling
//	            task_id=bdc0947e chat_session_id=2fccd3c9 chat_input_task_id=bdc0947e
//
// In the database afterwards, that user row still had task_id = NULL while every
// other message in the session had one, and the task owned zero user messages.
// The user saw their message accepted, a task appear and vanish, and no reply.
//
// The batch seal skipped the message because a non-user row existed after it —
// but that row was the PREDECESSOR's reply to an already-sealed batch, not an
// answer to this message. The claim guard is correct to fail closed on an empty
// input batch; the batch should never have been empty.
