package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func newV7() string {
	id, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return id.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullTime(t time.Time, valid bool) sql.NullString {
	if !valid {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(t), Valid: true}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64(v int64, valid bool) sql.NullInt64 {
	if !valid {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		return parseDBTime(t)
	case sql.NullString:
		if !t.Valid {
			return time.Time{}
		}
		return parseDBTime(t.String)
	default:
		return time.Time{}
	}
}

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func timeString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case sql.NullString:
		if !t.Valid {
			return ""
		}
		return t.String
	case time.Time:
		return formatTime(t)
	default:
		return ""
	}
}

func timePtr(v any) *string {
	s := timeString(v)
	if s == "" {
		return nil
	}
	return &s
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

type stubResult struct{ n int64 }

func (s stubResult) LastInsertId() (int64, error) { return 0, nil }
func (s stubResult) RowsAffected() (int64, error) { return s.n, nil }

func errQueryRow() *sql.Row {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	return db.QueryRow("SELECT 1 WHERE 0")
}

func isSQLiteUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func stringListJSON(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func parseStringList(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int32:
		return int64(t)
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	default:
		return 0
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case string:
		return t != "" && t != "0" && t != "false"
	case sql.NullString:
		return t.Valid && t.String != "" && t.String != "0" && t.String != "false"
	default:
		return v != nil
	}
}

func asNullString(v any) sql.NullString {
	switch t := v.(type) {
	case sql.NullString:
		return t
	case string:
		return nullString(t)
	case []byte:
		return nullString(string(t))
	default:
		if v == nil {
			return sql.NullString{}
		}
		return nullString(fmt.Sprint(v))
	}
}

func rowsAffected(r sql.Result) int64 {
	if r == nil {
		return 0
	}
	n, _ := r.RowsAffected()
	return n
}


var (
	mockDriverOnce sync.Once
	mockDriverMu   sync.Mutex
	mockDriverSeq  int64
	mockDriverData = map[string]mockQuerySpec{}
)

type mockQuerySpec struct {
	rows [][]any
	err  error
}

type mockDriver struct{}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockDriverConn{id: name}, nil
}

type mockDriverConn struct {
	id string
}

func (c *mockDriverConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c *mockDriverConn) Close() error              { return nil }
func (c *mockDriverConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (c *mockDriverConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	mockDriverMu.Lock()
	spec := mockDriverData[c.id]
	delete(mockDriverData, c.id)
	mockDriverMu.Unlock()
	if spec.err != nil {
		return nil, spec.err
	}
	cols := 1
	if len(spec.rows) > 0 {
		cols = len(spec.rows[0])
	}
	colNames := make([]string, cols)
	for i := range colNames {
		colNames[i] = fmt.Sprintf("c%d", i)
	}
	return &mockDriverRows{cols: colNames, rows: spec.rows}, nil
}

type mockDriverRows struct {
	cols []string
	rows [][]any
	idx  int
}

func (r *mockDriverRows) Columns() []string { return r.cols }
func (r *mockDriverRows) Close() error      { return nil }
func (r *mockDriverRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i := range dest {
		if i >= len(row) {
			dest[i] = nil
			continue
		}
		dest[i] = toDriverValue(row[i])
	}
	return nil
}

func toDriverValue(v any) driver.Value {
	switch x := v.(type) {
	case nil:
		return nil
	case sql.NullString:
		if !x.Valid {
			return nil
		}
		return x.String
	case sql.NullInt64:
		if !x.Valid {
			return nil
		}
		return x.Int64
	case sql.NullInt32:
		if !x.Valid {
			return nil
		}
		return int64(x.Int32)
	case sql.NullFloat64:
		if !x.Valid {
			return nil
		}
		return x.Float64
	case sql.NullBool:
		if !x.Valid {
			return nil
		}
		return x.Bool
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64, float64, bool, string, []byte, time.Time:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func structToRow(s any) []any {
	rv := reflect.ValueOf(s)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	out := make([]any, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		out[i] = rv.Field(i).Interface()
	}
	return out
}

func openMockSpec(spec mockQuerySpec) *sql.DB {
	mockDriverOnce.Do(func() {
		sql.Register("multica-service-mock", &mockDriver{})
	})
	mockDriverMu.Lock()
	mockDriverSeq++
	id := fmt.Sprintf("mock-%d", mockDriverSeq)
	mockDriverData[id] = spec
	mockDriverMu.Unlock()
	db, err := sql.Open("multica-service-mock", id)
	if err != nil {
		panic(err)
	}
	return db
}

func mockQueryRow(rows [][]any, err error) *sql.Row {
	db := openMockSpec(mockQuerySpec{rows: rows, err: err})
	return db.QueryRow("SELECT 1")
}

func mockQueryRows(rows [][]any, err error) (*sql.Rows, error) {
	db := openMockSpec(mockQuerySpec{rows: rows, err: err})
	return db.Query("SELECT 1")
}
