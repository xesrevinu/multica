package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)


// TestCreateWorkspace_DoesNotMarkOnboarded guards the onboarding
// contract: creating a workspace MUST leave user.onboarded_at NULL so
// the route guard in apps/web/app/[workspaceSlug]/layout.tsx (and the
// desktop App.tsx overlay decision) can redirect the un-onboarded user
// back to /onboarding to finish Step 3. The previous behavior atomically
// set onboarded_at inside CreateWorkspace; this test makes the new
// invariant explicit and regression-protected.
//
// CompleteOnboarding (Step 3 exit) and AcceptInvitation are the only
// remaining handlers that flip onboarded_at.

// TestCreateWorkspace_DisabledByConfig guards the self-host gate added by
// #3433: when DisableWorkspaceCreation is true on the handler config, every
// caller — even an already-authenticated user — must receive 403 and the
// workspace row must not be written.

// TestDeleteWorkspace_RequiresOwner exercises the in-handler authorization
// added to DeleteWorkspace by calling the handler directly (bypassing the
// router-level RequireWorkspaceRoleFromURL middleware). Without the handler
// check, a non-owner member request would reach DeleteWorkspace and erase the
// workspace; with it, the handler must return 403 and leave the workspace
// intact.

// TestDeleteWorkspace_OwnerSucceeds is the positive counterpart: an owner
// calling DeleteWorkspace directly must succeed (204) and the workspace must
// be gone. This guards the handler check against being too strict.




// TestUpdateWorkspace_AvatarURL covers the avatar_url field added to
// UpdateWorkspaceRequest: a PATCH with avatar_url is persisted and surfaced
// back on the response, and partial updates leave other fields untouched.
// Route-level authorization (owner/admin) is enforced by middleware in
// router.go; the handler test calls UpdateWorkspace directly to verify the
// payload wiring.

// recordingWorkspaceRefreshNotifier captures the daemon wakeups a workspace
// write fans out to its members.
type recordingWorkspaceRefreshNotifier struct {
	userIDs []string
}

func (n *recordingWorkspaceRefreshNotifier) NotifyWorkspacesChanged(userID string) {
	n.userIDs = append(n.userIDs, userID)
}

// Daemons cache workspace settings and read the GitHub master switch and the
// Co-authored-by toggle from them. A settings edit that does not wake them
// leaves the old verdict live until the workspace's next repo checkout, which
// is how a disabled Co-authored-by trailer kept landing in commits (MUL-6921).


// revocationFixture is a minimal (workspace, member-to-revoke, runtime,
// agent, queued-task, daemon-token) bundle used to drive the revocation
// tests. The "requester" is always testUserID (owner of the workspace) so
// `newRequest` passes the existing fixtures' auth context unchanged.
type revocationFixture struct {
	WorkspaceID  string
	TargetUserID string
	MemberID     string
	RuntimeID    string
	AgentID      string
	TaskID       string
	DaemonID     string
	TokenHash    string
}

func setupRevocationFixture(t *testing.T, slug, daemonID string) revocationFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Revocation " + slug,
		"slug":         slug,
		"description":  "revocation test",
		"issue_prefix": "REV",
	})

	// Requester (= testUserID) is always an owner so DeleteMember authorization
	// passes. Two owners total so LeaveWorkspace doesn't trip the "must keep
	// at least one owner" guard.
	dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID)

	targetEmail := fmt.Sprintf("revocation-%s@multica.ai", slug)
	targetUserID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Revocation Target " + slug,
		"email": targetEmail,
	})

	// Cleanup ordering: workspace first (cascade clears agent_runtime,
	// agent, member, daemon_token), then user (whose deletion would
	// otherwise be blocked by agent.owner_id / agent_runtime.owner_id FKs).
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	memberID := dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": wsID,
		"user_id":      targetUserID,
		"role":         "owner",
	})

	runtimeID := dbfx.Runtime(t, "Target Runtime", testutil.Cols{
		"workspace_id": wsID,
		"daemon_id":    daemonID,
		"runtime_mode": "local",
		"provider":     "multica_daemon",
		"owner_id":     targetUserID,
	})

	agentID := dbfx.Agent(t, "Target Agent", runtimeID, testutil.Cols{
		"workspace_id": wsID,
		"runtime_mode": "local",
		"visibility":   "workspace",
		"owner_id":     targetUserID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
	})

	// daemon_token row — paired with the runtime's daemon_id so the
	// revocation should sweep its hash up via DeleteDaemonTokensByWorkspaceAndDaemons.
	rawToken := "mdt_test_" + slug
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	dbfx.Exec(t, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, now() + interval '1 day')
`, tokenHash, wsID, daemonID)

	return revocationFixture{
		WorkspaceID:  wsID,
		TargetUserID: targetUserID,
		MemberID:     memberID,
		RuntimeID:    runtimeID,
		AgentID:      agentID,
		TaskID:       taskID,
		DaemonID:     daemonID,
		TokenHash:    tokenHash,
	}
}

func assertRevoked(t *testing.T, fx revocationFixture) {
	t.Helper()

	var memberExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists)
	if memberExists {
		t.Fatal("member row was not deleted")
	}

	var runtimeStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus)
	if runtimeStatus != "offline" {
		t.Fatalf("expected runtime offline, got %q", runtimeStatus)
	}

	var archivedAt *string
	dbfx.QueryRow(t, `SELECT archived_at::text FROM agent WHERE id = $1`, fx.AgentID).Scan(&archivedAt)
	if archivedAt == nil {
		t.Fatal("agent was not archived")
	}

	var taskStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.TaskID).Scan(&taskStatus)
	if taskStatus != "cancelled" {
		t.Fatalf("expected task cancelled, got %q", taskStatus)
	}

	var tokenExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, fx.TokenHash).Scan(&tokenExists)
	if tokenExists {
		t.Fatal("daemon_token row was not deleted")
	}
}

// TestDeleteMember_RevokesTargetRuntimes verifies that when an admin removes
// another member from a workspace, every runtime owned by the removed member
// has its agents archived, its in-flight tasks cancelled, its row flipped
// offline, and its daemon_token rows deleted — all atomically with the member
// row deletion.

// TestDeleteMember_PrunesChannelUserBindings verifies the application-layer
// replacement for the channel_user_binding member-FK cascade (MUL-3515 §4):
// removing a member prunes that member's channel bindings, in the same tx as
// the member-row delete, while leaving a remaining member's binding intact.

// TestDeleteMember_PrunesAutopilotSubscribers verifies the application-layer
// cleanup for the FK-free autopilot_subscriber table. DeleteMember and
// LeaveWorkspace both use revokeAndRemoveMember, so this pins their shared
// transaction while also proving the delete is scoped to the departed user.

// TestLeaveWorkspace_RevokesOwnRuntimes is the self-removal counterpart: when
// a member leaves a workspace voluntarily, their own runtimes are revoked
// with the same atomic write set as DeleteMember.

// TestDeleteMember_CancelsTasksFromAgentReassignment covers a subtle
// case: an agent's runtime_id can be changed via UpdateAgent, but
// agent_task_queue.runtime_id keeps the value from when the task was
// queued. So after a leaving member is removed, an agent currently bound
// to their runtime gets archived — but tasks that agent queued under a
// PRIOR runtime (still owned by another active member) keep their old
// runtime_id and would not be caught by a runtime-only sweep. Because
// ClaimAgentTask does not gate on agent.archived_at, those orphaned
// queued tasks would remain claimable.

// TestDeleteMember_CancelsDeferredTasks covers the second caller of
// CancelAgentTasksByRuntimeOrAgent. Member revocation must cancel a scheduled
// fallback just like queued/running work; otherwise it could become claimable
// after its owner and runtime access have been removed.

// TestDeleteMember_NoRuntimes_DeletesMember covers the empty-revocation
// path: a member with no owned runtimes should still have their member row
// deleted by the same atomic transaction, with no spurious archive/cancel
// writes.

// TestDefaultIssuePrefixFromSlug pins the derivation new workspaces get
// (MUL-6050): alphanumerics of the slug, first 4, uppercased. The Chinese
// cases are the point of the change — under the old name-based derivation
// every one of them collapsed to "WS".
//
// Keep this table in sync with the client-side preview
// (packages/views/onboarding/steps/step-workspace.tsx). If the two ever
// disagree, the create screen lies about the identifier the user will get.

// TestLegacyIssuePrefixFromName_Frozen guards the read-time fallback for
// workspaces whose stored prefix is empty. Issue identifiers are computed
// from the current prefix on every read, so changing what this returns would
// silently rewrite the identifier of every historical issue in those
// workspaces. The product decision on MUL-6050 was explicit: no backfill,
// existing workspaces are left exactly as they are — which means this
// function must keep returning what it always did, including "WS" for
// non-ASCII names.

// TestIssuePrefixForWorkspace_LegacyFallbackFrozen guards the resolution rule
// itself, at the seam every read-time caller goes through (getIssuePrefix and
// the GitHub close-intent scan, which holds the row already).
//
// A stored prefix always wins; only an empty one falls back, and that fallback
// must stay on the old name-based derivation. Pointing it at
// defaultIssuePrefixFromSlug would rewrite the identifier of every issue in
// those legacy workspaces on the next read — the exact outcome the "no
// backfill, leave existing workspaces alone" decision on MUL-6050 rules out.


// TestCreateWorkspace_ChineseNameDerivesPrefixFromSlug is the end-to-end
// regression for MUL-6050: a workspace whose name has no ASCII letters used
// to be created with prefix "WS" — deterministically, for every Chinese team
// on the instance. It must now take its prefix from the slug, which the same
// form already forced the user to choose in ASCII.

// TestCreateWorkspace_HonorsExplicitIssuePrefix covers the create screen's new
// editable prefix field: whatever the user typed is what gets persisted.

// TestCreateWorkspace_RejectsInvalidIssuePrefix closes the API-side hole the
// settings UI already guarded: before MUL-6050 the create and update handlers
// only trimmed and uppercased the caller-supplied prefix, so a direct API call
// could persist CJK text or a 100-character string as an issue prefix.

// TestUpdateWorkspace_RejectsInvalidIssuePrefix is the PATCH half of the same
// hole: settings normalizes client-side, but the endpoint took anything.
