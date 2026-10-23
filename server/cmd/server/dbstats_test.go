package main

import (
	"context"
	"testing"
	"time"
)

func TestNewDBPoolCapsMaxOpenConnections(t *testing.T) {
	ctx := context.Background()
	pool, err := newDBPool(ctx, ":memory:", 5*time.Second)
	if err != nil {
		t.Fatalf("newDBPool: %v", err)
	}
	defer pool.Close()

	if got := pool.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	logPoolConfig("primary", pool)
}
