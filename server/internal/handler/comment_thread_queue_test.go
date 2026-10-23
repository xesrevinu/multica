package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)


func TestDeferredCommentThreadsPromoteIndependently(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "thread promotion runtime")
	agentID := dbfx.Agent(t, "thread promotion agent", runtimeID)
	issueID := dbfx.Issue(t, "thread promotion issue")
	rootA := dbfx.Comment(t, issueID, "A")
	rootB := dbfx.Comment(t, issueID, "B")
	create := func(root string) string {
		return dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root, "status": "deferred", "fire_at": testutil.Raw("now()-interval '1 minute'")})
	}
	a, b := create(rootA), create(rootB)
	anotherA := create(rootA)
	promoted, err := testHandler.Queries.PromoteDueDeferredTasksForRuntime(ctx, db.PromoteDueDeferredTasksForRuntimeParams{RuntimeID: nullString(parseUUID(runtimeID)), RuntimeStaleSecs: nullSeconds(120)})
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 2 {
		t.Fatalf("promoted %d runs, want one per thread", len(promoted))
	}
	for _, id := range []string{a, b} {
		row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(id))
		if err != nil || row.Status != "queued" {
			t.Fatalf("thread did not promote: %s / %v", row.Status, err)
		}
	}
	row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(anotherA))
	if err != nil || row.Status != "deferred" {
		t.Fatalf("second run in A must stay deferred: %s / %v", row.Status, err)
	}
}
