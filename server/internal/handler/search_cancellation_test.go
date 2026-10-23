package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// searchFailureTx injects a query failure after the transaction-local settings
// have been applied, including cancellation while the search is in flight.
type searchFailureTx struct {
	*sql.Tx

	err          error
	cancel       context.CancelFunc
	rows         *sql.Rows
	commitErr    error
	commitCancel context.CancelFunc
}

func (tx searchFailureTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (tx searchFailureTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	if tx.rows != nil {
		return tx.rows, nil
	}
	if tx.cancel != nil {
		tx.cancel()
	}
	return nil, tx.err
}

func (tx searchFailureTx) Rollback() error { return nil }

func (tx searchFailureTx) Commit() error {
	if tx.commitCancel != nil {
		tx.commitCancel()
	}
	return tx.commitErr
}

type searchFailureTxStarter struct{ tx searchFailureTx }

func (s searchFailureTxStarter) BeginTx(_ context.Context, _ *sql.TxOptions) (handlerTx, error) {
	return s.tx, nil
}



func assertSearchStatus(t *testing.T, h *Handler, endpoint string, ctx context.Context, status int) {
	t.Helper()
	registry := metrics.NewRegistry(metrics.RegistryOptions{})
	router := chi.NewRouter()
	router.Use(registry.HTTP.Middleware)
	path := "/api/" + endpoint + "/search"
	if endpoint == "issues" {
		router.Get(path, h.SearchIssues)
	} else {
		router.Get(path, h.SearchProjects)
	}
	req := newRequest(http.MethodGet, path+"?q=SearchCancellationEnrichment&include_closed=true", nil).WithContext(ctx)
	response := testutil.Call(t, router.ServeHTTP, req).Want(status)
	if status == 499 && response.Body.Len() != 0 {
		t.Fatalf("disconnected search wrote a response body: %s", response.Body.String())
	}

	rec := httptest.NewRecorder()
	metrics.NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	want := `multica_http_requests_total{method="GET",route="` + path + `",status="` + strconv.Itoa(status) + `"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("missing request classification %q in metrics:\n%s", want, body)
	}
	if status == 499 {
		for _, incorrect := range []string{`status="200"`, `status="500"`, `status="503"`} {
			if strings.Contains(body, incorrect) {
				t.Fatalf("canceled search counted as %s:\n%s", incorrect, body)
			}
		}
	}
}




// These wrappers cancel only after a real supplementary read completes. The
// primary search and commit succeed, so their error branches cannot catch it.
type searchEnrichmentDB struct {
	db.DBTX
	cancel   context.CancelFunc
	canceled bool
}

func (d *searchEnrichmentDB) stop() {
	d.canceled = true
	d.cancel()
}

func (d *searchEnrichmentDB) QueryRowContext(ctx context.Context, sqlQuery string, args ...any) *sql.Row {
	row := d.DBTX.QueryRowContext(ctx, sqlQuery, args...)
	if strings.HasPrefix(sqlQuery, "-- name: GetWorkspace :one") {
		d.stop()
	}
	return row
}

func (d *searchEnrichmentDB) QueryContext(ctx context.Context, sqlQuery string, args ...any) (*sql.Rows, error) {
	rows, err := d.DBTX.QueryContext(ctx, sqlQuery, args...)
	if err == nil && strings.HasPrefix(sqlQuery, "-- name: GetProjectResourceCounts :many") {
		d.stop()
	}
	return rows, err
}




type searchBlockingTxStarter struct {
	txStarter
	started chan struct{}
}

func (s searchBlockingTxStarter) BeginTx(ctx context.Context, _ *sql.TxOptions) (handlerTx, error) {
	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return searchBlockingTx{handlerTx: tx, started: s.started}, nil
}

type searchBlockingTx struct {
	handlerTx
	started chan struct{}
}

const cancellationSleepQuery = "WITH RECURSIVE r(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM r WHERE i < 100000000) SELECT sum(i) FROM r"

func (tx searchBlockingTx) QueryContext(ctx context.Context, _ string, _ ...any) (*sql.Rows, error) {
	if tx.started != nil {
		close(tx.started)
	}
	return tx.handlerTx.QueryContext(ctx, cancellationSleepQuery)
}

