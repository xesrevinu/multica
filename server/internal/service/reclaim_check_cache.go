package service

import (
	"context"
	"time"
)

const (
	// The backstop remains at or below the PostgreSQL recovery window so a
	// missed hint cannot delay recovery beyond one window. With no cache,
	// every claim checks PostgreSQL directly.
	ReclaimCheckBackstopInterval = claimResponseRecoveryWindow
	// A due hint that produces no reclaimed row is retried at the daemon's next
	// normal polling cadence instead of becoming inert until the backstop.
	ReclaimCheckRetryInterval = 30 * time.Second
	// Leave a small margin for the dispatch command to commit so a hint cannot
	// fire just before the database's strict recovery-window predicate does.
	ReclaimCheckHintSafetyMargin = 5 * time.Second
	// Kept so call sites that size a hint window stay aligned with the
	// historical schedule TTL. Nothing is written to an external cache.
	ReclaimCheckScheduleTTL = 2*claimResponseRecoveryWindow + ReclaimCheckHintSafetyMargin
)

// ReclaimCheckCache is a nil-safe no-op. Missing state means "check
// PostgreSQL now", which is the only path in the single-process server.
type ReclaimCheckCache struct{}

// NewReclaimCheckCache disables caching.
func NewReclaimCheckCache() *ReclaimCheckCache { return nil }

// DueRuntimeIDs fails open: every non-empty runtime id is due, preserving
// input order. PostgreSQL remains authoritative for reclaim eligibility.
func (c *ReclaimCheckCache) DueRuntimeIDs(_ context.Context, runtimeIDs []string, _ time.Time) []string {
	if len(runtimeIDs) == 0 {
		return nil
	}
	valid := make([]string, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		if runtimeID == "" {
			continue
		}
		valid = append(valid, runtimeID)
	}
	if len(valid) == 0 {
		return nil
	}
	return valid
}

// Track is a no-op.
func (c *ReclaimCheckCache) Track(context.Context, string, string, time.Time) {}

// TrackLater is a no-op.
func (c *ReclaimCheckCache) TrackLater(context.Context, string, string, time.Time) {}

// Forget is a no-op.
func (c *ReclaimCheckCache) Forget(context.Context, string, string) {}

// MarkChecked is a no-op.
func (c *ReclaimCheckCache) MarkChecked(context.Context, []string, time.Time, time.Time) {}
