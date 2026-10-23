package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Exercise evaluation and diagnostic persistence even without local PostgreSQL.
// The DB-backed test covers the full registration/dispatch and parent-link path.
type prConditionTx struct {
	*sql.Tx
	issue     string
	prs       [][]any
	queryErr  error
	lastError sql.NullString
	next      sql.NullString
	writes    int
}

func (tx *prConditionTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	memDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	if strings.Contains(query, "UPDATE issue_wakeup_receipt") {
		return memDB.QueryContext(ctx, "SELECT '' WHERE 0")
	}
	if !strings.Contains(query, "WHERE ipr.issue_id=$1") || len(args) != 1 || args[0] != tx.issue {
		return nil, fmt.Errorf("unexpected PR scope: %s %v", query, args)
	}
	if _, err := memDB.ExecContext(ctx, "CREATE TABLE pr_rows (pr_number INTEGER, state TEXT, head TEXT, checks TEXT)"); err != nil {
		return nil, err
	}
	for _, r := range tx.prs {
		if _, err := memDB.ExecContext(ctx, "INSERT INTO pr_rows VALUES (?, ?, ?, ?)", r[0], r[1], r[2], r[3]); err != nil {
			return nil, err
		}
	}
	return memDB.QueryContext(ctx, "SELECT pr_number, state, head, checks FROM pr_rows")
}

func (tx *prConditionTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "NoteWakeupFailure"):
		tx.lastError = args[0].(sql.NullString)
		tx.writes++
	case strings.Contains(query, "SetWakeupConditionState"):
		tx.next = args[1].(sql.NullString)
	default:
		return stubResult{}, fmt.Errorf("unexpected write: %s", query)
	}
	return stubResult{}, nil
}

func TestWakeupConditionPREvaluation(t *testing.T) {
	row := func(state, head, checks string) []any { return []any{int32(1), state, head, checks} }
	for _, tc := range []struct {
		name, event string
		available   bool
		prs         [][]any
		wantErr     error
		wantMet     bool
	}{
		{"unconfigured", "checks_finished", false, nil, errPRSnapshotsUnavailable, false},
		{"unlinked", "checks_finished", true, nil, errPRNotLinked, false},
		{"unlinked merged", "merged", false, nil, errPRNotLinked, false},
		{"no snapshot", "checks_finished", true, [][]any{row("open", "", "SUCCESS")}, errPRSnapshotPending, false},
		{"pending", "checks_finished", true, [][]any{row("open", "head", "PENDING")}, nil, false},
		{"no checks", "checks_finished", true, [][]any{row("open", "head", "")}, nil, false},
		{"success", "checks_finished", true, [][]any{row("open", "head", "SUCCESS")}, nil, true},
		{"failure", "checks_finished", true, [][]any{row("open", "head", "FAILURE")}, nil, true},
		{"error", "checks_finished", true, [][]any{row("open", "head", "ERROR")}, nil, true},
		{"one matching PR", "checks_finished", true, [][]any{row("open", "", ""), row("open", "head", "SUCCESS")}, nil, true},
		{"merge without snapshots", "merged", false, [][]any{row("merged", "", "")}, nil, true},
		{"open without snapshots", "merged", false, [][]any{row("open", "", "")}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &IssueWakeupService{Tasks: &TaskService{PRSnapshotsEnabled: tc.available}}
			w := db.IssueWakeup{IssueID: newV7(), Condition: nullString(string(condition(t, map[string]any{"type": "pull_request", "event": tc.event})))}
			tx := &prConditionTx{issue: w.IssueID, prs: tc.prs}
			met, fingerprint, observed, err := s.evaluateCondition(context.Background(), tx, db.New(tx), w)
			if met != tc.wantMet || !errors.Is(err, tc.wantErr) {
				t.Fatalf("met=%t err=%v; want met=%t err=%v", met, err, tc.wantMet, tc.wantErr)
			}
			if met && (fingerprint == "" || len(observed["pull_requests"].([]map[string]any)) != 1) {
				t.Fatalf("missing terminal PR evidence: fingerprint=%q observed=%v", fingerprint, observed)
			}
		})
	}
}

func TestWakeupConditionPRDiagnosticPolling(t *testing.T) {
	ctx := context.Background()
	s := &IssueWakeupService{Tasks: &TaskService{}}
	w := db.IssueWakeup{ID: newV7(), IssueID: newV7(), Condition: nullString(string(condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})))}
	tx := &prConditionTx{issue: w.IssueID}
	q := db.New(tx)
	now := time.Now()
	wantUnavailable := conditionDiagnosticPrefix + errPRSnapshotsUnavailable.Error()
	if err := s.baselineCondition(ctx, tx, q, w, now); err != nil || tx.lastError.String != wantUnavailable {
		t.Fatalf("registration must retain diagnostic without rejecting: %v, %+v", err, tx.lastError)
	}
	w.LastError = tx.lastError
	if err := s.baselineCondition(ctx, tx, q, w, now); err != nil || tx.writes != 1 {
		t.Fatalf("unchanged diagnostic must not be rewritten: err=%v writes=%d", err, tx.writes)
	}
	s.Tasks.PRSnapshotsEnabled = true
	for _, step := range []struct {
		prs  [][]any
		want error
	}{
		{nil, errPRNotLinked},
		{[][]any{{int32(1), "open", "", ""}}, errPRSnapshotPending},
		{[][]any{{int32(1), "open", "head", "PENDING"}}, nil},
	} {
		tx.prs = step.prs
		w.LastError = tx.lastError
		next, err := s.pollCondition(ctx, tx, q, w, now)
		if err != nil || !next.Valid || !asTime(next).Equal(now.Add(conditionPollInterval)) {
			t.Fatalf("poll must remain scheduled: next=%+v err=%v", next, err)
		}
		if step.want == nil {
			if tx.lastError.Valid {
				t.Fatalf("recovery retained diagnostic: %+v", tx.lastError)
			}
		} else if tx.lastError.String != conditionDiagnosticPrefix+step.want.Error() {
			t.Fatalf("diagnostic=%+v, want %v", tx.lastError, step.want)
		}
	}
	// Real query failures still propagate; they are never treated as unmet facts.
	tx.queryErr = errors.New("database offline")
	if _, err := s.pollCondition(ctx, tx, q, w, now); !errors.Is(err, tx.queryErr) {
		t.Fatalf("database error swallowed: %v", err)
	}
}

func TestSaveConditionDiagnosticPreservesDispatchErrors(t *testing.T) {
	ctx := context.Background()
	tx := &prConditionTx{}
	q := db.New(tx)
	realFailure := sql.NullString{String: "dispatch failed", Valid: true}
	w := db.IssueWakeup{ID: newV7(), LastError: realFailure}

	if err := saveConditionDiagnostic(ctx, q, w, errPRNotLinked); err != nil {
		t.Fatal(err)
	}
	if err := saveConditionDiagnostic(ctx, q, w, nil); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 0 || tx.lastError.Valid {
		t.Fatalf("condition evaluation changed an unrelated error: writes=%d error=%+v", tx.writes, tx.lastError)
	}

	w.LastError = sql.NullString{String: conditionDiagnosticPrefix + errPRNotLinked.Error(), Valid: true}
	if err := saveConditionDiagnostic(ctx, q, w, errPRSnapshotPending); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 1 || tx.lastError.String != conditionDiagnosticPrefix+errPRSnapshotPending.Error() {
		t.Fatalf("owned diagnostic was not updated: writes=%d error=%+v", tx.writes, tx.lastError)
	}
	w.LastError = tx.lastError
	if err := saveConditionDiagnostic(ctx, q, w, nil); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 2 || tx.lastError.Valid {
		t.Fatalf("owned diagnostic was not cleared: writes=%d error=%+v", tx.writes, tx.lastError)
	}
}
