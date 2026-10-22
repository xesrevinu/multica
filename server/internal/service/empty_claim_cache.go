package service

import (
	"context"
	"time"
)

// EmptyClaimCacheTTL is the historical bound on a cached "no queued task"
// verdict. The single-process server does not cache that verdict; claim
// always reads Postgres.
const EmptyClaimCacheTTL = 3 * time.Minute

// EmptyClaimCache is a nil-safe no-op. A nil receiver reports a cache miss
// so TaskService falls through to the database.
type EmptyClaimCache struct{}

// NewEmptyClaimCache disables caching.
func NewEmptyClaimCache() *EmptyClaimCache { return nil }

// CurrentVersion reports an unknown version so callers do not trust a
// cached empty verdict.
func (c *EmptyClaimCache) CurrentVersion(context.Context, string) int64 { return 0 }

// IsEmpty reports a cache miss.
func (c *EmptyClaimCache) IsEmpty(context.Context, string) bool { return false }

// MarkEmpty is a no-op.
func (c *EmptyClaimCache) MarkEmpty(context.Context, string, int64) {}

// Bump is a no-op.
func (c *EmptyClaimCache) Bump(context.Context, string) {}
