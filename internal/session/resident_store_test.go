package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestThreadMembersCRUD(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	noel := participant.Participant{ID: "prt-noel", Kind: participant.KindNamed, Name: "Noel"}
	reviewer := participant.Participant{ID: "prt-reviewer", Kind: participant.KindNamed, Name: "Reviewer"}
	worker := participant.Participant{ID: "prt-worker", Kind: participant.KindEphemeral, Name: "Worker"}
	for _, p := range []participant.Participant{noel, reviewer, worker} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}

	if err := AddThreadMember(dir, "thread-1", noel.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "thread-1", noel.ID); err != nil {
		t.Fatalf("duplicate add should be idempotent: %v", err)
	}
	if err := AddThreadMember(dir, "thread-1", reviewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "thread-1", worker.ID); err == nil {
		t.Fatal("ephemeral participants must not be thread members")
	}

	members, err := ListThreadMembers(dir, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || !containsString(members, noel.ID) || !containsString(members, reviewer.ID) {
		t.Fatalf("members = %v, want Noel and Reviewer once", members)
	}

	if err := RemoveThreadMember(dir, "thread-1", noel.ID); err != nil {
		t.Fatal(err)
	}
	members, err = ListThreadMembers(dir, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != reviewer.ID {
		t.Fatalf("members after remove = %v, want only Reviewer", members)
	}
	if _, err := ListThreadMembers(dir, "missing-thread"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListThreadMembers missing thread = %v, want ErrSessionNotFound", err)
	}
}

func TestRemoveThreadMemberProtectsThreadOwnerAndTaskLead(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "group", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := SetGroupThread(dir, "group", true); err != nil {
		t.Fatal(err)
	}
	owner := participant.Participant{ID: "prt-owner", Kind: participant.KindNamed, Name: "Owner"}
	if err := UpsertParticipant(dir, owner); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "group", owner.ID); err != nil {
		t.Fatal(err)
	}
	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "group", AnchorItemID: "message-1", ParentSeq: 1,
		ParentAuthorParticipantID: owner.ID, ThreadOwnerParticipantID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveThreadMember(dir, "group", owner.ID); err == nil || !strings.Contains(err.Error(), "owns open Thread") {
		t.Fatalf("open Thread owner removal = %v, want authority refusal", err)
	}
	if err := RemoveConversationThreadMember(dir, cth.ID, owner.ID); err == nil || !strings.Contains(err.Error(), "owns open Thread") {
		t.Fatalf("open Thread owner subset removal = %v, want authority refusal", err)
	}
	if _, err := EscalateConversationThread(dir, cth.ID, "human", "Ship"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThreadMember(dir, "group", owner.ID); err == nil || !strings.Contains(err.Error(), "leads active Task") {
		t.Fatalf("active Task lead removal = %v, want authority refusal", err)
	}
	if err := RemoveConversationThreadMember(dir, cth.ID, owner.ID); err == nil || !strings.Contains(err.Error(), "leads active Task") {
		t.Fatalf("active Task lead subset removal = %v, want authority refusal", err)
	}
	cth = readyTaskForConclusionForTest(t, dir, cth)
	if _, err := ConcludeConversationThread(dir, cth.ID, owner.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThreadMember(dir, "group", owner.ID); err != nil {
		t.Fatalf("resolved Task lead should be removable: %v", err)
	}
	if err := UpdateConversationThreadStatus(dir, cth.ID, ConversationThreadOpen); err == nil || !strings.Contains(err.Error(), "cannot be reopened") {
		t.Fatalf("resolved Task reopen = %v, want terminal-state refusal", err)
	}
}

func TestResidentInboxOrderAndConsumedIdempotence(t *testing.T) {
	dir := t.TempDir()
	noel := participant.Participant{ID: "prt-noel", Kind: participant.KindNamed, Name: "Noel"}
	worker := participant.Participant{ID: "prt-worker", Kind: participant.KindEphemeral, Name: "Worker"}
	for _, p := range []participant.Participant{noel, worker} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	later, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID:            "env-later",
		ParticipantID: noel.ID,
		EnvelopeJSON:  json.RawMessage(`{"text":"second"}`),
		CreatedAt:     base.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID:            "env-earlier",
		ParticipantID: noel.ID,
		EnvelopeJSON:  json.RawMessage(`{"text":"first"}`),
		CreatedAt:     base,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID:            "env-ephemeral",
		ParticipantID: worker.ID,
		EnvelopeJSON:  json.RawMessage(`{"text":"bad"}`),
	}); err == nil {
		t.Fatal("ephemeral participants must not receive resident inbox envelopes")
	}

	pending, err := PendingResidentEnvelopes(dir, noel.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != earlier.ID || pending[1].ID != later.ID {
		t.Fatalf("pending order = %+v, want earlier then later", pending)
	}
	if string(pending[0].EnvelopeJSON) != `{"text":"first"}` {
		t.Fatalf("envelope json = %s", pending[0].EnvelopeJSON)
	}

	limited, err := PendingResidentEnvelopes(dir, noel.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ID != earlier.ID {
		t.Fatalf("limited pending = %+v, want earlier only", limited)
	}

	if err := MarkResidentEnvelopesConsumed(dir, []string{earlier.ID}, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := MarkResidentEnvelopesConsumed(dir, []string{earlier.ID, "missing"}, base.Add(3*time.Minute)); err != nil {
		t.Fatalf("consume should be idempotent: %v", err)
	}
	pending, err = PendingResidentEnvelopes(dir, noel.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != later.ID {
		t.Fatalf("pending after consume = %+v, want later only", pending)
	}
}

func TestHistoryRecordEnvelopeMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	meta := json.RawMessage(`{"source_thread_id":"group-1","addressed":true,"hop":1,"sender_participant_id":"prt-a"}`)
	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{
		Role:         "user",
		Content:      "envelope prompt",
		EnvelopeMeta: meta,
	}); err != nil {
		t.Fatal(err)
	}
	history, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || string(history[0].EnvelopeMeta) != string(meta) {
		t.Fatalf("loaded envelope meta = %+v, want %s", history, meta)
	}
}

func TestAppendHistoryRecordAndConsumeResidentEnvelopes(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithMetadata(dir, "group-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryRecord(dir, "group-1", HistoryRecord{Role: "user", Content: "source"}); err != nil {
		t.Fatal(err)
	}
	noel := participant.Participant{ID: "prt-noel", Kind: participant.KindNamed, Name: "Noel"}
	if err := UpsertParticipant(dir, noel); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID:            "env-1",
		ParticipantID: noel.ID,
		EnvelopeJSON:  json.RawMessage(`{"text":"first"}`),
	}); err != nil {
		t.Fatal(err)
	}
	meta := json.RawMessage(`[{"source_thread_id":"group-1","addressed":true,"hop":0}]`)
	if _, err := AppendHistoryRecordAndCommitResidentAdmission(dir, "thread-1", HistoryRecord{
		Role:         "user",
		Content:      "incoming",
		EnvelopeMeta: meta,
	}, []string{"env-1"}, []MessageMark{{
		SessionID: "group-1", Seq: 1, ParticipantID: noel.ID,
		Kind: MessageMarkKindSeen, Status: SeenStatusInProgress,
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	history, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || string(history[0].EnvelopeMeta) != string(meta) {
		t.Fatalf("loaded history = %+v, want envelope meta %s", history, meta)
	}
	pending, err := PendingResidentEnvelopes(dir, noel.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after atomic append = %+v, want empty", pending)
	}
	marks, err := ListMessageMarks(dir, "group-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].Status != SeenStatusInProgress || marks[0].ParticipantID != noel.ID {
		t.Fatalf("resident admission receipt = %+v, want one in-progress mark", marks)
	}

	if _, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID: "env-rollback", ParticipantID: noel.ID, EnvelopeJSON: json.RawMessage(`{"text":"rollback"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendHistoryRecordAndCommitResidentAdmission(dir, "thread-1", HistoryRecord{
		Role: "user", Content: "must roll back",
	}, []string{"env-rollback"}, []MessageMark{{
		SessionID: "missing-source", Seq: 1, ParticipantID: noel.ID,
		Kind: MessageMarkKindSeen, Status: SeenStatusInProgress,
	}}, time.Now().UTC()); err == nil {
		t.Fatal("expected missing source receipt to roll back resident admission")
	}
	history, err = LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("failed resident admission appended history: %+v", history)
	}
	pending, err = PendingResidentEnvelopes(dir, noel.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "env-rollback" {
		t.Fatalf("failed resident admission consumed envelope: %+v", pending)
	}
}

func TestCompensateResidentAdmissionRestoresOnlyOwnedInProgressReceipts(t *testing.T) {
	dir := t.TempDir()
	task := attemptTaskForTest(t, dir, "attempt-group", "owner-a", "worker-a")
	attempt, _, err := ReserveTaskAttempt(dir, task.ID, "node-1", "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	started, err := StartTaskAttempt(dir, attempt.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"resident-dm", "source-a", "source-b", "source-c"} {
		if _, err := CreateWithMetadata(dir, id, "/tmp/project"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"source-a", "source-b", "source-c"} {
		if err := AppendHistoryRecord(dir, id, HistoryRecord{Role: "user", Content: id}); err != nil {
			t.Fatal(err)
		}
	}
	envelope, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID: "env-compensate", ParticipantID: "worker-a", EnvelopeJSON: json.RawMessage(`{"text":"push"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	admittedAt := time.Now().UTC().Truncate(time.Millisecond)
	marks := []MessageMark{
		{SessionID: "source-a", Seq: 1, ParticipantID: "worker-a", Kind: MessageMarkKindSeen, Status: SeenStatusInProgress, At: admittedAt},
		{SessionID: "source-b", Seq: 1, ParticipantID: "worker-a", Kind: MessageMarkKindSeen, Status: SeenStatusInProgress, At: admittedAt},
		{SessionID: "source-c", Seq: 1, ParticipantID: "worker-a", Kind: MessageMarkKindSeen, Status: SeenStatusInProgress, At: admittedAt},
	}
	if _, err := AppendHistoryRecordAndCommitResidentAdmission(dir, "resident-dm", HistoryRecord{
		Role: "user", Content: "admitted batch",
	}, []string{envelope.ID}, marks, admittedAt); err != nil {
		t.Fatal(err)
	}

	// Validation failure must leave every admitted side effect intact; there is
	// no sequence of independent requeue calls that can partially succeed.
	pending := ResidentAdmissionCompensation{
		ID: "compensate-owned", ParticipantID: "worker-a", ThreadID: "resident-dm",
		AttemptIDs:         []string{started.ID},
		AttemptStartedAt:   map[string]time.Time{started.ID: started.StartedAt},
		EnvelopeIDs:        []string{envelope.ID},
		EnvelopeConsumedAt: admittedAt,
		Marks:              append([]MessageMark(nil), marks...),
		CreatedAt:          admittedAt,
	}
	pending.Marks[0].At = time.Time{}
	if err := SaveResidentAdmissionCompensation(dir, pending); err == nil {
		t.Fatal("expected compensation validation failure")
	}
	if got, err := TaskAttemptByID(dir, attempt.ID); err != nil || got.Status != TaskAttemptRunning {
		t.Fatalf("attempt after rejected compensation = %+v, %v", got, err)
	}
	if pending, err := PendingResidentEnvelopes(dir, "worker-a", 0); err != nil || len(pending) != 0 {
		t.Fatalf("inbox after rejected compensation = %+v, %v", pending, err)
	}

	// Simulate another process finishing one receipt and refreshing another
	// in-progress receipt after this admission. Compensation must preserve both.
	completedAt := admittedAt.Add(time.Second)
	if err := MarkMessageSeen(dir, "source-b", 1, "worker-a", SeenStatusCompleted, "", completedAt); err != nil {
		t.Fatal(err)
	}
	refreshedAt := admittedAt.Add(2 * time.Second)
	if err := MarkMessageSeen(dir, "source-c", 1, "worker-a", SeenStatusInProgress, "", refreshedAt); err != nil {
		t.Fatal(err)
	}
	pending.Marks = marks
	if err := SaveResidentAdmissionCompensation(dir, pending); err != nil {
		t.Fatal(err)
	}
	if err := ResolveResidentAdmissionCompensation(dir, pending); err != nil {
		t.Fatal(err)
	}

	if got, err := TaskAttemptByID(dir, attempt.ID); err != nil || got.Status != TaskAttemptQueued || !got.StartedAt.IsZero() {
		t.Fatalf("compensated attempt = %+v, %v", got, err)
	}
	if pending, err := PendingResidentEnvelopes(dir, "worker-a", 0); err != nil || len(pending) != 1 || pending[0].ID != envelope.ID {
		t.Fatalf("compensated inbox = %+v, %v", pending, err)
	}
	if got, err := ListMessageMarks(dir, "source-a"); err != nil || len(got) != 0 {
		t.Fatalf("owned receipt after compensation = %+v, %v", got, err)
	}
	if got, err := ListMessageMarks(dir, "source-b"); err != nil || len(got) != 1 || got[0].Status != SeenStatusCompleted {
		t.Fatalf("completed receipt after compensation = %+v, %v", got, err)
	}
	if got, err := ListMessageMarks(dir, "source-c"); err != nil || len(got) != 1 || got[0].Status != SeenStatusInProgress || got[0].At.UnixMilli() != refreshedAt.UnixMilli() {
		t.Fatalf("refreshed receipt after compensation = %+v, %v", got, err)
	}
}

func TestResidentAdmissionCompensationJournalUsesExactAdmissionIdentity(t *testing.T) {
	dir := t.TempDir()
	task := attemptTaskForTest(t, dir, "journal-group", "journal-owner", "journal-worker")
	attempt, _, err := ReserveTaskAttempt(dir, task.ID, "node-1", "journal-worker")
	if err != nil {
		t.Fatal(err)
	}
	started, err := StartTaskAttempt(dir, attempt.ID, time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"journal-dm", "journal-source"} {
		if _, err := CreateWithMetadata(dir, id, "/tmp/project"); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendHistoryRecord(dir, "journal-source", HistoryRecord{Role: "user", Content: "pull"}); err != nil {
		t.Fatal(err)
	}
	envelope, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID: "journal-envelope", ParticipantID: "journal-worker", EnvelopeJSON: json.RawMessage(`{"text":"push"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	admittedAt := time.Now().UTC().Truncate(time.Millisecond)
	marks := []MessageMark{{
		SessionID: "journal-source", Seq: 1, ParticipantID: "journal-worker",
		Kind: MessageMarkKindSeen, Status: SeenStatusInProgress, At: admittedAt,
	}}
	if _, err := AppendHistoryRecordAndCommitResidentAdmission(dir, "journal-dm", HistoryRecord{
		Role: "user", Content: "journal batch",
	}, []string{envelope.ID}, marks, admittedAt); err != nil {
		t.Fatal(err)
	}
	pending := ResidentAdmissionCompensation{
		ID: "journal-exact", ParticipantID: "journal-worker", ThreadID: "journal-dm",
		AttemptIDs:         []string{started.ID},
		AttemptStartedAt:   map[string]time.Time{started.ID: started.StartedAt},
		EnvelopeIDs:        []string{envelope.ID},
		EnvelopeConsumedAt: admittedAt,
		Marks:              marks,
		CreatedAt:          admittedAt,
	}
	if err := SaveResidentAdmissionCompensation(dir, pending); err != nil {
		t.Fatal(err)
	}
	if journal, err := PendingResidentAdmissionCompensations(dir); err != nil || len(journal) != 1 || journal[0].ID != pending.ID {
		t.Fatalf("saved compensation journal = %+v, %v", journal, err)
	}
	if _, err := Delete(dir, "journal-dm"); err == nil || !strings.Contains(err.Error(), "pending resident admission compensation") {
		t.Fatalf("delete crossed compensation barrier: %v", err)
	}

	// Simulate a later owner advancing the same durable rows. Exact timestamp
	// receipts ensure a stale compensation cannot undo that newer admission.
	newer := admittedAt.Add(time.Second)
	db, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE task_attempts SET started_at = ? WHERE id = ?`, newer.UnixMilli(), started.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE resident_inbox SET consumed_at = ? WHERE id = ?`, newer.UnixMilli(), envelope.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := MarkMessageSeen(dir, "journal-source", 1, "journal-worker", SeenStatusInProgress, "", newer); err != nil {
		t.Fatal(err)
	}
	if err := ResolveResidentAdmissionCompensation(dir, pending); err != nil {
		t.Fatal(err)
	}
	if journal, err := PendingResidentAdmissionCompensations(dir); err != nil || len(journal) != 0 {
		t.Fatalf("resolved compensation journal = %+v, %v", journal, err)
	}
	wakes, err := PendingResidentWakeIntents(dir)
	if err != nil || len(wakes) != 1 || wakes[0].ID != pending.ID || wakes[0].ParticipantID != pending.ParticipantID || wakes[0].ThreadID != pending.ThreadID {
		t.Fatalf("resolved compensation wake = %+v, %v", wakes, err)
	}
	if removed, err := AcknowledgeResidentWakeIntents(dir, []string{pending.ID}); err != nil || removed != 1 {
		t.Fatalf("acknowledge resolved compensation wake = %d, %v", removed, err)
	}
	if got, err := TaskAttemptByID(dir, started.ID); err != nil || got.Status != TaskAttemptRunning || got.StartedAt.UnixMilli() != newer.UnixMilli() {
		t.Fatalf("newer attempt after stale compensation = %+v, %v", got, err)
	}
	if pendingEnvelopes, err := PendingResidentEnvelopes(dir, "journal-worker", 0); err != nil || len(pendingEnvelopes) != 0 {
		t.Fatalf("newer envelope after stale compensation = %+v, %v", pendingEnvelopes, err)
	}
	if got, err := ListMessageMarks(dir, "journal-source"); err != nil || len(got) != 1 || got[0].At.UnixMilli() != newer.UnixMilli() {
		t.Fatalf("newer mark after stale compensation = %+v, %v", got, err)
	}

	// A fresh exact intent is replayable on boot and clears all three admission
	// effects together with its own journal row.
	db, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE task_attempts SET status = ?, started_at = ? WHERE id = ?`, TaskAttemptRunning, started.StartedAt.UnixMilli(), started.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE resident_inbox SET consumed_at = ?, expired_at = NULL WHERE id = ?`, admittedAt.UnixMilli(), envelope.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := MarkMessageSeen(dir, "journal-source", 1, "journal-worker", SeenStatusInProgress, "", admittedAt); err != nil {
		t.Fatal(err)
	}
	pending.ID = "journal-recovery"
	if err := SaveResidentAdmissionCompensation(dir, pending); err != nil {
		t.Fatal(err)
	}
	if recovered, err := RecoverResidentAdmissionCompensationsForThread(dir, "unrelated-thread"); err != nil || recovered != 0 {
		t.Fatalf("recover unrelated compensation journal = %d, %v", recovered, err)
	}
	if recovered, err := RecoverResidentAdmissionCompensationsForThread(dir, "journal-dm"); err != nil || recovered != 1 {
		t.Fatalf("recover thread compensation journal = %d, %v", recovered, err)
	}
	wakes, err = PendingResidentWakeIntents(dir)
	if err != nil || len(wakes) != 1 || wakes[0].ID != pending.ID {
		t.Fatalf("recovered thread wake = %+v, %v", wakes, err)
	}
	if got, err := TaskAttemptByID(dir, started.ID); err != nil || got.Status != TaskAttemptQueued || !got.StartedAt.IsZero() {
		t.Fatalf("recovered attempt = %+v, %v", got, err)
	}
	if pendingEnvelopes, err := PendingResidentEnvelopes(dir, "journal-worker", 0); err != nil || len(pendingEnvelopes) != 1 || pendingEnvelopes[0].ID != envelope.ID {
		t.Fatalf("recovered envelope = %+v, %v", pendingEnvelopes, err)
	}
	if got, err := ListMessageMarks(dir, "journal-source"); err != nil || len(got) != 0 {
		t.Fatalf("recovered mark = %+v, %v", got, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
