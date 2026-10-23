package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// actAsRun makes a request come from an agent's run, as the CLI does inside a
// task.
func actAsRun(req *http.Request, agentID, taskID string) *http.Request {
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Actor-Source", "task_token")
	return req
}

func setStatusAsRun(t *testing.T, issueID, status, agentID, taskID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID)
	if agentID != "" {
		req = actAsRun(req, agentID, taskID)
	}
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set status %q: %d %s", status, w.Code, w.Body.String())
	}
}

func commentOn(t *testing.T, issueID, content, agentID, taskID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	if agentID != "" {
		req = actAsRun(req, agentID, taskID)
	}
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", w.Code, w.Body.String())
	}
}

func wakeupRunsOf(t *testing.T, wakeupID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id' = $1`, wakeupID)
}

func lastWakeupOutcome(t *testing.T, issueID string) (outcome, taskID string) {
	t.Helper()
	var raw []byte
	dbfx.QueryRow(t, `SELECT details FROM activity_log WHERE issue_id = $1 AND action = 'wakeup_triggered'
		ORDER BY created_at DESC, id DESC LIMIT 1`, issueID).Scan(&raw)
	var details struct {
		Outcome string `json:"outcome"`
		TaskID  string `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &details)
	return details.Outcome, details.TaskID
}

// claimRun claims a queued run as a daemon does and returns what its prompt
// gets from wakeups that joined it. Only a daemon that advertises
// joined-wakeups-v1 takes them along.
func claimRun(t *testing.T, taskID string, rendersJoined bool) string {
	t.Helper()
	ctx := context.Background()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = clock_timestamp() WHERE id = $1`, taskID)
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, task.RuntimeID.String)
	if err != nil {
		t.Fatal(err)
	}
	req := newDaemonTokenRequest("POST", "/claim", nil, testWorkspaceID, "joined-wakeups-test")
	if rendersJoined {
		req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityJoinedWakeupsV1)
	}
	resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, uuidToString(task.RuntimeID.String), testWorkspaceID)
	if failure != nil {
		t.Fatalf("claim: %+v", failure)
	}
	return resp.WakeupJoined
}

// startRun marks a claimed run started, as the daemon's start call does.
func startRun(t *testing.T, taskID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = clock_timestamp() WHERE id = $1`, taskID)
}

func createRule(t *testing.T, issueID, agentID string, source string, in service.WakeupInput) string {
	t.Helper()
	in.AgentID = agentID
	w, err := (&service.IssueWakeupService{Tasks: testHandler.TaskService}).Create(context.Background(), parseUUID(issueID), parseUUID(testUserID), source, in)
	if err != nil {
		t.Fatal(err)
	}
	return uuidToString(w.ID)
}

// #8849: the parent's agent closes the last sub-issue of a stage from its own
// run on the parent. That run knows, so the rule records the stage without
// queuing a second run; once that run has ended, or when someone else closes
// the stage, the agent is woken as before.
func TestChildDoneDoesNotWakeTheRunThatClosedTheStage(t *testing.T) {
	cases := []struct {
		name      string
		runStatus string
		byAgent   bool
		wantRuns  int
		wantEntry string
	}{
		{"own run still going", "running", true, 0, "acknowledged"},
		{"own run already ended", "completed", true, 1, "woke"},
		{"someone else closed it", "running", false, 1, "woke"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newChildDoneFixture(t, "in_progress")
			agentID := handlerTestAgentID(t)
			setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
			run := dbfx.Task(t, agentID, testutil.Cols{"issue_id": fx.parent.ID, "runtime_id": testRuntimeID, "status": tc.runStatus})
			if tc.byAgent {
				setStatusAsRun(t, fx.child.ID, "done", agentID, run)
			} else {
				updateChildStatus(t, fx.child.ID, "done")
			}
			if runs := childDoneRuns(t, fx.parent.ID); len(runs) != tc.wantRuns {
				t.Fatalf("runs = %+v, want %d", runs, tc.wantRuns)
			}
			if entries := childDoneEntries(t, fx.parent.ID); len(entries) != 1 || entries[0].Outcome != tc.wantEntry {
				t.Fatalf("entries = %+v, want %s", entries, tc.wantEntry)
			}
		})
	}
}

// A comment from the agent itself never wakes it; a member's comment does.

// A rule that fires while its agent already has a run waiting on the issue
// keeps its inputs for that run: no second run, and the run carries the
// instruction and every fact once it is claimed.

// A daemon that cannot render joined wakeups claims the waiting run without
// them; the rule then starts its own run.

// A condition the agent's own unfinished run satisfied does not wake it when
// the agent set up the rule; a person's rule still does, because the running
// agent does not have that instruction.
