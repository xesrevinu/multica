package issueposition

import (
	"context"
	"database/sql"
	"fmt"
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NextTopPosition returns a position that sorts before every existing issue in
// the workspace/status column when manual sorting orders by position ASC.
//
// UpdateIssue and UpdateIssueStatus in queries/issue.sql inline the same
// MIN(position) - 1 policy to re-rank an issue whose status moves it to a
// different column. Change them together.
func NextTopPosition(ctx context.Context, q queryRower, workspaceID string, status string) (float64, error) {
	var minPos float64
	if err := q.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(position), 0) FROM issue WHERE workspace_id = ?1 AND status = ?2`,
		workspaceID, status,
	).Scan(&minPos); err != nil {
		return 0, fmt.Errorf("query min issue position: %w", err)
	}
	return minPos - 1, nil
}
