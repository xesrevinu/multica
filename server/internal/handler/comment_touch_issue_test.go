package handler

import (
	"context"
	"testing"
	"time"

)

// TestCreateComment_BumpsIssueActivity pins MUL-5009 and MUL-6343: a new
// comment advances both the legacy updated_at clock and the semantic activity
// clock in the same statement.

// TestCreateComment_WorkspaceMismatchPersistsNothing pins the tenant-integrity
// guarantee of the CreateComment CTE (MUL-5009 nit2): CreateComment is the
// single carrier of "a comment always belongs to an issue in the same
// workspace and always bumps it". If the passed workspace does not match the
// target issue's workspace, the leading UPDATE matches no issue row, the
// dependent INSERT selects nothing, and the :one query returns sql.ErrNoRows —
// so no mis-attributed comment is written and the issue is not touched.



// A conditional update that loses after waiting for the comment row must not
// advance the parent issue. The old sibling CTEs could run the issue UPDATE
// from a stale target snapshot even when the comment UPDATE later matched no
// rows, producing phantom activity and a phantom owner revision.

// A delete that loses after waiting for the same comment row likewise must not
// mutate the issue. The issue touch runs in the delete transaction only after
// the comment lock was won, so a lost race rolls back without touching it.

// Issue teardown locks the owner and then cascades into comments. A comment
// mutation must wait for that owner lock before taking the child lock; taking
// them in the opposite order would deadlock when teardown reaches the cascade.

func waitForCommentMutationLock(t *testing.T, queryName string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("%s completed before the competing row lock was released: %v", queryName, err)
		default:
		}
		var waiting bool
		if err := testPool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%' || $1 || '%'
			)
		`, "-- name: "+queryName+" :").Scan(&waiting); err != nil {
			t.Fatalf("observe blocked %s: %v", queryName, err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not block on the competing comment lock", queryName)
}

func assertIssueActivityState(t *testing.T, issueID string, wantRevision int64, wantActivity time.Time) {
	t.Helper()
	var revision int64
	var activity time.Time
	dbfx.QueryRow(t, `SELECT revision, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&revision, &activity)
	if revision != wantRevision || !activity.Equal(wantActivity) {
		t.Fatalf("issue activity = (revision %d, %s), want (%d, %s)", revision, activity, wantRevision, wantActivity)
	}
}
