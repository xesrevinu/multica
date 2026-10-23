package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// setWorkspaceDeleteLockTimeoutForTest is a package-private hook used only by
// the tests below to keep them fast.
func setWorkspaceDeleteLockTimeoutForTest(t *testing.T, v time.Duration) {
	t.Helper()
	workspaceDeleteLockTimeoutOverride = v
	t.Cleanup(func() { workspaceDeleteLockTimeoutOverride = 0 })
}

// TestDeleteWorkspace_FailsFastWhenRollupLockHeld is the regression test for
// MUL-5983: "clicking delete workspace does nothing".
//
// The teardown transaction takes global advisory lock 4246 so the
// task_usage_hourly rollup cannot write aggregates for a workspace that is
// being torn down. That lock is batch-held — a rollup tick, a backfill run, or
// (before migration 272) a cancelled tick that leaked it — and nothing above
// this handler has a deadline: the desktop client's fetch has no timeout, so
// an unbounded wait leaves the delete dialog spinning on "Deleting…" until the
// app is force quit, and the workspace is never deleted.
//
// With SET LOCAL lock_timeout in place the same contention has to surface as a
// bounded, retryable 503 while the workspace stays intact.

// TestDeleteWorkspace_SucceedsWhenRollupLockIsFree is the counterpart: the
// lock timeout must not make an uncontended delete flaky.
func TestDeleteWorkspace_SucceedsWhenRollupLockIsFree(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres workspace delete lock test")
	}
	ctx := context.Background()

	// The teardown itself waits on 4246 under a 500 ms budget, so a rollup
	// test holding that lock in the parallel internal/scheduler binary would
	// turn this "uncontended" delete into a 503. Same guard, same reason.
	lockRollupSingleton(t)

	setWorkspaceDeleteLockTimeoutForTest(t, 500*time.Millisecond)

	const slug = "handler-tests-delete-lock-free"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete Lock Free", slug, "DeleteWorkspace lock timeout test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from an uncontended delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after a successful delete")
	}
}
