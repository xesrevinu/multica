package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Expand step for duplicate marks (MUL-7349). Nothing writes
// duplicate_of_issue_id in this release, so these tests seed it the way the
// release that enables marking will write it, and check that this server keeps
// it correct: a reopened issue loses its mark, cancelling it again does not
// bring the mark back, and deleting an original clears the marks pointing at
// it. That is what makes enabling marks safe to roll back to this release.

func duplicateState(t *testing.T, issueID string) (status string, duplicateOf *string, revision int64) {
	t.Helper()
	dbfx.QueryRow(t, `SELECT status, duplicate_of_issue_id::text, revision FROM issue WHERE id = $1`, issueID).
		Scan(&status, &duplicateOf, &revision)
	return status, duplicateOf, revision
}

func updateIssueRequest(issueID string, body map[string]any) *http.Request {
	return withURLParam(newRequest("PUT", "/api/issues/"+issueID, body), "id", issueID)
}

// seedDuplicate creates a cancelled issue already marked as a duplicate of
// originalID, standing in for a mark written by a later release.
func seedDuplicate(t *testing.T, title, originalID string) string {
	t.Helper()
	return dbfx.Issue(t, title, testutil.Cols{
		"status":                "cancelled",
		"duplicate_of_issue_id": testutil.Raw("'" + originalID + "'::uuid"),
	})
}


// The sequence a rollback has to survive: this release reopens a marked issue
// and then cancels it again. The mark must not come back.




// An ordinary update that waits behind a transaction which cleared the mark
// must not write the mark back. UpdateIssue computes its next values in a CTE,
// so without the row lock there it resumes with the values it read before the
// wait — an unrelated priority or status edit would restore a mark the other
// transaction had just removed.

func concurrentWriteAfterPointerCleared(t *testing.T, deleteOriginal bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	original := dbfx.Issue(t, "maint-race-original")
	duplicate := seedDuplicate(t, "maint-race-duplicate", original)

	// Hold the clearing transaction open so the update below has to wait for
	// the row lock. That makes the interleaving deterministic instead of racy.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	q := testHandler.Queries.WithTx(tx)

	body := map[string]any{"priority": "high"}
	if deleteOriginal {
		if _, err := q.LockIssueForDelete(ctx, db.LockIssueForDeleteParams{
			ID: parseUUID(original), WorkspaceID: parseUUID(testWorkspaceID),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := q.ClearIssueDuplicatesOf(ctx, db.ClearIssueDuplicatesOfParams{
			WorkspaceID: parseUUID(testWorkspaceID), IssueID: nullString(parseUUID(original)),
		}); err != nil {
			t.Fatal(err)
		}
		if err := q.DeleteIssue(ctx, db.DeleteIssueParams{
			ID: parseUUID(original), WorkspaceID: parseUUID(testWorkspaceID),
		}); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: parseUUID(duplicate), WorkspaceID: parseUUID(testWorkspaceID), Status: "todo",
		}); err != nil {
			t.Fatal(err)
		}
		body = map[string]any{"status": "cancelled"}
	}

	done := make(chan *testutil.Response, 1)
	go func() {
		done <- testutil.Call(t, testHandler.UpdateIssue, updateIssueRequest(duplicate, body))
	}()

	waited := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var blocked bool
		if err := testPool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '-- name: UpdateIssue :one%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		response.Want(http.StatusOK)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !waited {
		t.Fatal("the update never waited on the row lock, so the interleaving under test did not happen")
	}

	if status, pointer, _ := duplicateState(t, duplicate); pointer != nil {
		t.Fatalf("a concurrent write restored the cleared mark: status=%s pointer=%s", status, *pointer)
	}
}
