package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
)

// Open opens a single-writer SQLite database.
//
// databaseURL accepts a file: URI or a bare absolute/relative path.
// postgres:// and postgresql:// return ErrPostgresURL.
// The pool is capped at one connection so this process is the only writer.
func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	dsn, err := buildDSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}
