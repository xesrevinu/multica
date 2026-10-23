package handler

import (
	"context"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)


func TestIssueActivityTimestampIsMonotonic(t *testing.T) {
	created := createIssueForTest(t, map[string]any{"title": "monotonic activity"})
	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET last_activity_at = $2 WHERE id = $1`, created.ID, future); err != nil {
		t.Fatalf("seed future last_activity_at: %v", err)
	}
	_, err := testHandler.Queries.SetIssueMetadataKey(context.Background(), db.SetIssueMetadataKeyParams{
		Key:         nullString("activity_test"),
		Value:       []byte(`"changed"`),
		ID:          parseUUID(created.ID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("SetIssueMetadataKey: %v", err)
	}
	var updatedActivity time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&updatedActivity); err != nil {
		t.Fatalf("read updated last_activity_at: %v", err)
	}
	if !updatedActivity.Equal(future) {
		t.Fatalf("activity timestamp regressed: got=%v want=%s", updatedActivity, future)
	}
}

func TestIssueDecorationActivityOnlyAdvancesOnChange(t *testing.T) {
	ctx := context.Background()
	created := createIssueForTest(t, map[string]any{"title": "decoration activity"})
	issueID := parseUUID(created.ID)
	workspaceID := parseUUID(testWorkspaceID)
	labelID := parseUUID(createTestIssueLabel(t, "activity-label-"+created.ID[:8]))
	base := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)

	reset := func() {
		t.Helper()
		if _, err := testPool.Exec(ctx, `UPDATE issue SET last_activity_at = $2 WHERE id = $1`, issueID, base); err != nil {
			t.Fatalf("reset last_activity_at: %v", err)
		}
	}
	read := func() time.Time {
		t.Helper()
		var got time.Time
		if err := testPool.QueryRow(ctx, `SELECT last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&got); err != nil {
			t.Fatalf("read last_activity_at: %v", err)
		}
		return got
	}

	reset()
	if _, err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: issueID, LabelID: labelID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("AttachLabelToIssue: %v", err)
	}
	if got := read(); !got.After(base) {
		t.Fatalf("label attach did not advance activity: base=%s got=%s", base, got)
	}
	reset()
	if _, err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: issueID, LabelID: labelID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("duplicate AttachLabelToIssue: %v", err)
	}
	if got := read(); !got.Equal(base) {
		t.Fatalf("duplicate label attach changed activity: base=%s got=%s", base, got)
	}

	reset()
	if _, err := testHandler.Queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
		Key:         nullString("00000000-0000-0000-0000-000000000001"),
		Value:       []byte(`"selected"`),
		ID:          issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("SetIssuePropertyValue: %v", err)
	}
	if got := read(); !got.After(base) {
		t.Fatalf("property change did not advance activity: base=%s got=%s", base, got)
	}
	reset()
	if _, err := testHandler.Queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
		Key:         nullString("00000000-0000-0000-0000-000000000001"),
		Value:       []byte(`"selected"`),
		ID:          issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("duplicate SetIssuePropertyValue: %v", err)
	}
	if got := read(); !got.Equal(base) {
		t.Fatalf("duplicate property write changed activity: base=%s got=%s", base, got)
	}
}

func TestMarkIssueFirstExecutedDoesNotChangeActivity(t *testing.T) {
	created := createIssueForTest(t, map[string]any{"title": "execution is not activity"})
	if created.LastActivityAt == nil {
		t.Fatal("CreateIssue response omitted last_activity_at")
	}
	var before time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&before); err != nil {
		t.Fatalf("read last_activity_at before: %v", err)
	}
	if _, err := testHandler.Queries.MarkIssueFirstExecuted(context.Background(), parseUUID(created.ID)); err != nil {
		t.Fatalf("MarkIssueFirstExecuted: %v", err)
	}
	var after time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&after); err != nil {
		t.Fatalf("read last_activity_at: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("execution lifecycle changed activity: before=%s after=%s", before, after)
	}
}
