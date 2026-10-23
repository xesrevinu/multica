package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type TxWrapper interface {
	WrapTx(tx DBTX) DBTX
}

type sqliteDBTX struct {
	inner DBTX
}

func wrapDBTX(d DBTX) DBTX {
	if d == nil {
		return nil
	}
	if _, ok := d.(sqliteDBTX); ok {
		return d
	}
	return sqliteDBTX{inner: d}
}

func (s sqliteDBTX) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.inner.ExecContext(ctx, query, normalizeSQLiteArgs(query, args)...)
}

func (s sqliteDBTX) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return s.inner.PrepareContext(ctx, query)
}

func (s sqliteDBTX) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return s.inner.QueryContext(ctx, query, normalizeSQLiteArgs(query, args)...)
}

func (s sqliteDBTX) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return s.inner.QueryRowContext(ctx, query, normalizeSQLiteArgs(query, args)...)
}

func isBlobQuery(query string) bool {
	return strings.Contains(query, "raw_body") ||
		strings.Contains(query, "app_secret_encrypted") ||
		strings.Contains(query, "plugin_archive") ||
		strings.Contains(query, "plugin_secret") ||
		strings.Contains(query, "pending_body")
}

func normalizeSQLiteArgs(query string, args []interface{}) []interface{} {
	if len(args) == 0 {
		return args
	}
	blobQuery := isBlobQuery(query)
	var out []interface{}
	for i, arg := range args {
		norm, changed := normalizeSQLiteArg(blobQuery, arg)
		if changed {
			if out == nil {
				out = make([]interface{}, len(args))
				copy(out, args)
			}
			out[i] = norm
		}
	}
	if out != nil {
		return out
	}
	return args
}

func normalizeSQLiteArg(blobQuery bool, arg interface{}) (interface{}, bool) {
	if arg == nil {
		return nil, false
	}
	switch v := arg.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			return nil, true
		}
		return string(v), true
	case *json.RawMessage:
		if v == nil || len(*v) == 0 {
			return nil, true
		}
		return string(*v), true
	case []byte:
		if v == nil {
			return nil, true
		}
		if !blobQuery {
			if len(v) == 0 {
				return nil, true
			}
			return string(v), true
		}
		return v, false
	case []string:
		if v == nil {
			return nil, true
		}
		b, err := json.Marshal(v)
		if err != nil {
			return "[]", true
		}
		return string(b), true
	case time.Time:
		if v.IsZero() {
			return "", true
		}
		return v.UTC().Format("2006-01-02T15:04:05.000000000Z"), true
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil, true
		}
		return v.UTC().Format("2006-01-02T15:04:05.000000000Z"), true
	}

	rv := reflect.ValueOf(arg)
	if rv.Kind() == reflect.Slice {
		if rv.IsNil() {
			return nil, true
		}
		b, err := json.Marshal(arg)
		if err != nil {
			return "[]", true
		}
		return string(b), true
	}
	return arg, false
}
