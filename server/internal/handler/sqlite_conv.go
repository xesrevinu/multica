package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// formatUUID renders a UUID value the way nullable UUID columns are stored in
// SQLite: invalid/empty becomes "", otherwise the canonical string.
func formatUUID(u uuid.UUID, valid bool) string {
	if !valid || u == uuid.Nil {
		return ""
	}
	return u.String()
}

func formatTS(t time.Time, valid bool) string {
	if !valid || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatDate(t time.Time, valid bool) sql.NullString {
	if !valid || t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.DateOnly), Valid: true}
}

func jsonText(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func int64ToBool(v int64) bool { return v != 0 }

// flagSet is the SQLite stand-in for a sqlc boolean "was this column touched"
// argument. NULL/invalid is false; "1" casts numeric and is true.
func flagSet(on bool) sql.NullString {
	if !on {
		return sql.NullString{}
	}
	return sql.NullString{String: "1", Valid: true}
}

func nullJSON(s sql.NullString) json.RawMessage {
	if !s.Valid || s.String == "" {
		return nil
	}
	return json.RawMessage(s.String)
}

func ifaceInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	case []byte:
		i, _ := strconv.ParseInt(strings.TrimSpace(string(n)), 10, 64)
		return i
	default:
		return 0
	}
}

func ifaceFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	case []byte:
		f, _ := strconv.ParseFloat(strings.TrimSpace(string(n)), 64)
		return f
	default:
		return 0
	}
}

func ifaceStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case nil:
		return []string{}
	case []string:
		if t == nil {
			return []string{}
		}
		return t
	case string:
		if t == "" {
			return []string{}
		}
		var out []string
		if err := json.Unmarshal([]byte(t), &out); err != nil {
			return []string{t}
		}
		if out == nil {
			return []string{}
		}
		return out
	case []byte:
		return ifaceStringSlice(string(t))
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, ifaceString(item))
		}
		return out
	default:
		return []string{}
	}
}

func ifaceDate(v interface{}) string {
	switch d := v.(type) {
	case time.Time:
		if d.IsZero() {
			return ""
		}
		return d.Format("2006-01-02")
	case string:
		if t, err := time.Parse(time.RFC3339Nano, d); err == nil {
			return t.Format("2006-01-02")
		}
		if len(d) >= 10 && d[4] == '-' && d[7] == '-' {
			return d[:10]
		}
		return d
	case []byte:
		return ifaceDate(string(d))
	default:
		return ifaceString(v)
	}
}

// toPGUUID / fromPGUUID adapt handler strings to packages that still speak pgtype.
func toPGUUID(s string) string {
	return s
}

func fromPGUUID(u any) string {
	return ifaceString(u)
}

func toPGUUIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

func toPGText(s sql.NullString) pgtype.Text {
	if !s.Valid {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s.String, Valid: true}
}

func fromPGText(t pgtype.Text) sql.NullString {
	if !t.Valid {
		return sql.NullString{}
	}
	return sql.NullString{String: t.String, Valid: true}
}

func toPGTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return pgtype.Timestamptz{}
}

func fromPGTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// sqlDBBeginner adapts *sql.DB to handler txStarter/dbExecutor.
type sqlDBBeginner struct{ DB *sql.DB }

func (s sqlDBBeginner) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if s.DB == nil {
		return nil, sql.ErrConnDone
	}
	return s.DB.BeginTx(ctx, opts)
}

func (s sqlDBBeginner) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, query, args...)
}

func (s sqlDBBeginner) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, query, args...)
}

func (s sqlDBBeginner) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.DB.QueryRowContext(ctx, query, args...)
}

func toPGTimeNull(s sql.NullString) pgtype.Timestamptz {
	if !s.Valid {
		return pgtype.Timestamptz{}
	}
	return toPGTime(s.String)
}

func nullSeconds(v any) sql.NullString {
	switch x := v.(type) {
	case int:
		return sql.NullString{String: strconv.Itoa(x), Valid: true}
	case int32:
		return sql.NullString{String: strconv.FormatInt(int64(x), 10), Valid: true}
	case int64:
		return sql.NullString{String: strconv.FormatInt(x, 10), Valid: true}
	case float64:
		return sql.NullString{String: strconv.FormatFloat(x, 'f', -1, 64), Valid: true}
	case string:
		return nullString(x)
	default:
		return sql.NullString{}
	}
}

func decodeStringSlice(raw string) []string {
	return ifaceStringSlice(raw)
}

func encodeStringSlice(vals []string) string {
	if vals == nil {
		return "[]"
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func ifaceBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return ifaceInt64(v) != 0
}
