//go:build windows

package session

// Windows crash recovery remains conservative without a process handle: a
// stale heartbeat is not sufficient evidence to interrupt another runtime.
// Explicitly closed leases are still recoverable.
func inferenceRuntimeProcessAlive(pid int) bool {
	return pid > 0
}
