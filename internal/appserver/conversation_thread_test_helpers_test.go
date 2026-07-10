package appserver

import (
	"context"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// createPromotedTaskForTest exercises the real product path: append a named
// group message, open its Thread, then promote it as owner.
func createPromotedTaskForTest(ctx context.Context, m *residentTaskManager, threadID, title string) (tools.TaskView, error) {
	seq, err := session.AppendHistoryRecordReturningSeq(m.server.rt.SessionDir, threadID, session.HistoryRecord{
		Role: "participant", ParticipantID: m.participantID, PostKind: "result",
		Content: "Task proposal: " + title, At: time.Now().UTC(),
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	opened, err := m.OpenThread(ctx, threadID, seq, title)
	if err != nil {
		return tools.TaskView{}, err
	}
	return m.PromoteThread(ctx, opened.ID, title)
}

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
