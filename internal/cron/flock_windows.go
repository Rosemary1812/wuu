//go:build windows

package cron

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// The whole file (offset 0, length 1) is locked, matching the unix
// whole-file flock semantics used by the scheduler.

func flockExclusive(file *os.File) error {
	overlapped := &windows.Overlapped{}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped)
}

func flockTryExclusive(file *os.File) (bool, error) {
	overlapped := &windows.Overlapped{}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

func flockUnlock(file *os.File) {
	overlapped := &windows.Overlapped{}
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}
