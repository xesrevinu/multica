package daemon

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTestDeadline = errors.New("deadline")

func TestPausableDeadlineFires(t *testing.T) {
	ctx, d := newPausableDeadline(context.Background(), 30*time.Millisecond, errTestDeadline)
	defer d.cancel()
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), errTestDeadline) {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
}

// Queue-wait time (paused) must not count toward the prepare budget.
func TestPausableDeadlineIgnoresQueuedTime(t *testing.T) {
	ctx, d := newPausableDeadline(context.Background(), 60*time.Millisecond, errTestDeadline)
	defer d.cancel()
	d.pause()
	select {
	case <-ctx.Done():
		t.Fatal("deadline fired while paused (queued for the directory)")
	case <-time.After(150 * time.Millisecond):
	}
	d.resume()
	select {
	case <-ctx.Done():
		t.Fatal("resume must restart the full budget")
	case <-time.After(20 * time.Millisecond):
	}
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), errTestDeadline) {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
}

func TestPausableDeadlineCancelIsNotTimeout(t *testing.T) {
	ctx, d := newPausableDeadline(context.Background(), time.Hour, errTestDeadline)
	d.cancel()
	<-ctx.Done()
	if errors.Is(context.Cause(ctx), errTestDeadline) {
		t.Fatal("explicit cancel classified as timeout")
	}
}
