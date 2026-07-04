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
