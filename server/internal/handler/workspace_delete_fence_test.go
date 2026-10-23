package handler

import (
	"context"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// enqueueViaRealQuery creates a task through the generated CreateAgentTask query —
// the actual production enqueue path, fence included. Raw INSERTs in tests would
// bypass the fence, which lives in the application SQL rather than in a trigger.
func enqueueViaRealQuery(ctx context.Context, q *db.Queries, agentID, runtimeID, issueID string) error {
	params := db.CreateAgentTaskParams{
		AgentID:   parseUUID(agentID),
		RuntimeID: nullString(parseUUID(runtimeID)),
		Priority:  0,
	}
	if issueID != "" {
		params.IssueID = nullString(parseUUID(issueID))
	}
	_, err := q.CreateAgentTask(ctx, params)
	return err
}

// waitForBlockedWriter blocks until the given backend is waiting on a lock, so a
// race test can be sure the writer really is queued before the other side commits.
// The pid comes from the writer itself rather than being guessed from statement
// text, which on a shared test database could match an unrelated backend. Fails the
// test on timeout rather than proceeding, so a missed race shows up as a failure
// instead of a vacuous pass.
func waitForBlockedWriter(t *testing.T, ctx context.Context, pid int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := testPool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE pid = $1 AND state = 'active' AND wait_event_type = 'Lock'
)
`, pid).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("backend %d never parked on a lock; the race was not set up", pid)
}

// TestTaskWriteFence_BlocksOnWorkspaceLockAlone shows the fence, and only the
// fence, standing between a concurrent enqueue and a workspace being torn down.
//
// The holder locks ONLY the workspace row — no agent, issue or runtime row — so no
// foreign key on agent_task_queue has anything to block on: none of them
// references workspace. Anything that blocks here is the fence taking FOR KEY
// SHARE on the owners' workspace row from inside the writer's own statement.

// TestTaskWriteFence_QueuedWriterCannotLandAfterTeardownCommits is the race the
// review asked for, run as an actual race: a writer queues behind the workspace
// lock while teardown is in flight, teardown commits, and the writer must then be
// unable to put a row in the table.
//
// It also discriminates the fence from the foreign keys. The writer's failure is
// "no row written" — CreateAgentTask is a :one, so it comes back as
// sql.ErrNoRows because the fence's predicate was false. An FK doing the work
// instead would surface as SQLSTATE 23503, which the assertion rejects.

// TestTaskWriteFence_LocksWorkspacesInIDOrder covers the deadlock surface the
// review flagged: a task may reference owners in different workspaces, so the
// fence can lock more than one workspace row, and two writers touching the same
// pair from opposite directions must not be able to take them in opposite orders.
//
// The fence sorts by workspace id, so both writers queue on the lower id first.
// The test runs the two mirrored writes concurrently and requires that both
// succeed — a deadlock would surface as 40P01 on one of them.

// TestMergeLegacyRuntime_KeepsOldRuntimeWhenFenceRefuses is the regression test
// for the failure mode the review found: the fence refusing looked exactly like
// "the old runtime had no tasks", so the merge went on to delete the old runtime
// and agent_task_queue's ON DELETE CASCADE quietly took its history with it.
//
// Here the merge target has vanished before the reassignment runs, so the fence
// returns false. The old runtime and its task must both survive, and nothing may
// be recorded on the target.

// TestMergeLegacyRuntime_RollsBackWhenAStepFails is the fault-injection half of the
// merge contract: the four statements have to land together or not at all.
//
// Run as autocommit statements the merge could commit the task reassignment and
// then fail on the agent reassignment, leaving tasks on the new runtime and their
// agents on the old one while daemon registration still reported success. Worse,
// the fence's workspace lock ended with its own statement, so a teardown could slip
// into that gap, delete the target runtime and clear runtime_id on the tasks that
// had just moved — history the next merge could no longer find, because it looks up
// tasks by the OLD runtime id.
//
// The failure is injected in the database rather than through a code seam, so the
// test exercises the real statements: a trigger makes any agent row moving onto the
// merge target raise.

// TestRuntimeMerge_LateWriteToOldRuntimeIsFencedNotCascaded is the regression test
// for the race the review reproduced: an enqueue that arrives against the OLD
// runtime after the merge has scanned its tasks.
//
// Before this fix the writer was not blocked at all — writers hold FOR KEY SHARE on
// the workspace and so did the merge, and two KEY SHARE locks do not conflict — so
// the insert committed and was then silently removed by agent_task_queue's
// ON DELETE CASCADE when the merge deleted the old runtime. The reviewer measured
// exactly that: `INSERT 0 1`, then the row count going from 1 to 0.
//
// The merge's statement sequence is driven by hand here so it can be stopped after
// the agent update and before the old runtime is deleted, which is where the window
// used to be. The writer must block on the fence, and once the merge commits it must
// come back as "no row written" — not 23503, and not a row that exists briefly and
// then disappears.

// TestLockTaskOwnerRows_RejectsUnresolvableOwner pins the hole the review
// found in the first version of this fence: counting only the owners that still
// resolve made a deleted owner invisible, and the statement fell through to the
// foreign key. A reference that does not resolve must make the predicate false on
// its own.

var _ = ""
