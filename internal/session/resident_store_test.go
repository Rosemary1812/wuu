package session

import (
	"encoding/json"
	"errors"
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
