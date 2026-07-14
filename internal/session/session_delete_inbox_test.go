package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestDeleteRemovesAllUnconsumedEnvelopesFromSourceThread(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"source-thread", "other-thread"} {
		if _, err := CreateWithMetadata(dir, id, t.TempDir()); err != nil {
			t.Fatalf("create session %q: %v", id, err)
		}
	}
	recipient := participant.Participant{ID: "prt-recipient", Kind: participant.KindNamed, Name: "Recipient"}
	if err := UpsertParticipant(dir, recipient); err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	enqueue := func(id, source string) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"source_thread_id": source, "text": id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
			ID: id, ParticipantID: recipient.ID, EnvelopeJSON: payload,
		}); err != nil {
			t.Fatalf("enqueue %q: %v", id, err)
		}
	}

	enqueue("expired-source", "source-thread")
	if _, err := MarkPendingResidentEnvelopesExpired(dir, time.Now().UTC()); err != nil {
		t.Fatalf("expire source envelope: %v", err)
	}
	enqueue("pending-source", "source-thread")
	enqueue("pending-other", "other-thread")
	enqueue("consumed-source", "source-thread")
	if err := MarkResidentEnvelopesConsumed(dir, []string{"consumed-source"}, time.Now().UTC()); err != nil {
		t.Fatalf("consume source envelope: %v", err)
	}

	if _, err := Delete(dir, "source-thread"); err != nil {
		t.Fatalf("delete source thread: %v", err)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct {
		id   string
		want int
	}{
		{id: "expired-source", want: 0},
		{id: "pending-source", want: 0},
		{id: "pending-other", want: 1},
		{id: "consumed-source", want: 1},
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(1) FROM resident_inbox WHERE id = ?`, tc.id).Scan(&count); err != nil {
			t.Fatalf("count envelope %q: %v", tc.id, err)
		}
		if count != tc.want {
			t.Fatalf("envelope %q count = %d, want %d", tc.id, count, tc.want)
		}
	}
}

func TestDeleteFailsClosedOnMalformedPendingEnvelope(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "source-thread", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	recipient := participant.Participant{ID: "prt-recipient", Kind: participant.KindNamed, Name: "Recipient"}
	if err := UpsertParticipant(dir, recipient); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueResidentEnvelope(dir, ResidentEnvelope{
		ID: "malformed", ParticipantID: recipient.ID, EnvelopeJSON: json.RawMessage(`{"source_thread_id":`),
	}); err != nil {
		t.Fatalf("enqueue malformed envelope: %v", err)
	}

	if _, err := Delete(dir, "source-thread"); err == nil || !strings.Contains(err.Error(), "decode pending resident envelope") {
		t.Fatalf("delete error = %v, want malformed-envelope failure", err)
	}
	if _, ok, err := Find(dir, "source-thread"); err != nil || !ok {
		t.Fatalf("failed delete removed the source session: ok=%t err=%v", ok, err)
	}
}
