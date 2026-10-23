package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Resource-label junction tables (agent_to_label / skill_to_label) deliberately
// carry no foreign keys, so every bulk hard-delete entry point that removes the
// owning agents/skills must clear their label links in the same transaction.
// These tests pin that cleanup on the four batch paths that never pass through a
// per-entity delete: runtime delete (strict + cascade), runtime-profile delete,
// and workspace delete. Without the sweep, a labelled agent/skill leaves a
// permanent, invisible orphan row once resource labels are enabled.

// insertLabelRow creates a real issue_label so the seeded junction row is valid
// regardless of whether a given database still carries the pre-release label_id
// foreign key. Registers cleanup.
func insertLabelRow(t *testing.T, ctx context.Context, workspaceID, resourceType string) string {
	t.Helper()
	var labelID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_label (workspace_id, resource_type, name, color)
		VALUES ($1, $2, $3, '#3b82f6')
		RETURNING id
	`, workspaceID, resourceType, resourceType+"-"+uuid.NewString()[:8]).Scan(&labelID); err != nil {
		t.Fatalf("insert issue_label: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, labelID)
	})
	return labelID
}

func seedAgentLabel(t *testing.T, ctx context.Context, workspaceID, agentID string) {
	t.Helper()
	labelID := insertLabelRow(t, ctx, workspaceID, "agent")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_to_label (agent_id, label_id) VALUES ($1, $2)`,
		agentID, labelID); err != nil {
		t.Fatalf("seed agent_to_label: %v", err)
	}
}

func seedSkillLabel(t *testing.T, ctx context.Context, workspaceID, skillID string) {
	t.Helper()
	labelID := insertLabelRow(t, ctx, workspaceID, "skill")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO skill_to_label (skill_id, label_id) VALUES ($1, $2)`,
		skillID, labelID); err != nil {
		t.Fatalf("seed skill_to_label: %v", err)
	}
}

func countAgentLabelAssignments(t *testing.T, ctx context.Context, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_to_label WHERE agent_id = $1`, agentID).Scan(&n); err != nil {
		t.Fatalf("count agent_to_label: %v", err)
	}
	return n
}

func countSkillLabelAssignments(t *testing.T, ctx context.Context, skillID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM skill_to_label WHERE skill_id = $1`, skillID).Scan(&n); err != nil {
		t.Fatalf("count skill_to_label: %v", err)
	}
	return n
}

// TestDeleteAgentRuntime_KeepsUnboundAgentLabelAssignments: since MUL-5559 the
// strict runtime delete unbinds the archived agent instead of hard-deleting it,
// so its label links must SURVIVE. Clearing them by runtime — which is what the
// old sweep did — would strip labels off an agent that is still there.

// TestUnbindAgentsAndDeleteRuntime_KeepsAgentLabelAssignments: the confirmed
// endpoint unbinds the active agent, so its labels stay attached too.

// TestDeleteRuntimeProfile_KeepsAgentLabelAssignments: the profile teardown runs
// the same unbind, so the archived agent and its label links survive there too.

// TestDeleteAgentRuntime_CleansSystemAgentLabelAssignments: system agents are
// still hard-deleted with their runtime (they are invisible infrastructure with
// no rebind affordance), so their label links must still be swept — otherwise
// they become the invisible orphan rows the sweep exists to prevent.

func seedWorkspaceResourceLabelFixture(t *testing.T, ctx context.Context, slug string) (string, string, string) {
	t.Helper()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Handler Test Delete Labels", slug, "resource-label atomic cleanup test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	// agent.runtime_id is NOT NULL, so the labelled agent needs a runtime in the
	// same workspace. Both cascade away with the workspace.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'ws-label-runtime', 'cloud', 'ws-label-test', 'online', 'dev', '{}'::jsonb, $2, now())
		RETURNING id
	`, wsID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'ws-label-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'ws-label-skill', 'fixture', '# x', '{}'::jsonb, $2)
		RETURNING id
	`, wsID, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	seedAgentLabel(t, ctx, wsID, agentID)
	seedSkillLabel(t, ctx, wsID, skillID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_to_label WHERE agent_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill_to_label WHERE skill_id = $1`, skillID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	return wsID, agentID, skillID
}

// TestDeleteWorkspace_CleansResourceLabelAssignments: workspace delete cascades
// away the agents and skills, but the junction tables have no workspace_id and
// no foreign key, so both must be swept before the cascade or they orphan.

// TestDeleteWorkspace_RollsBackResourceLabelCleanup verifies the resource-label
// sweep and the later administration cleanup share one transaction. Locking the
// workspace's member row makes that late step time out; both junction rows must
// be restored when the transaction rolls back.
