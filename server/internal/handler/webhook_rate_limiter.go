package handler

import (
	"context"
	"sync"
	"time"
)

// SlidingWindowRateLimit configures a per-key sliding-window limiter.
type SlidingWindowRateLimit struct {
	Limit  int           // maximum requests per window; zero disables the gate
	Window time.Duration // sliding window length
}

// WebhookRateLimit keeps the webhook-facing API stable while the underlying
// sliding-window implementation is shared with other admission gates.
type WebhookRateLimit = SlidingWindowRateLimit

// DefaultWebhookRateLimit is a coarse per-token sliding-window limiter.
//
// Defaults: 60 requests per 60s (1 RPS sustained, with bursts up to 60). The
// goal is "stop a misconfigured or malicious sender from hammering us
// indefinitely" — not "shape traffic to a precise budget" — so the
// implementation aims for cheap and good-enough rather than exact.
func DefaultWebhookRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 60, Window: time.Minute}
}

// DefaultWebhookIPRateLimit is the per-IP coarse budget applied BEFORE the
// trigger lookup. Set lower than the per-token budget on purpose: a single
// IP should rarely sustain more than 30 webhook deliveries / minute across
// all its tokens, while a malicious IP spraying random tokens hits this
// gate before it can probe Postgres.
func DefaultWebhookIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 30, Window: time.Minute}
}

// DefaultWebhookAbsoluteIPRateLimit is the high emergency ceiling applied to
// every webhook request before token lookup. Normal valid traffic should sit
// far below this budget; unlike the lower bad-credential debt limiter it is
// intentionally consumed by successful deliveries too.
func DefaultWebhookAbsoluteIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 600, Window: time.Minute}
}

// SlidingWindowRateLimiter is the contract implemented by both the in-memory
// and Redis-backed limiters.
//
// Implementations may expose non-consuming checks and retry hints through the
// optional interfaces below. Allow consumes one unit.
type SlidingWindowRateLimiter interface {
	Allow(ctx context.Context, key string) bool
	// Implementations may also satisfy slidingWindowRateLimiterInspector. Keeping
	// the base contract at Allow preserves compatibility with billing and
	// tests that inject simple one-method safety gates.
}

// WebhookRateLimiter keeps the webhook-facing contract stable.
type WebhookRateLimiter = SlidingWindowRateLimiter

type slidingWindowRateLimiterWithError interface {
	AllowWithError(ctx context.Context, key string) (bool, error)
}

type slidingWindowRateLimiterInspector interface {
	Check(ctx context.Context, key string) bool
	RetryAfter(ctx context.Context, key string) time.Duration
}

type slidingWindowRateLimiterCheckWithError interface {
	CheckWithError(ctx context.Context, key string) (bool, error)
}

// slidingWindowLimiterCheck keeps webhook ingress fail-open when an optional
// backend-backed check is unavailable.
func slidingWindowLimiterCheck(ctx context.Context, limiter SlidingWindowRateLimiter, key string) bool {
	allowed, err := slidingWindowLimiterCheckWithError(ctx, limiter, key)
	return err != nil || allowed
}

// slidingWindowLimiterCheckWithError exposes check failures to callers, such
// as invitation admission, that must fail closed without consuming a budget.
func slidingWindowLimiterCheckWithError(ctx context.Context, limiter SlidingWindowRateLimiter, key string) (bool, error) {
	if limiter == nil {
		return true, nil
	}
	if withError, ok := limiter.(slidingWindowRateLimiterCheckWithError); ok {
		return withError.CheckWithError(ctx, key)
	}
	if inspector, ok := limiter.(slidingWindowRateLimiterInspector); ok {
		return inspector.Check(ctx, key), nil
	}
	return true, nil
}

func slidingWindowLimiterRetryAfter(ctx context.Context, limiter SlidingWindowRateLimiter, key string) time.Duration {
	if inspector, ok := limiter.(slidingWindowRateLimiterInspector); ok {
		return inspector.RetryAfter(ctx, key)
	}
	return time.Minute
}

// slidingWindowLimiterAllow exposes backend errors to callers that cannot
// safely fail open. The legacy Allow method intentionally keeps webhook
// ingress fail-open on Redis errors.
func slidingWindowLimiterAllow(ctx context.Context, limiter SlidingWindowRateLimiter, key string) (bool, error) {
	if limiter == nil {
		return true, nil
	}
	if withError, ok := limiter.(slidingWindowRateLimiterWithError); ok {
		return withError.AllowWithError(ctx, key)
	}
	return limiter.Allow(ctx, key), nil
}

// ── In-memory implementation ────────────────────────────────────────────────

// memoryWebhookRateLimiter keeps per-key timestamps in a slice and prunes them
// on every call. Adequate for single-node dev / tests; production multi-node
// deployments should use the Redis-backed implementation so rate budgets are
// shared across pods.
type memoryWebhookRateLimiter struct {
	cfg       SlidingWindowRateLimit
	mu        sync.Mutex
	hit       map[string][]time.Time
	lastSweep time.Time
}

func newMemorySlidingWindowRateLimiter(cfg SlidingWindowRateLimit) SlidingWindowRateLimiter {
	return &memoryWebhookRateLimiter{cfg: cfg, hit: make(map[string][]time.Time)}
}

func NewMemoryWebhookRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return newMemorySlidingWindowRateLimiter(cfg)
}

func (l *memoryWebhookRateLimiter) Allow(ctx context.Context, key string) bool {
	allowed, _ := l.AllowWithError(ctx, key)
	return allowed
}

func (l *memoryWebhookRateLimiter) AllowWithError(_ context.Context, key string) (bool, error) {
	return l.evaluate(key, true), nil
}

func (l *memoryWebhookRateLimiter) Check(ctx context.Context, key string) bool {
	allowed, _ := l.CheckWithError(ctx, key)
	return allowed
}

func (l *memoryWebhookRateLimiter) CheckWithError(_ context.Context, key string) (bool, error) {
	return l.evaluate(key, false), nil
}

func (l *memoryWebhookRateLimiter) evaluate(key string, consume bool) bool {
	if l.cfg.Limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.cfg.Window)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepExpired(now, cutoff)

	hits := l.hit[key]
	// Trim entries that fell out of the window.
	keep := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.cfg.Limit {
		l.hit[key] = keep
		return false
	}
	if consume {
		keep = append(keep, now)
	}
	l.hit[key] = keep
	return true
}

func (l *memoryWebhookRateLimiter) sweepExpired(now, cutoff time.Time) {
	sweepInterval := l.cfg.Window
	if sweepInterval <= 0 || sweepInterval > time.Minute {
		sweepInterval = time.Minute
	}
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	for key, hits := range l.hit {
		if len(hits) == 0 || !hits[len(hits)-1].After(cutoff) {
			delete(l.hit, key)
		}
	}
	l.lastSweep = now
}

// NewMemoryWebhookIPRateLimiter is the single-process per-IP limiter.
func NewMemoryWebhookIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
}

func NewMemoryWebhookAbsoluteIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
}

func (l *memoryWebhookRateLimiter) RetryAfter(_ context.Context, key string) time.Duration {
	if l.cfg.Limit <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hit[key]
	if len(hits) == 0 {
		return l.cfg.Window
	}
	retry := time.Until(hits[0].Add(l.cfg.Window))
	if retry <= 0 {
		return time.Second
	}
	return retry
}
