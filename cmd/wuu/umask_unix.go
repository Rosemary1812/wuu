//go:build !windows

package main

import "syscall"

// lockProcessUmask locks the process umask to 0o077 so every subsequent
// os.OpenFile(O_CREATE, ...) — and any sibling files a third-party
// library creates on our behalf, most notably sqlite's <db>-wal /
// <db>-shm — lands as 0o600 instead of the typical 0o644. Done before
// run() so the umask applies even to startup helpers that touch state
// on disk. Daemon is a single long-lived process, so the global side
// effect is intentional — no other code path in Wuu relies on a
// permissive umask.
func lockProcessUmask() {
	syscall.Umask(0o077)
}
