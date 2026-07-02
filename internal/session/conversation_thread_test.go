package session

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConversationThreadCRUD(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	first, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    " thread-1 ",
		AnchorItemID: " item-1 ",
		Title:        " Review auth flow ",
		CreatedBy:    " prt-reviewer ",
		CreatedAt:    time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || !strings.HasPrefix(first.ID, "cth-") {
		t.Fatalf("expected generated cth id, got %+v", first)
	}
	if first.SessionID != "thread-1" || first.AnchorItemID != "item-1" || first.Title != "Review auth flow" || first.CreatedBy != "prt-reviewer" {
		t.Fatalf("thread fields were not normalized: %+v", first)
	}
	if first.Status != ConversationThreadOpen {
		t.Fatalf("default status = %q, want open", first.Status)
	}

	second, err := CreateConversationThread(dir, ConversationThread{
		ID:           "cth-custom",
		SessionID:    "thread-1",
		AnchorItemID: "item-2",
		Status:       ConversationThreadResolved,
		CreatedAt:    time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "cth-custom" || second.Status != ConversationThreadResolved {
		t.Fatalf("custom thread not persisted: %+v", second)
	}

	if err := UpdateConversationThreadStatus(dir, first.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	list, err := ListConversationThreads(dir, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversation threads, got %+v", list)
	}
	if list[0].ID != first.ID || list[0].Status != ConversationThreadResolved || list[1].ID != second.ID {
		t.Fatalf("unexpected listed threads: %+v", list)
	}
}

func TestCreateConversationThreadRequiresExistingSession(t *testing.T) {
	dir := t.TempDir()
	_, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "missing",
		AnchorItemID: "item-1",
	})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CreateConversationThread() error = %v, want ErrSessionNotFound", err)
	}
}

func TestConversationThreadRejectsInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	_, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "item-1",
		Status:       "paused",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
}

func TestDeleteSessionRemovesConversationThreads(t *testing.T) {
	dir := t.TempDir()
	sess, err := CreateWithMetadata(dir, "thread-1", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    sess.ID,
		AnchorItemID: "item-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Delete(dir, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ListConversationThreads(dir, sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListConversationThreads() after delete = %v, want ErrSessionNotFound", err)
	}
}
