package appserver

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/sidethread"
)

func newSideThreadServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s := &Server{
		rt:      &runtime.Session{SessionDir: dir},
		threads: make(map[string]*threadState),
	}
	s.sideThreadStore = sidethread.NewStore(filepath.Join(dir, "sidethreads"))
	return s
}

func seedSideThread(t *testing.T, s *Server, mainID string) *sidethread.SideThread {
	t.Helper()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	st := &sidethread.SideThread{
		SideThreadID: "st_seeded",
		MainThreadID: mainID,
		Status:       sidethread.StatusIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
		Messages: []sidethread.Message{
			{ID: "m1", SideThreadID: "st_seeded", Role: sidethread.RoleUser, Text: "进度?", CreatedAt: now},
		},
	}
	if err := s.sideThreadStore.Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return st
}

func TestOpenSideThreadLazyReturnsNilSummary(t *testing.T) {
	s := newSideThreadServer(t)
	res, err := s.openSideThread("main_lazy")
	if err != nil {
		t.Fatalf("openSideThread: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.Summary != nil {
		t.Fatalf("expected nil summary, got %+v", res.Summary)
	}
}

func TestOpenSideThreadSeededSummary(t *testing.T) {
	s := newSideThreadServer(t)
	seedSideThread(t, s, "main_seed")
	res, err := s.openSideThread("main_seed")
	if err != nil {
		t.Fatalf("openSideThread: %v", err)
	}
	if res.Summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if res.Summary.SideThreadID != "st_seeded" {
		t.Fatalf("side_thread_id mismatch: %q", res.Summary.SideThreadID)
	}
	if res.Summary.Status != string(sidethread.StatusIdle) {
		t.Fatalf("status mismatch: %q", res.Summary.Status)
	}
}

func TestOpenSideThreadNoStore(t *testing.T) {
	s := &Server{}
	res, err := s.openSideThread("main_idk")
	if err != nil {
		t.Fatalf("openSideThread: %v", err)
	}
	if res.Summary != nil {
		t.Fatalf("expected nil summary, got %+v", res.Summary)
	}
}

func TestOpenSideThreadInvalidMainID(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.openSideThread("   "); err == nil {
		t.Fatal("expected error for empty main_thread_id")
	}
}

func TestGetSideThreadHistoryHits(t *testing.T) {
	s := newSideThreadServer(t)
	seedSideThread(t, s, "main_hist")
	res, err := s.getSideThreadHistory("main_hist")
	if err != nil {
		t.Fatalf("getSideThreadHistory: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Text != "进度?" {
		t.Fatalf("messages mismatch: %+v", res.Messages)
	}
	if res.Summary.SideThreadID != "st_seeded" {
		t.Fatalf("summary mismatch: %+v", res.Summary)
	}
}

func TestGetSideThreadHistoryMissing(t *testing.T) {
	s := newSideThreadServer(t)
	_, err := s.getSideThreadHistory("ghost")
	if !errors.Is(err, sidethread.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetSideThreadHistoryNoStore(t *testing.T) {
	s := &Server{}
	_, err := s.getSideThreadHistory("anything")
	if !errors.Is(err, sidethread.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetSideThreadHistoryInvalidMainID(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.getSideThreadHistory(" "); err == nil {
		t.Fatal("expected error for empty main_thread_id")
	}
}

// Validation-path coverage lives in TestOpenSideThreadInvalidMainID
// and TestGetSideThreadHistoryInvalidMainID above, which exercise the
// inner helpers. The handleXxx variants would need a working
// writeResponse plumbing (non-nil s.out) that adds nothing the unit
// tests above don't already prove; the full JSON-RPC dispatch path
// is exercised end-to-end in server_test.go once the renderer side
// lands.

func TestSendSideThreadMessageLazyCreate(t *testing.T) {
	s := newSideThreadServer(t)
	res, err := s.sendSideThreadMessage("main_lazy", "进度如何?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.UserMessageID == "" {
		t.Fatal("expected non-empty user_message_id")
	}
	if res.Summary.SideThreadID == "" {
		t.Fatal("expected side_thread_id to be assigned on first send")
	}
}

func TestSendSideThreadMessageAppendsUserMsg(t *testing.T) {
	s := newSideThreadServer(t)
	res, err := s.sendSideThreadMessage("main_first", "你好")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	st, err := s.sideThreadStore.Load("main_first")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(st.Messages))
	}
	if st.Messages[0].Text != "你好" {
		t.Fatalf("message text mismatch: %q", st.Messages[0].Text)
	}
	if st.Status != sidethread.StatusRunning {
		t.Fatalf("status mismatch: %q", st.Status)
	}
	if res.Summary.SideThreadID != st.SideThreadID {
		t.Fatalf("side_thread_id mismatch: wire %q vs store %q", res.Summary.SideThreadID, st.SideThreadID)
	}
}

func TestSendSideThreadMessageMultipleTurns(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_multi", "first"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if _, err := s.sendSideThreadMessage("main_multi", "second"); err != nil {
		t.Fatalf("second send: %v", err)
	}
	st, err := s.sideThreadStore.Load("main_multi")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(st.Messages))
	}
	if st.Messages[1].Text != "second" {
		t.Fatalf("second message text mismatch: %q", st.Messages[1].Text)
	}
}

func TestSendSideThreadMessageRejectsEmptyPrompt(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_e", "   "); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestSendSideThreadMessageRejectsEmptyMainID(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("  ", "prompt"); err == nil {
		t.Fatal("expected error for empty main_id")
	}
}

func TestSendSideThreadMessageNoStore(t *testing.T) {
	s := &Server{}
	if _, err := s.sendSideThreadMessage("main", "prompt"); err == nil {
		t.Fatal("expected error when store absent")
	}
}

func TestInterruptSideThreadFlipsStatus(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_int", "first"); err != nil {
		t.Fatalf("send: %v", err)
	}
	res, err := s.interruptSideThread("main_int")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if !res.Ok {
		t.Fatal("expected ok=true")
	}
	st, err := s.sideThreadStore.Load("main_int")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if st.Status != sidethread.StatusInterrupted {
		t.Fatalf("status mismatch: got %q want %q", st.Status, sidethread.StatusInterrupted)
	}
}

func TestInterruptSideThreadNoRecord(t *testing.T) {
	s := newSideThreadServer(t)
	res, err := s.interruptSideThread("ghost")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if res.Ok {
		t.Fatal("expected ok=false for missing side thread")
	}
}

func TestInterruptSideThreadNoStore(t *testing.T) {
	s := &Server{}
	res, err := s.interruptSideThread("anything")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if res.Ok {
		t.Fatal("expected ok=false when store absent")
	}
}

func TestInterruptSideThreadNoOpWhenIdle(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_idle", "x"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Manually flip to idle to simulate "turn finished before interrupt".
	if err := s.sideThreadStore.Mutate("main_idle", func(st *sidethread.SideThread) error {
		st.Status = sidethread.StatusCompleted
		return nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	res, err := s.interruptSideThread("main_idle")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if !res.Ok {
		t.Fatal("expected ok=true (no-op but reported ok)")
	}
	st, _ := s.sideThreadStore.Load("main_idle")
	if st.Status != sidethread.StatusCompleted {
		t.Fatalf("status must stay completed; got %q", st.Status)
	}
}

func TestCascadeSideThreadForMainRemovesFile(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_cascade", "seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	exists, err := s.sideThreadStore.Exists("main_cascade")
	if err != nil || !exists {
		t.Fatalf("pre-cascade exists=%v err=%v", exists, err)
	}
	s.cascadeSideThreadForMain("main_cascade")
	exists, err = s.sideThreadStore.Exists("main_cascade")
	if err != nil {
		t.Fatalf("post-cascade exists: %v", err)
	}
	if exists {
		t.Fatal("side thread file must be removed after cascade")
	}
}

func TestCascadeSideThreadForMainNoRecordIsNoOp(t *testing.T) {
	s := newSideThreadServer(t)
	// No side thread on disk for "ghost"; the cascade must not panic.
	s.cascadeSideThreadForMain("ghost")
}

func TestCascadeSideThreadForMainNilStore(t *testing.T) {
	s := &Server{}
	// Nil store / nil server must be safe.
	s.cascadeSideThreadForMain("anything")
}

func TestCascadeSideThreadForMainEmptyID(t *testing.T) {
	s := newSideThreadServer(t)
	if _, err := s.sendSideThreadMessage("main_empty_check", "seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Empty / whitespace ids are filtered.
	s.cascadeSideThreadForMain("   ")
	s.cascadeSideThreadForMain("")
	// The real record must still exist.
	exists, err := s.sideThreadStore.Exists("main_empty_check")
	if err != nil || !exists {
		t.Fatalf("post-empty-cascade exists=%v err=%v", exists, err)
	}
}

func TestMainTaskSnapshotRunningFalse(t *testing.T) {
	s := newSideThreadServer(t)
	snap := s.mainTaskSnapshot("not_in_threads_map")
	if snap == nil {
		t.Fatal("expected non-nil snapshot (no record means running=false)")
	}
	if snap.Running {
		t.Fatalf("running should be false for unknown thread; got true")
	}
}
