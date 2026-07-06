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

func TestFindConversationThreadByID(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-7",
		Title:        "Discuss retry",
		CreatedBy:    "prt-author",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := FindConversationThreadByID(dir, " "+created.ID+" ")
	if err != nil {
		t.Fatalf("FindConversationThreadByID() error = %v", err)
	}
	if got.ID != created.ID || got.SessionID != "thread-1" || got.AnchorItemID != "seq-7" {
		t.Fatalf("unexpected thread: %+v", got)
	}
	if got.Title != "Discuss retry" || got.CreatedBy != "prt-author" || got.Status != ConversationThreadOpen {
		t.Fatalf("thread fields not loaded: %+v", got)
	}

	if _, err := FindConversationThreadByID(dir, "cth-missing"); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("FindConversationThreadByID(missing) error = %v, want ErrConversationThreadNotFound", err)
	}
	if _, err := FindConversationThreadByID(dir, "   "); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("FindConversationThreadByID(blank) error = %v, want ErrConversationThreadNotFound", err)
	}
}

func TestEscalateConversationThread(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-3",
		Title:        "discuss",
		CreatedBy:    "prt-opener",
	})
	if err != nil {
		t.Fatal(err)
	}

	escalated, err := EscalateConversationThread(dir, created.ID, " human ", " prt-lead ", " Ship the fix ")
	if err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}
	if escalated.Status != ConversationThreadTask {
		t.Fatalf("status = %q, want task", escalated.Status)
	}
	if escalated.EscalatedBy != "human" {
		t.Fatalf("escalated_by = %q, want human", escalated.EscalatedBy)
	}
	if escalated.LeadParticipantID != "prt-lead" {
		t.Fatalf("lead_participant_id = %q, want prt-lead", escalated.LeadParticipantID)
	}
	if escalated.Title != "Ship the fix" {
		t.Fatalf("title = %q, want Ship the fix", escalated.Title)
	}
	if escalated.EscalatedAt.IsZero() {
		t.Fatal("escalated_at must be set on escalation")
	}
	firstAt := escalated.EscalatedAt

	// Idempotent: re-escalating pins escalated_at to the first escalation and
	// does not regress an empty title/escalatedBy/lead over the stored ones.
	again, err := EscalateConversationThread(dir, created.ID, "", "", "")
	if err != nil {
		t.Fatalf("EscalateConversationThread (idempotent): %v", err)
	}
	if !again.EscalatedAt.Equal(firstAt) {
		t.Fatalf("escalated_at drifted: %v -> %v", firstAt, again.EscalatedAt)
	}
	if again.EscalatedBy != "human" || again.LeadParticipantID != "prt-lead" || again.Title != "Ship the fix" {
		t.Fatalf("idempotent escalate clobbered fields: %+v", again)
	}

	// A re-escalation can reassign the lead (overwrite-if-non-empty).
	reassigned, err := EscalateConversationThread(dir, created.ID, "", "prt-lead-2", "")
	if err != nil {
		t.Fatalf("EscalateConversationThread (reassign lead): %v", err)
	}
	if reassigned.LeadParticipantID != "prt-lead-2" {
		t.Fatalf("lead_participant_id = %q, want prt-lead-2 after reassignment", reassigned.LeadParticipantID)
	}

	// escalated_at survives resolve so a resolved task still reads as a task.
	if err := SetConversationThreadSummary(dir, created.ID, " done "); err != nil {
		t.Fatalf("SetConversationThreadSummary: %v", err)
	}
	if err := UpdateConversationThreadStatus(dir, created.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	got, err := FindConversationThreadByID(dir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ConversationThreadResolved {
		t.Fatalf("status = %q, want resolved", got.Status)
	}
	if got.EscalatedAt.IsZero() {
		t.Fatal("escalated_at must survive resolve")
	}
	// The lead survives resolve too — reclaim of lead authority is via status ->
	// resolved (the workflow gate requires status == task), not by nulling the field.
	if got.LeadParticipantID != "prt-lead-2" {
		t.Fatalf("lead_participant_id = %q, want prt-lead-2 to survive resolve", got.LeadParticipantID)
	}
	if got.Summary != "done" {
		t.Fatalf("summary = %q, want done", got.Summary)
	}
}

func TestEscalateConversationThreadMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := EscalateConversationThread(dir, "cth-missing", "", "", ""); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("EscalateConversationThread(missing) = %v, want ErrConversationThreadNotFound", err)
	}
	if err := SetConversationThreadSummary(dir, "cth-missing", "x"); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("SetConversationThreadSummary(missing) = %v, want ErrConversationThreadNotFound", err)
	}
}

func TestLeadTaskThreads(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "grp-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	// A task cth led by prt-lead.
	led, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-1", CreatedAt: time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EscalateConversationThread(dir, led.ID, "human", "prt-lead", "Ship it"); err != nil {
		t.Fatal(err)
	}
	// A task cth led by a different agent — must not leak to prt-lead.
	other, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-2", CreatedAt: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EscalateConversationThread(dir, other.ID, "human", "prt-other", ""); err != nil {
		t.Fatal(err)
	}
	// An open (never escalated) cth also created by prt-lead is not a task.
	if _, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-3", CreatedBy: "prt-lead",
	}); err != nil {
		t.Fatal(err)
	}

	leadTasks, err := LeadTaskThreads(dir, " prt-lead ")
	if err != nil {
		t.Fatalf("LeadTaskThreads: %v", err)
	}
	if len(leadTasks) != 1 || leadTasks[0].ID != led.ID {
		t.Fatalf("LeadTaskThreads(prt-lead) = %+v, want only %q", leadTasks, led.ID)
	}
	if leadTasks[0].Status != ConversationThreadTask || leadTasks[0].LeadParticipantID != "prt-lead" {
		t.Fatalf("unexpected lead task fields: %+v", leadTasks[0])
	}

	// Resolving the task reclaims authority: it no longer shows up as a lead task.
	if err := UpdateConversationThreadStatus(dir, led.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	after, err := LeadTaskThreads(dir, "prt-lead")
	if err != nil {
		t.Fatalf("LeadTaskThreads after resolve: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("resolved task still returned as lead task: %+v", after)
	}

	// Empty lead id yields nothing without touching the store.
	if got, err := LeadTaskThreads(dir, "  "); err != nil || got != nil {
		t.Fatalf("LeadTaskThreads(empty) = (%+v, %v), want (nil, nil)", got, err)
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
