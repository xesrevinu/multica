package auth

import (
	"context"
	"testing"
	"time"
)

func TestPATCache_NilSafe(t *testing.T) {
	var c *PATCache
	ctx := context.Background()
	if _, ok := c.Get(ctx, "hash"); ok {
		t.Fatal("nil cache must miss")
	}
	c.Set(ctx, "hash", "user", time.Minute)
	c.Invalidate(ctx, "hash")
}

func TestNewPATCache_Disabled(t *testing.T) {
	if c := NewPATCache(); c != nil {
		t.Fatalf("NewPATCache() = %#v, want nil", c)
	}
}

func TestTTLForExpiry(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	if got := TTLForExpiry(now, time.Time{}); got != AuthCacheTTL {
		t.Fatalf("zero expires_at: got %v, want %v", got, AuthCacheTTL)
	}

	far := now.Add(24 * time.Hour)
	if got := TTLForExpiry(now, far); got != AuthCacheTTL {
		t.Fatalf("far-future expires_at: got %v, want %v", got, AuthCacheTTL)
	}

	soon := now.Add(10 * time.Second)
	if got := TTLForExpiry(now, soon); got != 10*time.Second {
		t.Fatalf("sooner expires_at: got %v, want 10s", got)
	}

	if got := TTLForExpiry(now, now); got != 0 {
		t.Fatalf("expires_at == now: got %v, want 0", got)
	}
	if got := TTLForExpiry(now, now.Add(-time.Second)); got != 0 {
		t.Fatalf("past expires_at: got %v, want 0", got)
	}
}
