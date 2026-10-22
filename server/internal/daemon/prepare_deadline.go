package daemon

import (
	"context"
	"sync"
	"time"
)

// pausableDeadline is a context deadline that stops counting while the task is
// queued for a resource (pause) and restarts with the full budget once it has
// it (resume). Cancelled with cause when it fires.
type pausableDeadline struct {
	mu      sync.Mutex
	timer   *time.Timer
	budget  time.Duration
	cancelC context.CancelCauseFunc
	paused  bool
}

func newPausableDeadline(parent context.Context, budget time.Duration, cause error) (context.Context, *pausableDeadline) {
	ctx, cancel := context.WithCancelCause(parent)
	d := &pausableDeadline{budget: budget, cancelC: cancel}
	d.timer = time.AfterFunc(budget, func() { cancel(cause) })
	return ctx, d
}

func (d *pausableDeadline) pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.paused {
		d.timer.Stop()
		d.paused = true
	}
}

func (d *pausableDeadline) resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paused {
		d.timer.Reset(d.budget)
		d.paused = false
	}
}

func (d *pausableDeadline) cancel() {
	d.mu.Lock()
	d.timer.Stop()
	d.mu.Unlock()
	d.cancelC(context.Canceled)
}
