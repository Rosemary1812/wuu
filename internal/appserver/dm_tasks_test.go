package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

// DM tasks (task-rail design §7, 2026-07-07): the task rail opens to DM
// threads with the multi-party half removed — born owned by the DM's
// resident agent, no claim race, no ownerless wake broadcast, unclaim
// refused. Group behaviour is covered by the existing task tests; these
// pin the DM-specific semantics.

func dmTaskFixture(t *testing.T) (srv *Server, dmThreadID, ada, bea string) {
	t.Helper()
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv = New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada = saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea = saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	th, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	th.mu.Lock()
	dmThreadID = th.ID
	th.mu.Unlock()
	return srv, dmThreadID, ada, bea
}

func TestDMTaskBornOwnedFullChain(t *testing.T) {
	srv, dmThreadID, ada, bea := dmTaskFixture(t)
	manager := srv.residentTaskManager(ada)

	// create with claim=false: DM forces born-owned anyway (§7 claim 恒真).
	view, err := manager.CreateTask(context.Background(), dmThreadID, 0, "整理接口文档", false, "")
	if err != nil {
		t.Fatalf("CreateTask in DM: %v", err)
	}
	if view.Owner != ada {
		t.Fatalf("DM task must be born owned by the resident agent, owner=%q want %q", view.Owner, ada)
	}
	if view.Status != string(session.ConversationThreadTask) {
		t.Fatalf("DM task status = %q, want task", view.Status)
	}

	// No wake broadcast: a DM has no group members to call. Nobody's inbox
	// gains an envelope from this create (enqueue is synchronous, so an
	// empty inbox now is proof).
	for name, id := range map[string]string{"ada": ada, "bea": bea} {
		if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, id, 0); err != nil {
			t.Fatalf("PendingResidentEnvelopes %s: %v", name, err)
		} else if len(pending) != 0 {
			t.Fatalf("DM task create must wake nobody, %s has %d pending envelope(s)", name, len(pending))
		}
	}

	// list returns the DM's board.
	views, err := manager.ListTasks(context.Background(), dmThreadID)
	if err != nil {
		t.Fatalf("ListTasks in DM: %v", err)
	}
	if len(views) != 1 || views[0].ID != view.ID {
		t.Fatalf("DM board = %+v, want exactly the created task", views)
	}

	// unclaim refused: a DM task has exactly one possible executor.
	if _, err := manager.UnclaimTask(context.Background(), view.ID); err == nil {
		t.Fatal("unclaim of a DM task must be refused")
	} else if !strings.Contains(err.Error(), "DM") {
		t.Fatalf("unclaim refusal should say why (DM), got: %v", err)
	}

	// update_status files it for review, same state machine as groups.
	reviewed, err := manager.FileTaskReview(context.Background(), view.ID, "文档整理完,已核对接口清单")
	if err != nil {
		t.Fatalf("FileTaskReview in DM: %v", err)
	}
	if reviewed.Status != string(session.ConversationThreadReview) {
		t.Fatalf("DM task after update_status = %q, want review", reviewed.Status)
	}
}

func TestDMTaskBoardIsPrivateToItsResident(t *testing.T) {
	srv, dmThreadID, _, bea := dmTaskFixture(t)

	// Bea is not this DM's resident agent: create and list are both refused.
	outsider := srv.residentTaskManager(bea)
	if _, err := outsider.CreateTask(context.Background(), dmThreadID, 0, "偷塞任务", false, ""); err == nil {
		t.Fatal("another agent must not create tasks on someone else's DM board")
	}
	if _, err := outsider.ListTasks(context.Background(), dmThreadID); err == nil {
		t.Fatal("another agent must not list someone else's DM board")
	}
}
