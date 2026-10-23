package handler

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type slowBatchClaimDB struct {
	db.DBTX
	failingAgent  string
	expired       bool
	recoveryDelay time.Duration
}

func (d *slowBatchClaimDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if !d.expired && strings.Contains(query, "-- name: GetAgent :one") && args[0] == d.failingAgent {
		d.expired = true
		<-ctx.Done()
		return errSQLRow(ctx.Err())
	}
	if strings.Contains(query, "-- name: RequeueAgentTaskAfterClaimFailure :one") && ctx.Err() == nil {
		timer := time.NewTimer(d.recoveryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return errSQLRow(ctx.Err())
		}
	}
	return d.DBTX.QueryRowContext(ctx, query, args...)
}

// A builds successfully, B exhausts the build deadline, and recovery remains
// slow. A must still reach installed WS (7s) and HTTP (5s) clients. This failed
// at about 8s with the previous independent five-second recovery budget.

func TestBatchClaimDeadlines(t *testing.T) {
	start := time.Now()
	for _, tt := range []struct {
		name            string
		caller          time.Duration
		build, response time.Duration
	}{
		{name: "HTTP without deadline", build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "WS budget", caller: 5 * time.Second, build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "shorter caller", caller: 2 * time.Second, build: 1300 * time.Millisecond, response: 1800 * time.Millisecond},
		{name: "longer caller", caller: 30 * time.Second, build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "expired caller", caller: -time.Second, build: -time.Second, response: -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.caller != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, start.Add(tt.caller))
				defer cancel()
			}
			build, response := batchClaimDeadlines(ctx, start)
			if build.Sub(start) != tt.build || response.Sub(start) != tt.response {
				t.Fatalf("build=%s response=%s, want %s/%s", build.Sub(start), response.Sub(start), tt.build, tt.response)
			}
		})
	}
}

type retryChatInputDB struct {
	db.DBTX
	failures int
}

func (d *retryChatInputDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if d.failures > 0 && strings.Contains(query, "-- name: ListChatInputMessages :many") {
		d.failures--
		return nil, context.DeadlineExceeded
	}
	return d.DBTX.QueryContext(ctx, query, args...)
}

