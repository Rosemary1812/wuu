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
	created, err := CreateLegacyTaskConversationThread(dir, ConversationThread{
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
	reply := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-2",
	})
	_, _, err := ClaimConversationThread(dir, reply.ID, "prt-bella")
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

func TestSetConversationThreadLeadIfEmptyCAS(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)
	if created.LeadParticipantID != "" {
		t.Fatalf("born-task cth must start leadless, got %q", created.LeadParticipantID)
	}

	// First claimer wins: lead is stamped, becameLead true.
	claimed, became, err := SetConversationThreadLeadIfEmpty(dir, created.ID, "prt-andy")
	if err != nil || !became {
		t.Fatalf("first lead claim: became=%v err=%v", became, err)
	}
	if claimed.LeadParticipantID != "prt-andy" {
		t.Fatalf("lead = %q, want prt-andy", claimed.LeadParticipantID)
	}
	// Claiming the lead does not touch work ownership.
	if claimed.OwnerParticipantID != "" {
		t.Fatalf("lead claim must not set an owner, got %q", claimed.OwnerParticipantID)
	}

	// A second, different claimer loses the CAS: no error, becameLead false,
	// and the returned thread reports who already leads.
	current, became, err := SetConversationThreadLeadIfEmpty(dir, created.ID, "prt-bella")
	if err != nil {
		t.Fatalf("losing lead claim should not error: %v", err)
	}
	if became {
		t.Fatal("second lead claim should lose the CAS")
	}
	if current.LeadParticipantID != "prt-andy" {
		t.Fatalf("loser sees lead %q, want prt-andy", current.LeadParticipantID)
	}

	// The existing lead re-claiming is also a (harmless) CAS loss, not a
	// refresh: lead_participant_id is already non-empty.
	if _, became, err := SetConversationThreadLeadIfEmpty(dir, created.ID, "prt-andy"); err != nil || became {
		t.Fatalf("lead self re-claim: became=%v err=%v (want false, nil)", became, err)
	}
}

func TestSetConversationThreadLeadIfEmptyRejectsNonTask(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	// An open discussion reply has no orchestration lead to claim.
	reply := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-2",
	})
	if _, _, err := SetConversationThreadLeadIfEmpty(dir, reply.ID, "prt-andy"); err == nil ||
		!strings.Contains(err.Error(), "only a task has an orchestration lead") {
		t.Fatalf("claiming lead on an open reply should be refused, got %v", err)
	}

	// A missing subthread is a loud not-found, not a silent no-op.
	if _, _, err := SetConversationThreadLeadIfEmpty(dir, "cth-nope", "prt-andy"); err == nil {
		t.Fatal("claiming lead on a missing subthread should error")
	}
}

func TestConcludeConversationThreadOnlyByLead(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)
	if _, ok, err := ClaimConversationThread(dir, created.ID, "prt-bella"); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// A caller that is not the lead cannot conclude.
	if _, err := ConcludeConversationThread(dir, created.ID, "prt-carl", "done"); err == nil {
		t.Fatal("non-lead conclude should error")
	} else if !strings.Contains(err.Error(), "only the lead") {
		t.Fatalf("refusal should diagnose the caller, got %v", err)
	}
	// Work ownership no longer grants conclusion authority.
	if _, err := ConcludeConversationThread(dir, created.ID, "prt-bella", "done"); err == nil || !strings.Contains(err.Error(), "only the lead") {
		t.Fatalf("work owner conclusion should be refused by the lead gate, got %v", err)
	}
	if _, _, err := SetConversationThreadLeadIfEmpty(dir, created.ID, "prt-lead"); err != nil {
		t.Fatalf("set lead: %v", err)
	}
	// Summary is required.
	if _, err := ConcludeConversationThread(dir, created.ID, "prt-lead", "  "); err == nil {
		t.Fatal("empty summary should error")
	}

	// Filing the conclusion resolves the task in one step — no review gate.
	concluded, err := ConcludeConversationThread(dir, created.ID, "prt-lead", "fixed and verified")
	if err != nil {
		t.Fatalf("lead conclude: %v", err)
	}
	if concluded.Status != ConversationThreadResolved {
		t.Fatalf("status = %q, want resolved (filing IS completion)", concluded.Status)
	}
	if concluded.Summary != "fixed and verified" {
		t.Fatalf("summary = %q", concluded.Summary)
	}

	// A resolved task is no longer claimable and cannot be re-concluded.
	if _, _, err := ClaimConversationThread(dir, created.ID, "prt-carl"); err == nil {
		t.Fatal("claiming a resolved task should error")
	}
	if _, err := ConcludeConversationThread(dir, created.ID, "prt-lead", "again"); err == nil {
		t.Fatal("double conclude should error")
	} else if !strings.Contains(err.Error(), `status is "resolved"`) {
		t.Fatalf("refusal should diagnose the status, got %v", err)
	}
}

func TestConcludeConversationThreadByLeadOfUnclaimedTask(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)
	// Grant a lead and leave the task unclaimed (plan-task shape: the lead
	// orchestrates; no single owner ever claims it).
	if _, _, err := SetConversationThreadLeadIfEmpty(dir, created.ID, "prt-lead"); err != nil {
		t.Fatalf("set lead: %v", err)
	}

	concluded, err := ConcludeConversationThread(dir, created.ID, "prt-lead", "all pieces landed")
	if err != nil {
		t.Fatalf("lead conclude of unclaimed task: %v", err)
	}
	if concluded.Status != ConversationThreadResolved {
		t.Fatalf("status = %q, want resolved", concluded.Status)
	}
	if concluded.Summary != "all pieces landed" {
		t.Fatalf("summary = %q", concluded.Summary)
	}
}

func TestSetConversationThreadExecState(t *testing.T) {
	dir := t.TempDir()
	created := seedTaskThread(t, dir)

	// The pre-execution zero value reads back empty.
	if created.ExecState != "" {
		t.Fatalf("fresh cth exec state = %q, want empty", created.ExecState)
	}

	// Every vocabulary member round-trips.
	for _, state := range []string{
		ExecStatePlanning, ExecStateExecuting, ExecStateBlocked,
		ExecStateNeedsHuman, ExecStateCompleted, ExecStateFailed,
	} {
		if err := SetConversationThreadExecState(dir, created.ID, state); err != nil {
			t.Fatalf("set exec state %q: %v", state, err)
		}
		got, err := FindConversationThreadByID(dir, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ExecState != state {
			t.Fatalf("exec state = %q, want %q", got.ExecState, state)
		}
	}

	// The vocabulary is closed: unknown values and the empty string are loud
	// errors, never stored.
	if err := SetConversationThreadExecState(dir, created.ID, "reviewing"); err == nil {
		t.Fatal("unknown exec state should error")
	} else if !strings.Contains(err.Error(), "invalid conversation thread exec state") {
		t.Fatalf("unknown exec state should be diagnosed, got %v", err)
	}
	if err := SetConversationThreadExecState(dir, created.ID, ""); err == nil {
		t.Fatal("empty exec state should error (zero value is never set explicitly)")
	}
	if err := SetConversationThreadExecState(dir, "cth-missing", ExecStatePlanning); err == nil {
		t.Fatal("missing thread should error")
	}
}
