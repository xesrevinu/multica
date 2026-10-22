package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

const (
	// dbStatsInterval is how often the pool stats are sampled and logged.
	dbStatsInterval = 15 * time.Second
)

// newDBPool opens the process-local SQLite database. sqliteutil.Open caps the
// pool at one connection so this process is the only writer. connectTimeout is
// retained so startup settings stay wired, but a file open does not dial.
func newDBPool(ctx context.Context, dbURL string, connectTimeout time.Duration) (*sql.DB, error) {
	_ = connectTimeout
	return sqliteutil.Open(ctx, dbURL)
}

func logPoolConfig(role string, pool *sql.DB) {
	stats := pool.Stats()
	slog.Info("db pool config",
		"role", role,
		"max_open_conns", stats.MaxOpenConnections,
		"open_conns", stats.OpenConnections,
		"in_use", stats.InUse,
		"idle", stats.Idle,
	)
}

// runDBStatsLogger samples database/sql pool stats. WaitCount is the SQLite
// equivalent of an empty acquire: a caller blocked because the single writer
// connection was busy.
func runDBStatsLogger(ctx context.Context, role string, pool *sql.DB) {
	ticker := time.NewTicker(dbStatsInterval)
	defer ticker.Stop()

	var lastWait int64
	var lastWaitDur time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s := pool.Stats()
		waitDelta := s.WaitCount - lastWait
		waitDurDelta := s.WaitDuration - lastWaitDur
		var avgWaitMs int64
		if waitDelta > 0 {
			avgWaitMs = waitDurDelta.Milliseconds() / waitDelta
		}
		fields := []any{
			"role", role,
			"max_open_conns", s.MaxOpenConnections,
			"open_conns", s.OpenConnections,
			"in_use", s.InUse,
			"idle", s.Idle,
			"wait_count_delta", waitDelta,
			"avg_wait_ms", avgWaitMs,
		}
		if waitDelta > 0 {
			slog.Warn("db pool pressure", fields...)
		} else {
			slog.Info("db pool stats", fields...)
		}
		lastWait = s.WaitCount
		lastWaitDur = s.WaitDuration
	}
}
