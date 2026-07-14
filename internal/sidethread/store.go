package sidethread

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned by Load / Mutate when no side thread exists
// for the requested main thread id. Appserver translates this to
// summary==null on the IPC boundary.
var ErrNotFound = errors.New("side thread not found")

// Store persists side threads keyed by main thread id. One file per
// main thread under <dir>/<main_thread_id>.json. All operations are
// concurrency-safe; writes are atomic (tmp file + rename) to survive
// crashes mid-write.
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore returns a Store rooted at dir. The directory is created
// lazily on the first write, so Load / Exists can probe an absent side
// thread without leaving artifacts behind.
func NewStore(dir string) *Store {
	return &Store{dir: strings.TrimSpace(dir)}
}

// Dir returns the configured storage directory.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Load returns the side thread bound to mainThreadID, or ErrNotFound if
// none exists yet.
func (s *Store) Load(mainThreadID string) (*SideThread, error) {
	if s == nil || s.dir == "" {
		return nil, ErrNotFound
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return nil, ErrNotFound
	}
	path := s.pathFor(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read side thread %s: %w", key, err)
	}
	var st SideThread
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode side thread %s: %w", key, err)
	}
	return &st, nil
}

// Exists reports whether a side thread file is on disk for mainThreadID.
func (s *Store) Exists(mainThreadID string) (bool, error) {
	if s == nil || s.dir == "" {
		return false, nil
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return false, nil
	}
	_, err := os.Stat(s.pathFor(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Save persists st atomically. The directory is created on demand and
// CreatedAt / UpdatedAt are filled in when missing.
func (s *Store) Save(st *SideThread) error {
	if s == nil || s.dir == "" {
		return errors.New("sidethread.Store: nil or unconfigured store")
	}
	if st == nil {
		return errors.New("sidethread.Store: nil side thread")
	}
	key := normalizeKey(st.MainThreadID)
	if key == "" {
		return errors.New("sidethread.Store: main_thread_id is required")
	}
	now := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	st.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create side thread dir: %w", err)
	}
	if err := writeAtomic(s.pathFor(key), st); err != nil {
		return err
	}
	return nil
}

// Mutate reads, applies fn, and writes back atomically. The persisted
// copy is only updated if fn returns no error. ErrNotFound is
// propagated when no side thread exists.
func (s *Store) Mutate(mainThreadID string, fn func(*SideThread) error) error {
	if s == nil || s.dir == "" {
		return ErrNotFound
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.pathFor(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("read side thread %s: %w", key, err)
	}
	var st SideThread
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("decode side thread %s: %w", key, err)
	}
	if fn != nil {
		if err := fn(&st); err != nil {
			return err
		}
	}
	st.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create side thread dir: %w", err)
	}
	if err := writeAtomic(path, &st); err != nil {
		return err
	}
	return nil
}

// Delete removes the side thread file for mainThreadID. It is a no-op
// if no file exists so callers can invoke it on main-thread deletion
// unconditionally.
func (s *Store) Delete(mainThreadID string) error {
	if s == nil || s.dir == "" {
		return nil
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.pathFor(key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete side thread %s: %w", key, err)
	}
	return nil
}

// MarkDetached sets status to StatusDetached for mainThreadID. It is a
// no-op if no side thread exists.
func (s *Store) MarkDetached(mainThreadID string) error {
	return s.Mutate(mainThreadID, func(st *SideThread) error {
		st.Status = StatusDetached
		return nil
	})
}

// NewSideThreadID generates a fresh hex-encoded side thread id (16 hex
// chars / 8 random bytes). Enough entropy to make collisions negligible
// across the lifetime of a single session dir while staying short
// enough for human-readable logs.
func NewSideThreadID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate side thread id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func (s *Store) pathFor(mainThreadID string) string {
	return filepath.Join(s.dir, mainThreadID+".json")
}

func normalizeKey(mainThreadID string) string {
	trimmed := strings.TrimSpace(mainThreadID)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\`) {
		return ""
	}
	return trimmed
}

func writeAtomic(path string, st *SideThread) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode side thread: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sidethread.*.tmp")
	if err != nil {
		return fmt.Errorf("create side thread tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write side thread tmp: %w", err)
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write side thread newline: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close side thread tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename side thread file: %w", err)
	}
	return nil
}
