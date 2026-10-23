package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type webhookTaskReadTrace struct {
	db.DBTX
	reads   int
	maxRows int64
}

func (tr *webhookTaskReadTrace) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "FROM agent_task_queue") {
		tr.reads++
		tr.maxRows = max(tr.maxRows, 1)
	}
	return tr.DBTX.QueryRowContext(ctx, query, args...)
}

func (tr *webhookTaskReadTrace) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM agent_task_queue") {
		tr.reads++
	}
	return tr.DBTX.QueryContext(ctx, query, args...)
}

func TestWebhookRecoveryTaskExistenceReadIsBounded(t *testing.T) {
	ctx := context.Background()
	pool := sharedTestPool(t)
	tr := &webhookTaskReadTrace{DBTX: pool}
	fx := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	fx.UserID = fx.User(t, "webhook reader", fmt.Sprintf("webhook-reader-%d@example.test", suffix))
	fx.WorkspaceID = fx.Workspace(t, "webhook reader", fmt.Sprintf("webhook-reader-%d", suffix))
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "webhook reader")
	agentID := fx.Agent(t, "webhook reader", runtimeID)
	q := db.New(tr)
	svc := &AutopilotService{Queries: q}
	largeJSON := fmt.Sprintf(`{"large":%q}`, strings.Repeat("x", 65536))
	for _, status := range []string{"completed", "failed", "cancelled", "queued", "deferred"} {
		t.Run(status, func(t *testing.T) {
			issueID := fx.Issue(t, "already handed off")
			count := 64
			if status == "queued" || status == "deferred" {
				count = 1
			}
			for i := 0; i < count; i++ {
				taskStatus := status
				fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": taskStatus, "context": largeJSON, "result": largeJSON})
			}
			tr.reads = 0
			tr.maxRows = 0
			if err := svc.ensureWebhookCreateIssueTask(ctx, db.Autopilot{}, db.AutopilotRun{IssueID: nullString(util.MustParseUUID(issueID))}); err != nil {
				t.Fatal(err)
			}
			if tr.reads != 1 || tr.maxRows > 1 {
				t.Fatalf("task inspection transferred %d rows across %d reads", tr.maxRows, tr.reads)
			}
		})
	}
	missing := util.MustParseUUID("ffffffff-ffff-ffff-ffff-fffffffffff1")
	if err := svc.ensureWebhookCreateIssueTask(ctx, db.Autopilot{}, db.AutopilotRun{IssueID: nullString(missing)}); err == nil || !strings.Contains(err.Error(), "load linked issue") {
		t.Fatalf("missing issue: %v", err)
	}
	parked := fx.Issue(t, "parked issue", testutil.Cols{"status": "backlog"})
	if err := svc.ensureWebhookCreateIssueTask(ctx, db.Autopilot{}, db.AutopilotRun{IssueID: nullString(util.MustParseUUID(parked))}); err != nil {
		t.Fatal(err)
	}
	runnable := fx.Issue(t, "repair missing task", testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agentID})
	svc.TaskSvc = NewTaskService(q, pool, nil, events.New())
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", runnable)
	for range 2 {
		if err := svc.ensureWebhookCreateIssueTask(ctx, db.Autopilot{}, db.AutopilotRun{IssueID: nullString(util.MustParseUUID(runnable))}); err != nil {
			t.Fatal(err)
		}
	}
	var taskCount int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", runnable).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("repair/replay produced %d tasks, want one", taskCount)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := svc.ensureWebhookCreateIssueTask(cancelled, db.Autopilot{}, db.AutopilotRun{IssueID: nullString(util.MustParseUUID(parked))}); err == nil || !strings.Contains(err.Error(), "inspect issue tasks") {
		t.Fatalf("query failure: %v", err)
	}
}
