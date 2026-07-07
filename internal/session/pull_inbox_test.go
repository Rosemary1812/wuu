package session

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

// Pull-inbox base: the read cursor (ThreadReadWatermark) plus the reverse
// follow query (ThreadsForParticipant) and the unread read (ChatMessagesSince)
// are the shared primitive under both the held-draft freshness check and the
// pull inbox — "how far has this agent read, which threads does it follow, and
// what chat has arrived past the cursor".

func TestPullInboxBasePrimitives(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"grp-a", "grp-b"} {
		if _, err := CreateWithMetadata(dir, id, "/tmp/p"); err != nil {
			t.Fatal(err)
		}
	}
	ada := participant.Participant{ID: "prt-ada", Kind: participant.KindNamed, Name: "Ada"}
	if err := UpsertParticipant(dir, ada); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "grp-a", ada.ID); err != nil {
		t.Fatal(err)
	}

	// Reverse follow query: Ada follows grp-a, not grp-b.
	threads, err := ThreadsForParticipant(dir, ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0] != "grp-a" {
		t.Fatalf("ThreadsForParticipant = %v, want [grp-a]", threads)
	}

	// Seed three messages; Ada has read up to the 2nd (watermark).
	var seqs []int
	for _, m := range []struct{ role, pid, kind, text string }{
		{"user", "", "", "一"},
		{"participant", "prt-bea", "result", "二"},
		{"participant", "prt-bea", "result", "三"},
	} {
		seq, err := AppendHistoryRecordReturningSeq(dir, "grp-a", HistoryRecord{
			Role: m.role, ParticipantID: m.pid, PostKind: m.kind, Content: m.text, At: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	// A telemetry participant row (no post kind) must NOT count as chat.
	if _, err := AppendHistoryRecordReturningSeq(dir, "grp-a", HistoryRecord{
		Role: "participant", ParticipantID: "prt-bea", Content: "meta", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := MarkMessageSeen(dir, "grp-a", seqs[1], ada.ID, SeenStatusCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// Read cursor is the 2nd seq.
	wm, err := ThreadReadWatermark(dir, "grp-a", ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wm != seqs[1] {
		t.Fatalf("watermark = %d, want %d", wm, seqs[1])
	}

	// Unread since the cursor = just the 3rd message (the meta row excluded).
	unread, err := ChatMessagesSince(dir, "grp-a", wm)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 || unread[0].Content != "三" {
		t.Fatalf("ChatMessagesSince = %+v, want just the 3rd chat message", unread)
	}
}
