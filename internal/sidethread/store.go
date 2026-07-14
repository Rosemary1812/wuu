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

	"github.com/blueberrycongee/wuu/internal/securefs"
	"github.com/blueberrycongee/wuu/internal/storelock"
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
	release, err := s.lockStore()
	if err != nil {
		return err
	}
	defer release()
	path := s.pathFor(key)
	previousRevision := uint64(0)
	if data, readErr := os.ReadFile(path); readErr == nil {
		var previous SideThread
		if err := json.Unmarshal(data, &previous); err != nil {
			return fmt.Errorf("decode side thread %s: %w", key, err)
		}
		previousRevision = previous.Revision
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read side thread %s: %w", key, readErr)
	}
	now := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	st.UpdatedAt = now
	st.Revision = max(st.Revision, previousRevision) + 1
	if err := writeSideThread(path, st); err != nil {
		return err
	}
	return nil
}

// Mutate reads, applies fn, and writes back atomically. The persisted
// copy is only updated if fn returns no error. ErrNotFound is
// propagated when no side thread exists.
func (s *Store) Mutate(mainThreadID string, fn func(*SideThread) error) error {
	_, err := s.mutateRecord(mainThreadID, fn)
	return err
}

func (s *Store) mutateRecord(mainThreadID string, fn func(*SideThread) error) (*SideThread, error) {
	if s == nil || s.dir == "" {
		return nil, ErrNotFound
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return nil, ErrNotFound
	}
	release, err := s.lockStore()
	if err != nil {
		return nil, err
	}
	defer release()
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
	if fn != nil {
		if err := fn(&st); err != nil {
			return nil, err
		}
	}
	st.UpdatedAt = time.Now().UTC()
	st.Revision++
	if err := writeSideThread(path, &st); err != nil {
		return nil, err
	}
	return cloneSideThread(&st), nil
}

// BeginTurn atomically creates or updates the side thread and appends both the
// user message and its assistant placeholder. Callers must hold the durable
// side-thread execution lease before calling it. A persisted running state is
// therefore stale and is settled before the new turn begins.
func (s *Store) BeginTurn(mainThreadID, prompt, userMessageID, assistantMessageID string) (*SideThread, error) {
	return s.BeginTurnWithSideThreadID(mainThreadID, "", prompt, userMessageID, assistantMessageID)
}

// BeginTurnWithSideThreadID is BeginTurn with a caller-provided id for a new
// record. Existing records retain their durable id. This lets callers finish
// all model/context validation before committing the first message.
func (s *Store) BeginTurnWithSideThreadID(mainThreadID, sideThreadID, prompt, userMessageID, assistantMessageID string) (*SideThread, error) {
	if s == nil || s.dir == "" {
		return nil, errors.New("sidethread.Store: nil or unconfigured store")
	}
	key := normalizeKey(mainThreadID)
	if key == "" {
		return nil, errors.New("sidethread.Store: main_thread_id is required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("sidethread.Store: prompt is required")
	}
	if strings.TrimSpace(userMessageID) == "" || strings.TrimSpace(assistantMessageID) == "" {
		return nil, errors.New("sidethread.Store: message ids are required")
	}

	release, err := s.lockStore()
	if err != nil {
		return nil, err
	}
	defer release()

	path := s.pathFor(key)
	var st SideThread
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, fmt.Errorf("decode side thread %s: %w", key, err)
		}
	case errors.Is(err, os.ErrNotExist):
		sideThreadID = strings.TrimSpace(sideThreadID)
		if sideThreadID == "" {
			generatedID, idErr := NewSideThreadID()
			if idErr != nil {
				return nil, idErr
			}
			sideThreadID = generatedID
		}
		st = SideThread{SideThreadID: sideThreadID, MainThreadID: key}
	default:
		return nil, fmt.Errorf("read side thread %s: %w", key, err)
	}
	if strings.TrimSpace(st.SideThreadID) == "" {
		st.SideThreadID = strings.TrimSpace(sideThreadID)
		if st.SideThreadID == "" {
			return nil, errors.New("sidethread.Store: side_thread_id is required")
		}
	}

	if st.Status == StatusRunning {
		settleInterrupted(&st)
	}
	now := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	st.MainThreadID = key
	st.Status = StatusRunning
	st.UpdatedAt = now
	st.Revision++
	st.Messages = append(st.Messages,
		Message{
			ID:           userMessageID,
			SideThreadID: st.SideThreadID,
			Role:         RoleUser,
			Text:         prompt,
			CreatedAt:    now,
		},
		Message{
			ID:           assistantMessageID,
			SideThreadID: st.SideThreadID,
			Role:         RoleAssistant,
			Status:       AssistantStreaming,
			CreatedAt:    now,
		},
	)
	if err := writeSideThread(path, &st); err != nil {
		return nil, err
	}
	return cloneSideThread(&st), nil
}

// FinishTurn commits the terminal assistant message. An interrupt already
// persisted by another app-server wins over a late successful provider reply.
func (s *Store) FinishTurn(mainThreadID, assistantMessageID, text string, status Status, errorText string) (*SideThread, Message, error) {
	var finished Message
	st, err := s.mutateRecord(mainThreadID, func(st *SideThread) error {
		persistedInterrupt := st.Status == StatusInterrupted
		effectiveStatus := status
		if persistedInterrupt {
			effectiveStatus = StatusInterrupted
		}
		finalText := text
		finalErrorText := errorText
		if persistedInterrupt && status != StatusInterrupted {
			// A peer interrupted before this owner observed the durable state.
			// Discard the owner's late final payload; it was produced after the
			// user-visible interrupt boundary.
			finalText = ""
		}
		if effectiveStatus == StatusInterrupted {
			finalErrorText = ""
		}
		assistantStatus, err := assistantStatusForThreadStatus(effectiveStatus)
		if err != nil {
			return err
		}
		for i := range st.Messages {
			if st.Messages[i].ID != assistantMessageID || st.Messages[i].Role != RoleAssistant {
				continue
			}
			st.Messages[i].Text = finalText
			st.Messages[i].Status = assistantStatus
			st.Messages[i].ErrorText = finalErrorText
			finished = st.Messages[i]
			st.Status = effectiveStatus
			return nil
		}
		return fmt.Errorf("assistant message %q not found", assistantMessageID)
	})
	if err != nil {
		return nil, Message{}, err
	}
	return st, finished, nil
}

// Interrupt marks the active assistant placeholder and side thread terminal.
// It is safe to call from an app-server that does not own the provider request;
// FinishTurn preserves this terminal state when the owner eventually returns.
func (s *Store) Interrupt(mainThreadID string) (*SideThread, bool, error) {
	changed := false
	st, err := s.mutateRecord(mainThreadID, func(st *SideThread) error {
		if st.Status != StatusRunning {
			return nil
		}
		settleInterrupted(st)
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return st, changed, nil
}

// RecoverRunning settles records left behind by a process exit. Callers must
// run this only while owning workspace boot recovery, so a second live
// app-server cannot interrupt the first one's provider request.
func (s *Store) RecoverRunning() (int, error) {
	if s == nil || s.dir == "" {
		return 0, nil
	}
	release, err := s.lockStore()
	if err != nil {
		return 0, err
	}
	defer release()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read side thread dir: %w", err)
	}
	var (
		recovered int
		errs      []error
	)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			errs = append(errs, fmt.Errorf("read side thread %s: %w", entry.Name(), readErr))
			continue
		}
		var st SideThread
		if decodeErr := json.Unmarshal(data, &st); decodeErr != nil {
			errs = append(errs, fmt.Errorf("decode side thread %s: %w", entry.Name(), decodeErr))
			continue
		}
		if st.Status != StatusRunning {
			continue
		}
		settleInterrupted(&st)
		st.UpdatedAt = time.Now().UTC()
		st.Revision++
		if writeErr := writeSideThread(path, &st); writeErr != nil {
			errs = append(errs, writeErr)
			continue
		}
		recovered++
	}
	return recovered, errors.Join(errs...)
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
	release, err := s.lockStore()
	if err != nil {
		return err
	}
	defer release()
	err = os.Remove(s.pathFor(key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete side thread %s: %w", key, err)
	}
	return nil
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

// NewMessageID returns a durable side-thread message id.
func NewMessageID() (string, error) {
	id, err := NewSideThreadID()
	if err != nil {
		return "", err
	}
	return "sm_" + id, nil
}

func settleInterrupted(st *SideThread) {
	if st == nil {
		return
	}
	st.Status = StatusInterrupted
	for i := range st.Messages {
		if st.Messages[i].Role == RoleAssistant && st.Messages[i].Status == AssistantStreaming {
			st.Messages[i].Status = AssistantInterrupted
		}
	}
}

func assistantStatusForThreadStatus(status Status) (AssistantMessageStatus, error) {
	switch status {
	case StatusCompleted:
		return AssistantCompleted, nil
	case StatusFailed:
		return AssistantFailed, nil
	case StatusInterrupted:
		return AssistantInterrupted, nil
	default:
		return "", fmt.Errorf("side thread status %q is not terminal", status)
	}
}

func cloneSideThread(st *SideThread) *SideThread {
	if st == nil {
		return nil
	}
	cloned := *st
	cloned.Messages = append([]Message(nil), st.Messages...)
	return &cloned
}

func (s *Store) lockStore() (func(), error) {
	s.mu.Lock()
	durable, err := storelock.Acquire(s.dir)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("acquire side thread store lock: %w", err)
	}
	return func() {
		_ = durable.Release()
		s.mu.Unlock()
	}, nil
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

func writeSideThread(path string, st *SideThread) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode side thread: %w", err)
	}
	data = append(data, '\n')
	if err := securefs.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("write side thread: %w", err)
	}
	return nil
}
