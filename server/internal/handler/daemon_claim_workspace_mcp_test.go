package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// claimAgentMcpConfigForTest runs one claim for the given runtime and returns
// the mcp_config the daemon would receive for the claimed task's agent.
func claimAgentMcpConfigForTest(t *testing.T, runtimeID string) json.RawMessage {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "ws-mcp-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.Agent == nil {
		t.Fatalf("missing task agent in claim response: %s", w.Body.String())
	}
	return claimResp.Task.Agent.McpConfig
}

// setupWorkspaceMcpClaimFixture creates a runtime, an agent with the given
// saved mcp_config, one queued task ready to claim, and binds the named
// workspace MCP servers to that agent.
func setupWorkspaceMcpClaimFixture(t *testing.T, ctx context.Context, name, agentMcpConfig string, bind ...string) string {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" agent")
	for _, serverID := range bind {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO agent_mcp_server (agent_id, server_id) VALUES ($1, $2)`, agentID, serverID); err != nil {
			t.Fatalf("setup: bind mcp server: %v", err)
		}
	}

	var stored []byte
	if agentMcpConfig != "" {
		stored = []byte(agentMcpConfig)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, stored, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	return runtimeID
}

// The claim payload is where a bound workspace server actually reaches an
// agent, so assert the resolved document on the wire — not just the resolver.

// The defining property, asserted end to end: a library entry nobody added
// reaches nothing, however many agents exist.

// A binding that is switched off is the same as not having it, without losing
// the assignment.

