package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// slowProbeLocalSkillListStore wraps a LocalSkillListStore but blocks inside
// HasPending until the provided context is cancelled. PopPending delegates
// to the underlying store. Used to verify that a stalled probe cannot wedge
// the heartbeat — the bound context must cut it short — while the ack-safe
// PopPending path is never reached because HasPending returns an error, not
// true.
type slowProbeLocalSkillListStore struct{ LocalSkillListStore }

func (s slowProbeLocalSkillListStore) HasPending(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

type slowProbeLocalSkillImportStore struct{ LocalSkillImportStore }

func (s slowProbeLocalSkillImportStore) HasPending(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// popRecordingLocalSkillListStore counts PopPending calls so a test can assert
// that the handler never reaches the ack-unsafe side-effecting claim path
// when HasPending reports an empty queue.
type popRecordingLocalSkillListStore struct {
	LocalSkillListStore
	popCalls int
}

func (s *popRecordingLocalSkillListStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	s.popCalls++
	return s.LocalSkillListStore.PopPending(ctx, runtimeID)
}

type popRecordingLocalSkillImportStore struct {
	LocalSkillImportStore
	popCalls int
}

func (s *popRecordingLocalSkillImportStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error) {
	s.popCalls++
	return s.LocalSkillImportStore.PopPending(ctx, runtimeID)
}

func setHandlerTestWorkspaceRepos(t *testing.T, repos []map[string]string) {
	t.Helper()
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, data, testWorkspaceID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, []byte("[]"), testWorkspaceID)
	})
}

// newDaemonTokenRequest creates an HTTP request with daemon token context set
// (simulating DaemonAuth middleware for mdt_ tokens).
func newDaemonTokenRequest(method, path string, body any, workspaceID, daemonID string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	// No X-User-ID — daemon tokens don't set it.
	ctx := middleware.WithDaemonContext(req.Context(), workspaceID, daemonID)
	return req.WithContext(ctx)
}




func createClaimReclaimRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()

	runtimeID := dbfx.Runtime(t, name, testutil.Cols{
		"device_info": "claim reclaim fixture",
	})

	return runtimeID
}

func createClaimReclaimAgentAndIssue(t *testing.T, ctx context.Context, runtimeID, name string) (string, string) {
	t.Helper()

	agentID := dbfx.Agent(t, name, runtimeID)

	var issueID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES (
			$1, $2, 'in_progress', 'none', $3, 'member',
			(SELECT COALESCE(MAX(number), 82649) + 1 FROM issue WHERE workspace_id = $1),
			0
		)
		RETURNING id
	`, testWorkspaceID, name+" issue", testUserID).Scan(&issueID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	return agentID, issueID
}

func createDispatchedClaimFixtureTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID, dispatchedAge string, started bool) string {
	t.Helper()

	var taskID string
	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now() - ($4::interval), CASE WHEN $5::boolean THEN now() ELSE NULL END)
		RETURNING id
	`, agentID, runtimeID, issueID, dispatchedAge, started).Scan(&taskID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	return taskID
}

func setTaskPrepareLeaseForTest(t *testing.T, ctx context.Context, taskID, expiresIn string) {
	t.Helper()
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET prepare_lease_expires_at = now() + ($2::interval)
		WHERE id = $1
	`, taskID, expiresIn)
}

func claimTaskByRuntimeForTest(t *testing.T, runtimeID string) (*struct {
	ID string `json:"id"`
}, string) {
	t.Helper()

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-reclaim-review")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	w.JSON(&resp)
	return resp.Task, w.Body.String()
}

// claimChatIntroForTest claims the next task for a runtime and returns the
// claimed task id plus its chat_intro flag (false when the field is absent, as
// it is omitempty).
func claimChatIntroForTest(t *testing.T, runtimeID string) (string, bool, string) {
	t.Helper()

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "chat-intro-review")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID        string `json:"id"`
			ChatIntro bool   `json:"chat_intro"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimable task, got none: %s", w.Body.String())
	}
	return resp.Task.ID, resp.Task.ChatIntro, w.Body.String()
}

// TestClaimTaskByRuntime_ChatIntroGateClearsAfterUserReplies pins the MUL-4259
// fix: an is_agent_intro session drives the self-introduction prompt only on
// its first, message-less turn. Once the creator has replied, later turns on
// the same (still is_agent_intro) session must claim with chat_intro=false so
// the agent answers instead of repeating its introduction.









// TestClaimTaskByRuntime_PopulatesWorkspaceContext verifies the claim
// response carries workspace.context so the daemon can inject the
// workspace-level system prompt into every agent brief. Regression coverage
// for MUL-2542: before this fix the field was never plumbed through, so
// even workspaces that had set a context got an empty brief.

// TestClaimTaskByRuntime_WorkspaceContextEmptyWhenUnset verifies the field
// is omitted (empty string after JSON decode) when the workspace owner has
// not set a context. Important because the daemon's brief skips the heading
// only on empty input — a stray "context: null" coming back as the string
// "null" would render as a bogus paragraph.






// TestHandleDaemonWSHeartbeat_RuntimeGoneReturnsAckNotError pins the receipt
// fallback for a deletion that missed active invalidation. The connection
// lease schedules an ID-only write, whose missing row becomes RuntimeGone
// instead of a handler error.


// TestDaemonHeartbeat_HTTPRuntimeGoneReturns404 pins the HTTP-path mirror:
// sql.ErrNoRows on the runtime lookup is the only DB error mapped to 404.
// Anything else (transient pool issue, schema mismatch, ...) must surface
// as 500 so the daemon does not mistake a hiccup for a deletion.

// TestDaemonHeartbeat_SlowProbeDoesNotWedge pins the invariant that a stalled
// HasPending probe cannot wedge the heartbeat endpoint past the per-probe
// timeout. The probe is the only bounded call; PopPending is ack-safe-
// critical and is intentionally left unbounded. Without the probe bound the
// heartbeat would hang on a slow shared store.

// TestDaemonHeartbeat_EmptyQueueSkipsPopPending pins the ack-safety property:
// when HasPending reports no work, the heartbeat must NOT invoke PopPending,
// because PopPending's Redis implementation has non-atomic side effects that
// a client-side cancel cannot cleanly un-run (see GH #1637 review).


// TestGetTaskStatus_TransientDBError_Returns500 verifies that a transient DB
// error from GetAgentTaskStatus is reported as 500 rather than 404. The daemon
// uses 404+"task not found" as a hard cancel signal; a transient lookup
// failure must therefore not be smuggled into that body, otherwise a single
// DB hiccup would kill an in-flight agent.

// TestGetTaskStatus_ErrNoRows_Returns404 verifies that an actually-missing
// task row still returns the 404+"task not found" body the daemon relies on
// to interrupt the running agent.



// withURLParams merges the given chi URL parameters into the request context.
// Unlike calling withURLParam twice (which replaces the whole chi.RouteContext
// and loses earlier params), this preserves previously-added params.
func withURLParams(req *http.Request, kv ...string) *http.Request {
	rctx := chi.NewRouteContext()
	if existing, ok := req.Context().Value(chi.RouteCtxKey).(*chi.Context); ok && existing != nil {
		for i, key := range existing.URLParams.Keys {
			rctx.URLParams.Add(key, existing.URLParams.Values[i])
		}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}


// setupForeignWorkspaceFixture creates an isolated workspace (not reachable
// from testUserID) with its own agent, runtime, issue, and queued task.
// Returns (issueID, taskID). All rows are cleaned up when the test ends.
func setupForeignWorkspaceFixture(t *testing.T) (string, string) {
	t.Helper()

	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Foreign Workspace",
		"slug":         "foreign-idor-tests",
		"description":  "Cross-tenant IDOR test workspace",
		"issue_prefix": "FOR",
	})

	runtimeID := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"daemon_id":    nil,
		"name":         "Foreign Runtime",
		"runtime_mode": "cloud",
		"provider":     "foreign_runtime",
		"status":       "online",
		"device_info":  "Foreign runtime",
		"metadata":     testutil.Raw("'{}'::jsonb"),
		"last_seen_at": testutil.Raw("now()"),
	})

	agentID := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id":         foreignWorkspaceID,
		"name":                 "Foreign Agent",
		"description":          "",
		"runtime_mode":         "cloud",
		"runtime_config":       testutil.Raw("'{}'::jsonb"),
		"runtime_id":           runtimeID,
		"visibility":           "workspace",
		"max_concurrent_tasks": 1,
	})

	issueID := dbfx.Issue(t, "foreign-workspace-issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"priority":     "medium",
		"creator_id":   agentID,
		"creator_type": "agent",
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
	})

	return issueID, taskID
}

// TestGetActiveTaskForIssue_CrossWorkspace_Returns404 verifies that a member of
// workspace A cannot discover tasks for an issue in workspace B by passing
// B's issue UUID in the URL while keeping A in X-Workspace-ID.

// TestCancelTask_CrossWorkspace_Returns404 verifies that a member of workspace
// A cannot cancel a task that lives in workspace B. Critically, the task must
// remain in its original status — no side effect before the access check.

// TestCancelTask_TaskBelongsToDifferentIssue_Returns404 verifies that a task
// UUID belonging to a *different* issue in the *same* accessible workspace
// cannot be cancelled by routing it through another issue's URL. This guards
// against the weaker fix that only validates the issue→workspace binding.

// TestCancelTask_SameIssue_Succeeds is the happy-path companion to the two
// negative tests above — same workspace, correct issue→task pairing → 200.

// TestListTasksByIssue_CrossWorkspace_Returns404 verifies that task history
// is not readable across workspaces via a bare issue UUID.

// TestGetIssueUsage_CrossWorkspace_Returns404 verifies that per-issue token
// usage is not readable across workspaces via a bare issue UUID.





// TestDaemonRegister_MergesLegacyDaemonIDRuntime simulates the migration path
// for an existing user whose runtime was previously keyed on a hostname-derived
// daemon_id (e.g. "MacBook-Pro.local"). After the daemon switches to a stable
// UUID, the registration payload lists the old id under `legacy_daemon_ids`.
// The server must:
//
//   - reassign every agent pointing at the old runtime row to the new row,
//   - reassign every task (agent_task_queue.runtime_id) onto the new row,
//   - delete the stale old row so there's exactly one runtime per machine,
//   - record the legacy daemon_id on the new row for traceability.
//
// This is the acceptance path from MUL-975: hostname drift must no longer
// orphan agents on stale runtime rows.

// TestDaemonRegister_MergesLegacyDaemonIDRuntime_ReverseDotLocal covers the
// direction missed by the initial implementation: the stored runtime row is
// `host` (no `.local`) but the daemon's current `os.Hostname()` now returns
// `host.local`. The daemon must emit the bare variant as a legacy candidate
// and the server must match it.

// TestDaemonRegister_MergesLegacyDaemonIDRuntime_CaseDrift verifies that
// case-only drift in os.Hostname() output (e.g. `Jiayuans-MacBook-Pro.local`
// vs `jiayuans-macbook-pro.local`) still merges the legacy row. The daemon
// emits the id in its current casing; the server-side lookup uses LOWER() on
// both sides so stored and emitted casings can differ without orphaning.

// TestDaemonRegister_MergesAllCaseDuplicateLegacyRuntimes covers the case
// where the DB already holds *two* legacy runtime rows that differ only in
// casing (e.g. `Jiayuans-MacBook-Pro.local` AND `jiayuans-macbook-pro.local`
// coexist under the same workspace+provider because earlier hostname drift
// already minted a duplicate). A single-row lookup would merge only one of
// them and leave the other orphaned; the lookup must return every row whose
// daemon_id case-insensitively matches and the handler must consolidate them
// all. This is the acceptance-standard path: after registration there must
// not be two runtime rows for the same machine.

// TestDaemonRegister_LegacyIDNoMatchIsNoop guards the common case where the
// daemon sends legacy candidates but no matching row exists (e.g. first
// registration on a fresh machine). Registration must still succeed, the new
// row must not have a spurious legacy_daemon_id recorded, and no unrelated
// rows may be touched.

// Regression test for #1224: tasks linked only via AutopilotRunID (run_only
// autopilots) must resolve to the autopilot's workspace. Before the fix,
// resolveTaskWorkspaceID fell through and every StartTask call returned 404.

// ClaimTaskByRuntime must surface the issue's project github_repo resources
// as resp.Repos and hide the workspace-bound repos. Without this the agent
// would see two repo lists in the meta-skill and have no signal about which
// belongs to the current issue.

// When an issue belongs to a project that has a description, the claim handler
// must surface that description as project_description so the daemon can inject
// it into the brief. This is the handler-side boundary the execenv tests can't
// cover: if this assignment regresses, the description silently stops reaching
// the agent while the execenv rendering tests still pass.

// The quick-create path resolves its project from the task context JSONB
// (not an issue row), so its project_description wiring is a separate branch
// in the claim handler and needs its own boundary assertion.

// When the issue's project has no github_repo resources, the claim handler
// must fall back to workspace repos (the pre-override behavior).

// Regression test for #1276: ClaimTaskByRuntime must populate both
// workspace and project context for run_only autopilot tasks. Project context
// is what lets the daemon select a bound local_directory and materialize the
// managed .multica/project/resources.json source manifest before launch.

// TestClaimTaskByRuntime_TaskWorkspaceMismatch_CancelsAndRejects verifies
// the defense-in-depth check in ClaimTaskByRuntime: if a task is somehow
// dispatched to a runtime whose workspace doesn't match the task's
// resolved workspace (upstream routing / data-integrity bug), the handler
// must 500 AND cancel the dispatched task so it doesn't sit in
// 'dispatched' until the 5-minute sweeper — which would also leave the
// agent stuck reporting 'working' in the UI.

// Regression test for MUL-1198: comment-triggered tasks that finish without
// the agent posting any comment must still deliver a synthesized result
// comment, threaded under the trigger. Before the fix, CompleteTask exempted
// comment-triggered tasks from the auto-synthesis path, so a Claude Code /
// Codex / etc. agent that ended its run with only terminal text (no
// `multica issue comment add` call) left the user staring at a "Completed"
// badge with no reply.

// Companion to the above: when the agent DID post its own comment during the
// run, CompleteTask must not synthesize a duplicate. Guards against the
// common case where the fix is over-eager and creates two comments per task.




type claimRuntimeGuardTask struct {
	PriorSessionID                string          `json:"prior_session_id"`
	PriorWorkDir                  string          `json:"prior_work_dir"`
	PriorSessionResumeUnavailable bool            `json:"prior_session_resume_unavailable"`
	ChatMessage                   string          `json:"chat_message"`
	ThreadName                    string          `json:"thread_name"`
	QuickCreateAttachmentIDs      []string        `json:"quick_create_attachment_ids"`
	QuickCreatePriority           string          `json:"quick_create_priority"`
	QuickCreateDueDate            string          `json:"quick_create_due_date"`
	ProjectID                     string          `json:"project_id"`
	ProjectDescription            string          `json:"project_description"`
	ParentIssueID                 string          `json:"parent_issue_id"`
	QuickCreateSourceContext      json.RawMessage `json:"quick_create_source_context"`
}

func claimTaskForRuntimeGuard(t *testing.T, runtimeID, daemonID string) *claimRuntimeGuardTask {
	t.Helper()
	return claimTaskForRuntimeGuardWithCapabilities(t, runtimeID, daemonID, "")
}

// claimTaskForRuntimeGuardWithCapabilities claims as a daemon advertising the
// given X-Client-Capabilities value ("" for a daemon that advertises none).
func claimTaskForRuntimeGuardWithCapabilities(t *testing.T, runtimeID, daemonID, capabilities string) *claimRuntimeGuardTask {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, daemonID)
	if capabilities != "" {
		req.Header.Set("X-Client-Capabilities", capabilities)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *claimRuntimeGuardTask `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response, got nil")
	}
	return resp.Task
}

func createRuntimeGuardAgent(t *testing.T, ctx context.Context) (agentID, runtimeID, daemonID string) {
	t.Helper()

	daemonID = "runtime-guard-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "runtime-guard-test",
		"runtimes": []map[string]any{
			{"name": "runtime-guard-current", "type": "opencode", "version": "test", "status": "online"},
		},
	}, testWorkspaceID, daemonID)
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)
	var resp map[string]any
	w.JSON(&resp)
	runtimes, ok := resp["runtimes"].([]any)
	if !ok || len(runtimes) == 0 {
		t.Fatalf("setup: expected registered runtime, got %v", resp)
	}
	runtimeID = runtimes[0].(map[string]any)["id"].(string)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })
	dbfx.Exec(t, `UPDATE agent_runtime SET owner_id = $1 WHERE id = $2`, testUserID, runtimeID)

	dbfx.QueryRow(t, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'workspace', 3, $4)
		RETURNING id
	`, testWorkspaceID, "Runtime Guard Agent "+t.Name(), runtimeID, testUserID).Scan(&agentID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	return agentID, runtimeID, daemonID
}

func createRuntimeGuardRuntime(t *testing.T, ctx context.Context, provider string) string {
	t.Helper()

	runtimeID := dbfx.Runtime(t, "Runtime Guard Fixture", testutil.Cols{
		"daemon_id":    testutil.Raw("'runtime-guard-' || gen_random_uuid()::text"),
		"runtime_mode": "local",
		"provider":     provider,
		"status":       "offline",
		"device_info":  testutil.Raw("'{}'::jsonb"),
	})
	return runtimeID
}



// TestClaimTask_ManualRetryReusesWorkdir is the MUL-4869 claim-layer contract: a
// manual retry (rerun_of_task_id set) ALWAYS hands back the source task's
// workdir, and resumes the session only when the source failure did not poison
// the conversation AND the source ran on the claiming runtime. The rerun row's
// force_fresh_session is always true (rollback-safe); the session decision is
// computed here from the source task, including the legacy 400 error-text guard.
// Contrast with a plain force_fresh task carrying no rerun lineage, which resumes
// nothing — covered by TestClaimTask_IssuePriorSessionRuntimeGuard.

// createAutoRetryForTest runs the production retry insert against a failed
// parent, so a claim test exercises the row CreateRetryTask actually writes.
func createAutoRetryForTest(t *testing.T, ctx context.Context, parentID string) {
	t.Helper()
	child, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parseUUID(parentID)})
	if err != nil {
		t.Fatalf("setup: CreateRetryTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, child.ID)
	})
}

// TestClaimTask_AutoRetryFreshSessionReusesParentWorkdir is the MUL-7034
// claim-layer contract for an automatic retry of a conversation-poisoning
// failure: a fresh session, the parent's workdir, and a disclosed continuity
// gap. The retry row comes from the real CreateRetryTask so the query and the
// claim are pinned together; the workdir reaches the daemon only when both
// carry it (GH #7998). The workdir is offered only to a daemon whose
// `multica repo checkout` keeps an existing checkout's work; an older daemon
// keeps getting a fresh directory, because its checkout would reset the very
// checkout being kept. Contrast a
// force_fresh task with no retry lineage, which resumes nothing
// (TestClaimTask_IssuePriorSessionRuntimeGuard).

// TestClaimTask_ChatAutoRetryFreshSessionReusesParentWorkdir is the chat half
// of the same contract. The chat_session pointer names a different session and
// workdir, so the assertions prove the retry continues its parent rather than
// the conversation-wide pointer. Contrast a force_fresh chat task with no retry
// lineage, which inherits nothing
// (TestClaimTask_ChatForceFreshSessionSkipsPriorSession).


// Different channel context generations have independent debounce timers. If
// the newer generation finishes first, its Chat-wide pointer must not become
// the resume source for a delayed task from the older generation.

// TestClaimTask_ChatDeliversAllUnansweredUserMessages pins the fix for the
// regression the MUL-2968 debounce exposed: when several user messages are
// debounced into a single run, the agent must receive ALL of them, not just
// the most recent. Before the fix the daemon prompt was the single latest
// user message, so "看上海天气" then "还有青岛" answered only Qingdao.

// TestClaimTask_ChatPopulatesInitiator verifies that a chat run acts on behalf
// of its stored sender, not the chat session creator (the installer in a Lark
// group). The originator and initiator_user_id are both the sender on this path.





// Locks the legacy-row fallback: chat_session.runtime_id IS NULL (e.g. a row
// the migration left untouched because no prior task matched the cs pointer)
// but a completed task on the claiming runtime exists. ClaimTaskByRuntime
// must recover the session from the task row, not start a fresh conversation.

// TestGetChatSessionGCCheck verifies the chat session gc-check endpoint
// matches the same anti-enumeration shape as GetIssueGCCheck: cross-workspace
// daemon tokens get 404, same-workspace tokens get the live status.

// TestGetAutopilotRunGCCheck verifies the autopilot-run gc-check endpoint:
// 200 with status+completed_at on success, 404 on cross-workspace probe.

// TestGetTaskGCCheck verifies the task gc-check endpoint that quick-create
// workdirs key on. Same anti-enumeration shape via requireDaemonTaskAccess.

// ---------------------------------------------------------------------------
// Membership Cache Integration Tests
//
// These tests don't just exercise the cache primitive (that's covered in
// auth/membership_cache_test.go). They prove two things that the unit tests
// can't:
//
//  1. requireDaemonWorkspaceAccess actually short-circuits the DB on a cache
//     hit (the "ghost user" trick below).
//  2. Each handler that mutates membership actually calls
//     h.MembershipCache.Invalidate(...) — so a future refactor that drops
//     one of those calls will fail CI instead of silently leaking a stale
//     authorization grant for up to MembershipCacheTTL.
// ---------------------------------------------------------------------------

// installFreshMembershipCache used to prime a Redis membership cache.
// Lookups now always read Postgres, so the cache-hit scenarios no longer apply.
func installFreshMembershipCache(t *testing.T) {
	t.Helper()
	t.Skip("membership cache disabled; access checks read Postgres directly")
}

// createEphemeralUser inserts a throwaway user with a unique email and
// deletes it on test cleanup. Returns the user id as a string.
func createEphemeralUser(t *testing.T, label string) string {
	t.Helper()
	email := fmt.Sprintf("membership-cache-%s-%s@multica.ai", label, uuid.NewString())
	userID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Membership Cache Test " + label,
		"email": email,
	})
	return userID
}

// createEphemeralMember creates a throwaway user AND a member row in the
// given workspace. Returns (userID, memberID). Both rows are cleaned up on
// test exit.
func createEphemeralMember(t *testing.T, workspaceID, label, role string) (string, string) {
	t.Helper()
	userID := createEphemeralUser(t, label)
	memberID := dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": workspaceID,
		"user_id":      userID,
		"role":         role,
	})
	return userID, memberID
}

// TestRequireDaemonWorkspaceAccess_CacheHit proves the cache lookup actually
// short-circuits the DB query. The trick: the request actor is a "ghost"
// user with NO member row in the workspace. With an empty cache the access
// check must fail; after priming the cache it must succeed. If a future
// change ever bypasses the cache and falls through to the DB, the priming
// step stops mattering and the second assertion catches it.


// TestMembershipCache_InvalidatedOnDeleteMember drives a real DeleteMember
// HTTP handler call and asserts the cache entry for the removed member is
// gone afterwards. Guards against future refactors that move or drop the
// h.MembershipCache.Invalidate(...) line in workspace.go.

// TestMembershipCache_InvalidatedOnUpdateMember drives a real UpdateMember
// (role change) call and asserts the cache entry is invalidated. The cache
// stores only the existence of membership, but UpdateMember still flushes
// the entry so any downstream caller that did add role-aware caching later
// would not silently see a stale role.

// TestMembershipCache_InvalidatedOnLeaveWorkspace exercises the self-removal
// path (LeaveWorkspace, not DeleteMember). Both handlers route through
// revokeAndRemoveMember, but the Invalidate call lives in the handler — a
// refactor that consolidates them could drop one invalidation without the
// other test catching it.

// TestMembershipCache_InvalidatedOnDeleteWorkspace exercises the bulk
// invalidation path: when the workspace is deleted, every member's cache
// entry must be flushed. We create an isolated workspace with two members
// (owner + extra) so the shared testWorkspace stays intact.

// createCommentTriggeredClaimTask seeds a queued comment-triggered task whose
// trigger comment is rooted under parentID (nil → trigger is itself a root).
// Returns the task id and the trigger comment id.
func createCommentTriggeredClaimTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string, parentID *string) (string, string) {
	t.Helper()
	commentID := dbfx.Comment(t, issueID, "trigger comment", testutil.Cols{
		"parent_id": parentID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
	})
	return taskID, commentID
}

type claimCommentTaskResp struct {
	Task *struct {
		ID               string `json:"id"`
		PriorSessionID   string `json:"prior_session_id"`
		TriggerCommentID string `json:"trigger_comment_id"`
		NewCommentCount  int    `json:"new_comment_count"`
		NewCommentsSince string `json:"new_comments_since"`
		DeltaKnown       bool   `json:"new_comments_delta_known"`

		IssueStateDeltaKnown bool     `json:"issue_state_delta_known"`
		IssueChangedFields   []string `json:"issue_changed_fields"`
		IssueStatus          string   `json:"issue_status"`
		IssueAssigneeType    string   `json:"issue_assignee_type"`
		IssueAssigneeID      string   `json:"issue_assignee_id"`
	} `json:"task"`
}

func claimCommentTask(t *testing.T, runtimeID, daemonID string) claimCommentTaskResp {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp claimCommentTaskResp
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimed task, got nil: %s", w.Body.String())
	}
	return resp
}

// TestClaimTaskByRuntime_CommentTaskPopulatesNewCommentCount
// verifies the claim response carries new_comment_count + new_comments_since for
// a comment task when the agent ran before: the count is ISSUE-WIDE (covers
// every thread, not just the triggering one), excludes the injected trigger
// comment, excludes the agent's own comments, and the since anchor is the prior
// run's started_at.

// TestClaimTaskByRuntime_CommentTaskMarksComputedZeroDelta covers the state the
// count fields cannot express on their own.
//
// A prior run exists and nothing was said on the issue since it started, so the
// delta is a real, server-checked zero — but the response carries the same
// new_comment_count: 0 as a failed anchor read, a failed count query, a cold
// start, and an old server that never sends these fields. Only the checked zero
// answers "has anything else been said here", and that is the only one allowed
// to waive the daemon's mandatory comment scan, so the claim has to say which
// zero this is (MUL-6984).

// TestClaimTaskByRuntime_CommentTaskPopulatesInitiator verifies the common
// member-comment path: the trigger author and authorization human are the same
// member. The claim uses the originator for the existing initiator wire fields.

// Assign-triggered runs have no comment author. The claim must still identify
// the human whose authority the task uses through the existing flat fields.

// TestClaimTaskByRuntime_DelegatedRunHydratesOriginator covers GH-8674: the
// agent-authored trigger remains in trigger_author_*, while the existing
// initiator wire fields carry the human whose authority the run uses.

// A retained trigger comment can name someone other than the member who
// manually reran the task. The claim identity must follow the latter.



// TestClaimTaskByRuntime_CommentResumeDefaultOn verifies comment-triggered tasks
// resume the prior session by default (no env flag), as long as the prior
// session ran on the same runtime.

// TestAckTaskCancelled verifies the cancel-ack endpoint settles a deferred
// chat finalization (marker claimed, Stopped. row written for a transcript
// that filled in late) and keeps the anti-enumeration shape of
// requireDaemonTaskAccess for cross-workspace tokens.

// The daemon GC decides whether a task workdir can be reclaimed from two fields
// of the gc-check response, and the SERVER owns what goes in both.
//
//   - `category` is the real answer: the four-value lifecycle a current daemon
//     tests for terminality (issueGCLifecycle in daemon/gc.go).
//   - `status` is the legacy seven-value enum. Installed daemons match it
//     literally against the built-ins and fail closed on anything else, so a
//     custom status has to be projected back onto that vocabulary here.
//
// Handing back the raw stored key instead breaks both kinds of daemon:
//
//   - an issue parked on a `done`-category custom status is never terminal, so
//     its workdir is retained forever, and
//   - `isKnownIssueStatus` rejects the raw key, silently disabling the
//     GCCompletedTaskTTL full-cleanup path for that issue (MUL-7364).

// Every installed daemon calls the batch endpoint on a timer, with up to
// maxIssueGCBatchSize ids per request. Resolving each row through the
// package-level issuestatus.Effective meant one GetIssueStatusEntryByKey per
// CUSTOM status in the batch — turning the endpoint that exists to replace
// per-issue requests into a per-issue query generator the moment a workspace
// enables custom statuses.
//
// A request-scoped Resolver reads the catalog lazily and at most once, so the
// cost is flat in the number of custom rows and still zero when there are none.

// The common case pays nothing: with no custom status in the batch the resolver
// never loads the catalog at all.

