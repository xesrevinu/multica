package service

import (
	"context"
	"testing"
)

func TestEmptyClaimCache_NilSafe(t *testing.T) {
	var c *EmptyClaimCache
	ctx := context.Background()

	if c.IsEmpty(ctx, "any-runtime") {
		t.Fatal("nil cache must report not-empty (cache miss)")
	}
	if v := c.CurrentVersion(ctx, "any-runtime"); v != 0 {
		t.Fatalf("nil cache CurrentVersion must be 0, got %d", v)
	}
	c.MarkEmpty(ctx, "any-runtime", 0)
	c.Bump(ctx, "any-runtime")
}

func TestNewEmptyClaimCache_Disabled(t *testing.T) {
	if c := NewEmptyClaimCache(); c != nil {
		t.Fatalf("NewEmptyClaimCache() = %#v, want nil", c)
	}
}
