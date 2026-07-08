package appserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

// waitForSeenStatus polls the message_marks store until participantID has a
// seen row on (threadID, seq) with the wanted status, or fails on timeout.
func waitForSeenStatus(t *testing.T, sessDir, threadID, participantID string, seq int, wantStatus string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		marks, err := session.ListMessageMarks(sessDir, threadID)
		if err == nil {
			for _, m := range marks {
				if m.Kind == session.MessageMarkKindSeen && m.ParticipantID == participantID && m.Seq == seq {
					if m.Status == wantStatus {
						return
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	marks, _ := session.ListMessageMarks(sessDir, threadID)
	t.Fatalf("timed out waiting for seen status %q (%s on %s#%d); marks=%+v", wantStatus, participantID, threadID, seq, marks)
}

func routeUserMessageAndSourceSeq(t *testing.T, srv *Server, groupID, member string) int {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"hello team"}}`, groupID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	_, memberHistory := waitForResidentDMHistory(t, srv, member, 1)
	seq := findEnvelopeMetaRecord(t, memberHistory).SourceSeq
	if seq <= 0 {
		t.Fatalf("routed envelope carried no source seq")
	}
	return seq
}

// thread/marks returns both read-receipt and reaction rows for a thread so the
// chat view can render on load.
func TestThreadMarksRPCReturnsSeenAndReactions(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	andy := saveNamedParticipant(t, rt, "Andy", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, andy); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	now := time.Now().UTC()
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, 2, andy, session.SeenStatusCompleted, "", now); err != nil {
		t.Fatal(err)
	}
	if err := session.SetMessageReaction(rt.SessionDir, groupID, 2, andy, "smug", now); err != nil {
		t.Fatal(err)
	}

	raw := fmt.Sprintf(`{"id":"marks","method":"thread/marks","params":{"thread_id":%q}}`, groupID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/marks: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "marks")
	if resp["error"] != nil {
		t.Fatalf("thread/marks error: %v", resp["error"])
	}
	result := remarshal[ThreadMarksResult](t, resp["result"])
	var seenOK, reactOK bool
	for _, m := range result.Marks {
		if m.Kind == session.MessageMarkKindSeen && m.Seq == 2 && m.ParticipantID == andy && m.Status == session.SeenStatusCompleted {
			seenOK = true
		}
		if m.Kind == session.MessageMarkKindReaction && m.Seq == 2 && m.ParticipantID == andy && m.Reaction == "smug" {
			reactOK = true
		}
	}
	if !seenOK || !reactOK {
		t.Fatalf("thread/marks missing rows: seen=%v react=%v marks=%+v", seenOK, reactOK, result.Marks)
	}
}

// A completed resident turn resolves the read receipt to "completed" — the
// real "seen".
func TestReadReceiptCompletedOnResidentTurn(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, bea); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	seq := routeUserMessageAndSourceSeq(t, srv, groupID, bea)
	waitForSeenStatus(t, rt.SessionDir, groupID, bea, seq, session.SeenStatusCompleted)
}

// A turn that starts (envelope consumed) but then errors resolves the receipt
// to "failed", not "completed" — a crashed turn must not read as "read". This
// is the two-state distinction: delivered/attempted vs finished cleanly.
func TestReadReceiptFailedOnResidentTurnError(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{err: fmt.Errorf("provider boom")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, bea); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	seq := routeUserMessageAndSourceSeq(t, srv, groupID, bea)
	waitForSeenStatus(t, rt.SessionDir, groupID, bea, seq, session.SeenStatusFailed)
}
