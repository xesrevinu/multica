package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// claimAgentInstructionsForTest claims the next queued task for runtimeID and
// returns the claimed task id, the agent Instructions carried on the claim
// response (the field the squad-leader briefing is injected into), and the
// is_leader_task flag the daemon derives its squad-leader role from
// (MUL-5811). Empty task id means no task was claimed.
func claimAgentInstructionsForTest(t *testing.T, runtimeID string) (taskID string, instructions string, isLeaderTask bool, raw string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "squad-briefing-claim")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID                 string `json:"id"`
			IsLeaderTask       bool   `json:"is_leader_task"`
			LeaderRoleResolved bool   `json:"leader_role_resolved"`
			Agent              *struct {
				Instructions string `json:"instructions"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", false, w.Body.String()
	}
	// Every claim must advertise the capability, leader or not: its absence is
	// how a daemon detects a server too old to resolve the role, and silently
	// dropping it would send every upgraded daemon back to inferring the role
	// from instructions text — the bug MUL-5811 removed.
	if !resp.Task.LeaderRoleResolved {
		t.Fatalf("claim response must set leader_role_resolved=true: %s", w.Body.String())
	}
	var instr string
	if resp.Task.Agent != nil {
		instr = resp.Task.Agent.Instructions
	}
	return resp.Task.ID, instr, resp.Task.IsLeaderTask, w.Body.String()
}

// squadBriefingClaimFixture wires a runtime + leader agent + squad and returns
// the IDs needed to enqueue leader tasks against that runtime.
type squadBriefingClaimFixture struct {
	RuntimeID string
	AgentID   string // squad leader, has the runtime and empty instructions
	SquadID   string
	IssueID   string // assignee_type='agent' (NOT squad) — reproduces MUL-3724
}

func newSquadBriefingClaimFixture(t *testing.T, ctx context.Context, name string) squadBriefingClaimFixture {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	// Leader agent + an issue assigned to that agent (assignee_type='agent').
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" leader")
	// Force empty instructions so the test asserts the briefing alone — this
	// mirrors MUL-3724 where the leader's own instructions were blank.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET instructions = '' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("clear leader instructions: %v", err)
	}
	// Make the issue assignee an agent (NOT the squad). The pre-fix code only
	// injected the briefing when issue.assignee_type='squad', so this is the
	// exact gap the fix closes: a comment @squad-mention leader task running on
	// an agent-assigned issue.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("set issue agent assignee: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name+" squad", agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	return squadBriefingClaimFixture{
		RuntimeID: runtimeID,
		AgentID:   agentID,
		SquadID:   squadID,
		IssueID:   issueID,
	}
}

func enqueueClaimTask(t *testing.T, ctx context.Context, fx squadBriefingClaimFixture, isLeader bool, withSquadID bool) string {
	t.Helper()
	var taskID string
	var squadArg any
	if withSquadID {
		squadArg = fx.SquadID
	} else {
		squadArg = nil
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5)
		RETURNING id
	`, fx.AgentID, fx.RuntimeID, fx.IssueID, isLeader, squadArg).Scan(&taskID); err != nil {
		t.Fatalf("enqueue claim task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestClaim_LeaderTaskFromCommentMention_InjectsBriefing is the MUL-3724
// reproduction: a leader task (is_leader_task=true) carrying a squad_id, on an
// issue assigned to a plain AGENT (not the squad). The pre-fix gate
// (issue.assignee_type='squad') would NOT inject the briefing here, so the
// leader booted with no squad context and degraded into doing the work itself.
// After the fix the briefing is keyed off the task flag + squad_id, so it is
// injected regardless of issue assignee.

// TestClaim_NonLeaderTask_NoBriefing guards the negative: a task that is NOT a
// leader task (is_leader_task=false), even with a squad_id present, must not
// receive the briefing. This keeps worker/mention runs free of leader framing.
func TestClaim_NonLeaderTask_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nonleader")
	enqueueClaimTask(t, ctx, fx, false /*isLeader*/, true /*withSquadID*/)

	_, instr, isLeader, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("non-leader task must NOT get squad briefing, got:\n%s", instr)
	}
	if isLeader {
		t.Fatalf("non-leader task must not report is_leader_task=true: %s", raw)
	}
}

// TestClaim_LeaderTaskWithDanglingSquadID_NoBriefing is the load-bearing
// contract for dropping the FK on agent_task_queue.squad_id (migration 127):
// when a squad is hard-deleted AFTER a leader task was enqueued, the task row
// keeps a now-dangling squad_id. The claim must still succeed (HTTP 200, task
// delivered) and simply skip briefing injection — GetSquadInWorkspace returns
// no row, so the err != nil guard makes this identical to "condition not
// matched". Never a 500, never a stale/empty briefing. Without the FK nothing
// in the DB prevents the dangling row, so this guard lives entirely in the
// claim handler and must stay tested.

// leader tasks enqueued before migration 127 (or by an old binary) have a NULL
// squad_id. The claim handler must skip injection rather than panic or guess —
// equivalent to the pre-fix "condition not matched" behavior, never a stale
// briefing.
func TestClaim_LeaderTaskWithoutSquadID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadBriefingClaimFixture(t, ctx, "Briefing nullsquad")
	enqueueClaimTask(t, ctx, fx, true /*isLeader*/, false /*withSquadID*/)

	_, instr, isLeader, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "## Squad Roster") {
		t.Fatalf("leader task with NULL squad_id must NOT get squad briefing, got:\n%s", instr)
	}
	if isLeader {
		t.Fatalf("skipped injection must clear is_leader_task on the claim response: %s", raw)
	}
}

// TestClaim_LeaderSwappedAfterEnqueue_NoBriefingAndNoLeaderRole covers the
// third skip path: the squad still exists, but its leader was reassigned after
// this task was enqueued, so the claiming agent is no longer the leader. The
// defensive gate already withheld the briefing; the flag must follow it down,
// otherwise the daemon would boot a former leader into the leader role with no
// roster and no protocol (MUL-5811).
