package appserver

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// React writes a reaction row and — the load-bearing invariant — never routes:
// no envelope is enqueued and no other agent is woken.
func TestReactWritesReactionWithoutRouting(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	andy := saveNamedParticipant(t, rt, "Andy", "reviewer", "")
	le := saveNamedParticipant(t, rt, "le", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, p := range []string{andy, le} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, p); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}

	// A real message to react to, and let the routed turns settle so both
	// inboxes are drained to a known-empty baseline.
	seq := routeUserMessageAndSourceSeq(t, srv, groupID, andy)
	waitForResidentQuiesce(t, srv)

	reactor, ok := srv.residentParticipantSpeech(andy).(tools.ParticipantReactor)
	if !ok {
		t.Fatal("resident speech does not implement ParticipantReactor")
	}
	if err := reactor.React(context.Background(), groupID, seq, "shrug"); err != nil {
		t.Fatalf("react: %v", err)
	}

	marks, err := session.ListMessageMarks(rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("list marks: %v", err)
	}
	found := false
	for _, m := range marks {
		if m.Kind == session.MessageMarkKindReaction && m.ParticipantID == andy && m.Seq == seq && m.Payload == "shrug" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reaction not written, marks=%+v", marks)
	}

	// Invariant: reacting is terminal — le's inbox must not have gained an
	// envelope from Andy's reaction.
	pending, err := session.PendingResidentEnvelopes(rt.SessionDir, le, 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("react must not route, but le has %d pending envelope(s)", len(pending))
	}

	// An unknown reaction key is rejected.
	if err := reactor.React(context.Background(), groupID, seq, "kaboom"); err == nil {
		t.Fatal("expected unknown reaction to be rejected")
	}
}
