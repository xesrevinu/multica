package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// insertCustomStatus adds one custom status directly, returning its id.
func insertCustomStatus(t *testing.T, key, category string, position int, archived bool) string {
	t.Helper()
	var id string
	category, ok := issuestatus.ParseCategory(category)
	if !ok {
		t.Fatalf("invalid fixture category %q", category)
	}
	archivedAt := "NULL"
	if archived {
		archivedAt = "now()"
	}
	err := testPool.QueryRow(context.Background(), fmt.Sprintf(`
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position, archived_at)
		VALUES ($1, $2, $3, '', $4, '#ff0000', $5, %s)
		RETURNING id
	`, archivedAt), testWorkspaceID, key, key, category, position).Scan(&id)
	if err != nil {
		t.Fatalf("insert custom status %q: %v", key, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE id = $1`, id)
	})
	return id
}

func reorderVia(t *testing.T, category string, ids []string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ReorderIssueStatuses(rec, newRequest(http.MethodPatch, "/api/issue-statuses/reorder", ReorderIssueStatusesRequest{
		Category: category,
		IDs:      ids,
	}))
	return rec
}

func positionsByID(t *testing.T, ids ...string) map[string]int {
	t.Helper()
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		var position int
		if err := testPool.QueryRow(context.Background(),
			`SELECT position FROM issue_status WHERE id = $1`, id).Scan(&position); err != nil {
			t.Fatalf("read position for %s: %v", id, err)
		}
		out[id] = position
	}
	return out
}


// The regression Elon caught: with "show archived" on, the client used to hand
// the whole rendered order — archived rows included — to a PATCH-per-row loop.
// The archived row rejected AFTER the rows before it had already been written,
// so the user saw a failure toast over a database that had partly reordered.


// TestReorderIssueStatusesSerializesAgainstConcurrentArchive is the sharp
// counterexample for "atomic".
//
// It reproduces the real interleaving rather than a sequential one: the reorder
// must be IN FLIGHT and parked on the catalog lock when the archive commits.
// A test that archives first and then calls reorder proves nothing — the
// active-set check would catch that even with the lock deleted, because the
// archive is already visible by the time the handler reads anything.
//
// Without the LockIssueStatusCatalog call, the reorder
// reads the catalog before the archive commits, sees both rows active, and
// commits a reorder that includes a row archived underneath it.

// A payload that names only SOME of a category's active custom statuses cannot
// be applied: positions come from the array index, so reordering a subset
// writes positions that collide with the rows left out of it.

