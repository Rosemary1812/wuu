package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// The Task workflow's two channels, pinned end to end:
//   - public_thread_message: a post_message kind=update to the task thread is
//     progress for the human — it wakes NO teammate.
//   - handoff payload: the structured result attached to manage_task
//     action=piece_done is the ONLY thing that wakes the downstream node, and
//     it arrives in that node's wake context.
// The two are never mixed.

// pieceHandoff returns the stored (marshaled) handoff on a plan piece, loading a
// fresh view of the task so it reflects what the engine persisted.
func pieceHandoff(t *testing.T, srv *Server, taskID, pieceID string) string {
	t.Helper()
	thread, err := session.FindConversationThreadByID(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	for _, p := range thread.Plan {
		if p.ID == pieceID {
			return p.Handoff
		}
	}
	t.Fatalf("task %q has no piece %q", taskID, pieceID)
	return ""
}

func findTaskEvent(t *testing.T, srv *Server, taskID, kind string) (session.TaskEvent, bool) {
	t.Helper()
	events, err := session.TaskEvents(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	for _, ev := range events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return session.TaskEvent{}, false
}

// A public progress update posted into a task thread wakes no teammate: the
// wake-gating two-channel contract. An explicit @mention inside such an update
// stays the escape hatch that DOES wake the named member.
func TestTaskPublicUpdateWakesNoTeammate(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "两步任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	// Mia's p1 is dispatched; Han's p2 is gated on p1 but is already a cth member
	// (set_plan pulls every assignee onto the team), so he is exactly the
	// "other member" a public update must not wake.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "块一", Assignee: mia, Prompt: "做块一"},
		{ID: "p2", Title: "块二", Assignee: han, Prompt: "做块二", DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "块一")
	waitForResidentQuiesce(t, srv)

	// Mia posts PUBLIC progress into the task thread.
	if posted, err := srv.residentParticipantSpeech(mia).PostMessage(
		context.Background(), "update", "进展一半PUBLICPROGRESS", task.ID, 0, false,
	); err != nil {
		t.Fatalf("public update PostMessage: %v", err)
	} else if posted.Held {
		t.Fatalf("public update should post, not hold: %+v", posted)
	}
	waitForResidentQuiesce(t, srv)

	// Han (gated cth member, not @'d) is not woken: no pending envelope and no
	// DM turn carrying the update text.
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, han, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Han: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("public update must not push to a teammate: Han has %d pending", len(pending))
	}
	if hanDMContains(t, srv, han, "PUBLICPROGRESS") {
		t.Fatal("public update must not wake a teammate into a DM turn")
	}

	// Red line §4.5 covers every plain participant post inside a task, not just
	// kind=update: a non-@ result or question to the task thread is public
	// progress too and must not wake a teammate either.
	if _, err := srv.residentParticipantSpeech(mia).PostMessage(
		context.Background(), "result", "阶段结论RESULTNOWAKE", task.ID, 0, false,
	); err != nil {
		t.Fatalf("public result PostMessage: %v", err)
	}
	if _, err := srv.residentParticipantSpeech(mia).PostMessage(
		context.Background(), "question", "一个广播问题QUESTIONNOWAKE", task.ID, 0, false,
	); err != nil {
		t.Fatalf("public question PostMessage: %v", err)
	}
	waitForResidentQuiesce(t, srv)
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, han, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Han after result/question: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("public result/question must not push to a teammate: Han has %d pending", len(pending))
	}
	if hanDMContains(t, srv, han, "RESULTNOWAKE") || hanDMContains(t, srv, han, "QUESTIONNOWAKE") {
		t.Fatal("a non-@ result/question in a task thread must not wake a teammate")
	}

	// The @mention escape hatch still wakes the named member on the same path.
	if _, err := srv.residentParticipantSpeech(mia).PostMessage(
		context.Background(), "update", "@Han 需要你先看一下ATMENTIONWAKE", task.ID, 0, false,
	); err != nil {
		t.Fatalf("mention update PostMessage: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, han, "ATMENTIONWAKE")
}

// hanDMContains reports whether a resident's DM history carries substr in a user
// message (the shape a wake envelope takes once drained into the DM turn).
func hanDMContains(t *testing.T, srv *Server, participantID, substr string) bool {
	t.Helper()
	dm, err := srv.ensureResidentDMThread(participantID)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	dm.mu.Lock()
	dmID := dm.ID
	dm.mu.Unlock()
	recs, err := session.LoadHistoryRecords(srv.rt.SessionDir, dmID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	return strings.Contains(historyUserContent(recs), substr)
}

// piece_done carries a structured handoff to the downstream node: it is written
// onto every dependent piece, rendered into that node's wake, and recorded as a
// handoff_created trace event — and this is the ONLY thing that wakes the
// downstream assignee.
func TestTaskHandoffWakesDownstream(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "接力任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "调研上游", Assignee: mia, Prompt: "做调研"},
		{ID: "p2", Title: "落地下游", Assignee: han, Prompt: "按调研落地", DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "调研上游")

	handoff := &tools.TaskHandoff{
		Done:       "调研完成,选定方案A",
		Findings:   "现有接口缺一个字段",
		Artifacts:  []string{"docs/research.md", "notes/api.md"},
		Limits:     "未验证并发",
		NextGoal:   "按方案A落地下游模块NEXTGOALMARK",
		Acceptance: "端到端用例通过ACCEPTMARK",
		Notes:      "注意兼容旧客户端",
	}
	afterP1, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1", handoff)
	if err != nil {
		t.Fatalf("Mia piece_done p1 with handoff: %v", err)
	}
	if got := pieceStatus(afterP1, "p2"); got != session.TaskPieceActive {
		t.Fatalf("p2 status = %q after p1 handoff, want active (auto-dispatched)", got)
	}

	// Han (the downstream assignee) is woken AND his wake carries the handoff's
	// next_goal — the handoff is his real input, arriving in-context.
	hist := waitForResidentDMHistoryContains(t, srv, han, "NEXTGOALMARK")
	wake := historyUserContent(hist)
	if !strings.Contains(wake, "ACCEPTMARK") {
		t.Fatalf("Han's wake should carry the handoff acceptance; got:\n%s", wake)
	}
	if !strings.Contains(wake, "docs/research.md") {
		t.Fatalf("Han's wake should carry the handoff artifacts; got:\n%s", wake)
	}
	if !strings.Contains(wake, "按调研落地") {
		t.Fatalf("Han's wake should carry his own node prompt; got:\n%s", wake)
	}

	// The downstream node's stored Handoff equals the marshaled payload.
	want, err := session.MarshalTaskHandoff(session.TaskHandoff{
		Done:       handoff.Done,
		Findings:   handoff.Findings,
		Artifacts:  handoff.Artifacts,
		Limits:     handoff.Limits,
		NextGoal:   handoff.NextGoal,
		Acceptance: handoff.Acceptance,
		Notes:      handoff.Notes,
	})
	if err != nil {
		t.Fatalf("MarshalTaskHandoff: %v", err)
	}
	if got := pieceHandoff(t, srv, task.ID, "p2"); got != want {
		t.Fatalf("downstream p2 stored handoff mismatch\n got: %s\nwant: %s", got, want)
	}
	// The upstream node itself is NOT written when it has a downstream.
	if got := pieceHandoff(t, srv, task.ID, "p1"); got != "" {
		t.Fatalf("upstream p1 must not store the handoff when a downstream exists, got: %s", got)
	}

	// The trace records handoff_created with the structured payload.
	ev, ok := findTaskEvent(t, srv, task.ID, session.TaskEventHandoffCreated)
	if !ok {
		t.Fatal("expected a handoff_created trace event")
	}
	if ev.NodeID != "p1" || ev.Actor != mia {
		t.Fatalf("handoff_created event node/actor = %q/%q, want p1/%s", ev.NodeID, ev.Actor, mia)
	}
	if !strings.Contains(ev.Payload, "NEXTGOALMARK") || !strings.Contains(ev.Payload, "ACCEPTMARK") {
		t.Fatalf("handoff_created payload should contain next_goal/acceptance; got: %s", ev.Payload)
	}
}

// A terminal piece (nothing depends on it) stores its handoff on the piece
// itself, for the lead's final wrap-up and the trace.
func TestTaskTerminalHandoffStoredOnPiece(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "单节点任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "only", Title: "唯一节点", Assignee: mia, Prompt: "做唯一节点"},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "唯一节点")

	handoff := &tools.TaskHandoff{
		Done:     "全部完成",
		NextGoal: "无下游TERMINALGOAL",
	}
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "only", handoff); err != nil {
		t.Fatalf("piece_done only with handoff: %v", err)
	}

	want, err := session.MarshalTaskHandoff(session.TaskHandoff{Done: handoff.Done, NextGoal: handoff.NextGoal})
	if err != nil {
		t.Fatalf("MarshalTaskHandoff: %v", err)
	}
	if got := pieceHandoff(t, srv, task.ID, "only"); got != want {
		t.Fatalf("terminal piece must store its own handoff\n got: %s\nwant: %s", got, want)
	}
	if _, ok := findTaskEvent(t, srv, task.ID, session.TaskEventHandoffCreated); !ok {
		t.Fatal("terminal handoff should still record handoff_created")
	}
}
