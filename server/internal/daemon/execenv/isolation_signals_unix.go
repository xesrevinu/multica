//go:build !windows

package execenv

import (
	"os"
	"syscall"
)

// helperInterruptSignals cancel the helper's prepare context so runWorktrunk
// can reap wt and its hooks. The parent still KillTree's the whole descendant
// set; this is the cooperative path when the helper is still scheduled.
var helperInterruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
