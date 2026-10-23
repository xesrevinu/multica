package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"


	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The MUL-6788 batch rewrite replaced the per-message ListAttachmentsByChatMessage
// loop in buildClaimedTaskResponse with one ListAttachmentsByChatMessageIDs read.
// These tests pin the two behaviours the review called out: the grouped output
// must reproduce the previous append order (messages in their own order, each
// message's attachments in created_at order), and a FAILED batch read must fail
// closed — the agent's only handle for user files is the attachment ID, so a
// swallowed error would silently start the run with files missing and no
// redelivery.

// claimAttachmentsResponse decodes just the attachment slice off a claim.
type claimAttachmentsResponse struct {
	Task *struct {
		ChatMessage            string `json:"chat_message"`
		ChatMessageAttachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"chat_message_attachments"`
	} `json:"task"`
}

// seedLegacyChatUserMessage inserts a user message on a legacy (NULL
// chat_input_task_id) chat task with an explicit created_at so message order is
// deterministic, and returns its id.
func seedLegacyChatUserMessage(t *testing.T, sessionID, content, createdAt string) string {
	t.Helper()
	return dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID,
		"role":            "user",
		"content":         content,
		"created_at":      createdAt,
	})
}

// seedMessageAttachment binds an attachment to a chat message with an explicit
// created_at, so a test can interleave two messages' attachment timestamps and
// prove the per-message grouping keeps each message's attachments in
// created_at order regardless of the global batch ordering.
func seedMessageAttachment(t *testing.T, sessionID, messageID, filename, createdAt string) string {
	t.Helper()
	return dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"chat_session_id": sessionID,
		"chat_message_id": messageID,
		"uploader_type":   "member",
		"uploader_id":     testUserID,
		"filename":        filename,
		"url":             "https://cdn.example/" + filename,
		"content_type":    "image/png",
		"size_bytes":      10,
		"created_at":      createdAt,
	})
}

// claimForRuntimeExpecting drives the single-runtime claim through handler h and
// returns the response recorder wrapper asserting the wanted status.
func claimForRuntimeExpecting(t *testing.T, h *Handler, runtimeID, daemonID string, want int) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	return testutil.Call(t, h.ClaimTaskByRuntime, req).Want(want)
}

// TestClaimChatAttachmentsPreserveMessageThenCreatedAtOrder seeds two unanswered
// messages whose attachment created_at values interleave globally, then claims
// and asserts the response lists attachments in message order first and, within
// a message, in created_at order — the exact append order the per-message loop
// produced before batching.

// chatAttachmentBatchFailDBTX passes every statement through to the real pool
// EXCEPT the batched chat-message attachment read, which it fails.
type chatAttachmentBatchFailDBTX struct{ inner db.DBTX }

func (f chatAttachmentBatchFailDBTX) ExecContext(ctx context.Context, sqlQuery string, args ...any) (sql.Result, error) {
	return f.inner.ExecContext(ctx, sqlQuery, args...)
}

func (f chatAttachmentBatchFailDBTX) PrepareContext(ctx context.Context, sqlQuery string) (*sql.Stmt, error) {
	return f.inner.PrepareContext(ctx, sqlQuery)
}

func (f chatAttachmentBatchFailDBTX) QueryContext(ctx context.Context, sql string, args ...any) (*sql.Rows, error) {
	if strings.Contains(sql, "FROM attachment") && (strings.Contains(sql, "chat_message_id = ANY(") || strings.Contains(sql, "chat_message_id IN (SELECT value FROM json_each(")) {
		return nil, errInjectedChatAttachmentRead
	}
	return f.inner.QueryContext(ctx, sql, args...)
}

func (f chatAttachmentBatchFailDBTX) QueryRowContext(ctx context.Context, sql string, args ...any) *sql.Row {
	return f.inner.QueryRowContext(ctx, sql, args...)
}

var errInjectedChatAttachmentRead = errors.New("injected chat attachment batch read failure")

func newChatAttachmentReadFailureHandler(t *testing.T) *Handler {
	t.Helper()
	return New(
		db.New(chatAttachmentBatchFailDBTX{inner: testSQLite}), sqlDBBeginner{DB: testSQLite}, testHandler.Hub,
		testHandler.Bus,
		testHandler.EmailService,
		nil,
		nil,
		analytics.NoopClient{},
		Config{},
	)
}

// TestClaimChatAttachmentBatchReadErrorFailsClosed pins the core review fix on
// the claim path: a failed attachment batch read must reject the claim with 500
// and leave the task eligible for the stale-dispatched reclaim. The claim UPDATE
// moves the task to 'dispatched' with started_at still NULL; the failure branch
// must leave it exactly there (not cancelled, not started) so reclaim can
// redeliver it once the recovery window lapses.
func TestClaimChatAttachmentBatchReadErrorFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "attachment error chat")

	m1 := seedLegacyChatUserMessage(t, sessionID, "with a file", "2020-01-01T00:00:01Z")
	seedMessageAttachment(t, sessionID, m1, "doc.png", "2020-01-01T00:00:01Z")

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": sessionID,
		"status":          "queued",
	})

	h := newChatAttachmentReadFailureHandler(t)
	claimForRuntimeExpecting(t, h, runtimeID, daemonID, http.StatusInternalServerError)

	// The task must be parked exactly at the claim's dispatched state so the
	// stale-dispatched reclaim can redeliver it — not cancelled, and not moved
	// on to started/running.
	var status string
	var startedAt *string
	dbfx.QueryRow(t, `SELECT status, started_at::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &startedAt)
	if status != "dispatched" {
		t.Errorf("task status = %q, want dispatched (parked for stale-dispatched reclaim)", status)
	}
	if startedAt != nil {
		t.Errorf("task started_at = %v, want NULL (the failed claim must not have started the run)", *startedAt)
	}
}
