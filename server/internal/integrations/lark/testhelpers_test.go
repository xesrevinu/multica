package lark

import (
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func uuidFromString(t *testing.T, s string) string {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u.String()
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
