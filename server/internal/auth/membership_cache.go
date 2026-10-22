package auth

import (
	"context"
	"time"
)

// MembershipCacheTTL is the historical bound on a cached membership grant.
// Membership checks now always read Postgres.
const MembershipCacheTTL = 5 * time.Minute

// MembershipCache is a nil-safe no-op. It does not store role data and it
// does not store membership either; callers fall through to the database.
type MembershipCache struct{}

// NewMembershipCache disables caching.
func NewMembershipCache() *MembershipCache { return nil }

// Get reports a cache miss.
func (c *MembershipCache) Get(context.Context, string, string) bool { return false }

// Set is a no-op.
func (c *MembershipCache) Set(context.Context, string, string) {}

// Invalidate is a no-op.
func (c *MembershipCache) Invalidate(context.Context, string, string) {}
