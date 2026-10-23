package handler

import (
	"context"
	"strings"

	"database/sql"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Expire the request only after the service has committed the claim. The next
// request uses the same live WebSocket and database, without the injected stall.
type expireClaimBuildDB struct {
	db.DBTX
	expired bool
}

func (d *expireClaimBuildDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if !d.expired && strings.Contains(query, "-- name: GetWorkspace :one") {
		d.expired = true
		<-ctx.Done()
		return errSQLRow(ctx.Err())
	}
	return d.DBTX.QueryRowContext(ctx, query, args...)
}


