package session

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestParticipantCRUD(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindEphemeral,
		Name: "Reviewer·auth", Role: "reviewer", Avatar: "🧐",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	got, err := GetParticipant(dir, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != p.Name || got.Kind != p.Kind || got.Role != p.Role {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	list, err := ListParticipants(dir, participant.KindEphemeral)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestCompleteParticipantRunUpsertsMonotonicTerminalState(t *testing.T) {
	dir := t.TempDir()
	participantID := participant.NewID()
	if err := UpsertParticipant(dir, participant.Participant{
		ID: participantID, Kind: participant.KindNamed, Name: "Run owner",
	}); err != nil {
		t.Fatal(err)
	}

	const agentID = "agt-workflow-dispatched"
	// Workflow-dispatched named agents do not necessarily pass through the
	// participant/start handler that records the initial running row. Terminal
	// completion must still create an auditable durable run.
	if err := CompleteParticipantRun(dir, participantID, agentID, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	// A participant/start write can race behind an extremely fast worker. It may
	// fill in task/session metadata, but it must never downgrade the terminal row.
	if err := UpsertParticipantRun(dir, ParticipantRun{
		ID: agentID, ParticipantID: participantID, AgentID: agentID,
		TaskID: "task-late", SessionID: "session-late",
		Outcome: "running", Summary: "working",
	}); err != nil {
		t.Fatal(err)
	}
	// Replaying the same terminal write is safe. App-server finalization may
	// retry after losing the response to a committed SQLite transaction.
	if err := CompleteParticipantRun(dir, participantID, agentID, "completed", "done"); err != nil {
		t.Fatalf("replay terminal completion: %v", err)
	}
	runs, err := ListParticipantRuns(dir, participantID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != agentID || runs[0].Outcome != "completed" || runs[0].Summary != "done" ||
		runs[0].TaskID != "task-late" || runs[0].SessionID != "session-late" {
		t.Fatalf("completed participant run = %+v", runs)
	}

	// Existing stores may use an ID distinct from agent_id. Completion updates
	// that row in place instead of assuming the newer ID convention.
	if err := UpsertParticipantRun(dir, ParticipantRun{
		ID: "run-custom-id", ParticipantID: participantID, AgentID: "agt-custom-id",
		Outcome: "running", Summary: "custom running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := CompleteParticipantRun(dir, participantID, "agt-custom-id", "failed", "custom failed"); err != nil {
		t.Fatal(err)
	}
	runs, err = ListParticipantRuns(dir, participantID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("participant runs = %+v, want 2", runs)
	}
	var custom ParticipantRun
	for _, run := range runs {
		if run.AgentID == "agt-custom-id" {
			custom = run
		}
	}
	if custom.ID != "run-custom-id" || custom.Outcome != "failed" || custom.Summary != "custom failed" {
		t.Fatalf("custom-id participant run = %+v", custom)
	}
}

// TestParticipantForkedFromRoundTrip proves the decision-six分身 marker
// (ForkedFrom, the母体's participant id) persists through the store.
func TestParticipantForkedFromRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mother := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Rina", Role: "reviewer"}
	if err := UpsertParticipant(dir, mother); err != nil {
		t.Fatal(err)
	}
	fork := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindNamed,
		Name: "Rina 的分身", Role: "reviewer", ForkedFrom: mother.ID,
	}
	if err := UpsertParticipant(dir, fork); err != nil {
		t.Fatal(err)
	}
	got, err := GetParticipant(dir, fork.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForkedFrom != mother.ID {
		t.Fatalf("ForkedFrom round-trip = %q, want %q", got.ForkedFrom, mother.ID)
	}
	// The母体 carries no fork marker.
	gotMother, err := GetParticipant(dir, mother.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMother.ForkedFrom != "" {
		t.Fatalf("mother ForkedFrom = %q, want empty", gotMother.ForkedFrom)
	}
	// Summary() surfaces the marker for the wire.
	if got.Summary().ForkedFromID != mother.ID {
		t.Fatalf("Summary ForkedFromID = %q, want %q", got.Summary().ForkedFromID, mother.ID)
	}
}

func TestNamedParticipantUniqueName(t *testing.T) {
	dir := t.TempDir()
	a := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "reviewer"}
	b := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "qa"}
	if err := UpsertParticipant(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := UpsertParticipant(dir, b); err == nil {
		t.Fatal("expected unique-name violation for active named participants")
	}
}

func TestRetireParticipant(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindEphemeral,
		Name: "Reviewer·auth", Role: "reviewer",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := GetParticipant(dir, p.ID)
	if err != nil {
		t.Fatalf("retired participant should still be readable by ID: %v", err)
	}
	if got.RetiredAt == nil {
		t.Error("RetiredAt = nil, want non-nil after retire")
	}
	list, err := ListParticipants(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("ListParticipants after retire = %v, want empty", list)
	}
}

func TestRetireParticipantRefusesOpenThreadOwnerAndActiveTaskLead(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "group", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	open := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "group", AnchorItemID: "open-owner", ThreadOwnerParticipantID: "prt-open-owner",
	})
	if err := RetireParticipant(dir, "prt-open-owner"); err == nil || !strings.Contains(err.Error(), "owns an open Thread") {
		t.Fatalf("open Thread owner retirement must be rejected, got %v", err)
	}
	if err := UpdateConversationThreadStatus(dir, open.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, "prt-open-owner"); err != nil {
		t.Fatalf("resolved Thread owner should retire: %v", err)
	}
	if err := UpdateConversationThreadStatus(dir, open.ID, ConversationThreadOpen); err == nil || !strings.Contains(err.Error(), "cannot be reopened") {
		t.Fatalf("resolved Thread must remain terminal after owner retirement, got %v", err)
	}

	task := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "group", AnchorItemID: "task-lead", ThreadOwnerParticipantID: "prt-task-lead",
	})
	if _, err := EscalateConversationThread(dir, task.ID, "human", ""); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, "prt-task-lead"); err == nil || !strings.Contains(err.Error(), "leads an active Task") {
		t.Fatalf("active Task lead retirement must be rejected, got %v", err)
	}
	task = readyTaskForConclusionForTest(t, dir, task)
	if _, err := ConcludeConversationThread(dir, task.ID, "prt-task-lead", "done"); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, "prt-task-lead"); err != nil {
		t.Fatalf("resolved Task lead should retire: %v", err)
	}
}

// TestRetireParticipantCleansDerivedRows asserts that retiring is a full
// storage-side cleanup transaction, not just a retired_at stamp: the
// participant's thread_members rows disappear and its unconsumed
// resident_inbox envelopes are dropped, while consumed envelopes are kept
// as delivery history.
func TestRetireParticipantCleansDerivedRows(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindNamed,
		Name: "Noel", Role: "reviewer",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithMetadata(dir, "sess-group", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "sess-group", p.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ParticipantID: p.ID,
		EnvelopeJSON:  []byte(`{"text":"pending"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ParticipantID: p.ID,
		EnvelopeJSON:  []byte(`{"text":"consumed"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkResidentEnvelopesConsumed(dir, []string{consumed.ID}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := RetireParticipant(dir, p.ID); err != nil {
		t.Fatal(err)
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var memberCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM thread_members WHERE participant_id = ?`, p.ID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 0 {
		t.Errorf("thread_members rows after retire = %d, want 0", memberCount)
	}
	var pendingCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM resident_inbox WHERE participant_id = ? AND consumed_at IS NULL`, p.ID).Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 0 {
		t.Errorf("pending resident_inbox rows after retire = %d, want 0 (envelope %s should be dropped)", pendingCount, pending.ID)
	}
	var consumedCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM resident_inbox WHERE participant_id = ? AND consumed_at IS NOT NULL`, p.ID).Scan(&consumedCount); err != nil {
		t.Fatal(err)
	}
	if consumedCount != 1 {
		t.Errorf("consumed resident_inbox rows after retire = %d, want 1 (history must be kept)", consumedCount)
	}
}

// TestRetireParticipantIdempotent asserts a second retire keeps the original
// retired_at stamp (COALESCE) and succeeds, so cleanup callers can re-run
// the protocol after a partial failure.
func TestRetireParticipantIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindNamed, Name: "Ivy",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, p.ID); err != nil {
		t.Fatal(err)
	}
	first, err := GetParticipant(dir, p.ID)
	if err != nil || first.RetiredAt == nil {
		t.Fatalf("first retire: %+v, %v", first, err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := RetireParticipant(dir, p.ID); err != nil {
		t.Fatalf("second retire should be idempotent, got %v", err)
	}
	second, err := GetParticipant(dir, p.ID)
	if err != nil || second.RetiredAt == nil {
		t.Fatalf("second retire: %+v, %v", second, err)
	}
	if !second.RetiredAt.Equal(*first.RetiredAt) {
		t.Errorf("retired_at changed on re-retire: %v -> %v", first.RetiredAt, second.RetiredAt)
	}
}

func TestRetireParticipantNotFound(t *testing.T) {
	dir := t.TempDir()
	err := RetireParticipant(dir, "prt-doesnotexist0000")
	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("RetireParticipant unknown ID = %v, want ErrParticipantNotFound", err)
	}
}

func TestRetiredNamedParticipantFreesName(t *testing.T) {
	dir := t.TempDir()
	a := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "reviewer"}
	if err := UpsertParticipant(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	b := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "qa"}
	if err := UpsertParticipant(dir, b); err != nil {
		t.Errorf("upsert named participant with retired name = %v, want nil", err)
	}
}

func TestFindRetiredParticipantByName(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := FindRetiredParticipantByName(dir, participant.KindNamed, "Noel"); err != nil || ok {
		t.Fatalf("empty store: ok=%v err=%v, want no match", ok, err)
	}

	first := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "reviewer"}
	if err := UpsertParticipant(dir, first); err != nil {
		t.Fatal(err)
	}
	// Active rows never match: the guard is about ARCHIVED predecessors.
	if _, ok, err := FindRetiredParticipantByName(dir, participant.KindNamed, "Noel"); err != nil || ok {
		t.Fatalf("active row matched: ok=%v err=%v", ok, err)
	}
	if err := RetireParticipant(dir, first.ID); err != nil {
		t.Fatal(err)
	}
	got, ok, err := FindRetiredParticipantByName(dir, participant.KindNamed, "noel")
	if err != nil || !ok {
		t.Fatalf("case-insensitive match failed: ok=%v err=%v", ok, err)
	}
	if got.ID != first.ID {
		t.Errorf("predecessor ID = %q, want %q", got.ID, first.ID)
	}

	// A second same-name generation retires later; the most recent
	// retirement must win.
	time.Sleep(5 * time.Millisecond)
	second := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "qa"}
	if err := UpsertParticipant(dir, second); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, second.ID); err != nil {
		t.Fatal(err)
	}
	got, ok, err = FindRetiredParticipantByName(dir, participant.KindNamed, "Noel")
	if err != nil || !ok {
		t.Fatalf("two-generation match failed: ok=%v err=%v", ok, err)
	}
	if got.ID != second.ID {
		t.Errorf("most recent retiree = %q, want %q", got.ID, second.ID)
	}

	if _, ok, _ := FindRetiredParticipantByName(dir, participant.KindNamed, "Unknown"); ok {
		t.Error("unknown name should not match")
	}
	if _, _, err := FindRetiredParticipantByName(dir, participant.KindNamed, "  "); err == nil {
		t.Error("blank name should be rejected")
	}
}

func TestCountParticipantsByKindIncludesRetired(t *testing.T) {
	dir := t.TempDir()
	if got, err := CountParticipantsByKind(dir, participant.KindNamed); err != nil || got != 0 {
		t.Fatalf("empty dir: count=%d err=%v", got, err)
	}
	a := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Andy", Role: "general-purpose"}
	if err := UpsertParticipant(dir, a); err != nil {
		t.Fatal(err)
	}
	if got, err := CountParticipantsByKind(dir, participant.KindNamed); err != nil || got != 1 {
		t.Fatalf("after insert: count=%d err=%v", got, err)
	}
	if err := RetireParticipant(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := CountParticipantsByKind(dir, participant.KindNamed); err != nil || got != 1 {
		t.Errorf("retired row must still be counted: count=%d err=%v", got, err)
	}
	b := participant.Participant{ID: participant.NewID(), Kind: participant.KindEphemeral, Name: "Worker·task"}
	if err := UpsertParticipant(dir, b); err != nil {
		t.Fatal(err)
	}
	if got, err := CountParticipantsByKind(dir, participant.KindNamed); err != nil || got != 1 {
		t.Errorf("ephemeral insert should not change named count: count=%d err=%v", got, err)
	}
	if got, err := CountParticipantsByKind(dir, participant.KindEphemeral); err != nil || got != 1 {
		t.Errorf("ephemeral count: count=%d err=%v", got, err)
	}
	if _, err := CountParticipantsByKind(dir, ""); err == nil {
		t.Error("empty kind should be rejected")
	}
}
