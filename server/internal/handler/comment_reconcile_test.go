package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// completeTaskViaHandler drives the daemon CompleteTask endpoint for taskID.
func completeTaskViaHandler(t *testing.T, taskID, output string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	return w
}

// pendingTaskCountForAgentIssue counts claimable (queued/dispatched) tasks for
// an (issue, agent) pair.
func pendingTaskCountForAgentIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		issueID, agentID).Scan(&n)
	return n
}

// queuedTaskCountForAgentIssue counts only QUEUED (not dispatched) tasks for an
// (issue, agent) pair. Used to distinguish a freshly-enqueued follow-up from a
// pre-seeded dispatched task in the same assertion.
func queuedTaskCountForAgentIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID).Scan(&n)
	return n
}

// TestCompleteTask_ReconcilesMemberCommentPostedDuringRun proves the MUL-4195
// completion-reconciliation guarantee: a deliberate member comment that lands
// while the agent is busy (after the run's started_at) must earn a follow-up
// run instead of being silently lost.

// A daemon may replay /complete after the server committed but its response was
// lost. The task CAS makes that replay a 200, but the handler must also skip the
// transaction-external reconciliation; otherwise a follow-up that finished
// between deliveries leaves no pending dedupe row and the same member comment
// creates a second real agent run.

// TestCompleteTask_NoReconcileWhenNoNewMemberComment guards against spurious
// follow-ups: when no member comment arrived after the run started, completion
// must not enqueue any new task.

// TestCompleteTask_DoesNotReTriggerOtherAgentMentionedDuringRun is the MUL-4195
// review must-fix #2 regression test. Agent A is running on an issue when a
// member posts a comment that @-mentions a DIFFERENT agent B. B is triggered at
// comment-creation time (not exercised here). When A's run completes, the
// completion reconcile must NOT replay that comment through the full trigger
// pipeline and spawn a SECOND B run — reconcile is scoped to the agent that
// just ran (A). Before the fix, reconcile fanned the latest member comment out
// to every routed agent, so completing A re-woke B (and any other agent the
// comment mentioned), breaking the bounded-follow-up guarantee.

// TestCompleteTask_ReconcilesAgentAuthoredMentionToCompletedAgent is the
// MUL-4304 regression test. It drives the ACTUAL drop path (review must-fix):
//
//   - Agent B already has a DISPATCHED task on the issue. (This is the only
//     state that drops the mention. `running`/`queued` do not: a queued task
//     merges the comment in, and a running-only target is not AlreadyPending so
//     it takes the normal fresh-enqueue path.)
//   - Agent A posts an explicit `@B` comment through the real trigger path
//     (triggerTasksForComment, same entry CreateComment uses). Because B's task
//     is dispatched, `AlreadyPending` is true, mergeCommentIntoPendingTask finds
//     no QUEUED row to fold into, and the active-task check `continue`s — the
//     mention is dropped at creation time and deferred to completion reconcile.
//   - Before the fix, reconcile listed only member comments, so A's
//     agent-authored `@B` mention was NEVER replayed and B was silently never
//     re-woken. With the fix, completing B's task must enqueue exactly one
//     follow-up for B.
//
// The test asserts BOTH halves: no queued follow-up at creation (proving the
// drop actually happens), then exactly one after completion (proving reconcile
// recovers it).

// TestCompleteTask_DoesNotReconcilePlainAgentReply guards the anti-loop
// boundary of MUL-4304 on an agent-assigned issue: an agent-authored comment
// with NO explicit @mention (a plain reply / acknowledgement) must never earn a
// follow-up, even though reconcile now considers agent comments. Only explicit
// @agent/@squad mentions are replayed.

// TestCompleteTask_DoesNotReconcilePlainWorkerReplyOnSquadIssue is the MUL-4304
// review must-fix #2 regression test. On a SQUAD-assigned issue,
// computeCommentAgentTriggers routes a plain worker-agent reply (no mention) to
// the squad leader via routeAssignedSquadLeaderFallback (Source = issue
// assignee) — that is the create-time leader→worker→leader coordination path.
// This reply was never accepted into a run's input plan. Reconcile must NOT
// turn a timestamp-only plain reply into a new conversation. Accepted worker
// handoffs are covered separately by TestWorkerReplyDelivery.
func TestCompleteTask_DoesNotReconcilePlainWorkerReplyOnSquadIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var leaderRuntimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID)
	// A running leader task whose completion drives reconcile.
	leaderTaskID := dbfx.Task(t, fx.LeaderID, testutil.Cols{
		"runtime_id":     leaderRuntimeID,
		"issue_id":       issueID,
		"status":         "running",
		"is_leader_task": true,
		"squad_id":       fx.SquadID,
		"created_at":     testutil.Raw("now() - interval '10 minutes'"),
		"started_at":     testutil.Raw("now() - interval '5 minutes'"),
	})

	// A plain worker-agent reply (no mention) posted during the leader's run.
	// At create time this WOULD route to the leader via the squad-leader
	// fallback; reconcile must not replay it.
	dbfx.Exec(t, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'agent', $3, 'done — pushed the change', 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, fx.OtherID)

	if w := completeTaskViaHandler(t, leaderTaskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The squad-leader fallback is a non-mention route, so reconcile must not
	// enqueue any follow-up for the leader from a plain worker reply.
	if n := pendingTaskCountForAgentIssue(t, issueID, fx.LeaderID); n != 0 {
		t.Fatalf("plain worker reply must not reconcile-wake the squad leader, got %d leader task(s)", n)
	}
}

// handlerWorkspaceMember inserts a fresh user + workspace member and returns
// the user id (for a second distinct originator).
func handlerWorkspaceMember(t *testing.T, slug string) string {
	t.Helper()
	var userID string
	email := slug + "-" + time.Now().Format("150405.000000") + "@example.test"
	dbfx.QueryRow(t, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reconcile Test "+slug, email).Scan(&userID)
	dbfx.Exec(t, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		testWorkspaceID, userID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// TestConsecutiveCommentsDifferentOriginatorsFullEnqueuePath is the MUL-4195
// second-round must-fix #1 regression test, driving the FULL handler enqueue
// path (computeCommentAgentTriggers → enqueueCommentAgentTriggers → merge), not
// just the SQL. Member A's comment creates a queued task; member B (a different
// originator) then comments before the run starts. The earlier build returned
// ErrNoRows from the originator gate and fell through to a fresh enqueue that
// tripped the one-pending-per-(issue,agent) unique index, silently dropping B's
// comment. With recompute-on-merge, B's comment folds into the single task:
// still one task (no drop, no collision), trigger repointed to B, originator
// re-stamped to B, and A's comment preserved as coalesced.
func TestConsecutiveCommentsDifferentOriginatorsFullEnqueuePath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	dbfx.QueryRow(t,
		`SELECT id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID).Scan(&agentID)
	userB := handlerWorkspaceMember(t, "originatorB")

	issueID := dbfx.Issue(t, "diff-originator fixture", testutil.Cols{
		"status":        "in_progress",
		"number":        999004,
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	insertMemberComment := func(authorID, content string) db.Comment {
		t.Helper()
		id := dbfx.Comment(t, issueID, content, testutil.Cols{
			"author_id": authorID,
		})
		c, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(id))
		if err != nil {
			t.Fatalf("load comment: %v", err)
		}
		return c
	}

	// A's comment → creates the queued task (originator A).
	cA := insertMemberComment(testUserID, "first, from A")
	testHandler.triggerTasksForComment(ctx, issue, cA, nil, "member", testUserID, testUserID, nil, nil)
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after A's comment expected exactly 1 queued task, got %d", n)
	}

	// B's comment (different originator) before start → must fold in, NOT drop.
	cB := insertMemberComment(userB, "second, from B — different user")
	dbfx.Exec(t, `UPDATE comment SET parent_id=$2 WHERE id=$1`, cB.ID, cA.ID)
	cB.ParentID = nullString(cA.ID)
	testHandler.triggerTasksForComment(ctx, issue, cB, nil, "member", userB, userB, nil, nil)

	// Still exactly one task (bounded concurrency, no unique-index collision).
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after B's comment expected still exactly 1 task (folded in, not dropped/duplicated), got %d", n)
	}
	// Trigger repointed to B, originator re-stamped to B, A coalesced.
	trigger, originator, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
	if trigger != uuidToString(cB.ID) {
		t.Errorf("expected trigger repointed to B's comment %s, got %s", uuidToString(cB.ID), trigger)
	}
	if originator != userB {
		t.Errorf("expected originator re-stamped to B (%s), got %s", userB, originator)
	}
	if !containsUUID(coalesced, uuidToString(cA.ID)) {
		t.Errorf("expected A's comment %s preserved as coalesced, got %v", uuidToString(cA.ID), coalesced)
	}
}

// TestCompleteTask_ReconcilesDispatchedWindowComment is the MUL-4195
// second-round must-fix #2 regression test. A member comment that lands AFTER
// the claim response was built (after dispatched_at) but BEFORE StartTask
// (before started_at) must still earn a follow-up. The earlier reconcile
// anchored on started_at and missed this window; anchoring on dispatched_at
// catches it.

// taskTriggerOriginatorCoalesced returns (trigger_comment_id, originator_user_id,
// coalesced_comment_ids) as text for the most recent task of (issue, agent).
func taskTriggerOriginatorCoalesced(t *testing.T, issueID, agentID string) (string, string, []string) {
	t.Helper()
	var trigger, originator string
	var coalesced []string
	dbfx.QueryRow(t, `
		SELECT COALESCE(trigger_comment_id::text, ''),
		       COALESCE(originator_user_id::text, ''),
		       coalesced_comment_ids::text[]
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1
	`, issueID, agentID).Scan(&trigger, &originator, &coalesced)
	return trigger, originator, coalesced
}

func containsUUID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestCompleteTask_ReconcilesPreDispatchMergeRaceComment is the MUL-4195
// round-3 must-fix regression test. A member comment is created while the task
// is still queued, but its merge loses the race to the daemon claiming the task
// (queued→dispatched); the merge then finds no pre-claim row and the enqueue
// path defers to reconcile. The comment's created_at is BEFORE dispatched_at,
// so a dispatched_at-anchored reconcile would skip it and it would vanish. The
// created_at anchor + delivered-set exclusion must catch it — while NOT
// re-firing a comment that WAS delivered as a pre-claim coalesced entry.

// TestCompleteTask_ReconcilesPlannedButUndeliveredComments pins the distinction
// between the enqueue plan and the claim receipt. The planned comments predate
// the task (as they do on an auto-retry), so the planned-id query branch is the
// only way completion can recover payload-overflow or legacy-undelivered input.
