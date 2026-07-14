//go:build windows

package sessionmemory

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(path string) (func(), error) {
	release, _, err := acquireFileLock(path, false)
	return release, err
}

func tryLockFile(path string) (func(), bool, error) {
	return acquireFileLock(path, true)
}

func acquireFileLock(path string, nonBlocking bool) (func(), bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, defaultFileMode)
	if err != nil {
		return nil, false, err
	}
	overlapped := &windows.Overlapped{}
	handle := windows.Handle(file.Fd())
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if nonBlocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		if nonBlocking && errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		_ = file.Close()
	}, true, nil
}
