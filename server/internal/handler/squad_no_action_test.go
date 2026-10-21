package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type runningSquadLeaderTaskFixture struct {
	IssueID          string
	SquadID          string
	LeaderID         string
	TaskID           string
	TriggerCommentID string
}

func newRunningSquadLeaderTaskFixture(t *testing.T) runningSquadLeaderTaskFixture {
	t.Helper()

	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID)

	triggerCommentID := dbfx.Comment(t, issueID, "LGTM")

	// is_leader_task + squad_id are what RecordSquadLeaderEvaluation authorizes
	// against (MUL-6622); a leader task without them is not a leader turn.
	taskID := dbfx.Task(t, fx.LeaderID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": triggerCommentID,
		"status":             "running",
		"started_at":         testutil.Raw("now()"),
		"is_leader_task":     true,
		"squad_id":           fx.SquadID,
	})

	return runningSquadLeaderTaskFixture{
		IssueID:          issueID,
		SquadID:          fx.SquadID,
		LeaderID:         fx.LeaderID,
		TaskID:           taskID,
		TriggerCommentID: triggerCommentID,
	}
}

func countAgentCommentsForIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
	`, issueID, agentID).Scan(&count); err != nil {
		t.Fatalf("count agent comments: %v", err)
	}
	return count
}
