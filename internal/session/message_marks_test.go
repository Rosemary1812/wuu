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
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusInProgress, "", now); err != nil {
		t.Fatalf("mark in_progress: %v", err)
	}
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusCompleted, "", now.Add(time.Second)); err != nil {
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
	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", SeenStatusFailed, "", now.Add(2*time.Second)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	marks, _ = ListMessageMarks(dir, thread)
	if m, _ := findMark(marks, 3, "prt-andy", MessageMarkKindSeen); m.Status != SeenStatusFailed {
		t.Fatalf("seen status = %q, want failed", m.Status)
	}

	if err := MarkMessageSeen(dir, thread, 3, "prt-andy", "bogus", "", now); err == nil {
		t.Fatal("expected error for unsupported seen status")
	}
}

// A reply subthread (cth) shares the parent's seq space but keeps its own read
// cursor: a seen mark carries the scope it was read in (thread_id), so
// ThreadReadWatermark (main stream) and ThreadReadWatermarkScoped(cth) advance
// independently — reading a cth message never drags the main cursor forward and
// vice versa (T3 per-cth cursor).
func TestThreadReadWatermarkScopedPerCth(t *testing.T) {
	dir, thread := messageMarksTestSetup(t)
	now := time.Now().UTC()
	const cth = "cth-abc"

	// Main-stream read at seq 4; a cth read at seq 9 (a higher seq in the shared
	// space). The two cursors must not interfere.
	if err := MarkMessageSeen(dir, thread, 4, "prt-andy", SeenStatusCompleted, "", now); err != nil {
		t.Fatalf("mark main seen: %v", err)
	}
	if err := MarkMessageSeen(dir, thread, 9, "prt-andy", SeenStatusCompleted, cth, now); err != nil {
		t.Fatalf("mark cth seen: %v", err)
	}

	main, err := ThreadReadWatermark(dir, thread, "prt-andy")
	if err != nil {
		t.Fatalf("main watermark: %v", err)
	}
	if main != 4 {
		t.Fatalf("main watermark = %d, want 4 (the cth read at seq 9 must not advance it)", main)
	}
	scoped, err := ThreadReadWatermarkScoped(dir, thread, "prt-andy", cth)
	if err != nil {
		t.Fatalf("cth watermark: %v", err)
	}
	if scoped != 9 {
		t.Fatalf("cth watermark = %d, want 9", scoped)
	}
	// Reading further in the main stream advances only the main cursor.
	if err := MarkMessageSeen(dir, thread, 12, "prt-andy", SeenStatusCompleted, "", now.Add(time.Second)); err != nil {
		t.Fatalf("mark main seen 12: %v", err)
	}
	if main, _ := ThreadReadWatermark(dir, thread, "prt-andy"); main != 12 {
		t.Fatalf("main watermark after further main read = %d, want 12", main)
	}
	if scoped, _ := ThreadReadWatermarkScoped(dir, thread, "prt-andy", cth); scoped != 9 {
		t.Fatalf("cth watermark after a main read = %d, want it unchanged at 9", scoped)
	}
	// An unrelated cth has its own (empty) cursor.
	if other, _ := ThreadReadWatermarkScoped(dir, thread, "prt-andy", "cth-other"); other != 0 {
		t.Fatalf("unrelated cth watermark = %d, want 0", other)
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
	if err := MarkMessageSeen(dir, thread, 7, "prt-andy", SeenStatusCompleted, "", now); err != nil {
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
