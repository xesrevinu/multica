package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

const (
	dupRaceHeadA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dupRaceHeadB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// dupRaceFixture creates a workspace-invocable agent and an issue assigned to it.
func dupRaceFixture(t *testing.T, agentName string, issueNumber int) (agentID, issueID, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	agentID = createHandlerTestAgent(t, agentName, nil)
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dup-enqueue-race fixture', 'in_progress', 'none', $2, 'member', $3, 0, 'agent', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, issueNumber, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
	return agentID, issueID, runtimeID
}

func insertDupRaceComment(t *testing.T, issueID, content, age string) string {
	t.Helper()
	// These race cases compete for the same thread slot.
	var parentID *string
	dbfx.QueryRow(t, `SELECT (SELECT id::text FROM comment WHERE issue_id=$1 AND parent_id IS NULL ORDER BY created_at, id LIMIT 1)`, issueID).Scan(&parentID)
	id := dbfx.Comment(t, issueID, content, testutil.Cols{"parent_id": parentID})
	dbfx.Exec(t, `UPDATE comment SET created_at=now()-$2::interval WHERE id=$1`, id, age)
	return id
}

func commentCovered(t *testing.T, issueID, agentID, commentID, statusFilter string) bool {
	t.Helper()
	var ok bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT $3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids)
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = $4
	`, issueID, agentID, commentID, statusFilter).Scan(&ok); err != nil {
		t.Fatalf("check covered %s: %v", commentID, err)
	}
	return ok
}

// TestCommentEnqueueRaceQueuedWinnerFoldsLoser: when the lost-race winner is
// still QUEUED and shares the reviewed head, the losing comment is folded into
// it (the merge makes the newer comment the trigger and pushes the prior trigger
// into coalesced), so the single run covers both. Coalesced outcome, one pending
// task, and no warning / constraint-name leak.
//
// Run once per trigger source that can lose this race on an issue whose agent is
// the assignee. The mention source has been covered since #5958; the
// ISSUE-ASSIGNEE source is the half #5914 left behind, and it is not redundant:
// its enqueue surfaced the RAW unique violation, so the loser came back
// blocked/internal_error instead of coalescing. That dropped the comment's
// instruction outright — the comment-creation path has no obligation hand-off
// (unlike completion reconcile), and a losing comment usually PREDATES the
// winning task row, so completion reconcile's created_at window cannot pick it
// up either.

// TestCommentEnqueueRaceDispatchedWinnerDurablyCoversLoser is the regression for
// Elon round-2 must-fix 1: the losing comment PRECEDES a winner that is already
// DISPATCHED before the merge retry, so completion reconcile's created_at window
// cannot see it. The race path must durably register it as a planned (but NOT
// delivered) input on the active winner, and a follow-up must actually cover it
// after the winner completes — never a bare coalesced success that drops it.

// TestCommentEnqueueRaceDifferentHeadNotCoalesced is the regression for Elon
// round-2 must-fix 2 + round-7: a comment for a NEW head (PR updated A→B) collides
// with the still-pending head-A task. It must NOT be folded into the head-A run
// (TEN-356), and — per round 7 — it must NOT be reported as a fabricated deferred
// either, since a point-in-time "an older task exists" cannot durably promise
// coverage. The honest outcome is a non-success; the head-A task's completion
// reconcile still covers the head-B comment best-effort, earning head-B coverage.

// TestRegisterPlannedCommentForActiveTaskExcludesQueued is the regression for
// Elon round-3 must-fix 2: a planned-only append must never target a QUEUED task
// (it has no claim receipt, so the append would be delivered at claim time and
// bypass the atomic re-attribution a queued fold requires — MUL-4302). Only
// claim-receipt statuses (dispatched/running/waiting_local_directory) are valid
// planned-id targets; a queued task must miss so the caller routes it to the
// atomic merge instead.

// TestCommentEnqueueRaceQueuedWinnerReattributesOriginator is the regression for
// Elon round-3 must-fix 2: when the lost-race winner is a same-head QUEUED task,
// the losing comment must fold through the ATOMIC merge, which re-stamps the run
// to the NEW comment's originator — never a bare planned append that would leave
// a second member's comment executing under the first member's identity. Two
// different members: the winner is attributed to M1, the losing comment is M2's,
// and after the race the run must be re-attributed to M2.

// TestCommentEnqueueRaceNewerDifferentHeadNotDeferred is the regression for Elon
// round-5 must-fix: when the different-head task holding the slot is NEWER than
// the losing comment (and the comment is not in its planned ids), completion
// reconcile's `created_at > since` window never matches it — so the resolver must
// NOT fabricate a deferred off "head differs". It returns a truthful non-success
// (internal_error) instead, never a success-shaped outcome that drops the comment.

// TestCommentEnqueueRaceMixedCoveringAndNewerNotDeferred covers Elon rounds 6+9:
// an OLDER different-head task and a NEWER one can coexist (running sits outside
// the unique index). At decision time nothing has attached the comment, so the
// resolver must return a truthful non-success rather than a fabricated deferred
// (round 6). Draining the chain then pins the boundary of the hand-off: the newer
// blocker is at a DIFFERENT head, so the obligation is NOT folded into it — that
// would let an old-head run consume a current-head request (TEN-356, round 9).
// This is the one documented best-effort shape; correctness rests on the fact
// that nothing ever promised coverage here.

// commentCoveredByAnyActiveTask reports whether ANY active task for (issue, agent)
// has the comment as its trigger or in its planned (coalesced) batch.
func commentCoveredByAnyActiveTask(t *testing.T, issueID, agentID, commentID string) bool {
	t.Helper()
	var ok bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
			  AND status IN ('queued','dispatched','running','waiting_local_directory')
			  AND ($3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids))
		)
	`, issueID, agentID, commentID).Scan(&ok); err != nil {
		t.Fatalf("check coverage: %v", err)
	}
	return ok
}

// coveringTaskHead returns the head_sha of the active task that covers the comment
// (empty when none does), so a test can assert WHICH head finally covers it.
func coveringTaskHead(t *testing.T, issueID, agentID, commentID string) string {
	t.Helper()
	var head string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(context->>'head_sha','')
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND status IN ('queued','dispatched','running','waiting_local_directory')
		  AND ($3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids))
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID, commentID).Scan(&head); err != nil {
		t.Fatalf("read covering task head: %v", err)
	}
	return head
}

// seedDupRacePR links a PR at head B so the reviewed head resolves to head B.
func seedDupRacePR(t *testing.T, issueID string, prNumber int) {
	t.Helper()
	ctx := context.Background()
	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'multica-ai', 'multica', $2, 'review PR', 'open', 'https://example.test/pr', now(), now(), $3)
		RETURNING id
	`, testWorkspaceID, prNumber, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}
}

// TestReconcileBlockedReplayPropagatesToClaimedBlocker is the regression for Elon
// round-8 must-fix 2 (planned-registration path): a planned-id obligation must not
// be consumed once. C is registered as a planned input on running task A; before A
// completes, a NEWER different-head task B is claimed and holds the unique slot. A's
// completion replay of C is therefore blocked — and must be HANDED to B rather than
// discarded, so completing B finally covers C.

// TestReconcileBlockedReplayPropagatesToQueuedBlocker is the regression for Elon
// round-8 must-fix 1 (AlreadyPending path): C arrives while A is running, so A's
// reconcile finds it by timestamp — but a NEWER task B is QUEUED when A completes,
// blocking the replay. When B is at the CURRENT head the obligation is handed to it
// through the ATOMIC merge (never a bare append), so C ends up covered at that head.

// TestReconcileBlockedReplayNeverFoldsIntoDifferentHeadQueuedBlocker is the
// regression for Elon round-9 must-fix 2: the hand-off must NOT merge across heads.
// A different-head QUEUED blocker would make the comment that run's trigger and mark
// it delivered at claim time, so an old-head run would consume a new-head request and
// no new-head follow-up would ever exist (TEN-356). The hand-off must decline instead
// — leaving the blocker untouched — rather than "cover" the comment incorrectly.
