package handler

import (
	"context"
	"testing"
	"time"
)

// The comment-list endpoint shares ListCommentsForIssue with the timeline, so the
// newest-N window (MUL-5492) can cut a thread in half here too: an old root
// outside the window, a fresh reply inside it.
//
// Two separate properties are at stake:
//
//   - PARENT-CHAIN CLOSURE makes a reply renderable.
//   - THREAD COMPLETENESS is what foldResolvedThreads needs. Closure does NOT
//     provide it: older siblings and descendants of a retained reply stay outside
//     the window.
//
// completeCommentThreads now handles both: it restores a whole affected thread
// within explicit budgets or drops that thread as one unit. A completed default
// window is therefore safe to fold even though the endpoint still advertises
// that unrelated older comments were truncated.

// seedCommentRow inserts one comment at an exact timestamp and returns its id.
// resolvedAt marks it as the thread's resolution when non-nil.
func seedCommentRow(t *testing.T, issueID string, at time.Time, content string, parentID *string, resolvedAt *time.Time) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, parent_id, resolved_at, resolved_by_type, resolved_by_id)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5, $6, $7,
		        CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE 'member' END,
		        CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE $3::uuid END)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, at, parentID, resolvedAt).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment %q: %v", content, err)
	}
	return id
}

// assertNoOrphanCommentRows pins the invariant the UI depends on: every returned
// reply's parent is in the same response.
func assertNoOrphanCommentRows(t *testing.T, rows []CommentResponse) {
	t.Helper()
	present := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		present[c.ID] = struct{}{}
	}
	for _, c := range rows {
		if c.ParentID == nil || *c.ParentID == "" {
			continue
		}
		if _, ok := present[*c.ParentID]; !ok {
			t.Errorf("comment %s references parent %s absent from the response", c.ID, *c.ParentID)
		}
	}
}

// TestListComments_ExactlyAtCapStillFolds pins the probe read. An issue holding
// exactly commentHardCap comments is complete, so the fold must still run —
// inferring truncation from the row count would silently stop folding here.

// TestListComments_TruncatedCompleteThreadStillFolds covers the review finding. The
// resolution reply is inside the window while older ordinary replies on the same
// thread are outside it. completeCommentThreads restores those replies before
// folding, so folded_count reflects the complete thread rather than a partial
// window.

// TestListComments_RootResolvedTruncatedUsesCompleteFoldedCount is the
// root-resolved variant: completion restores all six replies before folding.

// TestListComments_RootsOnlyHardCapKeepsNewestRoots pins the roots-only window:
// the API must not retain the oldest commentHardCap roots and silently lose new
// discussion.


// TestListComments_UntailedThreadHardCapKeepsNewestReplies proves that a plain
// --thread read cannot lose the newest resolution reply. The returned thread is
// partial, so fold must remain suppressed even though the resolution is visible.


// TestListComments_DeepChainBeyondBudgetIsPrunedNotOrphaned exercises the depth
// budget. A chain deeper than commentThreadMaxDepth cannot be closed, so the
// affected reply is dropped rather than returned as an unrenderable orphan. The
// response must stay bounded and must terminate.

// TestListComments_SharedAncestorFetchedOnce checks the dedup in the walk: many
// replies pointing at one old root must not duplicate that root.

// TestListComments_CrossIssueParentNeverCrossesBoundary is the negative tenant
// test. parent_id carries a foreign key to comment(id) but NOT to a matching
// issue, so a stray cross-issue parent reference is representable in stored data.
// The walk filters on issue_id and workspace_id at every level, so the foreign
// comment must never appear and the anomalous reply must be pruned.

// TestListComments_CrossWorkspaceParentNeverCrossesBoundary independently pins
// the workspace predicate. The parent deliberately carries the target issue_id
// but a different workspace_id, so retaining issue_id filtering while removing
// workspace_id filtering must still make this test fail.

// TestListComments_CrossWorkspaceDescendantNeverCrossesBoundary covers the
// downward half of completeCommentThreads. The restored root belongs to this
// workspace, but one of its stored children does not; the batched descendant
// query must not pull that child into the response.
