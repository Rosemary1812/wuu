package session

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func messageMarksTestSetup(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []participant.Participant{
		{ID: "prt-andy", Kind: participant.KindNamed, Name: "Andy"},
		{ID: "prt-le", Kind: participant.KindNamed, Name: "le"},
	} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	return dir, "thread-1"
}

func findMark(marks []MessageMark, seq int, participantID, kind string) (MessageMark, bool) {
	for _, m := range marks {
		if m.Seq == seq && m.ParticipantID == participantID && m.Kind == kind {
			return m, true
		}
	}
	return MessageMark{}, false
}

func TestMessageMarksSeenLifecycle(t *testing.T) {
	dir, thread := messageMarksTestSetup(t)
	now := time.Now().UTC()

	// in_progress -> completed advances the SAME row (no duplicate).
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusInProgress, now); err != nil {
		t.Fatalf("mark in_progress: %v", err)
	}
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusCompleted, now.Add(time.Second)); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	marks, err := ListMessageMarks(dir, thread)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := 0
	for _, m := range marks {
		if m.Kind == MessageMarkKindSeen {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("want exactly one seen row after advance, got %d (%+v)", seen, marks)
	}
	m, ok := findMark(marks, 3, "prt-andy", MessageMarkKindSeen)
	if !ok || m.Status != SeenStatusCompleted {
		t.Fatalf("seen row = %+v, ok=%v, want status completed", m, ok)
	}

	// A later turn can fail (e.g. retry after a network drop) — status must
	// reflect the failure, not stay stuck at completed's predecessor.
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusFailed, now.Add(2*time.Second)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	marks, _ = ListMessageMarks(dir, thread)
	if m, _ := findMark(marks, 3, "prt-andy", MessageMarkKindSeen); m.Status != SeenStatusFailed {
		t.Fatalf("seen status = %q, want failed", m.Status)
	}

	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", "bogus", now); err == nil {
		t.Fatal("expected error for unsupported seen status")
	}
}

func TestMessageMarksReactionReplace(t *testing.T) {
	dir, thread := messageMarksTestSetup(t)
	now := time.Now().UTC()

	if err := SetMessageReaction(dir, thread, 5, "prt-le", "eyes", now); err != nil {
		t.Fatalf("react eyes: %v", err)
	}
	if err := SetMessageReaction(dir, thread, 5, "prt-le", "shrug", now.Add(time.Second)); err != nil {
		t.Fatalf("react shrug: %v", err)
	}
	marks, err := ListMessageMarks(dir, thread)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	reactions := 0
	for _, m := range marks {
		if m.Kind == MessageMarkKindReaction {
			reactions++
		}
	}
	if reactions != 1 {
		t.Fatalf("re-reacting must replace, want 1 reaction row, got %d", reactions)
	}
	if m, _ := findMark(marks, 5, "prt-le", MessageMarkKindReaction); m.Payload != "shrug" {
		t.Fatalf("reaction payload = %q, want shrug", m.Payload)
	}
	if err := SetMessageReaction(dir, thread, 5, "prt-le", "  ", now); err == nil {
		t.Fatal("expected error for empty reaction")
	}
}

func TestMessageMarksSeenAndReactionCoexist(t *testing.T) {
	dir, thread := messageMarksTestSetup(t)
	now := time.Now().UTC()

	// The same (message, participant) may carry both a seen row and a
	// reaction row — different kinds, different PK.
	if err := MarkMessageSeen(dir, thread, 7, "prt-andy", SeenStatusCompleted, now); err != nil {
		t.Fatal(err)
	}
	if err := SetMessageReaction(dir, thread, 7, "prt-andy", "smug", now); err != nil {
		t.Fatal(err)
	}
	marks, err := ListMessageMarks(dir, thread)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findMark(marks, 7, "prt-andy", MessageMarkKindSeen); !ok {
		t.Fatal("missing seen row")
	}
	if _, ok := findMark(marks, 7, "prt-andy", MessageMarkKindReaction); !ok {
		t.Fatal("missing reaction row")
	}
}
