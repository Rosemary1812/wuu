package sidethread

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(filepath.Join(dir, "sidethreads"))
}

func sampleSideThread(mainID string) *SideThread {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return &SideThread{
		SideThreadID: "st_abc123",
		MainThreadID: mainID,
		Status:       StatusIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
		Messages: []Message{
			{
				ID:           "m1",
				SideThreadID: "st_abc123",
				Role:         RoleUser,
				Text:         "进度如何?",
				CreatedAt:    now,
			},
		},
	}
}

func TestStoreRoundtrip(t *testing.T) {
	s := newTempStore(t)
	st := sampleSideThread("main_1")
	if err := s.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load("main_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SideThreadID != st.SideThreadID {
		t.Fatalf("SideThreadID mismatch: got %q want %q", loaded.SideThreadID, st.SideThreadID)
	}
	if loaded.MainThreadID != st.MainThreadID {
		t.Fatalf("MainThreadID mismatch: got %q want %q", loaded.MainThreadID, st.MainThreadID)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Text != "进度如何?" {
		t.Fatalf("messages roundtrip mismatch: %+v", loaded.Messages)
	}
	if got := loaded.Summary(); got.SideThreadID != st.SideThreadID {
		t.Fatalf("Summary().SideThreadID=%q", got.SideThreadID)
	}
}

func TestStoreLoadMissing(t *testing.T) {
	s := newTempStore(t)
	_, err := s.Load("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreExists(t *testing.T) {
	s := newTempStore(t)
	exists, err := s.Exists("main_1")
	if err != nil || exists {
		t.Fatalf("Exists before save: exists=%v err=%v", exists, err)
	}
	if err := s.Save(sampleSideThread("main_1")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	exists, err = s.Exists("main_1")
	if err != nil || !exists {
		t.Fatalf("Exists after save: exists=%v err=%v", exists, err)
	}
}

func TestStoreDelete(t *testing.T) {
	s := newTempStore(t)
	if err := s.Save(sampleSideThread("main_1")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("main_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load("main_1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete expected ErrNotFound, got %v", err)
	}
	// Delete of absent is a no-op.
	if err := s.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestStoreMutate(t *testing.T) {
	s := newTempStore(t)
	if err := s.Save(sampleSideThread("main_1")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	err := s.Mutate("main_1", func(st *SideThread) error {
		st.Status = StatusRunning
		st.Messages = append(st.Messages, Message{
			ID:           "m2",
			SideThreadID: st.SideThreadID,
			Role:         RoleAssistant,
			Text:         "正在分析...",
			Status:       AssistantStreaming,
			CreatedAt:    time.Now().UTC(),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	loaded, err := s.Load("main_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Status != StatusRunning {
		t.Fatalf("status not updated: got %q", loaded.Status)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Messages))
	}
}

func TestStoreMutateMissing(t *testing.T) {
	s := newTempStore(t)
	err := s.Mutate("ghost", func(*SideThread) error { return nil })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreRejectsBadKey(t *testing.T) {
	s := newTempStore(t)
	if err := s.Save(&SideThread{MainThreadID: "../escape"}); err == nil {
		t.Fatal("expected error for main_thread_id with path traversal, got nil")
	}
	if _, err := s.Load(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty main thread id should report ErrNotFound, got %v", err)
	}
}

func TestStoreSavePopulatesTimestamps(t *testing.T) {
	s := newTempStore(t)
	st := &SideThread{
		MainThreadID: "main_timestamps",
		Status:       StatusIdle,
	}
	if err := s.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := s.Load("main_timestamps")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CreatedAt.IsZero() || loaded.UpdatedAt.IsZero() {
		t.Fatalf("expected non-zero timestamps, got %+v", loaded)
	}
}

func TestStoreConcurrentSaves(t *testing.T) {
	s := newTempStore(t)
	const writers = 8
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			st := sampleSideThread("main_concurrent")
			st.Status = StatusRunning
			if err := s.Save(st); err != nil {
				t.Errorf("concurrent Save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if _, err := s.Load("main_concurrent"); err != nil {
		t.Fatalf("Load after concurrent writes: %v", err)
	}
}

func TestStoreMarkDetached(t *testing.T) {
	s := newTempStore(t)
	if err := s.Save(sampleSideThread("main_1")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.MarkDetached("main_1"); err != nil {
		t.Fatalf("MarkDetached: %v", err)
	}
	loaded, err := s.Load("main_1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Status != StatusDetached {
		t.Fatalf("status=%q want %q", loaded.Status, StatusDetached)
	}
}

func TestNewSideThreadID(t *testing.T) {
	a, err := NewSideThreadID()
	if err != nil {
		t.Fatalf("NewSideThreadID: %v", err)
	}
	b, err := NewSideThreadID()
	if err != nil {
		t.Fatalf("NewSideThreadID: %v", err)
	}
	if a == b {
		t.Fatalf("two consecutive ids collided: %q == %q", a, b)
	}
	if len(a) != 16 || len(b) != 16 {
		t.Fatalf("expected 16-char ids, got %q and %q", a, b)
	}
}
