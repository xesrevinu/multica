package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

)

// GH #6340: a user stops a chat turn the agent had already started answering,
// then sends the next message and the agent replies with no memory of the
// conversation.
//
// The cancelled turn's provider session is real — the daemon pinned it onto the
// task row mid-flight and cancellation keeps it there — but nothing on the
// cancel path could hand it back: the resume lookups only considered
// 'completed' / 'failed' rows, and the chat-level resume pointer is written by
// CompleteTask / FailTask, neither of which runs for a cancelled task. These
// tests pin both halves, plus the guards that must survive them.

// TestClaimTask_ChatResumesCancelledTurnSession is the reported scenario at its
// worst: the FIRST turn of a chat is cancelled, so there is no earlier session
// to fall back to and the next turn used to start completely cold.

// TestClaimTask_ChatCancelledSessionStaysExcludedWhenRetired pins the guard the
// change must not weaken: a session some run deliberately abandoned stays
// unreachable even when a cancelled row still points at it.

// TestClaimTask_IssueResumesCancelledTaskSession is the issue-side half: stop a
// run, comment again, and the follow-up keeps the stopped run's context.

// TestCancelTask_AdvancesChatSessionResumePointer covers the pointer half. The
// claim handler reads chat_session.session_id FIRST, so on a chat that already
// has history a stale pointer would still rewind past the cancelled turn on any
// provider that mints a new session id per resume.

// TestCancelTask_KeepsChatPointerWhenNoSessionEstablished guards the other
// direction: a turn cancelled before the backend revealed a session must leave
// the existing pointer alone rather than blanking the chat's memory.

// TestCancelTask_PointerAdvanceIsAtomicWithStatusFlip is the adversarial case:
// a follow-up is already queued when the user cancels. If the status flip
// committed before the pointer advance, that follow-up could be claimed in the
// gap and resume the PREVIOUS turn's session — the exact failure this change
// exists to remove.
//
// The chat row is locked from another connection so the pointer write blocks
// mid-cancel; while it is blocked, no other connection may observe the task as
// cancelled.

// TestPinTaskSession_LateCancelledPinAdvancesChatPointer covers the other
// ordering: the cancel wins the race and finds no session to publish (a Codex
// pin is still waiting on its rollout), then the pin lands on the cancelled row.
// Filling the task row is not enough — an existing chat pointer shadows it, so
// the follow-up would still resume the previous turn.

// TestPinTaskSession_LateCancelledPinYieldsToNewerTurn is the guard on the
// path above: once a NEWER turn has recorded a session of its own, a straggler
// pin for the cancelled turn must not drag the conversation backwards.

// TestPinTaskSession_PointerAdvanceIsAtomicWithPin is the pin-path mirror of
// TestCancelTask_PointerAdvanceIsAtomicWithStatusFlip: landing the session on
// the task row and advancing the chat pointer must be one commit, or a
// follow-up claimed in between still resumes the previous turn.

// TestCancelTask_TakesChatSessionLockBeforeTask pins the lock order the repo
// documents (chat_session -> agent_task_queue, see LockChatSessionForTask).
// DeleteChatSession locks the session and then cascades into agent_task_queue;
// a cancel that took the task row first and only then reached for the session
// deadlocks against it — PostgreSQL aborts one side with 40P01 and runInTx has
// no deadlock retry.
//
// With the session held from another connection, a cancel must be waiting for
// the SESSION, having taken no lock on the task row yet — which a FOR UPDATE
// NOWAIT probe can prove.

// TestCancelTask_ConcurrentWithChatSessionDelete runs the two paths against
// each other for real: whoever wins, neither may come back with a deadlock.

// TestTerminalReports_TakeChatSessionLockBeforeTask extends the lock-order
// invariant to the terminal reports. Cancel and the pin take chat_session
// first; CompleteTask / FailTask writing the task row first is the other half
// of the same crossing pair, and the daemon issues its pin from a goroutine
// independent of the report, so the two really do overlap.
//
// Same proof as the cancel case: with the session held elsewhere, the report
// must be waiting on the SESSION and holding no lock on the task row.

// TestCancelAndPin_ConcurrentWithTerminalReport runs the crossing pairs for
// real. Whoever wins, neither side may come back with a deadlock.

// deleteChatSessionLikeHandler replays DeleteChatSession's locking protocol
// (lock the session, then cascade into agent_task_queue) so the concurrency
// test exercises the real order without going through HTTP auth.
func deleteChatSessionLikeHandler(ctx context.Context, chatSessionID string) error {
	tx, err := testHandler.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := testHandler.Queries.WithTx(tx)

	id := parseUUID(chatSessionID)
	if _, err := qtx.LockChatSessionForDelete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := qtx.CancelAgentTasksByChatSession(ctx, nullString(id)); err != nil {
		return err
	}
	return tx.Commit()
}

// assertRaceSucceeded fails on anything except the one benign outcome these
// races have: whoever moved the task to a terminal state first leaves the loser
// matching no row (sql.ErrNoRows).
//
// Deliberately NOT a 40P01-only check. A deadlock victim inside an HTTP handler
// is reported as a plain 500 with no *pgconn.PgError to unwrap, so matching the
// SQLSTATE alone would silently pass exactly the failure these tests exist to
// catch. Anything unexpected is a failure, and the error text says what it was.
func assertRaceSucceeded(t *testing.T, iteration int, label string, err error) {
	t.Helper()
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return
	}
	t.Fatalf("iteration %d: %s failed: %v", iteration, label, err)
}

// beginWarmedTx opens a transaction that is safe to hold a row lock in for the
// length of a test.
//
// The tests below block a writer by holding a row, then read other rows to
// observe what the writer has and has not committed. Doing those reads on a
// FRESH pooled connection is what made them hang: the whole suite shares one
// database, a sibling package can queue DDL (ACCESS EXCLUSIVE) against these
// tables at any moment, and once that request is pending every later
// ACCESS SHARE request queues behind it — including ours, while we are still
// holding the row the DDL is waiting for. Nothing here is a cycle PostgreSQL
// can break, so the package sat until the 10-minute test timeout.
//
// Taking both table locks up front means this transaction never asks for a new
// one while holding a row, and the reads then ride the same connection.
func beginWarmedTx(t *testing.T, ctx context.Context) *sql.Tx {
	t.Helper()

	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() {
		// Both are no-ops when the test already released them explicitly.
		tx.Rollback()
		conn.Release()
	})
	for _, warm := range []string{
		`SELECT 1 FROM chat_session LIMIT 1`,
		`SELECT 1 FROM agent_task_queue LIMIT 1`,
	} {
		if _, err := tx.ExecContext(ctx, warm); err != nil {
			t.Fatalf("warm table locks: %v", err)
		}
	}
	return tx
}

// raceTimeout bounds every racing call below so a stalled connection surfaces
// as a test failure with a real message instead of wedging the package until
// the 10-minute timeout. Far above the sub-second these paths actually take.
const raceTimeout = 30 * time.Second

func raceCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), raceTimeout)
}

// pinTaskSessionViaAPI drives the real daemon endpoint so the test covers the
// handler wiring, not just the query.
func pinTaskSessionViaAPI(t *testing.T, taskID, daemonID, sessionID, workDir string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/session",
		map[string]any{"session_id": sessionID, "work_dir": workDir}, testWorkspaceID, daemonID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.PinTaskSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PinTaskSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPinTaskSession_LateCancelledPin covers the mid-flight pin racing the
// cancel: it may fill an EMPTY session slot on the row it belongs to, but must
// never overwrite a session a terminal report already recorded.
