package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"database/sql"
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

// Cols is one fixture row: column name to value. Values are bound as query
// parameters unless they are Raw.
type Cols map[string]any

// Raw is a SQL expression inlined into the statement instead of being bound as
// a parameter, for the defaults a fixture cannot express as a Go value —
// `now() - interval '2 hours'`, or the next issue number in a workspace.
//
// It is inlined verbatim. Never build one out of anything but literals and
// ids the fixture itself produced.
type Raw string

// Fixture inserts rows for one test workspace and removes them again when the
// test that asked for them finishes.
//
// The zero value is not usable; call New. WorkspaceID and UserID are the
// defaults every builder falls back to, so a test that only cares about one
// column names only that column.
// Pool wraps *sql.DB with context-first helper methods (Begin, Exec, QueryRow,
// Query, Ping) so tests and TxStarter adapters that expect context-first
// signatures work directly over SQLite without pgxpool.
type Pool struct {
	*sql.DB
}

func WrapPool(db *sql.DB) *Pool {
	if db == nil {
		return nil
	}
	return &Pool{DB: db}
}

var (
	rePGCast           = regexp.MustCompile(`(?i)::(?:jsonb|json|uuid\[\]|text\[\]|int4\[\]|int8\[\]|uuid|text|timestamptz|timestamp|int8|int4|int2|integer|bigint|smallint|int|boolean|bool|bytea|numeric|float8|real)(?:\[\])?`)
	reNowMinusInterval = regexp.MustCompile(`(?i)\bnow\(\)\s*-\s*interval\s*'([^']+)'`)
	reNowPlusInterval  = regexp.MustCompile(`(?i)\bnow\(\)\s*\+\s*interval\s*'([^']+)'`)
	reForUpdate        = regexp.MustCompile(`(?i)\s+FOR\s+(?:UPDATE(?:\s+OF\s+\w+)?(?:\s+SKIP\s+LOCKED|\s+NOWAIT)?|SHARE)\b`)
)

func normalizeTestSQL(q string) string {
	q = rePGCast.ReplaceAllString(q, "")
	q = reNowMinusInterval.ReplaceAllString(q, "strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-$1')")
	q = reNowPlusInterval.ReplaceAllString(q, "strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+$1')")
	q = reForUpdate.ReplaceAllString(q, "")
	return q
}

func normalizeTestArgs(args []any) []any {
	if len(args) == 0 {
		return args
	}
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case time.Time:
			out[i] = v.UTC().Format(time.RFC3339Nano)
		case *time.Time:
			if v == nil {
				out[i] = nil
			} else {
				out[i] = v.UTC().Format(time.RFC3339Nano)
			}
		case uuid.UUID:
			out[i] = v.String()
		default:
			out[i] = a
		}
	}
	return out
}

func (p *Pool) Begin(ctx context.Context) (*sql.Tx, error) {
	return p.DB.BeginTx(ctx, nil)
}

func (p *Pool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.DB.ExecContext(ctx, normalizeTestSQL(query), normalizeTestArgs(args)...)
}

func (p *Pool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return p.DB.QueryRowContext(ctx, normalizeTestSQL(query), normalizeTestArgs(args)...)
}

func (p *Pool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.DB.QueryContext(ctx, normalizeTestSQL(query), normalizeTestArgs(args)...)
}

func (p *Pool) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.ExecContext(ctx, query, args...)
}

type TestRow struct {
	row *sql.Row
}

func (r *TestRow) Scan(dest ...any) error {
	return r.row.Scan(wrapScanDest(dest)...)
}

func (r *TestRow) Err() error {
	return r.row.Err()
}

type TestRows struct {
	*sql.Rows
}

func (r *TestRows) Scan(dest ...any) error {
	return r.Rows.Scan(wrapScanDest(dest)...)
}

func parseFlexTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

type flexScanner struct {
	dest any
}

func (fs *flexScanner) Scan(src any) error {
	switch d := fs.dest.(type) {
	case *time.Time:
		if src == nil {
			*d = time.Time{}
			return nil
		}
		switch v := src.(type) {
		case time.Time:
			*d = v
			return nil
		case string:
			t, err := parseFlexTime(v)
			if err != nil {
				return err
			}
			*d = t
			return nil
		case []byte:
			t, err := parseFlexTime(string(v))
			if err != nil {
				return err
			}
			*d = t
			return nil
		}
	case **time.Time:
		if src == nil {
			*d = nil
			return nil
		}
		var t time.Time
		if err := (&flexScanner{dest: &t}).Scan(src); err != nil {
			return err
		}
		*d = &t
		return nil
	case *sql.NullTime:
		if src == nil {
			*d = sql.NullTime{}
			return nil
		}
		var t time.Time
		if err := (&flexScanner{dest: &t}).Scan(src); err != nil {
			return err
		}
		*d = sql.NullTime{Time: t, Valid: true}
		return nil
	case *[]string:
		if src == nil {
			*d = nil
			return nil
		}
		var raw string
		switch v := src.(type) {
		case string:
			raw = strings.TrimSpace(v)
		case []byte:
			raw = strings.TrimSpace(string(v))
		default:
			return fmt.Errorf("cannot scan %T into *[]string", src)
		}
		if raw == "" || raw == "{}" || raw == "[]" || raw == "null" {
			*d = []string{}
			return nil
		}
		var out []string
		if err := json.Unmarshal([]byte(raw), &out); err == nil {
			*d = out
			return nil
		}
		if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
			inner := raw[1 : len(raw)-1]
			if inner == "" {
				*d = []string{}
				return nil
			}
			parts := strings.Split(inner, ",")
			for i := range parts {
				parts[i] = strings.Trim(strings.TrimSpace(parts[i]), `"`)
			}
			*d = parts
			return nil
		}
		return fmt.Errorf("cannot parse string slice from %q", raw)
	}
	return fmt.Errorf("unsupported flexScanner dest %T", fs.dest)
}

func wrapScanDest(dest []any) []any {
	out := make([]any, len(dest))
	for i, d := range dest {
		switch d.(type) {
		case *time.Time, **time.Time, *sql.NullTime, *[]string:
			out[i] = &flexScanner{dest: d}
		default:
			out[i] = d
		}
	}
	return out
}

func (p *Pool) QueryRow(ctx context.Context, query string, args ...any) *TestRow {
	return &TestRow{row: p.QueryRowContext(ctx, query, args...)}
}

func (p *Pool) Query(ctx context.Context, query string, args ...any) (*TestRows, error) {
	rows, err := p.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &TestRows{Rows: rows}, nil
}

func (p *Pool) Ping(ctx context.Context) error {
	return p.DB.PingContext(ctx)
}

func (p *Pool) Acquire(_ context.Context) (*Pool, error) {
	return p, nil
}

func (p *Pool) Release() {}

func (p *Pool) Conn() *Pool {
	return p
}

type Fixture struct {
	Pool        *Pool
	WorkspaceID string
	UserID      string
}

// New returns a Fixture bound to the workspace and user a test suite set up in
// its TestMain.
func New(pool any, workspaceID, userID string) *Fixture {
	var p *Pool
	switch v := pool.(type) {
	case *Pool:
		p = v
	case *sql.DB:
		p = &Pool{DB: v}
	case nil:
		p = nil
	default:
		panic(fmt.Sprintf("testutil.New: unsupported pool type %T", pool))
	}
	return &Fixture{Pool: p, WorkspaceID: workspaceID, UserID: userID}
}

// Insert writes one row into table, registers its deletion with t.Cleanup and
// returns the new row's id.
//
// Cleanup runs in reverse creation order, so a row inserted after the row it
// references is deleted before it and foreign keys hold without the test
// spelling out a teardown order.
func (f *Fixture) Insert(t TB, table string, cols Cols) string {
	t.Helper()

	if cols == nil {
		cols = Cols{}
	}
	if _, ok := cols["id"]; !ok {
		cols["id"] = uuid.NewString()
	}
	names, placeholders, args := compile(cols)
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(placeholders, ", "))

	var id string
	if err := f.Pool.QueryRowContext(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("setup: insert into %s: %v", table, err)
	}
	f.deleteOnCleanup(t, table, id)
	return id
}

// InsertNoID writes one row into a table that has no id column — a join table,
// typically — and deletes it again with the WHERE clause the caller gives.
// where is appended to `DELETE FROM <table> WHERE `, and its parameters are
// numbered from $1.
func (f *Fixture) InsertNoID(t TB, table string, cols Cols, where string, whereArgs ...any) {
	t.Helper()

	names, placeholders, args := compile(cols)
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(placeholders, ", "))
	if _, err := f.Pool.ExecContext(context.Background(), sql, args...); err != nil {
		t.Fatalf("setup: insert into %s: %v", table, err)
	}

	t.Cleanup(func() {
		_, _ = f.Pool.ExecContext(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(table), where), whereArgs...)
	})
}

// Exec runs a statement and fails the test if it errors.
func (f *Fixture) Exec(t TB, sql string, args ...any) {
	t.Helper()
	if _, err := f.Pool.ExecContext(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec: %v\nSQL: %s", err, strings.TrimSpace(sql))
	}
}

// Cleanup runs a statement when the test finishes, ignoring its error the way
// teardown always should: a failing test has usually already left the row in a
// state teardown cannot reach, and reporting that on top of the real failure
// buries it.
func (f *Fixture) Cleanup(t TB, sql string, args ...any) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = f.Pool.ExecContext(context.Background(), sql, args...)
	})
}

// Row is a pending single-row query.
type Row struct {
	f    *Fixture
	t    TB
	sql  string
	args []any
}

// QueryRow prepares a single-row query whose Scan fails the test on error.
func (f *Fixture) QueryRow(t TB, sql string, args ...any) *Row {
	t.Helper()
	return &Row{f: f, t: t, sql: sql, args: args}
}

// Scan reads the row into dest, failing the test if the query errors or
// matched nothing.
func (r *Row) Scan(dest ...any) {
	r.t.Helper()
	if err := r.f.Pool.QueryRowContext(context.Background(), r.sql, r.args...).Scan(wrapScanDest(dest)...); err != nil {
		r.t.Fatalf("query: %v\nSQL: %s", err, strings.TrimSpace(r.sql))
	}
}

// Count returns the count a `SELECT count(*) ...` query produced.
func (f *Fixture) Count(t TB, sql string, args ...any) int {
	t.Helper()
	var n int
	f.QueryRow(t, sql, args...).Scan(&n)
	return n
}

// ---- typed builders -------------------------------------------------------
//
// Each one fills the columns a row cannot be inserted without, and takes
// optional overrides for the column the test is actually about. Defaults are
// deliberately boring: a test that depends on one must name it.

// User inserts a user row.
func (f *Fixture) User(t TB, name, email string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "user", merge(Cols{
		"name":  name,
		"email": email,
	}, over))
}

// Workspace inserts a workspace row.
func (f *Fixture) Workspace(t TB, name, slug string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "workspace", merge(Cols{
		"name":         name,
		"slug":         slug,
		"description":  "",
		"issue_prefix": "",
	}, over))
}

// Member joins a user to a workspace.
func (f *Fixture) Member(t TB, workspaceID, userID, role string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "member", merge(Cols{
		"workspace_id": workspaceID,
		"user_id":      userID,
		"role":         role,
	}, over))
}

// Issue inserts an issue owned by the fixture's workspace and user.
//
// number is derived from the workspace's existing issues rather than fixed, so
// two issues built by the same test do not collide on the workspace's issue
// numbering.
func (f *Fixture) Issue(t TB, title string, over ...Cols) string {
	t.Helper()
	cols := merge(Cols{
		"workspace_id": f.WorkspaceID,
		"title":        title,
		"status":       "todo",
		"priority":     "none",
		"creator_type": "member",
		"creator_id":   f.UserID,
		"position":     0,
	}, over)
	if _, ok := cols["number"]; !ok {
		workspaceID, _ := cols["workspace_id"].(string)
		cols["number"] = Raw(fmt.Sprintf(
			"(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = %s)",
			quoteLiteral(workspaceID)))
	}
	return f.Insert(t, "issue", cols)
}

// Comment inserts a comment on issueID.
func (f *Fixture) Comment(t TB, issueID, content string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "comment", merge(Cols{
		"issue_id":     issueID,
		"workspace_id": f.WorkspaceID,
		"author_type":  "member",
		"author_id":    f.UserID,
		"content":      content,
		"type":         "comment",
	}, over))
}

// Runtime inserts an online cloud runtime.
func (f *Fixture) Runtime(t TB, name string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "agent_runtime", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"daemon_id":    nil,
		"name":         name,
		"runtime_mode": "cloud",
		"provider":     "handler_test_runtime",
		"status":       "online",
		"device_info":  "",
		"metadata":     "{}",
		"last_seen_at": Raw("(strftime('%Y-%m-%dT%H:%M:%fZ','now'))"),
		"visibility":   "private",
		"owner_id":     f.UserID,
	}, over))
}

// Agent inserts an agent bound to runtimeID. Pass an empty runtimeID for an
// agent with no runtime.
func (f *Fixture) Agent(t TB, name, runtimeID string, over ...Cols) string {
	t.Helper()
	cols := merge(Cols{
		"workspace_id":         f.WorkspaceID,
		"name":                 name,
		"description":          "",
		"runtime_mode":         "cloud",
		"runtime_config":       "{}",
		"visibility":           "private",
		"permission_mode":      "private",
		"max_concurrent_tasks": 1,
		"owner_id":             f.UserID,
	}, over)
	if _, ok := cols["runtime_id"]; !ok && runtimeID != "" {
		cols["runtime_id"] = runtimeID
	}
	return f.Insert(t, "agent", cols)
}

// Task queues a task for agentID.
func (f *Fixture) Task(t TB, agentID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "agent_task_queue", merge(Cols{
		"agent_id": agentID,
		"status":   "queued",
		"priority": 0,
	}, over))
}

// Squad inserts a squad led by leaderID.
func (f *Fixture) Squad(t TB, name, leaderID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "squad", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"name":         name,
		"description":  "",
		"leader_id":    leaderID,
		"creator_id":   f.UserID,
	}, over))
}

// SquadMember adds an agent or member to a squad.
func (f *Fixture) SquadMember(t TB, squadID, memberType, memberID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "squad_member", merge(Cols{
		"squad_id":    squadID,
		"member_type": memberType,
		"member_id":   memberID,
	}, over))
}

// Project inserts a project.
func (f *Fixture) Project(t TB, title string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "project", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"title":        title,
		"status":       "planned",
		"priority":     "none",
	}, over))
}

// ChatSession inserts a chat session with agentID.
func (f *Fixture) ChatSession(t TB, agentID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "chat_session", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"agent_id":     agentID,
		"creator_id":   f.UserID,
		"title":        "",
		"status":       "active",
	}, over))
}

// ---- internals ------------------------------------------------------------

func (f *Fixture) deleteOnCleanup(t TB, table, id string) {
	t.Cleanup(func() {
		_, _ = f.Pool.ExecContext(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE id = $1", quoteIdent(table)), id)
	})
}

// compile turns a column map into column names, value placeholders and bound
// arguments. Columns are sorted so the generated SQL is stable, which keeps a
// failing statement readable and diffable between runs.
func compile(cols Cols) (names []string, placeholders []string, args []any) {
	names = make([]string, 0, len(cols))
	for name := range cols {
		names = append(names, name)
	}
	sort.Strings(names)

	placeholders = make([]string, 0, len(names))
	args = make([]any, 0, len(names))
	for _, name := range names {
		if raw, ok := cols[name].(Raw); ok {
			placeholders = append(placeholders, string(raw))
			continue
		}
		args = append(args, cols[name])
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	for i, name := range names {
		names[i] = quoteIdent(name)
	}
	return names, placeholders, args
}

func merge(base Cols, over []Cols) Cols {
	out := make(Cols, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, o := range over {
		for k, v := range o {
			out[k] = v
		}
	}
	return out
}

// quoteIdent double-quotes an identifier so reserved words — `user`, the
// table, is one — survive as table and column names.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// OpenTestPool opens a fresh in-memory SQLite database with the full schema applied.
func OpenTestPool() (*sql.DB, error) {
	ctx := context.Background()
	_, thisFile, _, _ := runtime.Caller(0)
	schemaPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "schema", "sqlite.sql")
	dsn := fmt.Sprintf("file:testdb-%s?mode=memory&cache=shared", uuid.NewString())
	db, err := sqliteutil.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := sqliteutil.ApplySchema(ctx, db, schemaPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// OpenTestDB opens a fresh in-memory SQLite database for t and closes it in t.Cleanup.
func OpenTestDB(t TB) *sql.DB {
	t.Helper()
	db, err := OpenTestPool()
	if err != nil {
		t.Fatalf("testutil.OpenTestDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// SetupTestDB opens a fresh in-memory SQLite database wrapped in *Pool.
func SetupTestDB(t TB) *Pool {
	t.Helper()
	return &Pool{DB: OpenTestDB(t)}
}

func (p *Pool) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return p.DB.BeginTx(ctx, opts)
}
