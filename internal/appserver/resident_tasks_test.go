package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

// Task-board wake (task-rail design §6): a task landing on the board with no
// owner must wake the group's members, or a task created while nobody is
// mid-turn sits unclaimed forever. These tests drive the resident task
// manager directly (the manage_task backend) and observe the wake through
// the same DM-history lens as the router tests.

func newTaskWakeFixture(t *testing.T) (srv *Server, groupID, ada, bea string) {
	t.Helper()
	// Empty provider response: a woken resident produces no final answer, so
	// no fallback reply cascades and the test observes only the wake routing.
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv = New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada = saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea = saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID = startNamedGroupThreadForTest(t, srv, "task-wake").ID
	for _, participantID := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	return srv, groupID, ada, bea
}

func TestTaskBoardWakeOnBornOpenCreate(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)

	view, err := srv.residentTaskManager(ada).CreateTask(context.Background(), groupID, 0, "fix login flake", false)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Bea is woken by a from="system" board envelope pointing at the task cth.
	history := waitForResidentDMHistoryContains(t, srv, bea, `from="system"`)
	meta := findEnvelopeMetaRecord(t, history)
	if meta.SourceThreadID != groupID || meta.SourceSubthreadID != view.ID {
		t.Fatalf("wake envelope meta = %+v, want source=%s subthread=%s", meta, groupID, view.ID)
	}
	if meta.Addressed {
		t.Fatalf("board wake must not be addressed (claim-or-silence governs), meta=%+v", meta)
	}
	if meta.SenderParticipantID != ada {
		t.Fatalf("wake provenance = %q, want creator %q", meta.SenderParticipantID, ada)
	}
	joined := historyUserContent(history)
	for _, want := range []string{`sender="task board"`, "fix login flake", view.ID, "action=claim"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("wake envelope content missing %q:\n%s", want, joined)
		}
	}

	// The creator dispatched the task; it is not woken by its own board write.
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, ada, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Ada: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("creator was woken by its own task create: %d pending envelope(s)", len(pending))
	}
	if _, adaHistory := findDMHistoryIfExists(t, srv, ada); len(adaHistory) != 0 {
		t.Fatalf("creator got a DM turn from its own task create: %+v", adaHistory)
	}
}

func TestTaskBoardWakeSkippedWhenBornOwnedThenFiresOnUnclaim(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	manager := srv.residentTaskManager(ada)

	view, err := manager.CreateTask(context.Background(), groupID, 0, "self-owned refactor", true)
	if err != nil {
		t.Fatalf("CreateTask claim=true: %v", err)
	}
	// Wake routing enqueues synchronously inside CreateTask, so an empty inbox
	// now is proof a born-owned task woke nobody.
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, bea, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Bea: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("born-owned task must wake nobody, Bea has %d pending envelope(s)", len(pending))
	}
	if _, beaHistory := findDMHistoryIfExists(t, srv, bea); len(beaHistory) != 0 {
		t.Fatalf("born-owned task started a DM turn for Bea: %+v", beaHistory)
	}

	// Releasing ownership puts the task back on the board ownerless — that
	// transition is the same failure mode as born-open and wakes the group.
	if _, err := manager.UnclaimTask(context.Background(), view.ID); err != nil {
		t.Fatalf("UnclaimTask: %v", err)
	}
	history := waitForResidentDMHistoryContains(t, srv, bea, "released back to the board")
	meta := findEnvelopeMetaRecord(t, history)
	if meta.SourceSubthreadID != view.ID {
		t.Fatalf("unclaim wake points at %q, want task %q", meta.SourceSubthreadID, view.ID)
	}
}

func TestTaskBoardWakeOnBornOpenEscalate(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)

	discussion, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID:    groupID,
		AnchorItemID: "seq-1",
		Title:        "改造落地验收",
		Status:       session.ConversationThreadOpen,
		CreatedBy:    ada,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}

	view, err := srv.residentTaskManager(ada).EscalateTask(context.Background(), discussion.ID, "", false)
	if err != nil {
		t.Fatalf("EscalateTask: %v", err)
	}

	history := waitForResidentDMHistoryContains(t, srv, bea, `from="system"`)
	meta := findEnvelopeMetaRecord(t, history)
	if meta.SourceSubthreadID != view.ID {
		t.Fatalf("escalate wake points at %q, want task %q", meta.SourceSubthreadID, view.ID)
	}
	if !strings.Contains(historyUserContent(history), "改造落地验收") {
		t.Fatalf("escalate wake should carry the task title, history:\n%s", historyUserContent(history))
	}
}

// historyUserContent joins the raw (model-visible) user-record contents of a
// DM history, for asserting on rendered envelope text.
func historyUserContent(history []session.HistoryRecord) string {
	var parts []string
	for _, rec := range history {
		if rec.Role == "user" && strings.TrimSpace(rec.Content) != "" {
			parts = append(parts, rec.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
