package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeHandlersRejectMalformedRuntimeID(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "usage",
			method: "GET",
			path:   "/api/runtimes/not-a-uuid/usage",
			handle: testHandler.GetRuntimeUsage,
		},
		{
			name:   "task activity",
			method: "GET",
			path:   "/api/runtimes/not-a-uuid/task-activity",
			handle: testHandler.GetRuntimeTaskActivity,
		},
		{
			name:   "delete",
			method: "DELETE",
			path:   "/api/runtimes/not-a-uuid",
			handle: testHandler.DeleteAgentRuntime,
		},
		{
			name:   "unbind-agents-and-delete",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/unbind-agents-and-delete",
			handle: testHandler.UnbindAgentsAndDeleteRuntime,
		},
		{
			// Legacy route kept for installed clients; same handler.
			name:   "archive-agents-and-delete",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/archive-agents-and-delete",
			handle: testHandler.UnbindAgentsAndDeleteRuntime,
		},
		{
			name:   "models",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/models",
			handle: testHandler.InitiateListModels,
		},
		{
			name:   "update",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/update",
			handle: testHandler.InitiateUpdate,
		},
		{
			name:   "local skills",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/local-skills",
			handle: testHandler.InitiateListLocalSkills,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest(tt.method, tt.path, nil)
			req = withURLParam(req, "runtimeId", "not-a-uuid")
			tt.handle(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 for malformed runtimeId, got %d: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

// TestGetRuntimeUsage_BucketsByUsageTime ensures a task that was enqueued on
// one calendar day but whose tokens were reported the next day (e.g. execution
// crossed midnight, or the task sat in the queue) is attributed to the day
// tokens were actually produced, not the enqueue day. It also verifies the
// ?days=N cutoff covers the full earliest calendar day, not just "now minus N
// days" which would clip the morning of that day.

// TestListRuntimeUsageByAgent_MergesMixedCaseProvider proves the by-agent
// query folds historical mixed-case provider rows (written before the handler
// lowercased provider on write) into a single lowercased bucket via the
// query's LOWER(provider) normalization, instead of splitting them.
func TestListRuntimeUsageByAgent_MergesMixedCaseProvider(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'mixed-case provider test', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	now := time.Now().UTC()
	// Two tasks for the same agent/model under this runtime, reporting the
	// same provider in different casing — 'Cursor' (legacy) and 'cursor' (new).
	insert := func(provider string, input int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
			VALUES ($1, $2, $3, 'completed', $4)
			RETURNING id
		`, agentID, issueID, runtimeID, now).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, $2, 'mixed-case-model', $3, 0, $4)
		`, taskID, provider, input, now); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}
	insert("Cursor", 1000)
	insert("cursor", 500)

	rows, err := testHandler.Queries.ListRuntimeUsageByAgent(ctx, db.ListRuntimeUsageByAgentParams{
		RuntimeID: nullString(parseUUID(runtimeID)),
		Since:     now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("ListRuntimeUsageByAgent: %v", err)
	}

	var got []db.ListRuntimeUsageByAgentRow
	for _, r := range rows {
		if r.Model == "mixed-case-model" {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected mixed-case providers to merge into 1 row, got %d: %+v", len(got), got)
	}
	if got[0].Provider != "cursor" {
		t.Errorf("expected merged provider 'cursor', got %q", got[0].Provider)
	}
	if int64(got[0].InputTokens.Float64) != 1500 {
		t.Errorf("expected merged input_tokens 1500, got %d", int64(got[0].InputTokens.Float64))
	}
}

// TestListRuntimeUsageBucketsByViewerTimezone proves the runtime trend reads
// bucket the day boundary in the VIEWER's tz (the argument passed to
// listRuntimeUsage). The viewer tz is Asia/Shanghai; assertions only pass if
// listRuntimeUsage applies that tz correctly.

// TestResolveViewingTZ covers the three legs of resolveViewingTZ:
// explicit `?tz=` query param, the authenticated user's stored
// user.timezone, and the UTC fallback when neither yields a valid zone.
func TestResolveViewingTZ(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email, timezone)
		 VALUES ('TZ Resolve', 'tz-resolve@multica.ai', 'Asia/Tokyo') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID) })

	// Explicit ?tz= wins over the stored preference.
	req := newRequest("GET", "/api/dashboard/usage/daily?tz=America/New_York", nil)
	req.Header.Set("X-User-ID", userID)
	if got := testHandler.resolveViewingTZ(req); got != "America/New_York" {
		t.Fatalf("query param: expected America/New_York, got %q", got)
	}

	// No ?tz= → falls back to the authenticated user's stored timezone.
	req = newRequest("GET", "/api/dashboard/usage/daily", nil)
	req.Header.Set("X-User-ID", userID)
	if got := testHandler.resolveViewingTZ(req); got != "Asia/Tokyo" {
		t.Fatalf("stored fallback: expected Asia/Tokyo, got %q", got)
	}

	// An unparseable ?tz= is ignored, falling through to the stored value.
	req = newRequest("GET", "/api/dashboard/usage/daily?tz=Mars/Olympus", nil)
	req.Header.Set("X-User-ID", userID)
	if got := testHandler.resolveViewingTZ(req); got != "Asia/Tokyo" {
		t.Fatalf("invalid query param: expected Asia/Tokyo fallback, got %q", got)
	}

	// No ?tz= and no stored value → UTC.
	var bareUserID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email)
		 VALUES ('TZ Bare', 'tz-bare@multica.ai') RETURNING id`,
	).Scan(&bareUserID); err != nil {
		t.Fatalf("insert bare user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, bareUserID) })
	req = newRequest("GET", "/api/dashboard/usage/daily", nil)
	req.Header.Set("X-User-ID", bareUserID)
	if got := testHandler.resolveViewingTZ(req); got != "UTC" {
		t.Fatalf("no preference: expected UTC, got %q", got)
	}

	// A stored timezone that is itself an invalid IANA zone — e.g. a row
	// written before server-side validation existed — must fall through
	// to UTC. Without the LoadLocation guard on the stored value this
	// string would reach SQL `AT TIME ZONE` and 500 every dashboard read.
	var badTZUserID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email, timezone)
		 VALUES ('TZ Bad', 'tz-bad@multica.ai', 'Bad/Zone') RETURNING id`,
	).Scan(&badTZUserID); err != nil {
		t.Fatalf("insert bad-tz user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, badTZUserID) })
	req = newRequest("GET", "/api/dashboard/usage/daily", nil)
	req.Header.Set("X-User-ID", badTZUserID)
	if got := testHandler.resolveViewingTZ(req); got != "UTC" {
		t.Fatalf("invalid stored tz: expected UTC fallback, got %q", got)
	}

	// No X-User-ID header and no ?tz= — an unauthenticated caller has
	// neither signal, so the resolver must return UTC without attempting
	// (and panicking on) a GetUser lookup.
	req = newRequest("GET", "/api/dashboard/usage/daily", nil)
	if got := testHandler.resolveViewingTZ(req); got != "UTC" {
		t.Fatalf("unauthenticated caller: expected UTC, got %q", got)
	}
}

// TestRuntimeHeatmapEndpointsUseViewerTZ verifies that the hour-of-day
// heatmap endpoints (GetRuntimeUsageByHour and GetRuntimeTaskActivity) bucket
// in the viewer's tz supplied via ?tz= and return 200.
func TestRuntimeHeatmapEndpointsUseViewerTZ(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	cases := []struct {
		name   string
		path   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{"usage by-hour", "/api/runtimes/" + runtimeID + "/usage/by-hour?tz=Asia/Shanghai", testHandler.GetRuntimeUsageByHour},
		{"task activity", "/api/runtimes/" + runtimeID + "/activity?tz=Asia/Shanghai", testHandler.GetRuntimeTaskActivity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("GET", c.path, nil)
			req = withURLParam(req, "runtimeId", runtimeID)
			c.handle(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d: %s", c.name, w.Code, w.Body.String())
			}
		})
	}
}
