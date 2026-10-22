package sqliteutil

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrPostgresURL is returned when databaseURL uses a Postgres scheme.
// Callers should open a SQLite file instead.
var ErrPostgresURL = errors.New("postgres URLs are not supported; use a SQLite path such as file:./data/multica.db")

const (
	pragmaBusyTimeout = "busy_timeout(5000)"
	pragmaJournalMode = "journal_mode(WAL)"
	pragmaForeignKeys = "foreign_keys(OFF)"
	txLockImmediate   = "immediate"
)

// buildDSN normalizes databaseURL into a modernc.org/sqlite DSN.
//
// Accepted forms are file: URIs (including file::memory:) and bare absolute
// or relative paths. postgres:// and postgresql:// are rejected.
//
// The DSN always sets busy_timeout(5000), journal_mode(WAL),
// foreign_keys(OFF), and _txlock=immediate. Caller-supplied values for those
// keys are replaced; other query parameters are preserved.
func buildDSN(databaseURL string) (string, error) {
	filePart, query, err := splitFileAndQuery(databaseURL)
	if err != nil {
		return "", err
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("parse sqlite database url query: %w", err)
	}
	pragmas := make([]string, 0, len(values["_pragma"])+3)
	for _, pragma := range values["_pragma"] {
		if isManagedPragma(pragma) {
			continue
		}
		pragmas = append(pragmas, pragma)
	}
	pragmas = append(pragmas, pragmaBusyTimeout, pragmaJournalMode, pragmaForeignKeys)
	values["_pragma"] = pragmas
	values.Set("_txlock", txLockImmediate)
	return filePart + "?" + values.Encode(), nil
}

// FilePath returns the on-disk path for databaseURL.
// In-memory databases return "" without an error.
func FilePath(databaseURL string) (string, error) {
	filePart, _, err := splitFileAndQuery(databaseURL)
	if err != nil {
		return "", err
	}
	return filePathFromFileURI(filePart), nil
}

func splitFileAndQuery(databaseURL string) (filePart, query string, err error) {
	raw := strings.TrimSpace(databaseURL)
	if raw == "" {
		return "", "", errors.New("database url is empty")
	}
	base, query := raw, ""
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		base, query = raw[:i], raw[i+1:]
	}
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return "", "", ErrPostgresURL
	}
	if base == ":memory:" {
		return "file::memory:", query, nil
	}
	if strings.HasPrefix(lower, "file:") {
		return base, query, nil
	}
	if isWindowsDrive(base) {
		return "file:///" + strings.ReplaceAll(base, "\\", "/"), query, nil
	}
	return "file:" + base, query, nil
}

func filePathFromFileURI(filePart string) string {
	const prefix = "file:"
	rest := filePart[len(prefix):]
	if rest == "" || strings.Contains(strings.ToLower(rest), ":memory:") {
		return ""
	}
	if strings.HasPrefix(rest, "//") {
		rest = rest[2:]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return ""
		}
		host := rest[:slash]
		path := rest[slash:]
		if host != "" && !strings.EqualFold(host, "localhost") {
			return path
		}
		// file:///C:/data/app.db arrives as /C:/data/app.db.
		if len(path) >= 4 && path[0] == '/' && path[2] == ':' && isASCIIAlpha(path[1]) {
			path = path[1:]
		}
		return path
	}
	return rest
}

func isManagedPragma(pragma string) bool {
	p := strings.ToLower(strings.TrimSpace(pragma))
	return strings.HasPrefix(p, "busy_timeout") ||
		strings.HasPrefix(p, "journal_mode") ||
		strings.HasPrefix(p, "foreign_keys")
}

func isWindowsDrive(path string) bool {
	return len(path) >= 3 && isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
