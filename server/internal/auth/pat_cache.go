package auth

import (
	"context"
	"time"
)

// AuthCacheTTL is the historical revocation bound for a positive token
// lookup. The single-process server does not cache those lookups; every
// check reads Postgres. The constant remains so TTLForExpiry callers keep
// the same expiry clamp if a process-local cache is added later.
const AuthCacheTTL = 10 * time.Minute

// PATCache is retained as a nil-safe no-op. Authentication always reads
// Postgres; nothing is cached outside the process, and nothing is cached
// in-process either.
type PATCache struct{}

// NewPATCache disables caching. Callers treat a nil cache as a miss.
func NewPATCache() *PATCache { return nil }

// Get reports a cache miss.
func (c *PATCache) Get(context.Context, string) (userID string, ok bool) {
	return "", false
}

// Set is a no-op.
func (c *PATCache) Set(context.Context, string, string, time.Duration) {}

// TTLForExpiry returns the cache TTL for a token given its expires_at.
//   - Zero expiresAt (token never expires) → full AuthCacheTTL.
//   - expiresAt in the future → min(AuthCacheTTL, time until expiry).
//   - expiresAt at or before now → 0.
func TTLForExpiry(now, expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return AuthCacheTTL
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	if remaining < AuthCacheTTL {
		return remaining
	}
	return AuthCacheTTL
}

// Invalidate is a no-op.
func (c *PATCache) Invalidate(context.Context, string) {}
