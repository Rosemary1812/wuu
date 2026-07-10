package appserver

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

func createStoredOpenThreadForTest(t *testing.T, srv *Server, parentThreadID, ownerID, anchorItemID string, parentSeq int) session.ConversationThread {
	t.Helper()
	thread, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID:                 parentThreadID,
		AnchorItemID:              anchorItemID,
		CreatedBy:                 humanReactionParticipantID,
		ThreadOwnerParticipantID:  ownerID,
		ParentSeq:                 parentSeq,
		ParentAuthorParticipantID: ownerID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	return thread
}

func appendNamedAnchorForTest(t *testing.T, srv *Server, parentThreadID, name string) (string, int, string) {
	t.Helper()
	participantID := saveNamedParticipant(t, srv.rt, name, "reviewer", "")
	if err := session.AddThreadMember(srv.rt.SessionDir, parentThreadID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	seq, itemID := appendMainStreamAgentMessage(t, srv, parentThreadID, participantID, "anchor from "+name)
	return participantID, seq, itemID
}

func openNamedSubthreadForTest(t *testing.T, srv *Server, parentThreadID, name string) (session.ConversationThread, string, string) {
	t.Helper()
	participantID, _, anchorItemID := appendNamedAnchorForTest(t, srv, parentThreadID, name)
	thread, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     parentThreadID,
		AnchorItemID: anchorItemID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	return thread, participantID, anchorItemID
}
