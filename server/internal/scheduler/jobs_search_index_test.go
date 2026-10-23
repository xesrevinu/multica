package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSearchIndexChangePruneJobRemovesExpiredRowsAndRaisesMark(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	var previousMark *int64
	_ = pool.QueryRowContext(ctx, `SELECT pruned_through_seq FROM search_index_prune_mark`).Scan(&previousMark)
	t.Cleanup(func() {
		if previousMark == nil {
			_, _ = pool.ExecContext(context.Background(), `DELETE FROM search_index_prune_mark`)
			return
		}
		_, _ = pool.ExecContext(context.Background(), `UPDATE search_index_prune_mark SET pruned_through_seq = $1`, *previousMark)
	})

	workspaceID := uuid.New()
	expiredID, freshID := uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.ExecContext(context.Background(), `DELETE FROM search_index_change WHERE workspace_id = $1`, workspaceID.String())
	})
	planTime := time.Now().UTC().Truncate(time.Hour)
	var expiredSeq int64
	if err := pool.QueryRowContext(ctx, `
		INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
		VALUES ('comment', $1, $2, 1, $3)
		RETURNING change_seq
	`, expiredID.String(), workspaceID.String(), formatDBTime(planTime.Add(-SearchIndexChangeRetention-time.Hour))).Scan(&expiredSeq); err != nil {
		t.Fatalf("insert expired change: %v", err)
	}
	if _, err := pool.ExecContext(ctx, `
		INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
		VALUES ('comment', $1, $2, 2, $3)
	`, freshID.String(), workspaceID.String(), formatDBTime(planTime.Add(-time.Hour))); err != nil {
		t.Fatalf("insert fresh change: %v", err)
	}

	job := SearchIndexChangePruneJob(db.New(pool))
	result, err := job.Handler(ctx, HandlerInput{Job: &job, Scope: ScopeGlobal, PlanTime: planTime})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.RowsAffected < 1 {
		t.Fatalf("prune removed %d rows, want at least the expired one", result.RowsAffected)
	}

	var remaining []uuid.UUID
	rows, err := pool.QueryContext(ctx, `SELECT entity_id FROM search_index_change WHERE workspace_id = $1`, workspaceID.String())
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			t.Fatalf("scan remaining: %v", err)
		}
		id, _ := uuid.Parse(idStr)
		remaining = append(remaining, id)
	}
	if len(remaining) != 1 || remaining[0] != freshID {
		t.Fatalf("remaining = %v, want only the fresh row %s", remaining, freshID)
	}

	var covered bool
	if err := pool.QueryRowContext(ctx, `
		SELECT pruned_through_seq >= $1 FROM search_index_prune_mark WHERE singleton = 1
	`, expiredSeq).Scan(&covered); err != nil {
		t.Fatalf("read prune mark: %v", err)
	}
	if !covered {
		t.Fatalf("prune mark must cover the pruned seq %d", expiredSeq)
	}
}
