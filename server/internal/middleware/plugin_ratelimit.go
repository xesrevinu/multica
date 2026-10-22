package middleware

import (
	"net/http"
	"time"
)

// PluginRateLimit is a no-op. The previous Redis limiter failed open when
// no client was configured; the single-process server keeps that behavior
// instead of adding a new in-memory budget.
func PluginRateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}
