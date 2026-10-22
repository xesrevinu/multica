package main

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// newID mints a UUIDv7 string for columns that used to take pgtype.UUID.
// Entropy failure falls back to a v4 string so the insert still has an id;
// SQLite has no COALESCE(uuid) cast that would replace a NULL parameter.
func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func newNullID() sql.NullString {
	return sql.NullString{String: newID(), Valid: true}
}

func nullText(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullUUIDString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullJSON(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// jsonStringArray encodes ids for queries that bind json_each(?).
func jsonStringArray(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// nullSeconds formats a duration-in-seconds value for SQLite text parameters
// that sqlc models as sql.NullString (the old numeric/float columns).
func nullSeconds(v float64) sql.NullString {
	return sql.NullString{String: strconv.FormatFloat(v, 'f', -1, 64), Valid: true}
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

func nullDBTime(v sql.NullString) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return parseDBTime(v.String)
}
