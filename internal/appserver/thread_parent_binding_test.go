package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

// appendMainStreamAgentMessage folds an agent's main-stream post into a group
// thread's history and returns its seq and the reconstructed anchor item id the
// GUI would render — the exact id a reply is opened against.
func appendMainStreamAgentMessage(t *testing.T, srv *Server, groupID, participantID, text string) (int, string) {
	t.Helper()
	seq, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "participant", ParticipantID: participantID, PostKind: "result", Content: text, At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append agent message: %v", err)
	}
	itemID, err := srv.mainStreamItemIDForSeq(groupID, seq)
	if err != nil {
		t.Fatalf("resolve anchor item id for seq %d: %v", seq, err)
	}
	return seq, itemID
}

// Opening a reply from a main-stream message binds the reply to that message:
// the resolved seq and author land on the cth row (T3 parent binding).
func TestOpenSubthreadStoresParentBinding(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)

	seq, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "我提个方案")

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
		Title:        "converge on Andy's proposal",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	if cth.ParentSeq != seq {
		t.Fatalf("cth parent_seq = %d, want the anchored message seq %d", cth.ParentSeq, seq)
	}
	if cth.ParentAuthorParticipantID != andy {
		t.Fatalf("cth parent_author = %q, want the anchored message author %q", cth.ParentAuthorParticipantID, andy)
	}
	// Persisted, not just returned.
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.ParentSeq != seq || got.ParentAuthorParticipantID != andy {
		t.Fatalf("stored parent binding = %d/%q, want %d/%q", got.ParentSeq, got.ParentAuthorParticipantID, seq, andy)
	}
}

// You cannot open a reply thread on your own message: openConversationSubthread
// is the human's action, and a user/thread-owner message resolves to the "human"
// author — refused loudly (T3).
func TestOpenSubthreadOnOwnHumanMessageRefused(t *testing.T) {
	srv, groupID, _, _, _, _ := planFixture(t)

	seq, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "我的问题在这里", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append user message: %v", err)
	}
	anchorItemID, err := srv.mainStreamItemIDForSeq(groupID, seq)
	if err != nil {
		t.Fatalf("resolve anchor item id: %v", err)
	}

	_, err = srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
		CreatedBy:    "user",
	})
	if err == nil || !strings.Contains(err.Error(), "your own message") {
		t.Fatalf("opening a reply on the human's own message must be refused, got %v", err)
	}
	// The refusal must not leak a subthread.
	threads, err := session.ListConversationThreads(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListConversationThreads: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("refused open leaked %d subthread(s)", len(threads))
	}
}

// Agent escalate defaults the task lead to the parent message author when that
// author is a named agent (T3 point 5): the person whose message spawned the
// discussion leads the task it became.
func TestEscalateTaskLeadIsParentAuthorNamedAgent(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)

	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "登录链路我来定方向")
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	if cth.Status != session.ConversationThreadOpen {
		t.Fatalf("fresh reply status = %q, want open", cth.Status)
	}

	// Mia (not the parent author) escalates. The lead defaults to Andy, the
	// author of the message the reply hangs off of.
	if _, err := srv.residentTaskManager(mia).EscalateTask(context.Background(), cth.ID, "", false); err != nil {
		t.Fatalf("EscalateTask: %v", err)
	}
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.LeadParticipantID != andy {
		t.Fatalf("escalated task lead = %q, want parent author %q", got.LeadParticipantID, andy)
	}
}

// When the parent message author is the human (or otherwise not a named agent),
// escalation leaves the lead empty — the first planner claims it (T3/T6).
func TestEscalateTaskLeaderlessWhenParentAuthorIsHuman(t *testing.T) {
	srv, groupID, _, mia, _, _ := planFixture(t)

	// A reply bound to a human-authored parent (parent author == "human").
	cth, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID:                 groupID,
		AnchorItemID:              "grp-anchor-human",
		Title:                     "converge on the human's ask",
		Status:                    session.ConversationThreadOpen,
		ParentAuthorParticipantID: humanReactionParticipantID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}

	if _, err := srv.residentTaskManager(mia).EscalateTask(context.Background(), cth.ID, "", false); err != nil {
		t.Fatalf("EscalateTask: %v", err)
	}
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if strings.TrimSpace(got.LeadParticipantID) != "" {
		t.Fatalf("escalated task lead = %q, want empty (human parent author grants no lead)", got.LeadParticipantID)
	}
}

// You cannot escalate a reply on your own message: the escalating agent being
// the parent message author is refused (T3, the initiator == parent author
// path).
func TestEscalateTaskOnOwnMessageRefused(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)

	cth, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID:                 groupID,
		AnchorItemID:              "grp-anchor-andy",
		Title:                     "reply on Andy's own message",
		Status:                    session.ConversationThreadOpen,
		ParentAuthorParticipantID: andy,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}

	if _, err := srv.residentTaskManager(andy).EscalateTask(context.Background(), cth.ID, "", false); err == nil || !strings.Contains(err.Error(), "your own message") {
		t.Fatalf("escalating a reply on your own message must be refused, got %v", err)
	}
	// Still open — the refusal did not promote it.
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.Status != session.ConversationThreadOpen {
		t.Fatalf("refused escalate changed status to %q, want it left open", got.Status)
	}
}

// The human escalate RPC defaults the lead to the parent message author too
// (T3 point 5): no picked lead + a named parent author => that author leads.
func TestHumanEscalateLeadDefaultsToParentAuthor(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })

	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "这条我起的头")
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"esc","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"created_by":"user"}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "esc")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.LeadParticipantID != andy {
		t.Fatalf("human-escalated task lead = %q, want parent author %q (default when no lead picked)", view.LeadParticipantID, andy)
	}
}
