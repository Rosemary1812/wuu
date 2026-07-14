//go:build !windows

package sessionmemory

import (
	"errors"
	"os"
	"syscall"
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
	operation := syscall.LOCK_EX
	if nonBlocking {
		operation |= syscall.LOCK_NB
	}
	for {
		err = syscall.Flock(int(file.Fd()), operation)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		if nonBlocking && (errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		for {
			err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		_ = file.Close()
	}, true, nil
}
