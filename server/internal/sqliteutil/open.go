package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const compatDriverName = "multica-sqlite"

var registerDriverOnce sync.Once

func ensureCompatDriver() {
	registerDriverOnce.Do(func() {
		baseDB, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			panic(fmt.Sprintf("open base sqlite driver: %v", err))
		}
		baseDriver := baseDB.Driver()
		_ = baseDB.Close()
		sql.Register(compatDriverName, &compatDriver{base: baseDriver})
	})
}

var (
	reNumericCast      = regexp.MustCompile(`(?i)(\([^()]+\)|\$\d+|\?\d*)::numeric\b`)
	rePGCast           = regexp.MustCompile(`(?i)::(?:jsonb|json|uuid\[\]|text\[\]|int4\[\]|int8\[\]|float8\[\]|numeric\[\]|uuid|text|varchar|char|timestamptz|timestamp|date|time|interval|int8|int4|int2|integer|bigint|smallint|int|boolean|bool|bytea|numeric|float8|float4|real|regclass)(?:\[\])?`)
	reNowMinusInterval     = regexp.MustCompile(`(?i)\bnow\(\)\s*-\s*interval\s*'([^']+)'`)
	reNowPlusInterval      = regexp.MustCompile(`(?i)\bnow\(\)\s*\+\s*interval\s*'([^']+)'`)
	reNowMinusMakeInterval = regexp.MustCompile(`(?i)\bnow\(\)\s*-\s*make_interval\(\s*secs\s*=>\s*(\$\d+|\?\d*)\s*\)`)
	reNowPlusMakeInterval  = regexp.MustCompile(`(?i)\bnow\(\)\s*\+\s*make_interval\(\s*secs\s*=>\s*(\$\d+|\?\d*)\s*\)`)
	reForUpdate        = regexp.MustCompile(`(?i)\s+FOR\s+(?:UPDATE(?:\s+OF\s+[\w,\s]+)?(?:\s+SKIP\s+LOCKED|\s+NOWAIT)?|SHARE)\b`)
	reBoolOr           = regexp.MustCompile(`(?i)\bbool_or\s*\(`)
	reBoolAnd          = regexp.MustCompile(`(?i)\bbool_and\s*\(`)
	reILike            = regexp.MustCompile(`(?i)\bILIKE\b`)
	rePropsHasKey      = regexp.MustCompile(`(?i)\bi\.properties\s*\?\s*(\$\d+|'[^']*')`)
	reEqAny            = regexp.MustCompile(`(?i)=\s*ANY\s*\(\s*(\$\d+|\?\d*|[a-zA-Z_][a-zA-Z0-9_.]*)\s*\)`)
	reGenerateSeries   = regexp.MustCompile(`(?i)\bFROM\s+generate_series\s*\(\s*(\d+)\s*,\s*(\d+)\s*\)\s*(?:AS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)
	reBareArraySuffix  = regexp.MustCompile(`(\$\d+|\?\d*)\[\]`)
	reFromUnnest       = regexp.MustCompile(`(?i)\bFROM\s+unnest\s*\(\s*(\$\d+|\?\d*)\s*\)\s+AS\s+([a-zA-Z_]\w*)\s*\(\s*([a-zA-Z_]\w*)\s*\)`)
	reSelectUnnest     = regexp.MustCompile(`(?i)\bSELECT\s+unnest\s*\(\s*(\$\d+|\?\d*)\s*\)\s+AS\s+([a-zA-Z_]\w*)`)
	reJsonObjectAgg    = regexp.MustCompile(`(?i)\bjsonb?_object_agg\s*\(`)
	reGroupingWhen     = regexp.MustCompile(`WHEN GROUPING\((.+?)\) = 0 THEN '([^']+)'`)
)

func rewriteGroupingSets(q string) string {
	if !strings.Contains(q, "GROUP BY GROUPING SETS") {
		return q
	}
	fromIdx := strings.Index(q, "\nFROM issue i\nWHERE ")
	groupIdx := strings.Index(q, "\nGROUP BY GROUPING SETS (")
	if fromIdx < 0 || groupIdx < 0 || groupIdx <= fromIdx {
		return q
	}
	firstLineEnd := strings.IndexByte(q, '\n')
	if firstLineEnd < 0 || firstLineEnd > fromIdx {
		return q
	}
	firstLine := q[:firstLineEnd]
	matches := reGroupingWhen.FindAllStringSubmatch(firstLine, -1)
	if len(matches) == 0 {
		return q
	}
	whereClause := q[fromIdx+len("\nFROM issue i\nWHERE ") : groupIdx]
	groupingTail := q[groupIdx+len("\nGROUP BY GROUPING SETS ("):]
	includeTotal := strings.Contains(groupingTail, "()")
	branches := make([]string, 0, len(matches)+1)
	for _, m := range matches {
		expr := m[1]
		identity := m[2]
		branches = append(branches, fmt.Sprintf(
			"SELECT '%s', COALESCE(CAST((%s) AS TEXT), ''), COUNT(*) FROM _facet_base i GROUP BY (%s)",
			identity, expr, expr,
		))
	}
	if includeTotal {
		branches = append(branches, "SELECT '__total__', '', COUNT(*) FROM _facet_base i")
	}
	return fmt.Sprintf("WITH _facet_base AS (SELECT * FROM issue i WHERE %s)\n%s", whereClause, strings.Join(branches, "\nUNION ALL\n"))
}

// NormalizeSQL rewrites Postgres-specific SQL syntax into SQLite-compatible SQL.
func NormalizeSQL(q string) string {
	trimmed := strings.TrimSpace(q)
	if len(trimmed) >= 7 && strings.EqualFold(trimmed[:7], "CREATE ") {
		return q
	}
	q = rewriteGroupingSets(q)
	q = reNumericCast.ReplaceAllString(q, "CAST($1 AS REAL)")
	q = rePGCast.ReplaceAllString(q, "")
	q = reBareArraySuffix.ReplaceAllString(q, "$1")
	q = reFromUnnest.ReplaceAllString(q, "FROM (SELECT value AS $3 FROM json_each($1)) AS $2")
	q = reSelectUnnest.ReplaceAllString(q, "SELECT value AS $2 FROM json_each($1)")
	q = reJsonObjectAgg.ReplaceAllString(q, "json_group_object(")
	q = reNowMinusInterval.ReplaceAllString(q, "datetime('now', '-$1')")
	q = reNowPlusInterval.ReplaceAllString(q, "datetime('now', '+$1')")
	q = reNowMinusMakeInterval.ReplaceAllString(q, "datetime('now', '-' || ($1) || ' seconds')")
	q = reNowPlusMakeInterval.ReplaceAllString(q, "datetime('now', '+' || ($1) || ' seconds')")
	q = reForUpdate.ReplaceAllString(q, "")
	q = reBoolOr.ReplaceAllString(q, "MAX(")
	q = reBoolAnd.ReplaceAllString(q, "MIN(")
	q = reILike.ReplaceAllString(q, "LIKE")
	q = rePropsHasKey.ReplaceAllString(q, "(json_type(i.properties, '$.' || $1) IS NOT NULL)")
	q = reEqAny.ReplaceAllString(q, "IN (SELECT value FROM json_each($1))")
	q = reGenerateSeries.ReplaceAllString(q, "FROM (WITH RECURSIVE _gs($3) AS (SELECT $1 UNION ALL SELECT $3 + 1 FROM _gs WHERE $3 < $2) SELECT $3 FROM _gs) $3")
	return q
}

type compatDriver struct {
	base driver.Driver
}

func (d *compatDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &compatConn{Conn: c}, nil
}

type compatConn struct {
	driver.Conn
	inTx bool
}

type compatTx struct {
	driver.Tx
	conn *compatConn
}

func (t *compatTx) Commit() error {
	t.conn.inTx = false
	ClearLocalConfig()
	return t.Tx.Commit()
}

func (t *compatTx) Rollback() error {
	t.conn.inTx = false
	ClearLocalConfig()
	return t.Tx.Rollback()
}

func (c *compatConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(NormalizeSQL(query))
}

func (c *compatConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	query = NormalizeSQL(query)
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, query)
	}
	return c.Conn.Prepare(query)
}

func (c *compatConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	query = NormalizeSQL(query)
	if ec, ok := c.Conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *compatConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	query = NormalizeSQL(query)
	if qc, ok := c.Conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *compatConn) Begin() (driver.Tx, error) {
	ClearLocalConfig()
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	c.inTx = true
	return &compatTx{Tx: tx, conn: c}, nil
}

func (c *compatConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	ClearLocalConfig()
	var (
		tx  driver.Tx
		err error
	)
	if bt, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err = bt.BeginTx(ctx, opts)
	} else {
		tx, err = c.Conn.Begin()
	}
	if err != nil {
		return nil, err
	}
	c.inTx = true
	return &compatTx{Tx: tx, conn: c}, nil
}

func (c *compatConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *compatConn) ResetSession(ctx context.Context) error {
	if sr, ok := c.Conn.(driver.SessionResetter); ok {
		return sr.ResetSession(ctx)
	}
	return nil
}

func (c *compatConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *compatConn) CheckNamedValue(nv *driver.NamedValue) error {
	switch v := nv.Value.(type) {
	case time.Time:
		nv.Value = v.UTC().Format("2006-01-02T15:04:05.000000000Z")
		return nil
	case *time.Time:
		if v == nil {
			nv.Value = nil
		} else {
			nv.Value = v.UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
		return nil
	case uuid.UUID:
		nv.Value = v.String()
		return nil
	case *uuid.UUID:
		if v == nil {
			nv.Value = nil
		} else {
			nv.Value = v.String()
		}
		return nil
	case []string:
		if v == nil {
			nv.Value = "[]"
		} else {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			nv.Value = string(b)
		}
		return nil
	case []uuid.UUID:
		strs := make([]string, len(v))
		for i, u := range v {
			strs[i] = u.String()
		}
		b, err := json.Marshal(strs)
		if err != nil {
			return err
		}
		nv.Value = string(b)
		return nil
	case []int64:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		nv.Value = string(b)
		return nil
	case []int:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		nv.Value = string(b)
		return nil
	case json.RawMessage:
		if len(v) == 0 {
			nv.Value = nil
		} else {
			nv.Value = string(v)
		}
		return nil
	case []byte:
		if v == nil {
			nv.Value = nil
		} else if utf8.Valid(v) {
			nv.Value = string(v)
		}
		return nil
	}
	if nvc, ok := c.Conn.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	var err error
	nv.Value, err = driver.DefaultParameterConverter.ConvertValue(nv.Value)
	return err
}

// Open opens a single-writer SQLite database.
//
// databaseURL accepts a file: URI or a bare absolute/relative path.
// postgres:// and postgresql:// return ErrPostgresURL.
// The pool is capped at one connection so this process is the only writer.
func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	ensureCompatDriver()
	dsn, err := buildDSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(compatDriverName, dsn)
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
