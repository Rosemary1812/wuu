package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

const (
	appServerStartupGateFile = ".appserver-startup.lock"
	appServerPresenceFile    = ".appserver-presence.lock"
)

// AppServerPresenceLease holds a shared presence lock for one live app-server.
// Startup is serialized by a separate gate so the first process can settle
// crash leftovers under an exclusive presence lock before any peer becomes
// visible.
type AppServerPresenceLease struct {
	mu        sync.Mutex
	gate      *os.File
	presence  *os.File
	first     bool
	finalized bool
}

// AcquireAppServerPresence enters the serialized startup section. first is
// true only when no finalized app-server currently holds shared presence. The
// caller must perform first-process recovery, then call FinalizeStartup before
// serving requests.
func AcquireAppServerPresence(sessDir string) (*AppServerPresenceLease, bool, error) {
	sessDir = strings.TrimSpace(sessDir)
	if sessDir == "" {
		return nil, false, errors.New("session directory is required")
	}
	gate, err := securefs.OpenFile(filepath.Join(sessDir, appServerStartupGateFile), os.O_CREATE|os.O_RDWR, securefs.FileMode)
	if err != nil {
		return nil, false, fmt.Errorf("open app-server startup gate: %w", err)
	}
	if err := lockAppServerStartupGate(gate); err != nil {
		_ = gate.Close()
		return nil, false, fmt.Errorf("lock app-server startup gate: %w", err)
	}
	presence, err := securefs.OpenFile(filepath.Join(sessDir, appServerPresenceFile), os.O_CREATE|os.O_RDWR, securefs.FileMode)
	if err != nil {
		_ = unlockAppServerPresenceFile(gate)
		_ = gate.Close()
		return nil, false, fmt.Errorf("open app-server presence: %w", err)
	}
	first, err := tryLockAppServerPresenceExclusive(presence)
	if err != nil {
		_ = presence.Close()
		_ = unlockAppServerPresenceFile(gate)
		_ = gate.Close()
		return nil, false, fmt.Errorf("inspect app-server presence: %w", err)
	}
	if !first {
		if err := lockAppServerPresenceShared(presence); err != nil {
			_ = presence.Close()
			_ = unlockAppServerPresenceFile(gate)
			_ = gate.Close()
			return nil, false, fmt.Errorf("join app-server presence: %w", err)
		}
	}
	return &AppServerPresenceLease{gate: gate, presence: presence, first: first}, first, nil
}

// FinalizeStartup converts the first process's exclusive lock to shared
// presence and releases the startup gate. The gate makes the unlock/relock
// conversion safe on platforms without an atomic lock downgrade.
func (l *AppServerPresenceLease) FinalizeStartup() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.finalized {
		return nil
	}
	if l.presence == nil || l.gate == nil {
		return errors.New("app-server presence startup is not active")
	}
	if l.first {
		if err := unlockAppServerPresenceFile(l.presence); err != nil {
			return fmt.Errorf("downgrade app-server presence: %w", err)
		}
		if err := lockAppServerPresenceShared(l.presence); err != nil {
			return fmt.Errorf("acquire shared app-server presence: %w", err)
		}
	}
	gate := l.gate
	l.gate = nil
	if err := unlockAppServerPresenceFile(gate); err != nil {
		_ = gate.Close()
		return fmt.Errorf("release app-server startup gate: %w", err)
	}
	if err := gate.Close(); err != nil {
		return fmt.Errorf("close app-server startup gate: %w", err)
	}
	l.finalized = true
	l.first = false
	return nil
}

// Release leaves live presence. It is safe to call more than once and also
// cleans up a startup lease whose FinalizeStartup failed.
func (l *AppServerPresenceLease) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var releaseErr error
	if l.presence != nil {
		presence := l.presence
		l.presence = nil
		releaseErr = errors.Join(releaseErr, unlockAppServerPresenceFile(presence), presence.Close())
	}
	if l.gate != nil {
		gate := l.gate
		l.gate = nil
		releaseErr = errors.Join(releaseErr, unlockAppServerPresenceFile(gate), gate.Close())
	}
	return releaseErr
}
