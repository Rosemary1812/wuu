//go:build !windows

package sessionmemory

import (
	"errors"
	"os"
	"syscall"
)

func lockFile(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, defaultFileMode)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		for {
			err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		_ = file.Close()
	}, nil
}
