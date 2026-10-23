package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)





type commentDeliveryFixture struct {
	runtimeID string
	agentID   string
	issueID   string
	taskID    string
	commentID []string
	threadID  []string
	content   []string
}

type failNthBegin struct {
	delegate *testutil.Pool
	failAt   int
	calls    int
}

// lockCommentForDeleteTxStarter begins real transactions whose
// LockCommentForDelete statement answers with lockErr instead: an error
// simulates a storage failure, sql.ErrNoRows a concurrent delete that won.
type lockCommentForDeleteTxStarter struct {
	delegate *testutil.Pool
	lockErr  error
}

type lockCommentForDeleteTx struct {
	*sql.Tx

	lockErr error
}

type errorRow struct{ err error }

func (r errorRow) Scan(...interface{}) error { return r.err }

func (s *lockCommentForDeleteTxStarter) BeginTx(ctx context.Context, _ *sql.TxOptions) (handlerTx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return lockCommentForDeleteTx{Tx: tx, lockErr: s.lockErr}, nil
}

func (t lockCommentForDeleteTx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if strings.Contains(query, "-- name: LockCommentForDelete") {
		return errSQLRow(t.lockErr)
	}
	return t.Tx.QueryRowContext(ctx, query, args...)
}

func (f *failNthBegin) BeginTx(ctx context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
	f.calls++
	if f.calls == f.failAt {
		return nil, errors.New("injected claim finalization transaction failure")
	}
	return f.delegate.Begin(ctx)
}

func createCommentDeliveryFixture(t *testing.T, label string) commentDeliveryFixture {
	t.Helper()
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, label+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, label+" agent")

	contents := []string{"first cross-thread instruction", "second instruction", "latest instruction"}
	ids := make([]string, 3)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '3 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[0]).Scan(&ids[0]); err != nil {
		t.Fatalf("insert first comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '2 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[1]).Scan(&ids[1]); err != nil {
		t.Fatalf("insert second comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, now() - interval '1 minute')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[2], ids[1]).Scan(&ids[2]); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			trigger_comment_id, coalesced_comment_ids
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, ARRAY[$5::uuid, $6::uuid])
		RETURNING id
	`, agentID, runtimeID, issueID, ids[2], ids[1], ids[0]).Scan(&taskID); err != nil {
		t.Fatalf("insert comment delivery task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	return commentDeliveryFixture{
		runtimeID: runtimeID,
		agentID:   agentID,
		issueID:   issueID,
		taskID:    taskID,
		commentID: ids,
		threadID:  []string{ids[0], ids[1], ids[1]},
		content:   contents,
	}
}

func makeCommentDeliverySingleThread(t *testing.T, fixture commentDeliveryFixture) {
	t.Helper()
	root := dbfx.Comment(t, fixture.issueID, "thread root", testutil.Cols{"author_type": "agent", "author_id": fixture.agentID})
	dbfx.Exec(t, `UPDATE comment SET created_at=now()-interval '10 minutes' WHERE id=$1`, root)
	for _, id := range fixture.commentID {
		dbfx.Exec(t, `UPDATE comment SET parent_id=$2 WHERE id=$1`, id, root)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET trigger_comment_id=trigger_comment_id WHERE id=$1`, fixture.taskID)
}

func claimCommentDeliveryFixture(t *testing.T, fixture commentDeliveryFixture, capabilities string) AgentTaskResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+fixture.runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "comment-delivery-matrix")
	if capabilities != "" {
		req.Header.Set("X-Client-Capabilities", capabilities)
	}
	req = withURLParam(req, "runtimeId", fixture.runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if response.Task == nil {
		t.Fatalf("claim returned no task: %s", w.Body.String())
	}
	return *response.Task
}

func deliveredCommentIDsForTask(t *testing.T, taskID string) []string {
	t.Helper()
	var ids []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT delivered_comment_ids::text[]
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&ids); err != nil {
		t.Fatalf("load delivered comment ids: %v", err)
	}
	return ids
}


// deletedTriggerStates are the two shapes a deleted trigger leaves behind: a
// removed row (trigger_comment_id cleared by ON DELETE SET NULL), or a
// tombstone for a trigger that still had replies (#8296), which keeps the id.
var deletedTriggerStates = []struct {
	name string
	sql  string
}{
	{name: "removed", sql: `DELETE FROM comment WHERE id = $1`},
	{name: "tombstoned", sql: `UPDATE comment SET content = '', deleted_at = now() WHERE id = $1`},
}


func testClaimRepairsDeletedTrigger(t *testing.T, deleteTriggerSQL string) {
	fixture := createCommentDeliveryFixture(t, "Deleted trigger stale claim")
	makeCommentDeliverySingleThread(t, fixture)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign stale-claim issue: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), deleteTriggerSQL, fixture.commentID[2]); err != nil {
		t.Fatalf("delete trigger directly: %v", err)
	}

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+fixture.runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "stale-comment-plan-repair")
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	req = withURLParam(req, "runtimeId", fixture.runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("repair stale claim: got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode stale-plan repair response: %v", err)
	}
	if response.Task != nil {
		t.Fatalf("stale plan was dispatched instead of repaired: %+v", response.Task)
	}
	assertRepairedCommentBatch(t, fixture, fixture.commentID[1], fixture.commentID[:1])
}







func assertRepairedCommentBatch(t *testing.T, fixture commentDeliveryFixture, wantTrigger string, wantCoalesced []string) {
	t.Helper()
	var originalStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&originalStatus); err != nil {
		t.Fatalf("load original task: %v", err)
	}
	if originalStatus != "cancelled" {
		t.Fatalf("original task status = %s, want cancelled", originalStatus)
	}

	var trigger string
	var coalesced, delivered []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT trigger_comment_id::text, coalesced_comment_ids::text[], delivered_comment_ids::text[]
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, fixture.issueID, fixture.agentID).Scan(&trigger, &coalesced, &delivered); err != nil {
		t.Fatalf("load repaired task: %v", err)
	}
	if trigger != wantTrigger {
		t.Fatalf("repaired trigger = %s, want %s", trigger, wantTrigger)
	}
	gotCoalesced := append([]string{}, coalesced...)
	want := append([]string{}, wantCoalesced...)
	slices.Sort(gotCoalesced)
	slices.Sort(want)
	if !slices.Equal(gotCoalesced, want) {
		t.Fatalf("repaired coalesced ids = %v, want %v", gotCoalesced, want)
	}
	if len(delivered) != 0 {
		t.Fatalf("repaired batch inherited receipt: %v", delivered)
	}
}







func testRerunPromotesSurvivorAfterTriggerDeleted(t *testing.T, deleteTriggerSQL string) {
	ctx := context.Background()
	fixture := createCommentDeliveryFixture(t, "Deleted trigger manual rerun")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign deleted-trigger rerun issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', started_at = now() - interval '2 minutes', completed_at = now() - interval '1 minute'
		WHERE id = $1
	`, fixture.taskID); err != nil {
		t.Fatalf("complete deleted-trigger rerun source: %v", err)
	}
	if _, err := testPool.Exec(ctx, deleteTriggerSQL, fixture.commentID[2]); err != nil {
		t.Fatalf("delete rerun source trigger: %v", err)
	}

	rerun, err := testHandler.TaskService.RerunIssue(ctx, parseUUID(fixture.issueID), parseUUID(fixture.taskID), "", "", nil)
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}
	if got := uuidToString(rerun.TriggerCommentID.String); got != fixture.commentID[1] {
		t.Fatalf("rerun promoted trigger = %s, want newest survivor %s", got, fixture.commentID[1])
	}
	if got := uuidsToStrings(rerun.CoalescedCommentIds); !slices.Equal(got, []string{fixture.commentID[0]}) {
		t.Fatalf("rerun survivor plan = %v, want [%s]", got, fixture.commentID[0])
	}
	if len(rerun.DeliveredCommentIds) != 0 {
		t.Fatalf("rerun inherited delivery receipt: %v", uuidsToStrings(rerun.DeliveredCommentIds))
	}
	if got := uuidToString(rerun.OriginatorUserID.String); got != testUserID {
		t.Fatalf("rerun originator = %s, want promoted comment author %s", got, testUserID)
	}
}




