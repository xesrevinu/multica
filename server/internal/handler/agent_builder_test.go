package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

)



// newBuilderSession starts a builder conversation on testRuntimeID and registers
// cleanup for the carrier agents the flow creates.
func newBuilderSession(t *testing.T) CreateAgentBuilderSessionResponse {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(w, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
		"model":      "model-pinned-to-runtime-a",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var session CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return session
}

// newTestRuntime inserts an extra runtime in the fixture workspace so switch
// tests have somewhere to move to.
func newTestRuntime(t *testing.T, name, status string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, $4, 'switch test runtime', '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name, strings.ToLower(strings.ReplaceAll(name, " ", "_")), status, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func switchBuilderRuntime(t *testing.T, sessionID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+sessionID+"/runtime", map[string]any{
			"runtime_id": runtimeID,
		}),
		"sessionId", sessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(w, req)
	return w
}





func listBuilderSessions(t *testing.T) ListAgentBuilderSessionsResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListAgentBuilderSessions(w, newRequest(http.MethodGet, "/api/agent-builder/sessions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentBuilderSessions: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ListAgentBuilderSessionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return response
}

func insertChatMessage(t *testing.T, sessionID, role, content string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, $2, $3)
	`, sessionID, role, content); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
}

// The studio stopped deleting builder sessions on navigation, so this list is
// the only route back to one. It must carry the carrier's runtime: the client
// seeds its picker from it, and a wrong answer reintroduces MUL-5163 (picker
// shows A while every message runs on B).

func saveBuilderDraft(t *testing.T, sessionID string, draft any) *httptest.ResponseRecorder {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM agent_builder_draft WHERE chat_session_id = $1`, sessionID)
	})
	w := httptest.NewRecorder()
	req := withURLParams(
		newRequest(http.MethodPut, "/api/agent-builder/sessions/"+sessionID+"/draft",
			map[string]any{"draft": draft}),
		"sessionId", sessionID,
	)
	testHandler.SaveAgentBuilderDraft(w, req)
	return w
}

// The half of the configuration the user types by hand had no durable home
// before this: it lived in React state and died on every reload. The list is
// where it comes back, so the two have to be tested together.

// One conversation has one current configuration. Accumulating rows per save
// would make "which one is current" a question the read path has to answer.

// Without the carrier gate this endpoint would be a way to hang arbitrary JSON
// off any chat session the caller happens to own.

// agent_builder_draft carries no chat_session FK, so nothing reclaims the row
// on its own. A draft outliving its conversation is a leak the user can never
// see, let alone clear.

// newSaveDraftRequest prepares a save the way saveBuilderDraft does but hands
// the pieces back, so the two tests below can run the handler on their own
// goroutine — both need a save in flight while another transaction holds the
// session row.
func newSaveDraftRequest(t *testing.T, sessionID string, draft any) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM agent_builder_draft WHERE chat_session_id = $1`, sessionID)
	})
	return httptest.NewRecorder(), withURLParams(
		newRequest(http.MethodPut, "/api/agent-builder/sessions/"+sessionID+"/draft",
			map[string]any{"draft": draft}),
		"sessionId", sessionID,
	)
}

func countBuilderDrafts(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_builder_draft WHERE chat_session_id = $1
	`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count stored drafts: %v", err)
	}
	return n
}

// runSaveDraftAgainst holds the session row in a transaction, runs `hold`
// inside it, then starts a save and proves the save is waiting rather than
// racing. Returns once the save has finished, after the transaction commits.
//
// Asserting that the save blocks is the point: before the fix it read the
// session, sailed past the uncommitted transaction and wrote its row, so the
// outcome depended on which statement won rather than on the lock.
func runSaveDraftAgainst(t *testing.T, sessionID string, w *httptest.ResponseRecorder, req *http.Request, hold string) {
	t.Helper()
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	defer tx.Rollback()
	holderPID := holderBackendPID(t, ctx, tx)
	// The same row and lock mode DeleteChatSession and SetChatSessionArchived
	// take, as their transaction's first statement.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, sessionID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}
	if _, err := tx.ExecContext(ctx, hold, sessionID); err != nil {
		t.Fatalf("hold statement %q: %v", hold, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.SaveAgentBuilderDraft(w, req)
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		select {
		case <-done:
			t.Fatalf("save finished while the session row was held: %d %s", w.Code, w.Body.String())
		default:
			t.Fatalf("save never blocked on the session row held by pid %d", holderPID)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit holder transaction: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("save never returned after the holder committed")
	}
}

// The client autosaves on a debounce, so a save is routinely in flight when the
// user confirms "Discard this draft" — and because the conversation is
// addressable by URL, a second tab can autosave at any moment while this one
// discards. Unlocked, the save's read and its write straddled the delete: the
// upsert landed after the conversation was gone, and agent_builder_draft has no
// chat_session FK to reject it. What survived was the configuration the user had
// just explicitly discarded, invisible to the UI and reachable by no prune but
// the workspace teardown.

// Same race against the other writer of chat_session.status. Creating the agent
// archives the conversation, and the studio's last autosave can still be in
// flight — an unlocked status check let it write a draft onto a session that is
// already read-only, where the drafts list will never show it again.

// A rebind moves the carrier without touching chat_session.runtime_id, which is
// deliberately left stale as the daemon's resume pointer. Reading the list from
// that column instead of the carrier would resume the conversation onto a
// runtime that no longer executes anything.

// A session opened and abandoned untouched is not a draft. Listing it would put
// an empty row in front of the user on every accidental entry into the flow.

// The configuration form is editable from the moment the session exists and
// autosaves, so a user can open the builder, type a name and leave before the
// first turn. Keying "is this a draft" on messages alone made that work
// unreachable: the row existed and nothing could reach it.

// Ordinary chats must never leak into the creation-draft list: their carrier is
// a user agent, and the studio would try to resume a conversation it cannot
// drive.

// The regression this whole change exists for: a send that loaded the agent
// before a rebind committed must still enqueue on the runtime the session is
// bound to NOW. SendDirectChatMessage is handed the stale agent on purpose here
// — that is exactly what its caller does when the two requests interleave.

// holderBackendPID returns the server-side PID of the backend serving tx, so a
// waiter can later be attributed to this specific lock holder.
func holderBackendPID(t *testing.T, ctx context.Context, tx *sql.Tx) int {
	t.Helper()
	var pid int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read holder backend pid: %v", err)
	}
	return pid
}

// waitForWaiterBlockedBy blocks until some backend is waiting on a lock held by
// holderPID, which is how the interleaving tests below observe "the other side
// is parked on our chat_session row" without guessing at timings.
//
// pg_blocking_pids is what makes this specific. `go test ./...` runs package
// test binaries in parallel against one DATABASE_URL, so a probe for "any
// Lock-waiting backend" could match an unrelated package and let the holder
// commit early — after which the path under test would start clean, read the
// committed state, and pass even with its lock removed. Attributing the waiter
// to this transaction's PID removes that false-green path.
//
// Only row-lock waits (transactionid, tuple) and advisory-lock waits count.
// A relation-level wait is excluded for the same reason: a sibling package's
// DDL (CREATE/DROP TRIGGER on agent_task_queue) queues behind any transaction
// that has merely touched the table, which would satisfy the probe without the
// path under test ever reaching the lock.
//
// Returns false only after the deadline with no attributable waiter, which is
// the signal that the path under test never took the lock. A probe error is
// fatal rather than swallowed: a permissions or connectivity failure must not
// be reported as "that path is not locking".
func waitForWaiterBlockedBy(t *testing.T, holderPID int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var waiting int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND wait_event IN ('transactionid', 'tuple', 'advisory')
			  AND $1::int = ANY(pg_blocking_pids(pid))
		`, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("probe pg_stat_activity for waiters blocked by pid %d: %v", holderPID, err)
		}
		if waiting > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A rebind that has not committed yet must hold off a concurrent send, and the
// send must then observe the NEW runtime. This is the half of the protocol the
// lock owns: with the lock removed the send reads straight through the
// uncommitted rebind under READ COMMITTED, sees the pre-switch runtime, and
// enqueues the reply on the runtime the user just switched away from.



// The mirror image: a send that has not committed yet must hold off a rebind,
// and the rebind must then see the now-visible pending task and refuse. Without
// the lock on the switch side, GetPendingChatTask cannot see the uncommitted
// task, so the switch reports success while a reply is already in flight on the
// old runtime — the same "UI says B, execution is A" split, one turn later.


// The probe above is only as good as its attribution: a database-wide "is
// anything waiting on a lock?" check would match another package's test binary
// on the shared DATABASE_URL and let the interleaving tests commit their holder
// early, turning them green even with the lock under test removed. This pins the
// property directly — a waiter blocked by a DIFFERENT backend must not count.
