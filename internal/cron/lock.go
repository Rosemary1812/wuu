package cron

import (
	"os"
	"path/filepath"
	"sync"
)

// Lock owns durable cron scheduling while its file descriptor holds an
// advisory lock. The lock file remains in place so every contender locks the
// same inode.
type Lock struct {
	path string
	mu   sync.Mutex
	file *os.File
}

func NewLock(path string) *Lock {
	return &Lock{path: path}
}

func (l *Lock) TryAcquire() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return false, err
	}

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	acquired, err := flockTryExclusive(file)
	if err != nil {
		_ = file.Close()
		return false, err
	}
	if !acquired {
		_ = file.Close()
		return false, nil
	}

	l.file = file
	return true, nil
}

func (l *Lock) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return
	}
	flockUnlock(l.file)
	_ = l.file.Close()
	l.file = nil
}
