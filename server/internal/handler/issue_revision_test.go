package handler

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

)

var errInjectedIssueAttachmentLink = errors.New("injected issue attachment link failure")

type failIssueAttachmentLinkTxStarter struct {
	inner txStarter
}

func (s failIssueAttachmentLinkTxStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (handlerTx, error) {
	tx, err := s.inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &failIssueAttachmentLinkTx{handlerTx: tx}, nil
}

type failIssueAttachmentLinkTx struct {
	handlerTx
}

func (tx *failIssueAttachmentLinkTx) QueryRowContext(ctx context.Context, sqlQuery string, args ...any) *sql.Row {
	if strings.Contains(sqlQuery, "LinkAttachmentsToIssue") {
		return errSQLRow(errInjectedIssueAttachmentLink)
	}
	return tx.handlerTx.QueryRowContext(ctx, sqlQuery, args...)
}

type failingIssueAttachmentLinkRow struct{}

func (failingIssueAttachmentLinkRow) Scan(...any) error {
	return errInjectedIssueAttachmentLink
}

type pauseIssueAttachmentLinkTxStarter struct {
	inner   txStarter
	reached chan<- struct{}
	release <-chan struct{}
}

func (s pauseIssueAttachmentLinkTxStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (handlerTx, error) {
	tx, err := s.inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &pauseIssueAttachmentLinkTx{
		handlerTx: tx,
		reached:   s.reached,
		release:   s.release,
	}, nil
}

type pauseIssueAttachmentLinkTx struct {
	handlerTx

	reached chan<- struct{}
	release <-chan struct{}
}

func (tx *pauseIssueAttachmentLinkTx) QueryRowContext(ctx context.Context, sqlQuery string, args ...any) *sql.Row {
	if strings.Contains(sqlQuery, "LinkAttachmentsToIssue") {
		tx.reached <- struct{}{}
		select {
		case <-tx.release:
		case <-ctx.Done():
			return errSQLRow(errInjectedIssueAttachmentLink)
		}
	}
	return tx.handlerTx.QueryRowContext(ctx, sqlQuery, args...)
}

func insertWorkflowTestIssue(t *testing.T, title string, number int) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("insert workflow issue: %v", err)
	}
	return id
}



// A description stored with surrounding whitespace — every CLI/integration
// write that keeps a trailing newline — used to reject EVERY edit from the web
// editor, which sends its base trimmed. No second writer was involved, and the
// rejection repeated for the life of the session (MUL-6971).







