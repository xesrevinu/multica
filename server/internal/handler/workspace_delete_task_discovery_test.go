package handler

import (
	"context"
	"testing"

)

// workspaceDeletePathFixture builds the ownership shapes teardown has to handle:
// one task reachable from the victim workspace only through agent_id, one only
// through issue_id, one only through runtime_id, and one that belongs entirely to
// a neighbour workspace and must survive.
//
// The tokens cover the three explicit task_token paths: one owned by the victim
// workspace, one whose workspace_id points at the neighbour while its TASK is the
// victim's, one whose workspace_id points at the neighbour while its AGENT is the
// victim's, and one that is entirely the neighbour's.
type workspaceDeletePathFixture struct {
	victimID    string
	neighbourID string

	victimAgent   string
	victimIssue   string
	victimRuntime string

	neighbourAgent   string
	neighbourIssue   string
	neighbourRuntime string

	taskViaAgent   string
	taskViaIssue   string
	taskViaRuntime string
	neighbourTask  string

	victimToken        string
	crossTaskToken     string
	crossAgentToken    string
	neighbourOnlyToken string
}

func newWorkspaceDeletePathFixture(t *testing.T, slugSuffix string) workspaceDeletePathFixture {
	t.Helper()
	ctx := context.Background()

	victimSlug := "handler-tests-delete-paths-victim-" + slugSuffix
	neighbourSlug := "handler-tests-delete-paths-neighbour-" + slugSuffix
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = ANY($1::text[])`,
		[]string{victimSlug, neighbourSlug})

	newWorkspace := func(name, slug string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
`, name, slug).Scan(&id); err != nil {
			t.Fatalf("create workspace %s: %v", slug, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
		})
		return id
	}
	f := workspaceDeletePathFixture{
		victimID:    newWorkspace("Delete Paths Victim "+slugSuffix, victimSlug),
		neighbourID: newWorkspace("Delete Paths Neighbour "+slugSuffix, neighbourSlug),
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, f.victimID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	newRuntime := func(wsID, name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
VALUES ($1, $2, 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $3)
RETURNING id
`, wsID, name, testUserID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", name, err)
		}
		return id
	}
	newAgent := func(wsID, runtimeID, name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, owner_id)
VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, $4)
RETURNING id
`, wsID, name, runtimeID, testUserID).Scan(&id); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
		return id
	}
	newIssue := func(wsID, title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, $2, 'member', $3)
RETURNING id
`, wsID, title, testUserID).Scan(&id); err != nil {
			t.Fatalf("create issue %s: %v", title, err)
		}
		return id
	}

	f.victimRuntime = newRuntime(f.victimID, "victim runtime")
	f.victimAgent = newAgent(f.victimID, f.victimRuntime, "victim agent")
	f.victimIssue = newIssue(f.victimID, "victim issue")
	f.neighbourRuntime = newRuntime(f.neighbourID, "neighbour runtime")
	f.neighbourAgent = newAgent(f.neighbourID, f.neighbourRuntime, "neighbour agent")
	f.neighbourIssue = newIssue(f.neighbourID, "neighbour issue")

	f.taskViaAgent = insertDeletePathTask(t, f.victimAgent, f.neighbourIssue, f.neighbourRuntime)
	f.taskViaIssue = insertDeletePathTask(t, f.neighbourAgent, f.victimIssue, f.neighbourRuntime)
	f.taskViaRuntime = insertDeletePathTask(t, f.neighbourAgent, f.neighbourIssue, f.victimRuntime)
	f.neighbourTask = insertDeletePathTask(t, f.neighbourAgent, f.neighbourIssue, f.neighbourRuntime)

	f.victimToken = insertDeletePathToken(t, "mul5999-victim-"+slugSuffix, f.taskViaAgent, f.victimAgent, f.victimID)
	f.crossTaskToken = insertDeletePathToken(t, "mul5999-cross-task-"+slugSuffix, f.taskViaIssue, f.neighbourAgent, f.neighbourID)
	f.crossAgentToken = insertDeletePathToken(t, "mul5999-cross-agent-"+slugSuffix, f.neighbourTask, f.victimAgent, f.neighbourID)
	f.neighbourOnlyToken = insertDeletePathToken(t, "mul5999-neighbour-"+slugSuffix, f.neighbourTask, f.neighbourAgent, f.neighbourID)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM task_token WHERE id = ANY($1::uuid[])`,
			[]string{f.victimToken, f.crossTaskToken, f.crossAgentToken, f.neighbourOnlyToken})
		_, _ = testPool.Exec(bg, `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
			[]string{f.taskViaAgent, f.taskViaIssue, f.taskViaRuntime, f.neighbourTask})
	})

	return f
}

func insertDeletePathTask(t *testing.T, agentID, issueID, runtimeID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
RETURNING id
`, agentID, issueID, runtimeID).Scan(&id); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

func insertDeletePathToken(t *testing.T, hash, taskID, agentID, wsID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
RETURNING id
`, hash, taskID, agentID, wsID, testUserID).Scan(&id); err != nil {
		t.Fatalf("create task token %s: %v", hash, err)
	}
	return id
}

func rowExists(t *testing.T, table, id string) bool {
	t.Helper()
	var found bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&found); err != nil {
		t.Fatalf("check %s %s: %v", table, id, err)
	}
	return found
}

// TestDeleteWorkspaceTasks_IsSelfSufficientWithoutCascades asserts that the task
// sweep removes the rows on its own, inside the transaction and before any owner
// row is deleted. That ordering is the whole point: the legacy FK cascades are
// documented as an expand-phase safety net, so an assertion taken after the
// handler has finished cannot tell teardown apart from a cascade doing the work.

// TestDeleteWorkspace_CollectsTasksThroughEveryOwnershipPath is the end-to-end
// counterpart: the handler returns 204, every ownership path is gone, and the
// neighbour workspace is untouched.

// shrinkWorkspaceDeletePagesForTest makes the paging tests cross their page
// boundaries at dozens of rows instead of thousands. The paging logic does not
// depend on the page size. The batch test seeds two and a half task pages (25
// rows), which still fills both 10-row keyset probes it runs first.
func shrinkWorkspaceDeletePagesForTest(t *testing.T) {
	t.Helper()
	tasks, owners := workspaceDeleteTaskPageSize, workspaceDeleteOwnerPageSize
	workspaceDeleteTaskPageSize, workspaceDeleteOwnerPageSize = 10, 5
	t.Cleanup(func() { workspaceDeleteTaskPageSize, workspaceDeleteOwnerPageSize = tasks, owners })
}

// TestDeleteWorkspaceTasks_PagesPastTheBatchSize covers the bound itself: a
// single owner with more tasks than one batch must be swept by several
// iterations, and the loop must terminate. Nothing here may depend on the whole
// task set fitting in one statement or in this process (MUL-5999 review).

// TestDeleteWorkspaceTasks_PagesOwnersBeyondOnePage covers the other unbounded
// dimension: owner enumeration. A workspace with more agents than one owner page
// must still be swept completely, and the owner ids must never all be held here at
// once (MUL-5999 review).

// TestSweepTasksForOwner_FailsClosedWhenTasksKeepArriving pins the behaviour that
// makes the sweep safe without an application-wide enqueue protocol: if tasks keep
// appearing for an owner that has already been swept, teardown refuses to commit
// instead of leaving a half-cleaned tenant behind.
//
// The page function stands in for a broken fence — it keeps producing a task the
// sweep cannot get rid of.

// TestAgentTaskQueueOwnerFKsStillExist guards the precondition the write fence
// leans on. Blocking a concurrent enqueue depends on an INSERT taking FOR KEY
// SHARE on the agent / issue / runtime it references, which is a property of these
// foreign keys. The file header calls the legacy FKs an expand-phase safety net,
// so if a later migration removes them this test is what turns a silently missing
// fence into a red build — the replacement has to be an explicit protocol shared
// with every enqueue path.

// TestDeleteWorkspaceTasks_FencesConcurrentEnqueueAndReassignment covers the two
// races a per-batch sweep would otherwise lose:
//
//   - enqueue after the sweep: a task inserted for an already-swept owner would
//     be left behind with its own leaf rows;
//   - reassignment during the sweep: a task moved to another workspace's runtime
//     between read and delete would be deleted on a stale ownership claim.
//
// Both are closed by locks the sweep takes, so the assertion is that a concurrent
// writer is blocked (and hits its own lock_timeout) while teardown holds them.
