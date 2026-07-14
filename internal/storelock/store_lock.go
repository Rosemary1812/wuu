// Package storelock serializes durable file-store transactions across
// processes. Callers keep the returned lock for the complete write or
// read-modify-write transaction, including every related index and event-log
// mutation. Pure readers should not acquire it: the lock is exclusive with no
// timeout, and Acquire creates the store directory and lock file on first
// use, so stores whose writers replace files atomically serve reads directly.
package storelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

const fileName = ".store.lock"

// Lock owns an exclusive operating-system advisory lock. The sidecar remains
// in place after release so every contender continues locking the same inode.
type Lock struct {
	mu   sync.Mutex
	file *os.File
}

// Acquire blocks until it exclusively owns the durable store rooted at dir.
func Acquire(dir string) (*Lock, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("store directory is required")
	}
	file, err := securefs.OpenFile(filepath.Join(dir, fileName), os.O_CREATE|os.O_RDWR, securefs.FileMode)
	if err != nil {
		return nil, fmt.Errorf("open store lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock store: %w", err)
	}
	return &Lock{file: file}, nil
}

// Release relinquishes the lock. It is safe to call more than once.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockFile(file), file.Close())
}
