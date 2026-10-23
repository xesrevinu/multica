package handler

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)


func TestAgentActivityDurationUsesAllRuns(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "duration runtime")
	agentID := dbfx.Agent(t, "duration agent", runtimeID)
	now := time.Now().UTC().Truncate(time.Second)
	// More than one history page: duration must never be calculated from the
	// newest 200 rows or change when another page is opened.
	for i := 0; i < 201; i++ {
		duration := time.Minute
		if i == 0 {
			duration = 10 * time.Minute
		}
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "started_at": now.Add(-duration), "completed_at": now})
	}
	for _, cols := range []testutil.Cols{
		{"status": "completed", "started_at": now.Add(-32 * 24 * time.Hour), "completed_at": now.Add(-31 * 24 * time.Hour)},
		{"status": "cancelled", "completed_at": now},
		{"status": "failed", "started_at": now.Add(time.Second), "completed_at": now},
		{"status": "running", "started_at": now},
	} {
		cols["runtime_id"] = runtimeID
		dbfx.Task(t, agentID, cols)
	}
	req := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/agent-activity-30d", nil))
	var buckets []AgentActivityBucket
	testutil.Call(t, testHandler.GetWorkspaceAgentActivity30d, req).Want(http.StatusOK).JSON(&buckets)
	var count int32
	var duration float64
	for _, bucket := range buckets {
		if bucket.AgentID == agentID {
			count += bucket.DurationCount
			duration += bucket.DurationMs
		}
	}
	if count != 201 || duration != 210*60000 {
		t.Fatalf("duration/count = %v/%d, want %d/201", duration, count, 210*60000)
	}
}


func TestListAgentTasksCursorDoesNotGrantPrivateAccess(t *testing.T) {
	agentID, ownerID, memberID := privateAgentTestFixture(t)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		dbfx.Task(t, agentID, testutil.Cols{"status": "completed", "completed_at": now, "created_at": now.Add(-time.Duration(i) * time.Minute)})
	}
	var first []AgentTaskResponse
	req := withURLParam(newRequestAs(ownerID, http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1", nil), "id", agentID)
	response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&first)
	cursor := response.Header().Get(HeaderAgentTasksNextCursor)
	if len(first) != 1 || cursor == "" {
		t.Fatal("expected a first page and cursor")
	}
	for _, userID := range []string{memberID, ownerID} {
		status := http.StatusForbidden
		if userID == ownerID {
			status = http.StatusOK
		}
		req := withURLParam(newRequestAs(userID, http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", agentID)
		response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(status)
		if userID == memberID && response.Header().Get(HeaderAgentTasksNextCursor) != "" {
			t.Fatal("forbidden response leaked a continuation")
		}
		if userID == ownerID {
			var second []AgentTaskResponse
			response.JSON(&second)
			if len(second) != 1 || second[0].ID == first[0].ID {
				t.Fatalf("bad owner continuation: %+v", second)
			}
		}
	}
}

func TestListAgentTasksUsageAndAgentScopeAcrossPages(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "paged usage runtime")
	agentID := dbfx.Agent(t, "paged usage agent", runtimeID)
	otherID := dbfx.Agent(t, "other paged usage agent", runtimeID)
	now := time.Now().UTC()
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "created_at": now.Add(-time.Duration(i) * time.Minute)})
		dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": ids[i], "provider": "openai", "model": "test-model", "input_tokens": i + 10})
	}
	otherTask := dbfx.Task(t, otherID, testutil.Cols{"runtime_id": runtimeID, "created_at": now.Add(-time.Minute)})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": otherTask, "provider": "openai", "model": "private-other-model", "input_tokens": 999})
	cursor := ""
	for i, id := range ids {
		req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", agentID)
		var tasks []AgentTaskResponse
		response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
		if len(tasks) != 1 || tasks[0].ID != id {
			t.Fatalf("page %d: %+v", i, tasks)
		}
		usage := tasks[0].Usage
		if len(usage) != 1 || usage[0].InputTokens != int64(i+10) || usage[0].Model != "test-model" {
			t.Fatalf("page %d usage: %+v", i, usage)
		}
		cursor = response.Header().Get(HeaderAgentTasksNextCursor)
	}
	if cursor != "" {
		t.Fatal("last usage page still has continuation")
	}
	// A cursor minted for a different agent is only a position, never a way
	// to read that agent's rows or its usage.
	cursor = transcriptCursor(now, parseUUID(ids[0]))
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+otherID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", otherID)
	var tasks []AgentTaskResponse
	testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
	if len(tasks) != 1 || tasks[0].ID != otherTask || len(tasks[0].Usage) != 1 || tasks[0].Usage[0].Model != "private-other-model" {
		t.Fatalf("cross-agent cursor changed ownership scope: %+v", tasks)
	}
}
