package sidethread

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		Status:       StatusCompleted,
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
	if loaded.Revision == 0 {
		t.Fatal("persisted record is missing a revision")
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
		Status:       StatusCompleted,
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

func TestStoreWritesPrivateFilesAtomically(t *testing.T) {
	s := newTempStore(t)
	initial := sampleSideThread("main_atomic")
	initial.Messages[0].Text = strings.Repeat("a", 64*1024)
	if err := s.Save(initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	writer := NewStore(s.Dir())
	reader := NewStore(s.Dir())
	done := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			st := sampleSideThread("main_atomic")
			st.Messages[0].Text = strings.Repeat(string(rune('a'+i%2)), 64*1024)
			if err := writer.Save(st); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

readLoop:
	for {
		loaded, err := reader.Load("main_atomic")
		if err != nil {
			t.Fatalf("concurrent Load observed an incomplete write: %v", err)
		}
		if len(loaded.Messages) != 1 || len(loaded.Messages[0].Text) != 64*1024 {
			t.Fatalf("concurrent Load observed a partial record: messages=%d text_bytes=%d", len(loaded.Messages), len(loaded.Messages[0].Text))
		}
		select {
		case <-done:
			if err := <-writeErr; err != nil {
				t.Fatalf("concurrent Save: %v", err)
			}
			break readLoop
		default:
		}
	}

	if runtime.GOOS == "windows" {
		return
	}
	dirInfo, err := os.Stat(s.Dir())
	if err != nil {
		t.Fatalf("stat store directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("store directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(filepath.Join(s.Dir(), "main_atomic.json"))
	if err != nil {
		t.Fatalf("stat side thread file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("side thread file mode = %o, want 600", got)
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

func TestStoreTerminalOrderingAcrossInstances(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sidethreads")
	owner := NewStore(dir)
	peer := NewStore(dir)

	t.Run("interrupt before finish wins and rejects late final text", func(t *testing.T) {
		started, err := owner.BeginTurn("main_interrupt_first", "status?", "user-1", "assistant-1")
		if err != nil {
			t.Fatalf("BeginTurn: %v", err)
		}
		interrupted, changed, err := peer.Interrupt("main_interrupt_first")
		if err != nil || !changed {
			t.Fatalf("Interrupt: changed=%t err=%v", changed, err)
		}
		st, message, err := owner.FinishTurn("main_interrupt_first", "assistant-1", "late provider text", StatusCompleted, "late error")
		if err != nil {
			t.Fatalf("FinishTurn: %v", err)
		}
		if st.Status != StatusInterrupted || message.Status != AssistantInterrupted {
			t.Fatalf("interrupt must win: thread=%q message=%q", st.Status, message.Status)
		}
		if message.Text != "" || message.ErrorText != "" {
			t.Fatalf("late provider payload crossed interrupt boundary: %+v", message)
		}
		if !(started.Revision < interrupted.Revision && interrupted.Revision < st.Revision) {
			t.Fatalf("revisions are not monotonic: start=%d interrupt=%d finish=%d", started.Revision, interrupted.Revision, st.Revision)
		}
	})

	t.Run("finish before interrupt stays completed", func(t *testing.T) {
		if _, err := owner.BeginTurn("main_finish_first", "status?", "user-2", "assistant-2"); err != nil {
			t.Fatalf("BeginTurn: %v", err)
		}
		if _, _, err := owner.FinishTurn("main_finish_first", "assistant-2", "done", StatusCompleted, ""); err != nil {
			t.Fatalf("FinishTurn: %v", err)
		}
		st, changed, err := peer.Interrupt("main_finish_first")
		if err != nil {
			t.Fatalf("Interrupt: %v", err)
		}
		if changed || st.Status != StatusCompleted {
			t.Fatalf("late interrupt changed terminal result: changed=%t status=%q", changed, st.Status)
		}
	})
}

func TestStoreConcurrentMutationsAcrossInstancesDoNotLoseUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sidethreads")
	first := NewStore(dir)
	second := NewStore(dir)
	if err := first.Save(sampleSideThread("main_cross_instance")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for index, store := range []*Store{first, second} {
		index, store := index, store
		go func() {
			<-start
			errs <- store.Mutate("main_cross_instance", func(st *SideThread) error {
				st.Messages = append(st.Messages, Message{ID: fmt.Sprintf("peer-%d", index), SideThreadID: st.SideThreadID, Role: RoleUser, Text: "peer"})
				return nil
			})
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Mutate: %v", err)
		}
	}
	loaded, err := first.Load("main_cross_instance")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("cross-instance mutation lost an update: %+v", loaded.Messages)
	}
}

func TestStoreDeletePreventsLateFinishFromRecreatingRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sidethreads")
	owner := NewStore(dir)
	deleter := NewStore(dir)
	if _, err := owner.BeginTurn("main_delete", "status?", "user", "assistant"); err != nil {
		t.Fatalf("BeginTurn: %v", err)
	}
	if err := deleter.Delete("main_delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := owner.FinishTurn("main_delete", "assistant", "late", StatusCompleted, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late FinishTurn error = %v, want ErrNotFound", err)
	}
	if exists, err := owner.Exists("main_delete"); err != nil || exists {
		t.Fatalf("deleted record was recreated: exists=%t err=%v", exists, err)
	}
}

func TestStoreRecoverRunningMarksPlaceholderInterrupted(t *testing.T) {
	store := newTempStore(t)
	if _, err := store.BeginTurn("main_recover", "status?", "user", "assistant"); err != nil {
		t.Fatalf("BeginTurn: %v", err)
	}
	recovered, err := store.RecoverRunning()
	if err != nil {
		t.Fatalf("RecoverRunning: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1", recovered)
	}
	st, err := store.Load("main_recover")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Status != StatusInterrupted || st.Messages[len(st.Messages)-1].Status != AssistantInterrupted {
		t.Fatalf("running record was not settled: %+v", st)
	}
}
