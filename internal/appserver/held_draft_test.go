package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
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

	// Seed a message so the group has a tail; Ada's turn "read" up to this seq.
	seq, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "有人吗", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	// The room then MOVES: Bea answers before Ada commits her draft.
	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID: bea, ParticipantID: bea, Kind: "result", Text: "我在,我来看。", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Bea answers: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	// Ada's speech for this turn read the group up to `seq` (before Bea's answer).
	speech := srv.residentParticipantSpeechForTurn(ada, nil, map[string]int{groupID: seq})

	// Un-forced post: held, not published, and the note names Bea's arrival.
	held, err := speech.PostMessage(context.Background(), "result", "我来看这个问题。", groupID, false)
	if err != nil {
		t.Fatalf("PostMessage (held path): %v", err)
	}
	if !held.Held {
		t.Fatalf("expected the draft to be held (Bea answered while composing), got %+v", held)
	}
	if !strings.Contains(held.HeldNote, "Bea") {
		t.Fatalf("held note should name what arrived, got %q", held.HeldNote)
	}

	// Forced resend: publishes despite the room having moved.
	posted, err := speech.PostMessage(context.Background(), "result", "补一句:我也看到了。", groupID, true)
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
	posted, err := speech.PostMessage(context.Background(), "result", "开工。", groupID, false)
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if posted.Held {
		t.Fatalf("a fresh initiating post (no seen-seq) must not be held: %+v", posted)
	}
}
