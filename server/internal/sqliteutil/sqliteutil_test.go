package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildDSN(t *testing.T) {
	t.Parallel()

	dsn, err := buildDSN("file:./data/multica.db?mode=rwc&_pragma=foreign_keys(ON)&_txlock=deferred")
	if err != nil {
		t.Fatal(err)
	}
	filePart, query, ok := strings.Cut(dsn, "?")
	if !ok || filePart != "file:./data/multica.db" {
		t.Fatalf("file part = %q", dsn)
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("mode") != "rwc" {
		t.Fatalf("mode = %q", values.Get("mode"))
	}
	if values.Get("_txlock") != "immediate" {
		t.Fatalf("_txlock = %q", values.Get("_txlock"))
	}
	got := append([]string(nil), values["_pragma"]...)
	want := map[string]bool{
		"busy_timeout(5000)": false,
		"journal_mode(WAL)":  false,
		"foreign_keys(OFF)":  false,
	}
	for _, pragma := range got {
		if _, ok := want[pragma]; ok {
			want[pragma] = true
		}
		if strings.HasPrefix(strings.ToLower(pragma), "foreign_keys(on") {
			t.Fatalf("foreign_keys override was not replaced: %q", pragma)
		}
	}
	for pragma, seen := range want {
		if !seen {
			t.Fatalf("missing pragma %s in %v", pragma, got)
		}
	}

	bare, err := buildDSN("/tmp/multica.db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(bare, "file:/tmp/multica.db?") {
		t.Fatalf("bare absolute path dsn = %q", bare)
	}
	rel, err := buildDSN("./data/multica.db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "file:./data/multica.db?") {
		t.Fatalf("bare relative path dsn = %q", rel)
	}

	_, err = buildDSN("postgres://multica:multica@localhost:5432/multica")
	if !errors.Is(err, ErrPostgresURL) {
		t.Fatalf("postgres err = %v", err)
	}
	if !strings.Contains(err.Error(), "file:./data/multica.db") {
		t.Fatalf("postgres hint = %v", err)
	}
	_, err = buildDSN("postgresql://localhost/multica")
	if !errors.Is(err, ErrPostgresURL) {
		t.Fatalf("postgresql err = %v", err)
	}
}

func TestFilePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"file:./data/multica.db", "./data/multica.db"},
		{"file:/tmp/multica.db?mode=rwc", "/tmp/multica.db"},
		{"file:///tmp/multica.db", "/tmp/multica.db"},
		{"file://localhost/tmp/multica.db", "/tmp/multica.db"},
		{"./data/multica.db", "./data/multica.db"},
		{"/tmp/multica.db", "/tmp/multica.db"},
		{`C:\data\multica.db`, `C:/data/multica.db`},
		{"file:///C:/data/multica.db", `C:/data/multica.db`},
		{":memory:", ""},
		{"file::memory:?cache=shared", ""},
	}
	for _, tc := range cases {
		got, err := FilePath(tc.in)
		if err != nil {
			t.Fatalf("FilePath(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("FilePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenPragmasFunctionsAndPool(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.db")
	db, err := Open(ctx, bare)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d", got)
	}
	assertPragmas(t, db)

	var id string
	if err := db.QueryRowContext(ctx, "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	var id2 string
	if err := db.QueryRowContext(ctx, "SELECT gen_random_uuid()").Scan(&id2); err != nil {
		t.Fatal(err)
	}
	if id == id2 || parsed.String() != id {
		t.Fatalf("uuids = %q %q", id, id2)
	}

	var now string
	before := time.Now().UTC().Add(-2 * time.Second)
	if err := db.QueryRowContext(ctx, "SELECT now()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(2 * time.Second)
	ts, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Location() != time.UTC {
		t.Fatalf("now location = %v", ts.Location())
	}
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("now = %s, want between %s and %s", ts, before, after)
	}

	fileURL := "file:" + filepath.Join(dir, "uri.db")
	db2, err := Open(ctx, fileURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	assertPragmas(t, db2)
}

func assertPragmas(t *testing.T, db *sql.DB) {
	t.Helper()
	var busy int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy != 5000 {
		t.Fatalf("busy_timeout = %d", busy)
	}
	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q", journal)
	}
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 0 {
		t.Fatalf("foreign_keys = %d", fk)
	}
}

func TestSplitSQL(t *testing.T) {
	t.Parallel()

	script := `
-- keep; this comment
CREATE TABLE item (
  id TEXT PRIMARY KEY,
  note TEXT
);
INSERT INTO item (id, note) VALUES ('a', 'semi;colon');
INSERT INTO item (id, note) VALUES ('b', 'it''s; fine');
CREATE TRIGGER item_ai AFTER INSERT ON item
BEGIN
  UPDATE item
     SET note = CASE WHEN NEW.note = 'a;b' THEN 'kept;semi' ELSE note END
   WHERE id = NEW.id;
  UPDATE item SET note = "weir;d" WHERE id = NEW.id;
END;
BEGIN;
INSERT INTO item (id, note) VALUES ('c', 'tx');
COMMIT;
/* block; BEGIN END */
SELECT 1;
`
	stmts, err := splitSQL(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 8 {
		for i, stmt := range stmts {
			t.Logf("stmt %d: %s", i+1, stmt)
		}
		t.Fatalf("got %d statements, want 8", len(stmts))
	}
	if !strings.Contains(stmts[1], "'semi;colon'") {
		t.Fatalf("string split wrong: %s", stmts[1])
	}
	if !strings.Contains(stmts[3], "BEGIN") || !strings.HasSuffix(strings.TrimSpace(stmts[3]), "END") || !strings.Contains(stmts[3], "NEW.id;") {
		t.Fatalf("trigger was split: %s", stmts[3])
	}
	if !strings.Contains(stmts[4], "BEGIN") || strings.Contains(stmts[4], "INSERT") {
		t.Fatalf("transaction begin = %q", stmts[4])
	}

	if _, err := splitSQL("CREATE TRIGGER t BEGIN SELECT 1; "); err == nil {
		t.Fatal("expected unbalanced BEGIN error")
	}
	if _, err := splitSQL("SELECT 'unterminated"); err == nil {
		t.Fatal("expected unterminated literal error")
	}
}

func TestApplySchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	missing := filepath.Join(dir, "missing.sql")
	err = ApplySchema(ctx, db, missing)
	if err == nil || !strings.Contains(err.Error(), "schema file not found") || !strings.Contains(err.Error(), "schema/sqlite.sql") {
		t.Fatalf("missing schema err = %v", err)
	}

	schemaPath := filepath.Join(dir, "schema.sql")
	schema := `
CREATE TABLE item (
  id TEXT PRIMARY KEY DEFAULT (gen_random_uuid()),
  note TEXT NOT NULL DEFAULT (''),
  created_at TEXT NOT NULL DEFAULT (now())
);
CREATE TRIGGER item_ai AFTER INSERT ON item
BEGIN
  UPDATE item
     SET note = CASE
       WHEN NEW.note = 'a;b' THEN 'kept;semi'
       ELSE NEW.note || ';x'
     END
   WHERE id = NEW.id;
END;
INSERT INTO item (note) VALUES ('a;b');
`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db, schemaPath); err != nil {
		t.Fatal(err)
	}
	var id, note, created string
	if err := db.QueryRowContext(ctx, "SELECT id, note, created_at FROM item").Scan(&id, &note, &created); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatal(err)
	}
	if note != "kept;semi" {
		t.Fatalf("note = %q", note)
	}
	if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
		t.Fatal(err)
	}
}

func TestApplySchemaFullIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "full.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schemaPath := filepath.Join("..", "..", "schema", "sqlite.sql")
	if err := ApplySchema(ctx, db, schemaPath); err != nil {
		t.Fatalf("first ApplySchema: %v", err)
	}
	if err := ApplySchema(ctx, db, schemaPath); err != nil {
		t.Fatalf("second ApplySchema (idempotency): %v", err)
	}
}
