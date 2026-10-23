package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

)

// The refusals these tests cover are the ones a user acts on. A profile spans
// every machine that registered it, so "delete its runtime profile instead" —
// the advice the instance refusal used to give — points at a workspace-wide
// delete that takes healthy machines' runtimes with it, and that a bound agent
// refuses anyway. GH #8456 is a report of exactly that dead end. What both
// messages must now carry is asserted here rather than left to review, because
// the damage is done by the wording, not by the status code.

func decodeConflict(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func conflictMessage(t *testing.T, body map[string]any) string {
	t.Helper()
	msg, _ := body["error"].(string)
	if strings.TrimSpace(msg) == "" {
		t.Fatalf("expected a human-readable error message, got %#v", body)
	}
	return msg
}

// An offline profile-backed instance is the GH #8456 shape: the row the user
// wants gone, on a machine that is never coming back. The refusal has to tell
// them it is reclaimed on its own, because that is the whole answer — every
// other route they could take from here is destructive.

// Online is the one case where waiting is not the answer — the daemon would
// re-register the row immediately. The refusal has to say so instead of
// promising a cleanup that will not happen.

// The cascade endpoint shares the guard, so it must share the guidance —
// `multica runtime delete --cascade` is the retry a blocked user reaches for.

// The profile refusal's job is to show that the agents blocking the delete sit
// on a *different* machine than the stale one the user was cleaning up. A bare
// count cannot do that, and the user's reasonable next move — unbinding agents
// that were working fine — is the damage this message exists to prevent.

// Above maxNamedBlockingAgents the message must stay readable without hiding
// the scale of what the user is about to unbind.

// Retention GC skips a runtime that still has a bound agent, so an offline
// instance in that state must NOT be told to sit and wait — that would be a
// fresh piece of wrong advice replacing the old one.

// createSystemFixtureAgent inserts a product-owned agent. kind and system_key
// are independent here on purpose: Mika is kind='user' with system_key='mika'
// (it must stay visible and assignable), while a builder carrier is
// kind='system'. The refusals key off system_key for exactly that reason.
func createSystemFixtureAgent(t *testing.T, ctx context.Context, runtimeID, name, kind, systemKey string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id, kind, system_key
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, $5, $6)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, kind, systemKey).Scan(&agentID); err != nil {
		t.Fatalf("insert system fixture agent (%s): %v", systemKey, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// Mika cannot be archived — the archive endpoint rejects any agent carrying a
// system_key — but it CAN be rebound, so the generic "reassign or archive them"
// clause is wrong for it in both directions: half of it is impossible and the
// half that works needs a destination the generic wording never gives. The
// refusal has to carry Mika's own path instead. Capability itself is asserted
// against the real endpoints in TestMikaRemedyMatchesWhatMikaCanDo.

// A builder carrier is hidden from the agent list entirely, so neither
// "reassign" nor "archive" is reachable. The way out is its Builder session.

// With both kinds present each needs its own clause: the user agent is
// actionable, the carrier is not, and collapsing them loses one or the other.

// The response carries a bounded sample, not the profile's whole agent set:
// this query runs inside the delete transaction with rows locked, and the
// response is buffered whole before it is written.

// An offline instance held by Mika alone hits the same trap as the profile
// refusal: the generic clause does not fit, and here the destination is any
// other runtime rather than one outside the profile.

func deleteProfileExpectingConflict(t *testing.T, ctx context.Context, profileID string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	h := *testHandler
	h.DaemonRuntimeGone = &recordingRuntimeGoneNotifier{}
	h.DeleteRuntimeProfile(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	return decodeConflict(t, w)
}

// The remedies must come from the whole blocker set, not the sample. Sorting
// puts the sample's contents outside the caller's control, so a class can fall
// entirely past the cap: twenty ordinary agents that sort first push Mika to
// position 21, and a message built from the sample would then tell the user to
// archive all 21 — the exact unactionable instruction this change removes, just
// deferred until they have worked through the first twenty.

// Same boundary for a builder carrier: its remedy is a different surface
// entirely, so losing it past the cap strands the user just as badly.

// The class mapping exists twice — the SQL CASE in ListActiveAgentsByProfile,
// because per-class counts have to cover rows the LIMIT excludes, and
// classifyBlockingAgent in Go for the instance path, which reads plain agent
// rows. This pins them to each other: if a future system_key is added to one
// and not the other, the refusal would name a remedy for the wrong blocker.

// Retention GC needs both no bound agents AND no unfinished task. An agent
// rebound to another machine can leave a deferred run pinned to the old
// runtime, so "no agents bound" alone is not a cleanup promise this server can
// keep. Regression contributed by review.

// Builder sessions are creator-scoped reads, so an admin who is not the
// creator gets 403 from list, switch and discard alike. Telling that admin to
// reopen the session is another instruction they cannot carry out.
// Regression contributed by review.

// Forces the archive path's task-cancellation transaction to fail, which is
// how a task is realistically left behind on an agent that then gets archived.
type reviewArchiveCancelUnavailable struct{}

func (reviewArchiveCancelUnavailable) BeginTx(_ context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("injected task cancellation database outage")
}

// Entering the GC candidate set is not the same as being deletable. gcRuntime
// re-checks the drain across every user agent bound to the runtime, archived
// ones included, before it calls teardown — and those agents can own tasks
// pinned to a different machine. A refusal that only mirrored the candidate
// query's runtime-owned predicate promised cleanup for a row the sweeper skips
// on every pass. Regression contributed by review.

// The Mika clause makes two claims about product capability, and an earlier
// version of it had the second one backwards — it said Mika could not be moved,
// which told an owner who could have fixed the block in one edit to give up.
// Both halves are therefore asserted against the real endpoints rather than
// against anyone's reading of them, so the message cannot drift from the
// product again. Correction contributed by review.
