//go:build windows

package execenv

// pidAlive stays true for any named pid. Windows has no portable signal-0
// probe here, and guessing that a holder has exited would let GC delete a
// checkout a run is still using.
func pidAlive(pid int) bool {
	return pid > 0
}
