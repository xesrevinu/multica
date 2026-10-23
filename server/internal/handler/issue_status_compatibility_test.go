package handler

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These are the installed Web/Desktop consumer's literal accepted buckets,
// deliberately independent of the server's current category constants.
var installedStatusBuckets = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}

// Historical storage matrices are exercised by TestStatusCategoryContractUpgradePaths.
// API assertions here run against the fully migrated production schema.

func TestStatusCompatibilityCatalogWritesAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	key := "legacy_" + uuid.NewString()[:8]
	id := dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": testWorkspaceID, "key": key, "name": key, "category": "started", "color": "#123456", "position": 99999})
	// Position allocation and old API inputs share the contracted lifecycle.
	created, err := testHandler.Queries.CreateIssueStatusEntry(ctx, db.CreateIssueStatusEntryParams{
		WorkspaceID: parseUUID(testWorkspaceID), Key: key + "_new", Name: "New", Category: "started", Color: "#123456",
	})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, "DELETE FROM issue_status WHERE id=$1", created.ID)
	if created.Position != 100000 {
		t.Fatalf("new position=%v, want 100000", created.Position)
	}
	catalog, err := testHandler.Queries.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, entry := range catalog {
		if entry.Category == "started" && entry.IsSystem == 0 {
			ids = append(ids, uuid.MustParse(entry.ID).String())
		}
	}
	testutil.Call(t, testHandler.ReorderIssueStatuses, newRequest(http.MethodPost, "/api/issue-statuses/reorder", ReorderIssueStatusesRequest{Category: "in_review", IDs: ids})).Want(http.StatusOK)
	var stored string
	dbfx.QueryRow(t, "SELECT category FROM issue_status WHERE id=$1", id).Scan(&stored)
	if stored != "started" {
		t.Fatalf("reorder changed lifecycle category %s", stored)
	}
	foreign := dbfx.Workspace(t, "Other", "compat-"+uuid.NewString()[:8])
	foreignKey := "foreign_" + uuid.NewString()[:8]
	dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": foreign, "key": foreignKey, "name": "Foreign", "category": "closed", "color": "#123456"})
	keys, err := issuestatus.ExpandCategories(ctx, testHandler.Queries, parseUUID(testWorkspaceID), []string{"closed"})
	if err != nil || slices.Contains(keys, foreignKey) {
		t.Fatalf("cross-workspace filter expansion: %v, %v", keys, err)
	}
	if got := issuestatus.Effective(ctx, testHandler.Queries, parseUUID(testWorkspaceID), foreignKey); got != foreignKey {
		t.Fatalf("cross-workspace terminal behavior: %s", got)
	}
}

func TestStatusCompatibilityGCAfterContract(t *testing.T) {
	for _, old := range installedStatusBuckets {
		category, _ := issuestatus.CategoryForBehavior(old)
		for _, stored := range []string{category} {
			t.Run(old+"/"+stored, func(t *testing.T) {
				key := "gc_" + uuid.NewString()[:8]
				dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": testWorkspaceID, "key": key, "name": key, "category": stored, "color": "#123456"})
				id := dbfx.Issue(t, key, testutil.Cols{"status": key})
				type result struct {
					Status   string `json:"status"`
					Category string `json:"category"`
				}
				check := func(got result) {
					t.Helper()
					if got.Status != issuestatus.WireCategory(key, category) || got.Category != category {
						t.Fatalf("GC wire=%+v for stored %s", got, stored)
					}
				}
				var single result
				req := withURLParam(newDaemonTokenRequest("GET", "/api/daemon/issues/"+id+"/gc-check", nil, testWorkspaceID, "compat-daemon"), "issueId", id)
				testutil.Call(t, testHandler.GetIssueGCCheck, req).Want(http.StatusOK).JSON(&single)
				check(single)
				var batch struct {
					Issues []result `json:"issues"`
				}
				req = withURLParam(newDaemonTokenRequest("POST", "/api/daemon/workspaces/"+testWorkspaceID+"/issues/gc-check", map[string]any{"issue_ids": []string{id}}, testWorkspaceID, "compat-daemon"), "workspaceId", testWorkspaceID)
				testutil.Call(t, testHandler.BatchIssueGCCheck, req).Want(http.StatusOK).JSON(&batch)
				if len(batch.Issues) != 1 {
					t.Fatalf("GC results=%d", len(batch.Issues))
				}
				check(batch.Issues[0])
			})
		}
	}
}

func TestStatusCompatibilityCursorRejectsDifferentCategoryFormat(t *testing.T) {
	project, _ := seedStatusCategoryFixture(t)
	key := "status_category:in_progress"
	request := issueTableRowsRequest{Query: statusCategoryQuery(project), Group: issueTableGroupSpec{Kind: "status_category"}, GroupKey: &key, Page: issueTablePageRequest{Limit: 1}}
	var rows issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", request)).Want(http.StatusOK).JSON(&rows)
	if rows.NextCursor == nil {
		t.Fatal("expected a second legacy page")
	}
	request.Page.Cursor = rows.NextCursor
	request.Group.CategoryFormat = "lifecycle"
	testutil.Call(t, testHandler.ListIssueTableRows, newRequest(http.MethodPost, "/api/issues/table/rows", request)).Want(http.StatusConflict)
}

