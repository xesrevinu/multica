package auth

import (
	"context"
	"time"
)

// DaemonTokenIdentity is what DaemonAuth needs from a daemon-token lookup.
// Cache entries are unused; the middleware reads Postgres on every request.
type DaemonTokenIdentity struct {
	WorkspaceID string `json:"w"`
	DaemonID    string `json:"d"`
}

// DaemonTokenCache is a nil-safe no-op.
type DaemonTokenCache struct{}

// NewDaemonTokenCache disables caching.
func NewDaemonTokenCache() *DaemonTokenCache { return nil }

// Get reports a cache miss.
func (c *DaemonTokenCache) Get(context.Context, string) (DaemonTokenIdentity, bool) {
	return DaemonTokenIdentity{}, false
}

// Set is a no-op.
func (c *DaemonTokenCache) Set(context.Context, string, DaemonTokenIdentity, time.Duration) {}

// Invalidate is a no-op.
func (c *DaemonTokenCache) Invalidate(context.Context, string) {}
