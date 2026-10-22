package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

type wakeupTaskLockProbe struct {
	TxStarter
	beforeTaskLock func()
}

func (s wakeupTaskLockProbe) BeginTx(ctx context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
	tx, err := s.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

type wakeupProbeTx struct {
	*sql.Tx
	beforeTaskLock func()
}

func (tx wakeupProbeTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "-- name: FindPendingWakeupTask") {
		tx.beforeTaskLock()
	}
	return tx.Tx.QueryRowContext(ctx, query, args...)
}

func TestIssueWakeupTaskLockWaitDoesNotHoldReceipt(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "check"})
	f.Comment(t, util.UUIDToString(issue), "first")
	wakeDispatch(t, s, w)
	f.Comment(t, util.UUIDToString(issue), "pending")
	checked := false
	s.Tasks.TxStarter = wakeupTaskLockProbe{TxStarter: f.Pool, beforeTaskLock: func() {
		checked = true
		// Execute while dispatch has reached the task-lock boundary. Before the
		// reorder this blocks on a receipt already locked by dispatch.
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := f.Pool.ExecContext(ctx, `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'concurrent','comment')`, issue, f.WorkspaceID, f.UserID)
		if err != nil {
			t.Errorf("comment capture blocked behind task-lock wait: %v", err)
		}
	}}
	wakeDispatch(t, s, w)
	if !checked {
		t.Fatal("task-lock boundary not exercised")
	}
}
