package handler

import (
	"context"
	"testing"
	"time"
)

func TestMemoryWebhookRateLimiter_AllowsBelowLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 3, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestMemoryWebhookRateLimiter_RejectsAboveLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: time.Minute})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if !l.Allow(ctx, "tok") {
		t.Fatal("second should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("third should be rejected")
	}
	// Different key must have its own budget.
	if !l.Allow(ctx, "other") {
		t.Fatal("different key should pass")
	}
}

func TestMemoryWebhookRateLimiter_WindowExpiry(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: 10 * time.Millisecond})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("second should fail (within window)")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow(ctx, "tok") {
		t.Fatal("third should pass after window")
	}
}

func TestMemorySlidingWindowRateLimiter_PrunesExpiredKeys(t *testing.T) {
	limiter := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: 10 * time.Millisecond})
	memoryLimiter, ok := limiter.(*memoryWebhookRateLimiter)
	if !ok {
		t.Fatalf("limiter type = %T, want *memoryWebhookRateLimiter", limiter)
	}
	ctx := context.Background()
	if !limiter.Allow(ctx, "expired") {
		t.Fatal("first key should be allowed")
	}
	time.Sleep(20 * time.Millisecond)
	if !limiter.Allow(ctx, "current") {
		t.Fatal("current key should be allowed")
	}

	memoryLimiter.mu.Lock()
	defer memoryLimiter.mu.Unlock()
	if _, ok := memoryLimiter.hit["expired"]; ok {
		t.Fatal("expired key was not removed by the opportunistic sweep")
	}
	if _, ok := memoryLimiter.hit["current"]; !ok {
		t.Fatal("current key was removed by the opportunistic sweep")
	}
}

func TestMemoryWebhookRateLimiter_ZeroLimitDisabled(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 0, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("Limit=0 should be unbounded, rejected at %d", i)
		}
	}
}

func TestMemoryWebhookRateLimiter_CheckDoesNotConsumeBudget(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})
	ctx := context.Background()
	if !slidingWindowLimiterCheck(ctx, l, "shared-ip") || !slidingWindowLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("non-consuming checks should remain allowed before bad debt is charged")
	}
	if !l.Allow(ctx, "shared-ip") {
		t.Fatal("check unexpectedly consumed the only budget slot")
	}
	if slidingWindowLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("check should reject after bad debt reaches the limit")
	}
	if retry := slidingWindowLimiterRetryAfter(ctx, l, "shared-ip"); retry <= 0 || retry > time.Minute {
		t.Fatalf("unexpected retry interval: %v", retry)
	}
}
