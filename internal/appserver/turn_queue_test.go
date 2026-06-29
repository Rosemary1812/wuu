package appserver

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestReplaceQueuedUserTurnPreservesOrder(t *testing.T) {
	const threadID = "thread-1"
	s := &Server{}
	s.enqueueQueuedUserTurn(threadID, queuedTurn{
		id:  "queue-1",
		msg: providers.ChatMessage{Role: "user", Content: "first"},
	})
	s.enqueueQueuedUserTurn(threadID, queuedTurn{
		id:  "queue-2",
		msg: providers.ChatMessage{Role: "user", Content: "second"},
	})
	s.enqueueQueuedUserTurn(threadID, queuedTurn{
		id:  "queue-3",
		msg: providers.ChatMessage{Role: "user", Content: "third"},
	})

	updated, ok := s.replaceQueuedUserTurn(threadID, "queue-2", providers.ChatMessage{
		Role:    "user",
		Content: "second edited",
	})
	if !ok {
		t.Fatal("replaceQueuedUserTurn returned false")
	}
	if updated.id != "queue-2" || updated.msg.ClientID != "queue-2" {
		t.Fatalf("replacement lost queue id: %+v", updated)
	}
	if updated.msg.Steered {
		t.Fatalf("queued replacement should not be marked steered: %+v", updated.msg)
	}

	var got []string
	for {
		entry, ok := s.takeNextQueuedUserTurn(threadID)
		if !ok {
			break
		}
		got = append(got, entry.id+":"+entry.msg.Content)
	}
	want := []string{
		"queue-1:first",
		"queue-2:second edited",
		"queue-3:third",
	}
	if len(got) != len(want) {
		t.Fatalf("drained %d queued turns, want %d: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("drain order mismatch at %d: got %q want %q (all got %v)", index, got[index], want[index], got)
		}
	}
}

func TestReplaceQueuedUserTurnReturnsFalseWhenMissing(t *testing.T) {
	s := &Server{}
	s.enqueueQueuedUserTurn("thread-1", queuedTurn{
		id:  "queue-1",
		msg: providers.ChatMessage{Role: "user", Content: "first"},
	})

	if _, ok := s.replaceQueuedUserTurn("thread-1", "missing", providers.ChatMessage{
		Role:    "user",
		Content: "edited",
	}); ok {
		t.Fatal("replaceQueuedUserTurn returned true for a missing queue id")
	}

	entry, ok := s.takeNextQueuedUserTurn("thread-1")
	if !ok {
		t.Fatal("queued turn was removed by failed replace")
	}
	if entry.id != "queue-1" || entry.msg.Content != "first" {
		t.Fatalf("failed replace mutated queue: %+v", entry)
	}
}
