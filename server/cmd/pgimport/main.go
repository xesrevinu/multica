package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

const (
	defaultSQLiteURL   = "file:./data/multica.db"
	defaultSchemaPath  = "schema/sqlite.sql"
	defaultDockerImage = "pgvector/pgvector:pg17"
)

type config struct {
	pgURL         string
	dumpPath      string
	dockerImage   string
	sqliteURL     string
	schemaPath    string
	rowcountsPath string
	overwrite     bool
}

type columnMeta struct {
	pgName     string
	sqliteName string
	dataType   string
	udtName    string
	isNullable bool
}

type tableImportResult struct {
	Table       string
	PGRows      int64
	SQLiteRows  int64
	BackupCount *int64
}

func main() {
	logger.Init()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		slog.Error("pgimport failed", "error", err)
		os.Exit(1)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	fs := flag.NewFlagSet("pgimport", flag.ContinueOnError)
	cfg := config{}

	defaultPG := getenv("PG_DATABASE_URL")
	defaultSQLite := getenv("DATABASE_URL")
	if defaultSQLite == "" {
		defaultSQLite = defaultSQLiteURL
	}
	defaultSchema := getenv("MULTICA_SCHEMA_PATH")
	if defaultSchema == "" {
		defaultSchema = defaultSchemaPath
	}

	fs.StringVar(&cfg.pgURL, "pg-url", defaultPG, "PostgreSQL source connection URL")
	fs.StringVar(&cfg.dumpPath, "dump", "", "Path to PostgreSQL custom-format dump to restore in a temporary Docker container")
	fs.StringVar(&cfg.dockerImage, "docker-image", defaultDockerImage, "Docker image for temporary PostgreSQL restore")
	fs.StringVar(&cfg.sqliteURL, "sqlite-url", defaultSQLite, "Target SQLite database URL or file path")
	fs.StringVar(&cfg.schemaPath, "schema", defaultSchema, "Path to schema/sqlite.sql")
	fs.StringVar(&cfg.rowcountsPath, "rowcounts", "", "Optional path to rowcounts.txt for comparison")
	fs.BoolVar(&cfg.overwrite, "overwrite", true, "Remove existing target SQLite database before importing")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.pgURL == "" && cfg.dumpPath == "" {
		return config{}, errors.New("either --pg-url (or PG_DATABASE_URL) or --dump must be specified")
	}
	return cfg, nil
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return err
	}

	pgURL := cfg.pgURL
	if pgURL == "" {
		var cleanup func()
		pgURL, cleanup, err = startTempPostgresFromDump(ctx, cfg.dumpPath, cfg.dockerImage)
		if err != nil {
			return err
		}
		defer cleanup()
	}

	if err := prepareSQLiteTarget(cfg.sqliteURL, cfg.overwrite); err != nil {
		return err
	}

	sqliteDB, err := sqliteutil.Open(ctx, cfg.sqliteURL)
	if err != nil {
		return err
	}
	defer sqliteDB.Close()

	slog.Info("applying sqlite schema", "sqlite", cfg.sqliteURL, "schema", cfg.schemaPath)
	if err := sqliteutil.ApplySchema(ctx, sqliteDB, cfg.schemaPath); err != nil {
		return err
	}

	pgConn, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pgConn.Close(ctx)

	results, err := importAllTables(ctx, pgConn, sqliteDB, cfg.rowcountsPath)
	if err != nil {
		return err
	}

	printSummary(results, cfg.rowcountsPath != "")
	return nil
}

func startTempPostgresFromDump(ctx context.Context, dumpPath, dockerImage string) (string, func(), error) {
	absDump, err := filepath.Abs(dumpPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve dump path: %w", err)
	}
	if _, err := os.Stat(absDump); err != nil {
		return "", nil, fmt.Errorf("stat dump %q: %w", absDump, err)
	}

	port, err := findFreePort()
	if err != nil {
		return "", nil, err
	}
	containerName := fmt.Sprintf("multica-pgimport-%d", os.Getpid())
	slog.Info("starting temporary postgres container", "container", containerName, "image", dockerImage, "port", port)

	runCmd := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--name", containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
		"-e", "POSTGRES_HOST_AUTH_METHOD=trust",
		"-v", fmt.Sprintf("%s:/backup/multica.dump:ro", absDump),
		dockerImage,
	)
	if out, err := runCmd.CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("docker run failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	cleanup := func() {
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
	}

	ready := false
	for i := 0; i < 60; i++ {
		if exec.CommandContext(ctx, "docker", "exec", containerName, "pg_isready", "-U", "postgres").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		cleanup()
		return "", nil, errors.New("temporary postgres container did not become ready in time")
	}

	if out, err := exec.CommandContext(ctx, "docker", "exec", containerName, "createdb", "-U", "postgres", "multica_restore").CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("createdb failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	slog.Info("restoring dump into temporary postgres container", "dump", absDump)
	restoreCmd := exec.CommandContext(ctx, "docker", "exec", containerName,
		"pg_restore", "-U", "postgres", "-d", "multica_restore", "--no-owner", "--no-acl", "/backup/multica.dump",
	)
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		// pg_restore returns non-zero on harmless warnings (e.g. extension commentary); verify tables exist.
		slog.Warn("pg_restore exited with warning/error; verifying restored tables", "error", err, "output", strings.TrimSpace(string(out)))
	}

	pgURL := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/multica_restore?sslmode=disable", port)
	return pgURL, cleanup, nil
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free tcp port: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func prepareSQLiteTarget(sqliteURL string, overwrite bool) error {
	path, err := sqliteutil.FilePath(sqliteURL)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create sqlite parent directory %q: %w", dir, err)
		}
	}
	if overwrite {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove existing sqlite file %q: %w", path+suffix, err)
			}
		}
	}
	return nil
}

func importAllTables(ctx context.Context, pgConn *pgx.Conn, sqliteDB *sql.DB, rowcountsPath string) ([]tableImportResult, error) {
	pgTables, err := loadPGSchema(ctx, pgConn)
	if err != nil {
		return nil, err
	}
	sqliteTables, err := loadSQLiteSchema(ctx, sqliteDB)
	if err != nil {
		return nil, err
	}

	// Verify table correspondence between PG and SQLite.
	for t := range pgTables {
		if _, ok := sqliteTables[t]; !ok {
			return nil, fmt.Errorf("postgres table %q is missing in sqlite schema", t)
		}
	}
	for t := range sqliteTables {
		if t == "search_change_counter" {
			continue
		}
		if _, ok := pgTables[t]; !ok {
			return nil, fmt.Errorf("sqlite table %q (non-counter) is missing in postgres schema", t)
		}
	}

	// Order tables so search_index_change and search_index_prune_mark run last
	// (after issue, comment, project whose AFTER INSERT triggers touch search_index_change).
	var regularTables []string
	var deferredTables []string
	for t := range pgTables {
		if t == "search_index_change" || t == "search_index_prune_mark" || t == "issue_child_event" {
			deferredTables = append(deferredTables, t)
		} else {
			regularTables = append(regularTables, t)
		}
	}
	sort.Strings(regularTables)
	sort.Strings(deferredTables)

	backupCounts := map[string]int64{}
	if rowcountsPath != "" {
		backupCounts, err = loadRowcountsFile(rowcountsPath)
		if err != nil {
			return nil, err
		}
	}

	if _, err := sqliteDB.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return nil, fmt.Errorf("disable foreign_keys: %w", err)
	}

	resultsByName := make(map[string]tableImportResult, len(pgTables))

	for _, table := range regularTables {
		cols, err := resolveColumns(table, pgTables[table], sqliteTables[table])
		if err != nil {
			return nil, err
		}
		pgCount, sqCount, err := copyTable(ctx, pgConn, sqliteDB, table, cols)
		if err != nil {
			return nil, err
		}
		res := tableImportResult{Table: table, PGRows: pgCount, SQLiteRows: sqCount}
		if bc, ok := backupCounts[table]; ok {
			v := bc
			res.BackupCount = &v
		}
		resultsByName[table] = res
	}

	// Clear trigger-generated rows in search_index_change before importing actual PG rows.
	if _, err := sqliteDB.ExecContext(ctx, `DELETE FROM search_index_change`); err != nil {
		return nil, fmt.Errorf("clear trigger-populated search_index_change: %w", err)
	}
	if _, err := sqliteDB.ExecContext(ctx, `DELETE FROM search_index_prune_mark`); err != nil {
		return nil, fmt.Errorf("clear search_index_prune_mark: %w", err)
	}
	if _, err := sqliteDB.ExecContext(ctx, `DELETE FROM issue_child_event`); err != nil {
		return nil, fmt.Errorf("clear trigger-populated issue_child_event: %w", err)
	}

	for _, table := range deferredTables {
		cols, err := resolveColumns(table, pgTables[table], sqliteTables[table])
		if err != nil {
			return nil, err
		}
		pgCount, sqCount, err := copyTable(ctx, pgConn, sqliteDB, table, cols)
		if err != nil {
			return nil, err
		}
		res := tableImportResult{Table: table, PGRows: pgCount, SQLiteRows: sqCount}
		if bc, ok := backupCounts[table]; ok {
			v := bc
			res.BackupCount = &v
		}
		resultsByName[table] = res
	}

	// Sync search_change_counter to the maximum imported sequence number so future
	// triggers allocate strictly higher sequence numbers.
	if _, err := sqliteDB.ExecContext(ctx, `
		UPDATE search_change_counter
		SET n = MAX(
			COALESCE((SELECT MAX(change_seq) FROM search_index_change), 0),
			COALESCE((SELECT MAX(pruned_through_seq) FROM search_index_prune_mark), 0)
		)
		WHERE singleton = 1
	`); err != nil {
		return nil, fmt.Errorf("sync search_change_counter: %w", err)
	}

	// Verify FTS tables match their source tables.
	for _, pair := range [][2]string{
		{"issue", "issue_fts"},
		{"comment", "comment_fts"},
		{"project", "project_fts"},
	} {
		var ftsCount int64
		if err := sqliteDB.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, quoteIdent(pair[1]))).Scan(&ftsCount); err != nil {
			return nil, fmt.Errorf("count %s: %w", pair[1], err)
		}
		baseCount := resultsByName[pair[0]].SQLiteRows
		if ftsCount != baseCount {
			return nil, fmt.Errorf("fts row count mismatch for %s: got %d, want %d", pair[1], ftsCount, baseCount)
		}
	}

	allTables := make([]string, 0, len(resultsByName))
	for t := range resultsByName {
		allTables = append(allTables, t)
	}
	sort.Strings(allTables)

	out := make([]tableImportResult, 0, len(allTables))
	for _, t := range allTables {
		out = append(out, resultsByName[t])
	}
	return out, nil
}

func loadPGSchema(ctx context.Context, pgConn *pgx.Conn) (map[string][]columnMeta, error) {
	rows, err := pgConn.Query(ctx, `
		SELECT table_name, column_name, data_type, udt_name, (is_nullable = 'YES') AS is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN (
			SELECT table_name FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		  )
		ORDER BY table_name, ordinal_position
	`)
	if err != nil {
		return nil, fmt.Errorf("query postgres schema: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]columnMeta)
	for rows.Next() {
		var table string
		var col columnMeta
		if err := rows.Scan(&table, &col.pgName, &col.dataType, &col.udtName, &col.isNullable); err != nil {
			return nil, fmt.Errorf("scan postgres schema: %w", err)
		}
		col.sqliteName = mapColumnName(table, col.pgName)
		out[table] = append(out[table], col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func mapColumnName(table, pgCol string) string {
	if table == "search_index_change" && pgCol == "change_xid" {
		return "change_seq"
	}
	if table == "search_index_prune_mark" && pgCol == "pruned_through_xid" {
		return "pruned_through_seq"
	}
	return pgCol
}

func loadSQLiteSchema(ctx context.Context, sqliteDB *sql.DB) (map[string]map[string]bool, error) {
	rows, err := sqliteDB.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name NOT LIKE '%_fts%'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query sqlite tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]map[string]bool, len(tableNames))
	for _, t := range tableNames {
		pRows, err := sqliteDB.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(t)))
		if err != nil {
			return nil, fmt.Errorf("pragma table_info(%s): %w", t, err)
		}
		colSet := make(map[string]bool)
		for pRows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := pRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				pRows.Close()
				return nil, err
			}
			colSet[name] = true
		}
		pRows.Close()
		out[t] = colSet
	}
	return out, nil
}

func resolveColumns(table string, pgCols []columnMeta, sqliteColSet map[string]bool) ([]columnMeta, error) {
	if len(pgCols) != len(sqliteColSet) {
		return nil, fmt.Errorf("column count mismatch on table %q: pg=%d sqlite=%d", table, len(pgCols), len(sqliteColSet))
	}
	for _, c := range pgCols {
		if !sqliteColSet[c.sqliteName] {
			return nil, fmt.Errorf("table %q: mapped column %q (from pg %q) does not exist in sqlite", table, c.sqliteName, c.pgName)
		}
	}
	return pgCols, nil
}

func pgSelectExpr(c columnMeta) string {
	ident := quoteIdent(c.pgName)
	switch {
	case c.udtName == "xid8":
		return ident + "::text::bigint"
	case c.dataType == "ARRAY":
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE array_to_json(%s)::text END", ident, ident)
	case c.udtName == "jsonb" || c.udtName == "json" || c.udtName == "inet" || c.udtName == "uuid":
		return ident + "::text"
	default:
		return ident
	}
}

func copyTable(ctx context.Context, pgConn *pgx.Conn, sqliteDB *sql.DB, table string, cols []columnMeta) (int64, int64, error) {
	var pgCount int64
	if err := pgConn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM public.%s", quoteIdent(table))).Scan(&pgCount); err != nil {
		return 0, 0, fmt.Errorf("count postgres %s: %w", table, err)
	}
	if pgCount == 0 {
		var sqCount int64
		if err := sqliteDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", quoteIdent(table))).Scan(&sqCount); err != nil {
			return 0, 0, fmt.Errorf("count sqlite %s: %w", table, err)
		}
		return 0, sqCount, nil
	}

	selectExprs := make([]string, len(cols))
	insertCols := make([]string, len(cols))
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		selectExprs[i] = pgSelectExpr(c)
		insertCols[i] = quoteIdent(c.sqliteName)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	selectSQL := fmt.Sprintf("SELECT %s FROM public.%s", strings.Join(selectExprs, ", "), quoteIdent(table))
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(insertCols, ", "), strings.Join(placeholders, ", "))

	rows, err := pgConn.Query(ctx, selectSQL)
	if err != nil {
		return 0, 0, fmt.Errorf("query postgres %s: %w", table, err)
	}
	defer rows.Close()

	tx, err := sqliteDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin sqlite tx for %s: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare sqlite insert for %s: %w", table, err)
	}
	defer stmt.Close()

	var inserted int64
	for rows.Next() {
		rawVals, err := rows.Values()
		if err != nil {
			return 0, 0, fmt.Errorf("read postgres row from %s: %w", table, err)
		}
		sqliteArgs := make([]any, len(cols))
		for i, c := range cols {
			v, err := convertPGValue(rawVals[i], c.dataType, c.udtName)
			if err != nil {
				return 0, 0, fmt.Errorf("table %s column %s: %w", table, c.pgName, err)
			}
			sqliteArgs[i] = v
		}
		if _, err := stmt.ExecContext(ctx, sqliteArgs...); err != nil {
			return 0, 0, fmt.Errorf("insert sqlite row into %s (row %d): %w", table, inserted+1, err)
		}
		inserted++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate postgres %s: %w", table, err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit sqlite tx for %s: %w", table, err)
	}

	var sqCount int64
	if err := sqliteDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", quoteIdent(table))).Scan(&sqCount); err != nil {
		return 0, 0, fmt.Errorf("count sqlite %s: %w", table, err)
	}
	if sqCount != pgCount {
		return pgCount, sqCount, fmt.Errorf("row count mismatch for %s: postgres=%d sqlite=%d", table, pgCount, sqCount)
	}
	slog.Info("imported table", "table", table, "rows", sqCount)
	return pgCount, sqCount, nil
}

// convertPGValue converts a value read from PostgreSQL into its SQLite representation
// according to server/schema/README.md.
func convertPGValue(val any, dataType, udtName string) (any, error) {
	if val == nil {
		return nil, nil
	}

	switch {
	case udtName == "bool" || dataType == "boolean":
		switch b := val.(type) {
		case bool:
			if b {
				return int64(1), nil
			}
			return int64(0), nil
		case int64:
			return b, nil
		default:
			return nil, fmt.Errorf("unexpected bool value %T (%v)", val, val)
		}

	case udtName == "uuid":
		switch u := val.(type) {
		case string:
			parsed, err := uuid.Parse(u)
			if err != nil {
				return nil, fmt.Errorf("invalid uuid %q: %w", u, err)
			}
			return strings.ToLower(parsed.String()), nil
		case [16]byte:
			return strings.ToLower(uuid.UUID(u).String()), nil
		case []byte:
			if len(u) == 16 {
				var arr [16]byte
				copy(arr[:], u)
				return strings.ToLower(uuid.UUID(arr).String()), nil
			}
			parsed, err := uuid.ParseBytes(u)
			if err != nil {
				return nil, fmt.Errorf("invalid uuid bytes: %w", err)
			}
			return strings.ToLower(parsed.String()), nil
		default:
			return nil, fmt.Errorf("unexpected uuid value %T", val)
		}

	case udtName == "timestamptz" || udtName == "timestamp" || strings.HasPrefix(dataType, "timestamp"):
		switch t := val.(type) {
		case time.Time:
			return t.UTC().Format(time.RFC3339Nano), nil
		case string:
			parsed, err := time.Parse(time.RFC3339Nano, t)
			if err != nil {
				return nil, fmt.Errorf("invalid timestamp string %q: %w", t, err)
			}
			return parsed.UTC().Format(time.RFC3339Nano), nil
		default:
			return nil, fmt.Errorf("unexpected timestamp value %T", val)
		}

	case udtName == "date" || dataType == "date":
		switch t := val.(type) {
		case time.Time:
			return t.UTC().Format("2006-01-02"), nil
		case string:
			if len(t) >= 10 {
				return t[:10], nil
			}
			return t, nil
		default:
			return nil, fmt.Errorf("unexpected date value %T", val)
		}

	case dataType == "ARRAY" || strings.HasPrefix(udtName, "_"):
		switch a := val.(type) {
		case string:
			// Already converted by array_to_json(col)::text in SQL.
			if udtName == "_uuid" {
				var raw []string
				if err := json.Unmarshal([]byte(a), &raw); err != nil {
					return nil, fmt.Errorf("unmarshal uuid array json %q: %w", a, err)
				}
				for i, item := range raw {
					u, err := uuid.Parse(item)
					if err != nil {
						return nil, fmt.Errorf("invalid uuid in array %q: %w", item, err)
					}
					raw[i] = strings.ToLower(u.String())
				}
				b, err := json.Marshal(raw)
				if err != nil {
					return nil, err
				}
				return string(b), nil
			}
			return a, nil
		case []string:
			if a == nil {
				return "[]", nil
			}
			b, err := json.Marshal(a)
			if err != nil {
				return nil, err
			}
			return string(b), nil
		case [][16]byte:
			out := make([]string, len(a))
			for i, u := range a {
				out[i] = strings.ToLower(uuid.UUID(u).String())
			}
			b, err := json.Marshal(out)
			if err != nil {
				return nil, err
			}
			return string(b), nil
		case []any:
			out := make([]string, len(a))
			for i, elem := range a {
				switch e := elem.(type) {
				case string:
					out[i] = e
				case [16]byte:
					out[i] = strings.ToLower(uuid.UUID(e).String())
				default:
					out[i] = fmt.Sprint(e)
				}
			}
			b, err := json.Marshal(out)
			if err != nil {
				return nil, err
			}
			return string(b), nil
		default:
			b, err := json.Marshal(val)
			if err != nil {
				return nil, fmt.Errorf("marshal array %T: %w", val, err)
			}
			return string(b), nil
		}

	case udtName == "jsonb" || udtName == "json" || dataType == "jsonb" || dataType == "json":
		switch j := val.(type) {
		case string:
			return j, nil
		case []byte:
			return string(j), nil
		default:
			b, err := json.Marshal(j)
			if err != nil {
				return nil, fmt.Errorf("marshal json value %T: %w", val, err)
			}
			return string(b), nil
		}

	case udtName == "xid8":
		switch x := val.(type) {
		case int64:
			return x, nil
		case uint64:
			return int64(x), nil
		case int32:
			return int64(x), nil
		case string:
			n, err := strconv.ParseInt(x, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse xid8 %q: %w", x, err)
			}
			return n, nil
		default:
			return nil, fmt.Errorf("unexpected xid8 value %T", val)
		}

	case udtName == "bytea" || dataType == "bytea":
		switch b := val.(type) {
		case []byte:
			if b == nil {
				return nil, nil
			}
			if len(b) == 0 {
				return []byte{}, nil
			}
			return b, nil
		case string:
			return []byte(b), nil
		default:
			return nil, fmt.Errorf("unexpected bytea value %T", val)
		}

	case udtName == "inet" || dataType == "inet":
		switch ip := val.(type) {
		case string:
			return ip, nil
		case netip.Prefix:
			if ip.Bits() == ip.Addr().BitLen() {
				return ip.Addr().String(), nil
			}
			return ip.String(), nil
		case netip.Addr:
			return ip.String(), nil
		case net.IP:
			return ip.String(), nil
		case *net.IPNet:
			return ip.String(), nil
		default:
			return fmt.Sprint(val), nil
		}

	case udtName == "int2" || udtName == "int4" || udtName == "int8":
		switch n := val.(type) {
		case int16:
			return int64(n), nil
		case int32:
			return int64(n), nil
		case int64:
			return n, nil
		case int:
			return int64(n), nil
		default:
			return nil, fmt.Errorf("unexpected integer value %T", val)
		}

	case udtName == "float4" || udtName == "float8" || udtName == "numeric":
		switch f := val.(type) {
		case float32:
			return float64(f), nil
		case float64:
			return f, nil
		default:
			return nil, fmt.Errorf("unexpected float value %T", val)
		}

	default:
		switch s := val.(type) {
		case string:
			return s, nil
		case []byte:
			return string(s), nil
		default:
			return fmt.Sprint(val), nil
		}
	}
}

func loadRowcountsFile(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rowcounts file %q: %w", path, err)
	}
	defer f.Close()

	out := make(map[string]int64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid rowcounts line %q", line)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid count in line %q: %w", line, err)
		}
		out[strings.TrimSpace(parts[0])] = n
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func printSummary(results []tableImportResult, hasBackupCounts bool) {
	var totalPG, totalSQLite int64
	var nonEmpty int
	fmt.Println("=== PG -> SQLite Import Summary (Non-Empty Tables) ===")
	if hasBackupCounts {
		fmt.Printf("%-40s %10s %10s %12s\n", "TABLE", "PG_COUNT", "SQLITE", "ROWCOUNTS.TXT")
	} else {
		fmt.Printf("%-40s %10s %10s\n", "TABLE", "PG_COUNT", "SQLITE")
	}
	for _, r := range results {
		totalPG += r.PGRows
		totalSQLite += r.SQLiteRows
		if r.PGRows == 0 && r.SQLiteRows == 0 && r.BackupCount == nil {
			continue
		}
		nonEmpty++
		if hasBackupCounts {
			bcStr := "-"
			if r.BackupCount != nil {
				bcStr = strconv.FormatInt(*r.BackupCount, 10)
			}
			fmt.Printf("%-40s %10d %10d %12s\n", r.Table, r.PGRows, r.SQLiteRows, bcStr)
		} else {
			fmt.Printf("%-40s %10d %10d\n", r.Table, r.PGRows, r.SQLiteRows)
		}
	}
	fmt.Printf("Total tables verified: %d (%d non-empty), total rows copied: %d (PG=%d, SQLite=%d)\n",
		len(results), nonEmpty, totalSQLite, totalPG, totalSQLite)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
