package session

import (
	"strings"
	"sync"
	"testing"
)

// seedTaskThread creates a session and a born-task cth (the manage_task create
// shape: status task at creation, standalone anchor allowed, empty owner).
func seedTaskThread(t *testing.T, dir string) ConversationThread {
	t.Helper()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "thread-1",
		Status:    ConversationThreadTask,
		Title:     "fix the bug",
		CreatedBy: "prt-andy",
	})
	if err != nil {
		t.Fatalf("create born-task cth: %v", err)
	}
	return created
}

func TestCreateConversationThreadStandaloneTaskAllowed(t *testing.T) {
	created := seedTaskThread(t, t.TempDir())
	if created.AnchorItemID != "" {
		t.Fatalf("anchor = %q, want empty (standalone)", created.AnchorItemID)
	}
	if created.Status != ConversationThreadTask {
		t.Fatalf("status = %q, want task", created.Status)
	}
}

func TestCreateConversationThreadStandaloneOpenRefused(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "thread-1",
		Status:    ConversationThreadOpen,
	}); err == nil {
		t.Fatal("standalone open reply should be refused (anchor required)")
	}
}

func TestClaimConversationThreadCAS(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)

	claimed, ok, err := ClaimConversationThread(dir, created.ID, "prt-bella")
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if claimed.OwnerParticipantID != "prt-bella" {
		t.Fatalf("owner = %q, want prt-bella", claimed.OwnerParticipantID)
	}
	if claimed.LeadParticipantID != "" {
		t.Fatalf("claim must not touch lead, got %q", claimed.LeadParticipantID)
	}

	// Loser gets claimed=false with the current owner, and NO error.
	current, ok, err := ClaimConversationThread(dir, created.ID, "prt-carl")
	if err != nil {
		t.Fatalf("losing claim should not error: %v", err)
	}
	if ok {
		t.Fatal("second claim should lose the CAS")
	}
	if current.OwnerParticipantID != "prt-bella" {
		t.Fatalf("loser sees owner %q, want prt-bella", current.OwnerParticipantID)
	}

	// Re-claim by the owner is an explicit error, not a refresh.
	if _, _, err := ClaimConversationThread(dir, created.ID, "prt-bella"); err == nil {
		t.Fatal("owner re-claim should error")
	}
}

func TestClaimConversationThreadConcurrent(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)

	const racers = 8
	var wg sync.WaitGroup
	wins := make(chan string, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		id := string(rune('a' + i))
		go func() {
			defer wg.Done()
			if _, ok, err := ClaimConversationThread(dir, created.ID, "prt-"+id); err == nil && ok {
				wins <- id
			}
		}()
	}
	wg.Wait()
	close(wins)
	var winners []string
	for w := range wins {
		winners = append(winners, w)
	}
	if len(winners) != 1 {
		t.Fatalf("exactly one racer must win the claim, got %d (%v)", len(winners), winners)
	}
}

func TestClaimOpenReplyRefused(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	reply, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ClaimConversationThread(dir, reply.ID, "prt-bella")
	if err == nil || !strings.Contains(err.Error(), "only a task can be claimed") {
		t.Fatalf("claiming an open reply should be refused, got %v", err)
	}
}

func TestUnclaimConversationThread(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)
	if _, ok, err := ClaimConversationThread(dir, created.ID, "prt-bella"); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	if _, err := UnclaimConversationThread(dir, created.ID, "prt-carl"); err == nil {
		t.Fatal("non-owner unclaim should error")
	}
	released, err := UnclaimConversationThread(dir, created.ID, "prt-bella")
	if err != nil {
		t.Fatalf("owner unclaim: %v", err)
	}
	if released.OwnerParticipantID != "" {
		t.Fatalf("owner after unclaim = %q, want empty", released.OwnerParticipantID)
	}
	// Released task is claimable again.
	if _, ok, err := ClaimConversationThread(dir, created.ID, "prt-carl"); err != nil || !ok {
		t.Fatalf("re-claim after release: ok=%v err=%v", ok, err)
	}
}

func TestMarkConversationThreadReview(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)
	if _, ok, err := ClaimConversationThread(dir, created.ID, "prt-bella"); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Non-owner cannot file for review.
	if _, err := MarkConversationThreadReview(dir, created.ID, "prt-carl", "done"); err == nil {
		t.Fatal("non-owner review should error")
	}
	// Summary is required.
	if _, err := MarkConversationThreadReview(dir, created.ID, "prt-bella", "  "); err == nil {
		t.Fatal("empty summary should error")
	}

	reviewed, err := MarkConversationThreadReview(dir, created.ID, "prt-bella", "fixed and verified")
	if err != nil {
		t.Fatalf("owner review: %v", err)
	}
	if reviewed.Status != ConversationThreadReview {
		t.Fatalf("status = %q, want review", reviewed.Status)
	}
	if reviewed.Summary != "fixed and verified" {
		t.Fatalf("summary = %q", reviewed.Summary)
	}

	// A task in review is no longer claimable and cannot re-file.
	if _, _, err := ClaimConversationThread(dir, created.ID, "prt-carl"); err == nil {
		t.Fatal("claiming a review-status task should error")
	}
	if _, err := MarkConversationThreadReview(dir, created.ID, "prt-bella", "again"); err == nil {
		t.Fatal("double review should error")
	}

	// And it no longer counts as an active lead task for the workflow gate
	// even if a lead was somehow set: status must be task.
	leads, err := LeadTaskThreads(dir, "prt-bella")
	if err != nil {
		t.Fatal(err)
	}
	if len(leads) != 0 {
		t.Fatalf("review task must not appear as active lead task: %v", leads)
	}
}
