package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

func newTaskWakeFixture(t *testing.T) (srv *Server, groupID, ada, bea string) {
	t.Helper()
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv = New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada = saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea = saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID = startNamedGroupThreadForTest(t, srv, "task-workflow").ID
	for _, participantID := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	return srv, groupID, ada, bea
}

func TestAgentOpenThreadUsesRealAnchorAndStableOwner(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	seq, _ := appendMainStreamAgentMessage(t, srv, groupID, bea, "Bea 的方案")

	first, err := srv.residentTaskManager(ada).OpenThread(context.Background(), groupID, seq, "收敛方案")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != string(session.ConversationThreadOpen) || first.ThreadOwner != bea {
		t.Fatalf("opened Thread = %+v, want open owned by parent author %q", first, bea)
	}
	history := waitForResidentDMHistoryContains(t, srv, bea, "You own Thread")
	if !strings.Contains(historyUserContent(history), first.ID) {
		t.Fatalf("owner wake does not point to Thread %q: %s", first.ID, historyUserContent(history))
	}
	second, err := srv.residentTaskManager(ada).OpenThread(context.Background(), groupID, seq, "换一个标题")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ThreadOwner != bea {
		t.Fatalf("idempotent open changed identity/owner: first=%+v second=%+v", first, second)
	}
}

func TestAgentOpenThreadOnHumanMessageMakesCallerOwner(t *testing.T) {
	srv, groupID, ada, _ := newTaskWakeFixture(t)
	seq, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "请先讨论清楚", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := srv.residentTaskManager(ada).OpenThread(context.Background(), groupID, seq, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.ThreadOwner != ada {
		t.Fatalf("human-message Thread owner = %q, want caller %q", view.ThreadOwner, ada)
	}
}

func TestAgentOpenThreadKeepsSameTurnPostInsideThread(t *testing.T) {
	srv, groupID, ada, _ := newTaskWakeFixture(t)
	seq, err := session.AppendHistoryRecordReturningSeq(srv.rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "请先讨论清楚", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	speech := srv.residentParticipantSpeechForTurn(ada, nil, map[string]bool{groupID: true}, nil, nil)
	opened, err := srv.residentTaskManager(ada).OpenThread(context.Background(), groupID, seq, "收敛方案")
	if err != nil {
		t.Fatal(err)
	}
	posted, err := speech.PostMessage(context.Background(), "brief", "建议按这个方向收敛。", groupID, seq, false)
	if err != nil {
		t.Fatal(err)
	}
	if posted.ThreadID != opened.ID {
		t.Fatalf("same-turn post landed in %q, want opened Thread %q", posted.ThreadID, opened.ID)
	}
}

func TestAgentCannotCreateStandaloneThreadOrTask(t *testing.T) {
	srv, groupID, ada, _ := newTaskWakeFixture(t)
	if _, err := srv.residentTaskManager(ada).OpenThread(context.Background(), groupID, 0, "standalone"); err == nil || !strings.Contains(err.Error(), "anchor_seq is required") {
		t.Fatalf("standalone open = %v, want anchor refusal", err)
	}
}

func TestOnlyThreadOwnerPromotesAndListShowsBothPhases(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	seq, _ := appendMainStreamAgentMessage(t, srv, groupID, ada, "Ada 的方案")
	opened, err := srv.residentTaskManager(bea).OpenThread(context.Background(), groupID, seq, "收敛方案")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.residentTaskManager(bea).PromoteThread(context.Background(), opened.ID, "执行方案"); err == nil || !strings.Contains(err.Error(), "only its owner") {
		t.Fatalf("non-owner promote = %v, want owner refusal", err)
	}
	promoted, err := srv.residentTaskManager(ada).PromoteThread(context.Background(), opened.ID, "执行方案")
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ID != opened.ID || promoted.Lead != ada || promoted.ThreadOwner != ada {
		t.Fatalf("promoted Task = %+v", promoted)
	}
	seq2, _ := appendMainStreamAgentMessage(t, srv, groupID, bea, "另一个讨论")
	if _, err := srv.residentTaskManager(bea).OpenThread(context.Background(), groupID, seq2, "继续讨论"); err != nil {
		t.Fatal(err)
	}
	views, err := srv.residentTaskManager(ada).ListWorkflowThreads(context.Background(), groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("workflow list = %+v, want one open Thread and one Task", views)
	}
}

func historyUserContent(history []session.HistoryRecord) string {
	var parts []string
	for _, rec := range history {
		if rec.Role == "user" && strings.TrimSpace(rec.Content) != "" {
			parts = append(parts, rec.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
