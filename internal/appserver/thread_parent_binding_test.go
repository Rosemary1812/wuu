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
	if cth.ThreadOwnerParticipantID != andy {
		t.Fatalf("cth owner = %q, want anchored named author %q", cth.ThreadOwnerParticipantID, andy)
	}
	if cth.CreatedBy != humanReactionParticipantID {
		t.Fatalf("cth created_by = %q, want human provenance", cth.CreatedBy)
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

func TestOpenSubthreadUsesParentSeqAcrossLiveItemIDChanges(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)
	seq, canonicalAnchor := appendMainStreamAgentMessage(t, srv, groupID, andy, "稳定绑定")

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "live-turn-item-1",
		ParentSeq:    seq,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	if cth.AnchorItemID != canonicalAnchor || cth.ParentSeq != seq {
		t.Fatalf("binding = %q/%d, want %q/%d", cth.AnchorItemID, cth.ParentSeq, canonicalAnchor, seq)
	}
}

func TestOpenSubthreadRejectsNonMessageAnchor(t *testing.T) {
	srv, groupID, _, _, _, _ := planFixture(t)
	if _, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "start turn", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "assistant", Content: "internal assistant output", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	records, err := loadPersistedMessages(srv.rt.SessionDir, groupID, false)
	if err != nil {
		t.Fatal(err)
	}
	turns := turnsFromPersistedHistoryInScope(
		groupID, "", records, time.Now().UTC(), srv.resolveParticipantSummary,
	)
	var item ThreadItem
	for _, turn := range turns {
		for _, candidate := range turn.Items {
			if candidate.Type == ThreadItemAgentMessage {
				item = candidate
			}
		}
	}
	if item.ID == "" {
		t.Fatalf("fixture rendered no agent message: %+v", turns)
	}
	_, err = srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID: groupID, AnchorItemID: item.ID,
	})
	if err == nil || !strings.Contains(err.Error(), "does not resolve to a visible main-stream message") {
		t.Fatalf("non-message anchor must be rejected, got %v", err)
	}
}

func TestOpenSubthreadRejectsNamedParentAfterLeavingGroup(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)
	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "离组前的消息")
	if err := session.RemoveThreadMember(srv.rt.SessionDir, groupID, andy); err != nil {
		t.Fatalf("RemoveThreadMember: %v", err)
	}

	_, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
	})
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("departed named parent must be rejected, got %v", err)
	}
}

// A human-authored parent has no implied named owner. Opening requires an
// explicit active named group member, which becomes the reply owner.
func TestOpenSubthreadOnHumanMessageRequiresExplicitOwner(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)

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
	})
	if err == nil || !strings.Contains(err.Error(), "thread_owner_participant_id is required") {
		t.Fatalf("human parent without explicit owner must be refused, got %v", err)
	}

	threads, err := session.ListConversationThreads(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListConversationThreads: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("refused open leaked %d subthread(s)", len(threads))
	}

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:                 groupID,
		AnchorItemID:             anchorItemID,
		ThreadOwnerParticipantID: andy,
	})
	if err != nil {
		t.Fatalf("open human-parent thread with explicit owner: %v", err)
	}
	if cth.ThreadOwnerParticipantID != andy || cth.ParentAuthorParticipantID != humanReactionParticipantID {
		t.Fatalf("unexpected human-parent binding: %+v", cth)
	}
}

// A named agent may promote only a reply it owns. Promotion keeps the same
// thread and makes that owner the task lead.
func TestEscalateTaskLeadIsParentAuthorNamedAgent(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)

	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "登录链路我来定方向")
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	if cth.Status != session.ConversationThreadOpen {
		t.Fatalf("fresh reply status = %q, want open", cth.Status)
	}

	if _, err := srv.residentTaskManager(mia).PromoteThread(context.Background(), cth.ID, ""); err == nil || !strings.Contains(err.Error(), "only its owner may promote") {
		t.Fatalf("non-owner promotion must be refused, got %v", err)
	}
	if _, err := srv.residentTaskManager(andy).PromoteThread(context.Background(), cth.ID, ""); err != nil {
		t.Fatalf("owner EscalateTask: %v", err)
	}
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.LeadParticipantID != andy {
		t.Fatalf("escalated task lead = %q, want parent author %q", got.LeadParticipantID, andy)
	}
}

// A human-authored parent uses its explicitly selected named owner as the task
// lead when that owner promotes it.
func TestEscalateHumanParentTaskUsesExplicitOwnerAsLead(t *testing.T) {
	srv, groupID, _, mia, _, _ := planFixture(t)

	seq, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "请收敛这个问题", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append user message: %v", err)
	}
	anchorItemID, err := srv.mainStreamItemIDForSeq(groupID, seq)
	if err != nil {
		t.Fatalf("resolve anchor item id: %v", err)
	}
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:                 groupID,
		AnchorItemID:             anchorItemID,
		ThreadOwnerParticipantID: mia,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	if _, err := srv.residentTaskManager(mia).PromoteThread(context.Background(), cth.ID, ""); err != nil {
		t.Fatalf("EscalateTask: %v", err)
	}
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.LeadParticipantID != mia {
		t.Fatalf("escalated task lead = %q, want explicit owner %q", got.LeadParticipantID, mia)
	}
}

// The owner is expected to promote the reply anchored to their message.
func TestEscalateTaskOwnerMayPromoteOwnThread(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)

	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "我来负责这个 thread")
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	if _, err := srv.residentTaskManager(andy).PromoteThread(context.Background(), cth.ID, ""); err != nil {
		t.Fatalf("owner promotion failed: %v", err)
	}
	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.Status != session.ConversationThreadTask || got.LeadParticipantID != andy {
		t.Fatalf("owner promotion produced unexpected task: %+v", got)
	}
}

// The named parent author owns the Thread; human promotion copies that owner
// into the task lead without accepting a separate lead choice.
func TestHumanEscalateUsesThreadOwnerAsLead(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })

	_, anchorItemID := appendMainStreamAgentMessage(t, srv, groupID, andy, "这条我起的头")
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: anchorItemID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"esc","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "esc")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.LeadParticipantID != andy {
		t.Fatalf("human-escalated task lead = %q, want Thread owner %q", view.LeadParticipantID, andy)
	}
}
