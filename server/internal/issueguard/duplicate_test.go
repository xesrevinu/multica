package issueguard

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type catalogFailureDB struct {
	err error
}

type catalogFailureResult struct{}

func (catalogFailureResult) LastInsertId() (int64, error) { return 0, nil }
func (catalogFailureResult) RowsAffected() (int64, error) { return 0, nil }

func (f catalogFailureDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return catalogFailureResult{}, nil
}

func (f catalogFailureDB) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, nil
}

func (f catalogFailureDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, f.err
}

func (f catalogFailureDB) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

func TestLockAndFindActiveDuplicatePropagatesStatusCatalogFailure(t *testing.T) {
	catalogErr := errors.New("status catalog unavailable")
	q := db.New(catalogFailureDB{err: catalogErr})

	_, found, err := LockAndFindActiveDuplicate(
		context.Background(), q, testUUID(1), "", "", "duplicate title", false,
	)
	if !errors.Is(err, catalogErr) || found {
		t.Fatalf("LockAndFindActiveDuplicate = found %v, err %v; want false, catalog error", found, err)
	}
}

func TestLockAndFindRecentAutopilotDuplicatePropagatesStatusCatalogFailure(t *testing.T) {
	catalogErr := errors.New("status catalog unavailable")
	q := db.New(catalogFailureDB{err: catalogErr})

	_, found, err := LockAndFindRecentAutopilotDuplicate(
		context.Background(), q, testUUID(1), testUUID(2), "", "duplicate title", time.Hour,
	)
	if !errors.Is(err, catalogErr) || found {
		t.Fatalf("LockAndFindRecentAutopilotDuplicate = found %v, err %v; want false, catalog error", found, err)
	}
}

func testUUID(lastByte byte) string {
	return "00000000-0000-4000-8000-00000000000" + string('0'+lastByte)
}
