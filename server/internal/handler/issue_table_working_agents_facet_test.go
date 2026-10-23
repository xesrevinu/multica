package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// workingAgentsFacetCounts posts one facets request and returns the
// working-agents facet as agent id -> running task count.
func workingAgentsFacetCounts(
	t *testing.T,
	request *http.Request,
) map[string]int64 {
	t.Helper()

	var response issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets, request).Want(http.StatusOK).JSON(&response)
	counts := map[string]int64{}
	for _, facet := range response.Facets {
		if facet.Kind != "working_agents" {
			continue
		}
		if facet.Values == nil {
			t.Fatal("working-agents values must be an array, including when empty")
		}
		for _, value := range facet.Values {
			counts[value.Key] = value.Count
		}
	}
	return counts
}




func TestIssueTableWorkingAgentsFacetNoActiveAgents(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "working facet empty", fmt.Sprintf("working-facet-empty-%d", time.Now().UnixNano()))
	dbfx.Member(t, workspaceID, testUserID, "owner")
	agentID := dbfx.Agent(t, "working facet only archived", "", testutil.Cols{
		"workspace_id": workspaceID, "archived_at": testutil.Raw("now()"),
	})
	issueID := dbfx.Issue(t, "working facet archived issue", testutil.Cols{"workspace_id": workspaceID})
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
	req := workingAgentsFacetRequest(map[string]any{"kind": "workspace"}, nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	if got := workingAgentsFacetCounts(t, req); len(got) != 0 {
		t.Errorf("counts = %v, want empty for workspace with only archived agents", got)
	}
}

func workingAgentsFacetRequest(scope, filters map[string]any) *http.Request {
	if filters == nil {
		filters = map[string]any{}
	}
	return newRequest(http.MethodPost, "/api/issues/table/facets", map[string]any{
		"query": map[string]any{
			"scope":   scope,
			"filters": filters,
			"sort":    map[string]any{"field": "position", "direction": "asc"},
		},
		"facets":        []map[string]any{{"kind": "working_agents"}},
		"include_total": false,
	})
}

// MUL-5525. The header chip's count used to come from a workspace-wide
// projection while the list came from the surface's own compiled query, so a
// project page could advertise "2 agents working" and then open an empty list.
// The `working_agents` facet answers the same question against the same scope
// and filters the rows come from.

// The facet keys are agent ids, so it discloses agent identity and has to pass
// the same visibility gate as /api/working-agents: a plain member must not learn
// that someone else's private agent exists, not even as a count.
