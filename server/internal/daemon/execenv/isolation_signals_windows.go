//go:build windows

package execenv

import "os"

var helperInterruptSignals = []os.Signal{os.Interrupt}
