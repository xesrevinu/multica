package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

const (
	defaultDatabaseURL = "file:./data/multica.db"
	defaultSchemaPath  = "schema/sqlite.sql"
)

func main() {
	logger.Init()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	fmt.Println("Done.")
}

// run applies schema/sqlite.sql to the SQLite database in DATABASE_URL.
// The only subcommand is "up". Down migrations, advisory locks, and the
// historical Postgres migration chain are not supported.
func run(ctx context.Context, args []string, getenv func(string) string) error {
	if len(args) != 1 || args[0] != "up" {
		return fmt.Errorf("usage: migrate up (SQLite only; down is not supported). Default database is %s; override with DATABASE_URL. Schema defaults to %s; override with MULTICA_SCHEMA_PATH", defaultDatabaseURL, defaultSchemaPath)
	}

	dbURL := getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}
	schemaPath := getenv("MULTICA_SCHEMA_PATH")
	if schemaPath == "" {
		schemaPath = defaultSchemaPath
	}
	if err := ensureDBParent(dbURL); err != nil {
		return err
	}

	db, err := sqliteutil.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	slog.Info("applying sqlite schema", "database", dbURL, "schema", schemaPath)
	if err := sqliteutil.ApplySchema(ctx, db, schemaPath); err != nil {
		return err
	}
	return nil
}

func ensureDBParent(databaseURL string) error {
	path, err := sqliteutil.FilePath(databaseURL)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory %q: %w", dir, err)
	}
	return nil
}
