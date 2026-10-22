package service

import (
	"context"
	"testing"
	"time"
)

func TestReclaimCheckCache_NilFallsBackToEveryRuntime(t *testing.T) {
	var cache *ReclaimCheckCache
	runtimes := []string{"rt-a", "rt-b"}
	due := cache.DueRuntimeIDs(context.Background(), runtimes, time.Now())
	if len(due) != len(runtimes) || due[0] != runtimes[0] || due[1] != runtimes[1] {
		t.Fatalf("nil cache due runtimes = %v, want %v", due, runtimes)
	}
	cache.Track(context.Background(), "rt-a", "task-a", time.Now())
	cache.TrackLater(context.Background(), "rt-a", "task-a", time.Now())
	cache.Forget(context.Background(), "rt-a", "task-a")
	cache.MarkChecked(context.Background(), runtimes, time.Now(), time.Now().Add(ReclaimCheckRetryInterval))
}

func TestNewReclaimCheckCache_Disabled(t *testing.T) {
	if cache := NewReclaimCheckCache(); cache != nil {
		t.Fatalf("NewReclaimCheckCache() = %#v, want nil", cache)
	}
}

func TestReclaimCheckCache_SkipsEmptyRuntimeIDs(t *testing.T) {
	due := NewReclaimCheckCache().DueRuntimeIDs(context.Background(), []string{"", "rt-a", ""}, time.Now())
	if len(due) != 1 || due[0] != "rt-a" {
		t.Fatalf("due = %v, want [rt-a]", due)
	}
	if got := (*ReclaimCheckCache)(nil).DueRuntimeIDs(context.Background(), nil, time.Now()); got != nil {
		t.Fatalf("empty input = %v, want nil", got)
	}
}
