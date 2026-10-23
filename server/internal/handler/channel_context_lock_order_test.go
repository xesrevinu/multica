package handler

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"


	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type contextLockTestDB struct {
	db.DBTX
	pauseAfter    string
	paused        chan struct{}
	release       chan struct{}
	observeBefore string
	observed      chan struct{}
}

func (d *contextLockTestDB) WrapTx(tx db.DBTX) db.DBTX {
	cp := *d
	cp.DBTX = tx
	return &cp
}

func (d *contextLockTestDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if d.observeBefore != "" && strings.Contains(query, d.observeBefore) {
		close(d.observed)
	}
	row := d.DBTX.QueryRowContext(ctx, query, args...)
	if d.pauseAfter != "" && strings.Contains(query, d.pauseAfter) {
		close(d.paused)
		<-d.release
	}
	return row
}

// TestChannelContextLockOrder serializes the production enqueue and append
// paths at the exact lock edge that previously formed an ABBA cycle. Enqueue
// pauses while holding the target generation; a concurrent append then starts
// its binding lock. Both must complete after enqueue is released, proving that
// enqueue already owns binding before generation and append cannot invert it.

func waitContextLockSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal(failure)
	}
}

func waitContextLockResult(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not complete", label)
		return nil
	}
}
