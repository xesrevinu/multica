package handler

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A page resolves the originals its duplicates point at in one read, the way
// it loads their labels (MUL-7349). Per-row reads made a page of N duplicates
// of N different issues cost N extra round trips.

const (
	queryIssueRefs = "-- name: ListIssueRefsInWorkspace :many"
	queryIssueRef  = "-- name: GetIssueRefInWorkspace :one"
)

// issueRefSpy passes every statement through to the real pool and records the
// original lookups: how many ids each batched read was given, and how many
// single reads ran.
type issueRefSpy struct {
	inner db.DBTX

	mu          sync.Mutex
	batchedIDs  []int
	singleReads int
}

func (s *issueRefSpy) ExecContext(ctx context.Context, sqlQuery string, args ...any) (sql.Result, error) {
	return s.inner.ExecContext(ctx, sqlQuery, args...)
}

func (s *issueRefSpy) PrepareContext(ctx context.Context, sqlQuery string) (*sql.Stmt, error) {
	return s.inner.PrepareContext(ctx, sqlQuery)
}

func (s *issueRefSpy) QueryContext(ctx context.Context, sql string, args ...any) (*sql.Rows, error) {
	if strings.Contains(sql, queryIssueRefs) {
		s.mu.Lock()
		s.batchedIDs = append(s.batchedIDs, uuidArgLen(args, 1))
		s.mu.Unlock()
	}
	return s.inner.QueryContext(ctx, sql, args...)
}

func (s *issueRefSpy) QueryRowContext(ctx context.Context, sql string, args ...any) *sql.Row {
	if strings.Contains(sql, queryIssueRef) {
		s.mu.Lock()
		s.singleReads++
		s.mu.Unlock()
	}
	return s.inner.QueryRowContext(ctx, sql, args...)
}

func (s *issueRefSpy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchedIDs = nil
	s.singleReads = 0
}

func (s *issueRefSpy) snapshot() (batched []int, single int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.batchedIDs...), s.singleReads
}


// A response built on its own, with nothing passed up front, still resolves
// its original.
func TestDuplicateOriginalResolvesWithoutPriming(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-unprimed-original")
	duplicate := seedDuplicate(t, "dup-unprimed-duplicate", original)
	row, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(duplicate))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	resp := issueToResponse(row, "MUL")
	testHandler.newStatusCategoryFiller(context.Background(), row.WorkspaceID)(&resp)
	if resp.DuplicateOf == nil || resp.DuplicateOf.ID != original {
		t.Fatalf("duplicate_of = %+v, want %s", resp.DuplicateOf, original)
	}
}
