package issueguard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func NormalizeTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func DuplicateMessage(identifier, title, status string) string {
	return "Active duplicate issue exists: " + identifier + " " + title + " (status: " + status + "). Set allow_duplicate=true or use --allow-duplicate to create another."
}

type ActiveDuplicateError struct {
	ID         string
	Identifier string
	Title      string
	Status     string
}

func (e *ActiveDuplicateError) Error() string {
	return DuplicateMessage(e.Identifier, e.Title, e.Status)
}

func NewActiveDuplicateError(issue db.Issue, issuePrefix string) *ActiveDuplicateError {
	return &ActiveDuplicateError{
		ID:         util.UUIDToString(issue.ID),
		Identifier: fmt.Sprintf("%s-%d", issuePrefix, issue.Number),
		Title:      issue.Title,
		Status:     issue.Status,
	}
}

func inactiveStatusKeys(ctx context.Context, q *db.Queries, workspaceID string) ([]string, error) {
	return issuestatus.ExpandCategories(ctx, q, workspaceID, []string{
		issuestatus.CategoryDone,
		issuestatus.CategoryClosed,
	})
}

func LockAndFindActiveDuplicate(
	ctx context.Context,
	q *db.Queries,
	workspaceID string,
	projectID sql.NullString,
	parentIssueID sql.NullString,
	title string,
	allowDuplicate bool,
) (db.Issue, bool, error) {
	normalizedTitle := NormalizeTitle(title)
	if normalizedTitle == "" {
		return db.Issue{}, false, nil
	}
	if err := q.LockIssueDuplicateKey(ctx); err != nil {
		return db.Issue{}, false, err
	}
	if allowDuplicate {
		return db.Issue{}, false, nil
	}
	inactiveKeys, err := inactiveStatusKeys(ctx, q, workspaceID)
	if err != nil {
		return db.Issue{}, false, err
	}

	keysJSON, err := json.Marshal(inactiveKeys)
	if err != nil {
		return db.Issue{}, false, err
	}
	duplicate, err := q.FindActiveDuplicateIssue(ctx, db.FindActiveDuplicateIssueParams{
		WorkspaceID:        workspaceID,
		TerminalStatusKeys: string(keysJSON),
		ProjectID:          projectID,
		ParentIssueID:      parentIssueID,
		NormalizedTitle:    normalizedTitle,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, err
	}
	return duplicate, true, nil
}

func LockAndFindRecentAutopilotDuplicate(
	ctx context.Context,
	q *db.Queries,
	workspaceID string,
	autopilotID string,
	projectID sql.NullString,
	title string,
	window time.Duration,
) (db.Issue, bool, error) {
	normalizedTitle := NormalizeTitle(title)
	if normalizedTitle == "" || autopilotID == "" || window <= 0 {
		return db.Issue{}, false, nil
	}
	if err := q.LockIssueDuplicateKey(ctx); err != nil {
		return db.Issue{}, false, err
	}
	inactiveKeys, err := inactiveStatusKeys(ctx, q, workspaceID)
	if err != nil {
		return db.Issue{}, false, err
	}

	duplicate, err := q.FindRecentAutopilotDuplicateIssue(ctx, db.FindRecentAutopilotDuplicateIssueParams{
		WorkspaceID:        workspaceID,
		TerminalStatusKeys: inactiveKeys,
		OriginID:           sql.NullString{String: autopilotID, Valid: true},
		ProjectID:          projectID,
		NormalizedTitle:    normalizedTitle,
		CreatedAfter:       time.Now().UTC().Add(-window).Format(time.RFC3339Nano),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, err
	}
	return duplicate, true, nil
}
