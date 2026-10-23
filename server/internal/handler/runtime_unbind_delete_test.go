package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type recordingRuntimeGoneNotifier struct {
	runtimeIDs []string
}

func (n *recordingRuntimeGoneNotifier) NotifyRuntimeGone(runtimeID string) {
	n.runtimeIDs = append(n.runtimeIDs, runtimeID)
}

// parseExpectedActiveAgentIDs is the cascade endpoint's input validator.
// Empty list is a valid plan ("no active agents" — cascade just deletes the
// runtime); malformed UUIDs must surface as 400 so a bug in the front-end
// can't silently dilute the plan check.

// activeAgentSetMatches drives the runtime_delete_plan_changed branch: it
// must report mismatch for any divergence — extra agent, missing agent, or
// substituted agent — and accept order-insensitive set equality.

func uuidFromString(s string) (string, error) {
	return s, nil
}

// TestDeleteAgentRuntime_StructuredConflict covers the new 409 shape: the
// strict DELETE refuses with `runtime_has_active_agents` and the body carries
// the live active-agent list so the front-end can pivot to the cascade dialog
// without a second round-trip.






// TestUnbindAgentsAndDeleteRuntime_HappyPath exercises the confirmed endpoint
// end-to-end: with the correct expected_active_agent_ids snapshot, it must
// unbind the active agent, delete the runtime row, and respond 200 with the
// counts.



// TestUnbindAgentsAndDeleteRuntime_PlanChanged proves the dialog-confirm
// race guard: if the user's snapshot of active agents drifts from the live
// set (somebody added or archived an agent while the dialog was open), the
// cascade endpoint must refuse with 409 + runtime_delete_plan_changed and
// surface the new live snapshot so the dialog can re-prompt.

// createCascadeFixtureRuntime creates a fresh runtime owned by testUserID
// inside testWorkspaceID and registers cleanup. Each cascade test uses its
// own runtime so the destructive paths don't trample the shared fixture.
func createCascadeFixtureRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'cascade-test', 'online', $3, '{}'::jsonb, $4, now())
		RETURNING id
	`, testWorkspaceID, name, name+" device", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert cascade fixture runtime: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup. The cascade endpoint deletes the runtime;
		// these statements only matter when the test failed before the
		// cascade ran.
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createProfileBackedRuntime(t *testing.T, ctx context.Context, name string) (string, string) {
	t.Helper()
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_profile (
			workspace_id, display_name, protocol_family, command_name,
			fixed_args, visibility, created_by, enabled
		)
		VALUES ($1, $2, 'codex', 'custom-codex', '[]'::jsonb, 'workspace', $3, true)
		RETURNING id
	`, testWorkspaceID, name+" Profile", testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert runtime profile: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, profile_id, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'codex', 'online', $4, '{}'::jsonb, $5, $6, now())
		RETURNING id
	`, testWorkspaceID, "daemon-"+profileID, name, name+" device", testUserID, profileID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert profile-backed runtime: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})
	return runtimeID, profileID
}

func createCascadeFixtureAgent(t *testing.T, ctx context.Context, runtimeID, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert cascade fixture agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createArchivedWorkspaceMismatchedAgent(t *testing.T, runtimeID, name string) string {
	t.Helper()
	foreignWorkspaceID := dbfx.Workspace(t, name+" Workspace", "runtime-mismatch-"+runtimeID[:8])
	return dbfx.Agent(t, name, runtimeID, testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"archived_at":  testutil.Raw("now()"),
		"owner_id":     nil,
	})
}
