package auth

import (
	"context"
	"testing"
)

func TestMembershipCache_NilSafe(t *testing.T) {
	var c *MembershipCache
	ctx := context.Background()
	if c.Get(ctx, "user", "ws") {
		t.Fatal("nil cache must miss")
	}
	c.Set(ctx, "user", "ws")
	c.Invalidate(ctx, "user", "ws")
}

func TestNewMembershipCache_Disabled(t *testing.T) {
	if c := NewMembershipCache(); c != nil {
		t.Fatalf("NewMembershipCache() = %#v, want nil", c)
	}
}
