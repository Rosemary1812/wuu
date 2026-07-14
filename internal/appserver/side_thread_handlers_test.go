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
