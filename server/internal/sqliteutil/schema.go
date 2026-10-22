package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ApplySchema executes the SQLite script at path statement by statement.
// database/sql Exec runs a single statement, so scripts are split on
// semicolons that are outside strings and trigger BEGIN/END bodies.
// A missing file is an error; this function does not synthesize a schema.
func ApplySchema(ctx context.Context, db *sql.DB, path string) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("sqlite schema path is empty")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sqlite schema file not found at %q (expected server/schema/sqlite.sql; set MULTICA_SCHEMA_PATH to override)", path)
		}
		return fmt.Errorf("read sqlite schema %q: %w", path, err)
	}
	stmts, err := splitSQL(string(body))
	if err != nil {
		return fmt.Errorf("split sqlite schema %q: %w", path, err)
	}
	for i, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply sqlite schema %q statement %d: %w", path, i+1, err)
		}
	}
	return nil
}
