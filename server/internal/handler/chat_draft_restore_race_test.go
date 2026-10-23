package handler

import (
	"context"
	"testing"
	"time"


)

// chat_draft_restore has no FK to chat_session (MUL-3515), so an INSERT into it
// takes no lock on the session, and a deleter's prune only sees what committed
// before its snapshot. Prune-then-delete alone therefore leaves a window: a
// restore committed inside it outlives its cascade-deleted session, unreachable
// and undeletable, with the user's prompt in it (#5219 review).
//
// The close is a mutual-exclusion protocol on the chat_session row lock — every
// deleter locks before it sweeps, FinalizeDeferredCancelledChat locks before it
// inserts. Each half is useless without the other, so each has its own test
// below: pin one side, assert the other one blocks.

type draftRestoreRaceFixture struct {
	workspaceID   string
	chatSessionID string
	taskID        string
}

// seedDraftRestoreRaceFixture builds a disposable workspace holding one chat
// task that FinalizeDeferredCancelledChat will settle as "still empty": a
// deferred marker, an empty transcript, and the triggering user message it turns
// into a chat_draft_restore row.
func seedDraftRestoreRaceFixture(t *testing.T, slug string) draftRestoreRaceFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, '')
		RETURNING id
	`, "Draft Restore Race", slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Draft Restore Race Runtime', 'cloud', 'isolated_test', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Draft Restore Race Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'Draft Restore Race Session', 'active')
		RETURNING id
	`, wsID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, status, priority, context, runtime_id,
			started_at, chat_finalize_deferred_at
		)
		VALUES ($1, $2, 'cancelled', 0, '{}'::jsonb, $3, now(), now())
		RETURNING id
	`, agentID, sessionID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create deferred chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'the prompt the cancel ate', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("create user chat message: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		testPool.Exec(cleanupCtx, `DELETE FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	return draftRestoreRaceFixture{workspaceID: wsID, chatSessionID: sessionID, taskID: taskID}
}

// waitForBlockedBackend reports whether some backend parked on a lock — the
// observable proof that the side under test honoured the protocol. It gives up
// the moment the racing goroutine finishes instead: a side that never blocks
// skipped its lock, which is the bug these tests exist to catch.
func waitForBlockedBackend(t *testing.T, done <-chan struct{}) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		var blocked int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
		`).Scan(&blocked); err == nil && blocked > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// The writer's half. Without its own chat_session lock the finalizer can commit
// a restore in the gap between a deleter's lock and that deleter's sweep — the
// sweep's snapshot predates the commit and the row is stranded. Holding the
// session lock from another tx is what makes that observable: a finalizer that
// respects the protocol parks on it; one that doesn't sails past and writes.

// The deleters' half. A restore commits while the workspace teardown is parked
// on the session lock; the teardown's sweep runs on a snapshot taken after it
// wins the lock, so it must still see — and remove — that row.

// The session locks only cover sessions that exist when the teardown enumerates
// them. A chat_session created inside the delete window is invisible to that set,
// so a finalizer could lock it and insert a restore after the sweep's snapshot —
// and the restore would outlive the workspace cascade exactly like the TOCTOU
// case above. The window is closed by an EXPLICIT protocol: DeleteWorkspace locks
// the workspace row FOR UPDATE, and every session creator takes a conflicting
// FOR KEY SHARE on it (LockWorkspaceForChatSessionCreate) inside its own tx before
// inserting — so the block does not lean on the chat_session.workspace_id FK's
// implicit lock, which the codebase is moving off of (MUL-3515).
//
// This drives the REAL creator entry point (Handler.CreateChatSession), not a raw
// INSERT: holding the workspace row FOR UPDATE (the exact lock DeleteWorkspace
// takes) must park the handler on its explicit lock, and releasing it must let the
// create proceed. A handler that skipped the lock would sail straight through.

// The settle-against-a-gone-session case the lock protocol produces: the loser
// of the race finds no session to lock, so it must clear its marker and write
// nothing rather than strand a restore against a dead id.
