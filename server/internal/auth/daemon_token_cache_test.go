package auth

import (
	"context"
	"testing"
	"time"
)

func TestDaemonTokenCache_NilSafe(t *testing.T) {
	var c *DaemonTokenCache
	ctx := context.Background()
	if _, ok := c.Get(ctx, "hash"); ok {
		t.Fatal("nil cache must miss")
	}
	c.Set(ctx, "hash", DaemonTokenIdentity{WorkspaceID: "ws", DaemonID: "d"}, time.Minute)
	c.Invalidate(ctx, "hash")
}

func TestNewDaemonTokenCache_Disabled(t *testing.T) {
	if c := NewDaemonTokenCache(); c != nil {
		t.Fatalf("NewDaemonTokenCache() = %#v, want nil", c)
	}
}
