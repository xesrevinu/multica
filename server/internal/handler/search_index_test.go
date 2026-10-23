package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// searchIndexWorkspace creates an isolated workspace so manifest and snapshot
// totals are not affected by rows other tests leave in the shared workspace.
func searchIndexWorkspace(t *testing.T, prefix string) string {
	t.Helper()
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	wsID := dbfx.Workspace(t, "Search index "+token, "search-index-"+token, testutil.Cols{"issue_prefix": prefix})
	// Registered before any fixture row, so it runs after their cleanups and
	// also removes the change rows those deletes log.
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM search_index_change WHERE workspace_id = $1`, wsID)
	})
	return wsID
}

func searchIndexRequest(method, path, wsID string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Workspace-ID", wsID)
	return req
}

func searchIndexManifest(t *testing.T, wsID string) SearchIndexManifestResponse {
	t.Helper()
	return testutil.Decode[SearchIndexManifestResponse](t, testHandler.GetSearchIndexManifest,
		searchIndexRequest(http.MethodGet, "/api/search-index/manifest", wsID, nil), http.StatusOK)
}

func searchIndexChanges(t *testing.T, wsID, cursor string, limit int) SearchIndexChangesResponse {
	t.Helper()
	return testutil.Decode[SearchIndexChangesResponse](t, testHandler.ListSearchIndexChanges,
		searchIndexRequest(http.MethodPost, "/api/search-index/changes", wsID, map[string]any{"cursor": cursor, "limit": limit}),
		http.StatusOK)
}

func issueIDs(issues []SearchIndexIssue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.ID)
	}
	sort.Strings(out)
	return out
}

func commentIDs(comments []SearchIndexComment) []string {
	out := make([]string, 0, len(comments))
	for _, c := range comments {
		out = append(out, c.ID)
	}
	sort.Strings(out)
	return out
}

func sortedIDs(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func assertIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(sortedIDs(want...)) {
		t.Fatalf("%s = %v, want %v", label, got, sortedIDs(want...))
	}
}




// A writer that took its xid before the reader's snapshot but committed after
// it must still be delivered by the next catch-up. A watermark such as
// updated_at or the snapshot xmin alone would skip or stall on it.







func TestSearchIndexChangesLegacyPGSnapshotCursorReturns410(t *testing.T) {
	wsID := searchIndexWorkspace(t, "SI")
	legacyCursor := encodeSearchIndexCursor(searchIndexCursor{
		Since: "100:100:",
	})
	testutil.Call(t, testHandler.ListSearchIndexChanges,
		searchIndexRequest(http.MethodPost, "/api/search-index/changes", wsID, map[string]any{"cursor": legacyCursor}),
	).Want(http.StatusGone)

	manifest := searchIndexManifest(t, wsID)
	issueID := dbfx.Issue(t, "Indexed after manifest", testutil.Cols{"workspace_id": wsID})
	changes := searchIndexChanges(t, wsID, manifest.Cursor, 100)
	assertIDs(t, "changed issues", issueIDs(changes.Issues), []string{issueID})
}
