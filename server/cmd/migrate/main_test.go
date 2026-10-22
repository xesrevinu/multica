package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

func TestRunUp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nested", "multica.db")
	schemaPath := filepath.Join(dir, "schema.sql")
	schema := []byte(`
CREATE TABLE migrated (
  id TEXT PRIMARY KEY DEFAULT (gen_random_uuid())
);
INSERT INTO migrated DEFAULT VALUES;
`)
	if err := os.WriteFile(schemaPath, schema, 0o644); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{
		"DATABASE_URL":        "file:" + dbPath,
		"MULTICA_SCHEMA_PATH": schemaPath,
	}
	getenv := func(k string) string { return env[k] }
	if err := run(context.Background(), []string{"up"}, getenv); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var n int
	if err := db.QueryRow("SELECT count(*) FROM migrated").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d", n)
	}
}

func TestRunRejectsDownPostgresAndMissingSchema(t *testing.T) {
	t.Parallel()

	if err := run(context.Background(), nil, os.Getenv); err == nil || !strings.Contains(err.Error(), "migrate up") {
		t.Fatalf("usage err = %v", err)
	}
	if err := run(context.Background(), []string{"down"}, os.Getenv); err == nil || !strings.Contains(err.Error(), "down is not supported") {
		t.Fatalf("down err = %v", err)
	}

	dir := t.TempDir()
	err := run(context.Background(), []string{"up"}, func(k string) string {
		switch k {
		case "DATABASE_URL":
			return "postgres://multica:multica@localhost:5432/multica"
		case "MULTICA_SCHEMA_PATH":
			return filepath.Join(dir, "unused.sql")
		default:
			return ""
		}
	})
	if !errors.Is(err, sqliteutil.ErrPostgresURL) || !strings.Contains(err.Error(), "file:./data/multica.db") {
		t.Fatalf("postgres err = %v", err)
	}

	err = run(context.Background(), []string{"up"}, func(k string) string {
		switch k {
		case "DATABASE_URL":
			return "file:" + filepath.Join(dir, "missing-schema.db")
		case "MULTICA_SCHEMA_PATH":
			return filepath.Join(dir, "no-such.sql")
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "schema file not found") {
		t.Fatalf("missing schema err = %v", err)
	}
}
