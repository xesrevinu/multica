package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type startReplayDB struct {
	mockDBTX
	startErr error
	calls    int
}

func (m *startReplayDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	m.calls++
	if errors.Is(m.startErr, sql.ErrNoRows) {
		return mockQueryRow(nil, nil)
	}
	return mockQueryRow(nil, m.startErr)
}

func TestStartTaskLegacyDoesNotAcknowledgeReplayOrDatabaseFailure(t *testing.T) {
	for _, want := range []error{sql.ErrNoRows, errors.New("database unavailable")} {
		store := &startReplayDB{startErr: want}
		svc := &TaskService{Queries: db.New(store)}
		got, err := svc.StartTask(context.Background(), testUUID(1))
		if got != nil || !errors.Is(err, want) || store.calls != 1 {
			t.Fatalf("legacy start: task=%v err=%v queries=%d", got, err, store.calls)
		}
	}
}

func (*startReplayDB) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}
