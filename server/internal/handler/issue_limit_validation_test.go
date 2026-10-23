package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Backs issue #3563 / MUL-2847: ListIssues must validate and clamp the `limit`
// and `offset` query params the same way the sibling endpoints (SearchIssues,
// ListGroupedIssues) already do. Without these guards:
//   - limit=-1  → Postgres rejects the negative LIMIT → HTTP 500
//   - limit=0   → returns 0 rows today, not 500; guarded only for
//     sibling-consistency (treat as "use default")
//   - limit=100000000 → unbounded read in one response
//   - offset=-1 → same 500 from Postgres
//   - non-numeric limit/offset → silently ignored today (this test pins that)
//
// Default is 100 and clamp is 100, matching the upstream issue's suggestion
// and the current default. All cases below must return 200 and a well-formed
// JSON body, never 500.
func TestListIssues_LimitValidation(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Seed three issues in a dedicated project so the test is hermetic and
	// not polluted by other tests' fixtures in the workspace.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Limit Validation %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(title string) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5) RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}
	_ = insertIssue(fmt.Sprintf("limit-val-1-%d", suffix))
	_ = insertIssue(fmt.Sprintf("limit-val-2-%d", suffix))
	_ = insertIssue(fmt.Sprintf("limit-val-3-%d", suffix))

	type listResp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}

	call := func(query string) (int, listResp, string) {
		path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s%s",
			testWorkspaceID, projectID, query)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		var resp listResp
		body := w.Body.String()
		if w.Code == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode list response (q=%q): %v\nbody: %s", query, err, body)
			}
		}
		return w.Code, resp, body
	}

	// Cases that previously 500'd. All must return 200 with a well-formed
	// body. With only 3 seeded rows the clamp and default-fallback are
	// observably indistinguishable here — those behaviors are covered by
	// TestListIssues_LimitClamp below, which seeds 101 rows.
	cases := []struct {
		name  string
		query string
	}{
		{"negative limit falls back to default", "&limit=-1"},
		{"negative offset falls back to 0", "&offset=-1"},
		{"negative limit and offset", "&limit=-1&offset=-1"},
		{"non-numeric limit falls back to default", "&limit=abc"},
		{"non-numeric offset falls back to default", "&offset=abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, body := call(tc.query)
			if code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", code, body)
			}
			// We seeded exactly 3 issues, so any well-formed response must
			// report 3 of them.
			if resp.Total != 3 {
				t.Fatalf("total: want 3, got %d", resp.Total)
			}
		})
	}

	// Sanity: an explicit small limit is honored.
	t.Run("explicit limit below clamp is honored", func(t *testing.T) {
		code, resp, body := call("&limit=1")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if len(resp.Issues) != 1 {
			t.Fatalf("limit=1: want 1 issue, got %d", len(resp.Issues))
		}
		if resp.Total != 3 {
			t.Fatalf("total: want 3, got %d", resp.Total)
		}
	})

	// Sanity: a positive offset is honored and yields the empty tail of the
	// page when it runs past the seeded set.
	t.Run("positive offset is honored", func(t *testing.T) {
		code, resp, body := call("&limit=2&offset=2")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if len(resp.Issues) != 1 {
			t.Fatalf("limit=2 offset=2: want 1 issue (the 3rd of 3), got %d", len(resp.Issues))
		}
		if resp.Total != 3 {
			t.Fatalf("total: want 3, got %d", resp.Total)
		}
	})
}

// TestListIssues_LimitClamp proves the upper-bound clamp on `limit` actually
// fires. TestListIssues_LimitValidation above seeds only 3 rows, so a missing
// clamp would still return 3 rows for `limit=100000000` and the assertion
// would pass. Here we seed 101 rows and pin the clamp at exactly 100, the
// same boundary the production endpoint promises.
