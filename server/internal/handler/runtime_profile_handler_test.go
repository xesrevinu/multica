package handler

import (
	"context"
	"testing"

)

// insertRuntimeProfileFixture creates a runtime_profile in testWorkspaceID and
// returns its id, registering cleanup.
func insertRuntimeProfileFixture(t *testing.T, ctx context.Context, displayName, protocolFamily, commandName string) string {
	t.Helper()
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_profile (workspace_id, display_name, protocol_family, command_name, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, displayName, protocolFamily, commandName, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert runtime_profile fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})
	return profileID
}

// insertProfileRuntimeFixture creates an agent_runtime instance bound to the
// given profile (so profile_id is set), returning its id.
func insertProfileRuntimeFixture(t *testing.T, ctx context.Context, profileID, name, provider string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, profile_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'local', $3, 'online', $4, '{}'::jsonb, $5, $6, now())
		RETURNING id
	`, testWorkspaceID, name, provider, name+" device", testUserID, profileID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert profile runtime fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// TestDeleteRuntimeProfile_ArchivedAgentUnbindCascade is the regression guard for
// the FK-RESTRICT 500: a profile whose only remaining agent is ARCHIVED must
// still delete cleanly. agent.runtime_id is ON DELETE RESTRICT, so without the
// per-runtime teardown the DELETE on agent_runtime would raise a raw FK error and
// the handler would 500.
//
// Since MUL-5559 the teardown unbinds that agent instead of hard-deleting it, so
// this test also pins what must NOT happen: the agent survives with its channel
// installation intact. Sweeping the installation of a surviving agent would take
// a working bot away from it — the mirror of the #4810 orphan problem.

// TestDeleteRuntimeProfile_ActiveAgentBlocks confirms the guard still refuses
// (409) while an ACTIVE agent is bound to one of the profile's runtimes, and
// leaves the profile + runtime intact.





// TestCreateRuntimeProfile_ForcesWorkspaceVisibility is the regression guard
// for the visibility leak: visibility=private is not user-settable in v1
// because the read paths don't enforce it. A client that POSTs
// visibility:"private" must get a profile stored as 'workspace' — never
// private — so a "private" profile can't leak to other members or be
// registered by other daemons. Belt-and-suspenders: also assert the row in
// the DB is 'workspace'.



