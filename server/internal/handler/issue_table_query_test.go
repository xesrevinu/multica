package handler

import (
	"context"
	"database/sql"
	"strings"


)

type issueTableEnrichmentFailTxStarter struct {
	inner           txStarter
	labelCalls      *int
	tableQueryCalls *int
	facetQueryCalls *int
	rowQuerySQL     *string
	groupQuerySQL   *string
}

func (s issueTableEnrichmentFailTxStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (handlerTx, error) {
	tx, err := s.inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &issueTableEnrichmentFailTx{
		handlerTx:       tx,
		labelCalls:      s.labelCalls,
		tableQueryCalls: s.tableQueryCalls,
		facetQueryCalls: s.facetQueryCalls,
		rowQuerySQL:     s.rowQuerySQL,
		groupQuerySQL:   s.groupQuerySQL,
	}, nil
}

type issueTableEnrichmentFailTx struct {
	handlerTx

	labelCalls      *int
	tableQueryCalls *int
	facetQueryCalls *int
	rowQuerySQL     *string
	groupQuerySQL   *string
}

func (tx *issueTableEnrichmentFailTx) recordTableQuery(sql string) {
	if tx.tableQueryCalls != nil {
		if strings.Contains(sql, "page AS MATERIALIZED (") ||
			strings.Contains(sql, "SELECT COUNT(*)::bigint FROM issue i WHERE") {
			*tx.tableQueryCalls = *tx.tableQueryCalls + 1
		}
	}
	if tx.facetQueryCalls != nil && strings.Contains(sql, "GROUP BY GROUPING SETS") {
		*tx.facetQueryCalls = *tx.facetQueryCalls + 1
	}
	if tx.rowQuerySQL != nil && strings.Contains(sql, "page AS MATERIALIZED (") {
		*tx.rowQuerySQL = sql
	}
	if tx.groupQuerySQL != nil && strings.Contains(sql, "WITH grouped AS (") {
		*tx.groupQuerySQL = sql
	}
}

func (tx *issueTableEnrichmentFailTx) QueryContext(ctx context.Context, sqlQuery string, args ...any) (*sql.Rows, error) {
	tx.recordTableQuery(sqlQuery)
	if strings.Contains(sqlQuery, "ListLabelsForIssues") {
		*tx.labelCalls = *tx.labelCalls + 1
		_, err := tx.handlerTx.ExecContext(ctx, "SELECT * FROM issue_table_missing_enrichment_relation")
		return nil, err
	}
	return tx.handlerTx.QueryContext(ctx, sqlQuery, args...)
}

func (tx *issueTableEnrichmentFailTx) QueryRowContext(ctx context.Context, sqlQuery string, args ...any) *sql.Row {
	tx.recordTableQuery(sqlQuery)
	return tx.handlerTx.QueryRowContext(ctx, sqlQuery, args...)
}

















// TestIssueTableSelectPropertySortKeysetPagination walks the keyset cursor
// over a select-property sort. The sort expression ranks options by option
// order and carries the "::numeric" token that issueTableOrderBy sniffs into
// the cursor's cast type, so this pins three things at once: ordinal order,
// the numeric cursor round-trip (equal ranks tie, valueless rows trail), and
// no duplicated or skipped rows across pages. Explicit option ids in reverse
// lexical order make the ordinal assertions deterministic — a regression to
// raw-value ordering fails every run, not most runs.

