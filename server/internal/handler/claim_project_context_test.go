package handler

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The claim path resolves project context from a SOFT reference: issue.project_id,
// chat_session.project_id and the quick-create context JSONB are all plain
// columns with no foreign key behind them (see the repo's no-FK rule). These
// tests pin the tenant boundary that resolveClaimProjectContext is responsible
// for, using the only states that can actually produce a cross-tenant read:
// a dangling reference to a project in another workspace, and a project_resource
// row whose own workspace_id disagrees with its project's.
//
// Both were reachable before MUL-6547: the issue and quick-create branches read
// the project with GetProject (no workspace predicate) and every branch listed
// resources by project_id alone.

// foreignWorkspaceWithProject builds a complete project in a DIFFERENT workspace
// and returns (workspaceID, projectID). The caller points a same-workspace task
// at projectID to simulate the corrupt reference.
func foreignWorkspaceWithProject(t *testing.T, slug, prefix string) (string, string) {
	t.Helper()

	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Foreign " + slug,
		"slug":         slug,
		"description":  "",
		"issue_prefix": prefix,
	})
	foreignProjectID := dbfx.Project(t, foreignProjectTitle, testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"description":  foreignProjectDescription,
	})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    foreignProjectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"` + foreignRepoURL + `"}`,
		"position":      0,
	})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    foreignProjectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref":  `{"daemon_id":"foreign-daemon","local_path":"` + foreignLocalPath + `","execution_mode":"in_place"}`,
		"position":      1,
	})
	return foreignWorkspaceID, foreignProjectID
}

const (
	foreignProjectTitle       = "Foreign project must not leak"
	foreignProjectDescription = "Foreign project instructions must not leak"
	foreignRepoURL            = "https://github.com/example/foreign-tenant-repo"
	foreignLocalPath          = "/srv/foreign-tenant-project"
	localFallbackRepoURL      = "https://github.com/example/local-workspace-fallback"
)

// assertNoForeignContext fails when any part of the foreign tenant's project
// reached the claim response, on any field the daemon forwards to the agent.
func assertNoForeignContext(t *testing.T, body string, extra ...string) {
	t.Helper()
	for _, leaked := range append([]string{
		foreignProjectTitle,
		foreignProjectDescription,
		foreignRepoURL,
		foreignLocalPath,
	}, extra...) {
		if leaked == "" {
			continue
		}
		if strings.Contains(body, leaked) {
			t.Errorf("claim response leaked foreign tenant context %q: %s", leaked, body)
		}
	}
}

type claimProjectFields struct {
	WorkspaceID        string                `json:"workspace_id"`
	Repos              []RepoData            `json:"repos"`
	ProjectID          string                `json:"project_id"`
	ProjectTitle       string                `json:"project_title"`
	ProjectDescription string                `json:"project_description"`
	ProjectResources   []ProjectResourceData `json:"project_resources"`
}

// An issue whose project_id points at another workspace's project must degrade
// to workspace context. Before MUL-6547 the issue branch used GetProject, which
// has no workspace predicate, so the foreign project's title, description,
// repository URL and local path were all serialized into the claim.

// The quick-create branch resolves its project from the task context JSONB
// rather than an issue row, and used the same unscoped GetProject. It needs its
// own assertion: a requester who edits the stored project_id must not be able to
// pull another tenant's resources into the claim.

// A project_resource row carries its own workspace_id, so a resource can
// disagree with the project it hangs off. Listing by project_id alone — which
// every claim branch did before MUL-6547 — serialized that row anyway. The
// in-workspace resource on the same project must still come through, proving the
// new predicate filters rather than just emptying the list.

// A run_only autopilot owns its task's workspace. taskToResponse seeds
// resp.WorkspaceID with the RUNTIME's workspace, and the branch used to widen it
// only `if resp.WorkspaceID == ""` — a condition that never held, so the
// isolation check compared the runtime workspace against itself and a
// cross-workspace autopilot_run_id was dispatched instead of rejected.

// failQueryDBTX delegates every query to the real pool except the ones whose
// SQL contains failOn, which fail with a transient-looking error. db.Queries is
// built on the DBTX interface, so this is the seam that lets a single source
// read fail while the rest of the claim proceeds normally — the shape of a real
// DB blip, which a globally broken pool cannot reproduce (it trips the earlier
// agent-load guard instead).
type failQueryDBTX struct {
	db.DBTX
	failOn string
	err    error
}

func (f failQueryDBTX) QueryRowContext(ctx context.Context, sqlQuery string, args ...any) *sql.Row {
	if strings.Contains(sqlQuery, f.failOn) {
		return errSQLRow(f.err)
	}
	return f.DBTX.QueryRowContext(ctx, sqlQuery, args...)
}

func (f failQueryDBTX) QueryContext(ctx context.Context, sqlQuery string, args ...any) (*sql.Rows, error) {
	if strings.Contains(sqlQuery, f.failOn) {
		return nil, f.err
	}
	return f.DBTX.QueryContext(ctx, sqlQuery, args...)
}

// A claim resolves its SOURCE row — issue, chat session, autopilot_run,
// autopilot — before anything else. Those reads used to be written as
// `if x, err := ...; err == nil {`, with no else: a failed read silently
// skipped the whole branch. taskToResponse has already seeded resp.WorkspaceID
// with the RUNTIME's workspace by then, so the skipped claim sailed through the
// builder's backstop isolation check (which compared that seed against itself)
// and dispatched an agent with no issue, no chat input, or no autopilot
// instructions at all.
//
// A transient read failure must instead produce NO claim payload and preserve
// the task for the stale-dispatched reclaim — cancelling would destroy a valid
// task over a momentary DB error.

// The other half of the split: a source row that is GONE cannot be retried into
// existence, so it settles terminally instead of spinning in the reclaim loop.
// Legacy FKs keep this state from arising today (agent_task_queue.issue_id ON
// DELETE CASCADE and friends), so the branch is exercised directly rather than
// through a claim.
func TestRejectClaimSourceLoad_MissingRow_CancelsTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": nil})
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched' WHERE id = $1`, taskID)
	task := db.AgentTaskQueue{ID: parseUUID(taskID), AgentID: parseUUID(agentID), Status: "dispatched"}

	failure := testHandler.rejectClaimSourceLoad(
		context.Background(), &task, sql.ErrNoRows, "issue", uuidToString(task.IssueID.String))
	if failure == nil {
		t.Fatal("expected a failure for a missing source row")
	}
	if failure.outcome != "error_source_missing" {
		t.Errorf("outcome = %q, want error_source_missing", failure.outcome)
	}
	if failure.message != "task is missing its issue" {
		t.Errorf("message = %q, want it to name the missing source", failure.message)
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Errorf("task status = %q, want cancelled for an unrecoverable source reference", status)
	}
}
