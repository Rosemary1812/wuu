package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

// Held draft (task-rail design §8.3): when a resident composes a reply against
// a version of a thread that has since moved — a teammate or the user posted
// while it was thinking — the post is held (not published) and returned with
// what arrived, so the agent can revise, resend with force, or stay silent.

func TestHeldDraftHoldsWhenRoomMovedAndForcePublishes(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "held").ID
	for _, id := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}

	// Seed a message so the group has a tail, and record Ada's read receipt up
	// to it — the durable cursor says she has read this far.
	seq, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "有人吗", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, seq, ada, session.SeenStatusCompleted, time.Now().UTC()); err != nil {
		t.Fatalf("mark Ada seen: %v", err)
	}
	// The room MOVES while Ada is still composing this turn: Bea's answer lands
	// on the main stream but Ada has not consumed it yet (her read cursor is
	// still at `seq`). Append it to history directly so it advances the tail
	// without routing/marking it seen for Ada — the faithful mid-turn moment.
	if _, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "participant", ParticipantID: bea, PostKind: "result", Content: "我在,我来看。", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Bea answers: %v", err)
	}

	// Ada's speech for this turn engaged the group. She posts with an explicit
	// basis of `seq` — the message she generated her draft against.
	speech := srv.residentParticipantSpeechForTurn(ada, nil, map[string]bool{groupID: true})

	// Un-forced post: held, not published (Bea posted after Ada's basis), and
	// the note names Bea's arrival. The held result echoes the basis.
	held, err := speech.PostMessage(context.Background(), "result", "我来看这个问题。", groupID, seq, false)
	if err != nil {
		t.Fatalf("PostMessage (held path): %v", err)
	}
	if !held.Held {
		t.Fatalf("expected the draft to be held (Bea answered after Ada's basis), got %+v", held)
	}
	if held.BasisSeq != seq {
		t.Fatalf("held result should echo basis %d, got %d", seq, held.BasisSeq)
	}
	if !strings.Contains(held.HeldNote, "Bea") {
		t.Fatalf("held note should name what arrived, got %q", held.HeldNote)
	}

	// Forced resend: publishes despite the room having moved past the basis.
	posted, err := speech.PostMessage(context.Background(), "result", "补一句:我也看到了。", groupID, seq, true)
	if err != nil {
		t.Fatalf("PostMessage (force): %v", err)
	}
	if posted.Held {
		t.Fatalf("force=true must publish, not hold: %+v", posted)
	}
	if posted.Text == "" || posted.ThreadID != groupID {
		t.Fatalf("forced post not published correctly: %+v", posted)
	}
}

func TestHeldDraftDoesNotHoldAFreshPost(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "fresh").ID
	if err := session.AddThreadMember(rt.SessionDir, groupID, ada); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	// No seen-seq for this thread: the agent is initiating, not replying, so
	// the freshness check does not apply even though the thread is non-empty.
	if _, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "kickoff", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	speech := srv.residentParticipantSpeechForTurn(ada, nil, nil)
	posted, err := speech.PostMessage(context.Background(), "result", "开工。", groupID, 0, false)
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if posted.Held {
		t.Fatalf("a fresh initiating post (no basis) must not be held: %+v", posted)
	}
}
