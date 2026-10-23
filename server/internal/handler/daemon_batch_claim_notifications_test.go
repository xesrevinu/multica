package handler

import (
	"sync/atomic"

)

type countingRelayPublisher struct {
	calls atomic.Int64
}

func (c *countingRelayPublisher) PublishWithID(string, string, string, []byte, string) error {
	c.calls.Add(1)
	return nil
}


