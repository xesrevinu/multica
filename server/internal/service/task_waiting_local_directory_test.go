package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMarkTaskWaitingLocalDirectoryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fx := testutil.New(sharedTestPool(t), "", "")
	suffix := time.Now().UnixNano()
	fx.UserID = fx.User(t, "Wait local directory", fmt.Sprintf("wait-local-%d@multica.test", suffix))
	fx.WorkspaceID = fx.Workspace(t, "Wait local directory", fmt.Sprintf("wait-local-%d", suffix))
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Wait local directory runtime")
	agentID := fx.Agent(t, "Wait local directory agent", runtimeID)
	issueID := fx.Issue(t, "Wait local directory issue")
	taskID := fx.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
		"status":     "dispatched",
	})

	svc := NewTaskService(db.New(fx.Pool), fx.Pool, nil, events.New())

	first, err := svc.MarkTaskWaitingLocalDirectory(ctx, util.MustParseUUID(taskID), "holt")
	if err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if first.Status != "waiting_local_directory" {
		t.Fatalf("first status = %q", first.Status)
	}
	if first.WaitReason.String != "holt" {
		t.Fatalf("first reason = %q", first.WaitReason.String)
	}

	second, err := svc.MarkTaskWaitingLocalDirectory(ctx, util.MustParseUUID(taskID), "holt (held by task a1b2c3d4)")
	if err != nil {
		t.Fatalf("re-mark of an already-waiting row: %v", err)
	}
	if second.Status != "waiting_local_directory" {
		t.Fatalf("second status = %q", second.Status)
	}
	if second.WaitReason.String != "holt (held by task a1b2c3d4)" {
		t.Fatalf("second reason = %q, want the refreshed hint", second.WaitReason.String)
	}

	runningID := fx.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
		"status":     "running",
	})
	if _, err := svc.MarkTaskWaitingLocalDirectory(ctx, util.MustParseUUID(runningID), "holt"); err == nil {
		t.Fatal("marking a running task as waiting must fail")
	}
}
