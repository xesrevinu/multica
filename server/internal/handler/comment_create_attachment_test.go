package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"


	"github.com/multica-ai/multica/server/internal/testutil"
)

// A comment and the attachments it was posted with are one database outcome
// (#8296 review). Committing the comment first left a window in which it could
// gain a reply and be deleted into a tombstone, and the link would then bind
// the uploaded objects to that placeholder — invisible in the timeline, and
// missed by the prune's storage cleanup.

// failLinkAttachmentsTxStarter begins real transactions whose
// LinkAttachmentsToComment statement fails, standing in for any failure
// between creating the comment and linking its attachments.
type failLinkAttachmentsTxStarter struct {
	delegate *testutil.Pool
}

type failLinkAttachmentsTx struct {
	*sql.Tx
}

func (s *failLinkAttachmentsTxStarter) BeginTx(ctx context.Context, _ *sql.TxOptions) (handlerTx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failLinkAttachmentsTx{Tx: tx}, nil
}

func (t failLinkAttachmentsTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if strings.Contains(query, "-- name: LinkAttachmentsToComment") {
		return nil, errors.New("injected attachment link failure")
	}
	return t.Tx.ExecContext(ctx, query, args...)
}

func createCommentWithAttachment(t *testing.T, h *Handler, issueID, attachmentID string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":        "see the screenshot",
		"attachment_ids": []string{attachmentID},
	})
	return testutil.Call(t, h.CreateComment, withURLParam(req, "id", issueID))
}

func unlinkedIssueAttachment(t *testing.T, issueID string) string {
	t.Helper()
	return dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "shot.png", "url": "https://example.test/shot.png",
		"content_type": "image/png", "size_bytes": 1,
	})
}


// A link that fails takes the comment with it: no comment may exist whose
// attachments were never bound.

// Every mutation here takes its owners first — the issue, then the comment,
// then the attachment. A comment created with an attachment takes the issue in
// its CreateComment statement and only then locks the attachment rows, so it
// cannot close a cycle with the two writers that reach an attachment through
// its issue: DeleteAttachment (issue, then the row) and issue teardown (issue,
// then the rows its cascade removes). The holders below play each of those one
// statement at a time.
// waitForBlockedQuery waits until the request under test is blocked on a row
// lock and returns which of names it is waiting in. Unlike waiting for one
// specific statement, this lets the caller carry on and prove what the wrong
// lock order actually costs: the holder's next statement deadlocking.
func waitForBlockedQuery(t *testing.T, done <-chan error, names ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("request completed before it blocked on any row lock: %v", err)
		default:
		}
		for _, name := range names {
			var waiting bool
			if err := testPool.QueryRow(context.Background(), `
				SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname = current_database()
					  AND wait_event_type = 'Lock'
					  AND query LIKE '%' || $1 || '%'
				)
			`, "-- name: "+name+" :").Scan(&waiting); err != nil {
				t.Fatalf("observe blocked %s: %v", name, err)
			}
			if waiting {
				return name
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request blocked on none of %v", names)
	return ""
}


// The symmetric side: issue teardown holds the issue and then reaches the
// attachment through the issue_id cascade. A create that took the attachment
// first would deadlock with it; taking the issue first means the create simply
// waits, and then finds the issue gone.

// DeleteAttachment itself must take the issue before the attachment row, so it
// queues behind issue teardown instead of deadlocking with it.
