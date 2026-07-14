package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

const threadExecutionLeaseDirName = ".turn-locks"

// ThreadExecutionLease owns execution of one durable thread while its file
// descriptor holds an operating-system advisory lock. The sidecar remains in
// place after release so every contender continues locking the same inode.
type ThreadExecutionLease struct {
	mu   sync.Mutex
	file *os.File
}

// TryAcquireThreadExecutionLease attempts to become the sole executor of a
// durable thread across app-server instances and processes. A false acquired
// result means another live owner currently holds the lease.
func TryAcquireThreadExecutionLease(sessDir, threadID string) (*ThreadExecutionLease, bool, error) {
	sessDir = strings.TrimSpace(sessDir)
	threadID = strings.TrimSpace(threadID)
	if sessDir == "" {
		return nil, false, errors.New("session directory is required")
	}
	if threadID == "" {
		return nil, false, errors.New("thread id is required")
	}

	path := threadExecutionLeasePath(sessDir, threadID)
	file, err := securefs.OpenFile(path, os.O_CREATE|os.O_RDWR, securefs.FileMode)
	if err != nil {
		return nil, false, fmt.Errorf("open thread execution lease: %w", err)
	}
	acquired, err := tryLockThreadExecutionFile(file)
	if err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("lock thread execution lease: %w", err)
	}
	if !acquired {
		_ = file.Close()
		return nil, false, nil
	}
	return &ThreadExecutionLease{file: file}, true, nil
}

// Release relinquishes the lease. It is safe to call more than once.
func (l *ThreadExecutionLease) Release() error {
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
	unlockErr := unlockThreadExecutionFile(file)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}

func threadExecutionLeasePath(sessDir, threadID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(threadID)))
	return filepath.Join(sessDir, threadExecutionLeaseDirName, hex.EncodeToString(digest[:])+".lock")
}
