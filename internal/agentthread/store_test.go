package agentthread

import (
	"testing"
	"time"
)

func TestStorePersistsThreadIndexAndEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	meta := Metadata{
		ID:        "worker-1",
		Path:      "/root/check-tests",
		TaskName:  "check tests",
		Status:    StatusRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Source:    Source{Kind: SourceSpawn, ParentPath: RootPath, Depth: 2},
	}
	if err := store.UpsertThread(meta); err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}
	meta.Status = StatusCompleted
	if err := store.RecordStatus(meta); err != nil {
		t.Fatalf("RecordStatus: %v", err)
	}

	threads, err := store.ListThreads()
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].Status != StatusCompleted {
		t.Fatalf("unexpected thread index: %+v", threads)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != EventThreadUpsert || events[1].Type != EventStatusChange {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
}
