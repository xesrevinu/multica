package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

)

// TestSubtreeUnsubscribe_LosesToConcurrentRevoke covers MUL-5483 review round 8,
// finding 2.
//
// The handler validated membership from its own MVCC snapshot and only opened
// the serialized transaction afterwards, while UnsubscribeFromIssueSubtree
// upserts the root tombstone unconditionally. So:
//
//	subtree request reads "still a member"  →  revoke takes the lock, deletes
//	their subscriptions and their member row, commits  →  subtree finally gets
//	the lock and writes a fresh tombstone
//
// leaving an opt-out that outlives the membership. Because a tombstone means
// "this person chose to leave this issue" and is never resurrected by a rule,
// re-inviting them silently inherits it: the delegated rule refuses to
// subscribe them to a tree they never opted out of.
//
// Re-asserting membership inside the transaction, after the lock, is what makes
// the losing request a no-op instead.

func createRaceIssue(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "subtree revoke race",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		dw := httptest.NewRecorder()
		dreq := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		dreq = withURLParam(dreq, "id", issue.ID)
		testHandler.DeleteIssue(dw, dreq)
	})
	return issue.ID
}
