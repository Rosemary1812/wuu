package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestTaskConclusionSignalUsesFirstSubstantiveParagraphAndCapsLength(t *testing.T) {
	long := "# 调查结果\n\n" + strings.Repeat("结论成立，", 80) + "\n\n后续完整证据"
	got := taskConclusionSignal(long)
	if strings.Contains(got, "调查结果") || strings.Contains(got, "后续完整证据") {
		t.Fatalf("signal should contain only the first substantive paragraph: %q", got)
	}
	if utf8.RuneCountInString(got) > maxGroupBriefRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("signal was not capped to %d runes: %d %q", maxGroupBriefRunes, utf8.RuneCountInString(got), got)
	}
}

// Task state machine + auto-execution on escalation (T4): upgrading a reply
// to a task IS the start of execution. No user approval sits anywhere on the
// path escalate -> lead plans -> engine dispatches -> pieces done -> exec
// completed -> conclusion filed -> resolved. These tests pin the ExecState
// axis (planning/executing/needs_human/completed) and the conclude-completes
// semantics that replaced the review gate.

func taskEventKinds(t *testing.T, sessDir, taskID string) map[string]session.TaskEvent {
	t.Helper()
	events, err := session.TaskEvents(sessDir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	byKind := map[string]session.TaskEvent{}
	for _, ev := range events {
		byKind[ev.Kind] = ev
	}
	return byKind
}

// Human escalate RPC with a named lead: the cth becomes a task in exec state
// planning, the trace records task_created + lead_invoked, and the lead is
// woken with the planning directive. Nothing needs a further human RPC
// before execution — the woken lead can set_plan immediately.
func TestHumanEscalateEntersPlanningAndWakesLead(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)

	cth, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID: groupID, AnchorItemID: "seq-3", ParentSeq: 3,
		ParentAuthorParticipantID: andy, ThreadOwnerParticipantID: andy,
		Title: "收敛完的讨论", CreatedBy: humanReactionParticipantID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"esc","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"title":"改造登录链路"}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "esc")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.Status != string(session.ConversationThreadTask) {
		t.Fatalf("subthread status = %q, want task", view.Status)
	}
	if view.ExecState != session.ExecStatePlanning {
		t.Fatalf("exec_state = %q, want planning (escalation starts execution)", view.ExecState)
	}

	got, err := session.FindConversationThreadByID(srv.rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if got.Status != session.ConversationThreadTask || got.ExecState != session.ExecStatePlanning {
		t.Fatalf("stored task = status %q exec %q, want task/planning", got.Status, got.ExecState)
	}

	events := taskEventKinds(t, srv.rt.SessionDir, cth.ID)
	if _, ok := events[session.TaskEventTaskCreated]; !ok {
		t.Fatalf("trace missing task_created, have %v", events)
	}
	invoked, ok := events[session.TaskEventLeadInvoked]
	if !ok {
		t.Fatalf("trace missing lead_invoked, have %v", events)
	}
	if invoked.Actor != andy {
		t.Fatalf("lead_invoked actor = %q, want lead %q", invoked.Actor, andy)
	}

	// The lead's DM carries the planning directive: author the plan with
	// set_plan against this cth. This wake is the whole approval story —
	// there is no pending human step.
	hist := waitForResidentDMHistoryContains(t, srv, andy, "set_plan")
	joined := historyUserContent(hist)
	if !strings.Contains(joined, cth.ID) {
		t.Fatalf("planning directive should name the task cth %q:\n%s", cth.ID, joined)
	}
	if !strings.Contains(joined, "prompt") {
		t.Fatalf("planning directive should teach the per-piece prompt field:\n%s", joined)
	}
	for _, want := range []string{"Promotion does not broaden authorization", "investigate-only never becomes implementation", "smallest outcome-shaped graph"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("planning directive should preserve task authorization %q:\n%s", want, joined)
		}
	}

	// Proof of no approval gate: the lead can declare the plan right now and
	// the engine dispatches without any further human RPC.
	if _, err := srv.residentTaskManager(andy).SetPlan(context.Background(), cth.ID, []tools.TaskPiece{
		{ID: "p1", Title: "登录链路排查", Assignee: mia, Prompt: "从会话续期入手"},
	}); err != nil {
		t.Fatalf("SetPlan right after escalation must be allowed: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "登录链路排查")
}

// Agent promotion (manage_task action=promote): the Thread owner promotes it,
// becomes lead, and receives the same planning wake as the human path.
func TestAgentEscalateEntersPlanningAndTraces(t *testing.T) {
	srv, groupID, andy, _, _, _ := planFixture(t)

	discussion, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID: groupID, AnchorItemID: "seq-8", ParentSeq: 8,
		ParentAuthorParticipantID: andy, ThreadOwnerParticipantID: andy,
		Title: "验收遗留问题", CreatedBy: humanReactionParticipantID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}

	view, err := srv.residentTaskManager(andy).PromoteThread(context.Background(), discussion.ID, "")
	if err != nil {
		t.Fatalf("EscalateTask: %v", err)
	}
	if view.ExecState != session.ExecStatePlanning {
		t.Fatalf("exec_state = %q, want planning", view.ExecState)
	}
	events := taskEventKinds(t, srv.rt.SessionDir, view.ID)
	created, ok := events[session.TaskEventTaskCreated]
	if !ok {
		t.Fatalf("agent escalate must record task_created, have %v", events)
	}
	if created.Actor != andy {
		t.Fatalf("task_created actor = %q, want escalating agent %q", created.Actor, andy)
	}
	if invoked, ok := events[session.TaskEventLeadInvoked]; !ok || invoked.Actor != andy {
		t.Fatalf("agent promotion must wake the Thread owner as lead: %v", events)
	}
}

// Lead set_plan with per-piece prompts: exec state flips to executing, the
// first ready piece is dispatched automatically, and the stored plan carries
// the prompts.
func TestSetPlanWithPromptExecutesAndDispatches(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)

	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "带简报的团队任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	view, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "梳理推送协议", Assignee: han, Prompt: "从 host 派发层开始梳理,列出协议分支"},
		{ID: "p2", Title: "写落地文档", Assignee: mia, DependsOn: []string{"p1"}, Prompt: "基于 p1 的协议清单写文档"},
	})
	if err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if view.ExecState != session.ExecStateExecuting {
		t.Fatalf("exec_state after set_plan = %q, want executing", view.ExecState)
	}
	if got := pieceStatus(view, "p1"); got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want active (auto-dispatched)", got)
	}
	if got := pieceStatus(view, "p2"); got != session.TaskPiecePending {
		t.Fatalf("p2 status = %q, want pending (gated on p1)", got)
	}
	waitForResidentDMHistoryContains(t, srv, han, "梳理推送协议")

	stored, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if stored.ExecState != session.ExecStateExecuting {
		t.Fatalf("stored exec_state = %q, want executing", stored.ExecState)
	}
	if len(stored.Plan) != 2 || stored.Plan[0].Prompt != "从 host 派发层开始梳理,列出协议分支" || stored.Plan[1].Prompt != "基于 p1 的协议清单写文档" {
		t.Fatalf("stored plan must carry the lead-authored prompts: %+v", stored.Plan)
	}
}

// Every worker piece done -> awaiting_lead, with status still task. The lead's
// verified conclusion is the only transition to completed + resolved.
func TestAllPiecesDoneAwaitsLeadConclusion(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)

	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "两块并行的活")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "块一", Assignee: mia, Prompt: "做块一"},
		{ID: "p2", Title: "块二", Assignee: han, Prompt: "做块二"},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	mid, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1", nil)
	if err != nil {
		t.Fatalf("piece_done p1: %v", err)
	}
	if mid.Status != string(session.ConversationThreadTask) || mid.ExecState != session.ExecStateExecuting {
		t.Fatalf("mid-plan task = status %q exec %q, want task/executing", mid.Status, mid.ExecState)
	}
	if _, err := lead.ConcludeTask(context.Background(), task.ID, "too early"); err == nil || !strings.Contains(err.Error(), "not \"awaiting_lead\"") {
		t.Fatalf("live-node conclusion = %v, want awaiting-lead refusal", err)
	}

	final, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p2", nil)
	if err != nil {
		t.Fatalf("piece_done p2: %v", err)
	}
	if final.ExecState != session.ExecStateAwaitingLead {
		t.Fatalf("exec_state after all pieces done = %q, want awaiting_lead", final.ExecState)
	}
	if final.Status != string(session.ConversationThreadTask) {
		t.Fatalf("status after all pieces done = %q, want task (conclusion still pending)", final.Status)
	}
	concluded, err := lead.ConcludeTask(context.Background(), task.ID, "verified result")
	if err != nil {
		t.Fatalf("lead conclude: %v", err)
	}
	if concluded.Status != string(session.ConversationThreadResolved) || concluded.ExecState != session.ExecStateCompleted {
		t.Fatalf("concluded task = status %q exec %q, want resolved/completed", concluded.Status, concluded.ExecState)
	}
}

// ConcludeTask (manage_task conclude): filing the conclusion resolves
// the task immediately, bubbles a short result signal to the parent main stream
// under the caller's identity, and records the complete task result. A caller that is
// any non-lead is refused; the immutable lead concludes.
func TestConcludeTaskResolvesAndBubbles(t *testing.T) {
	srv, groupID, _, mia, han, _ := planFixture(t)

	owned, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID: groupID, AnchorItemID: "owner-conclude", ParentSeq: 1,
		ParentAuthorParticipantID: mia, ThreadOwnerParticipantID: mia,
		Title: "lead 收束的活", CreatedBy: humanReactionParticipantID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	owned, err = session.EscalateConversationThread(srv.rt.SessionDir, owned.ID, "user", "")
	if err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}
	// A non-lead is refused loudly.
	if _, err := srv.residentTaskManager(han).ConcludeTask(context.Background(), owned.ID, "不是我的活也想收"); err == nil {
		t.Fatal("a non-lead must not conclude")
	} else if !strings.Contains(err.Error(), "only the lead") {
		t.Fatalf("refusal should diagnose the caller, got %v", err)
	}

	markTaskReadyForConclusionForTest(t, srv, owned.ID, han)
	const fullConclusion = "结论：活干完了，已自测。\n\n**完整证据**\n" +
		"这里保留 Task 的详细结果、验证过程和后续注意事项，不应整段复制到群聊主流。"
	concluded, err := srv.residentTaskManager(mia).ConcludeTask(context.Background(), owned.ID, fullConclusion)
	if err != nil {
		t.Fatalf("ConcludeTask by lead: %v", err)
	}
	if concluded.Status != string(session.ConversationThreadResolved) {
		t.Fatalf("status after conclude = %q, want resolved immediately", concluded.Status)
	}
	if concluded.ExecState != session.ExecStateCompleted {
		t.Fatalf("exec_state after conclude = %q, want completed", concluded.ExecState)
	}

	// Only the conclusion's first paragraph bubbled to the parent's MAIN stream
	// (empty thread tag) under the owner's identity.
	history, err := session.LoadHistoryRecords(srv.rt.SessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var bubbled *session.HistoryRecord
	for i := range history {
		if history[i].Role == "participant" && strings.TrimSpace(history[i].ThreadID) == "" && history[i].Content == "结论：活干完了，已自测。" {
			bubbled = &history[i]
			break
		}
	}
	if bubbled == nil {
		t.Fatalf("conclusion not bubbled to the main stream: %+v", history)
	}
	if bubbled.ParticipantID != mia {
		t.Fatalf("bubbled conclusion author = %q, want owner %q", bubbled.ParticipantID, mia)
	}

	events := taskEventKinds(t, srv.rt.SessionDir, owned.ID)
	completed, ok := events[session.TaskEventTaskCompleted]
	if !ok {
		t.Fatalf("trace missing task_completed, have %v", events)
	}
	if completed.Actor != mia || completed.Summary != fullConclusion {
		t.Fatalf("task_completed = actor %q summary %q, want owner + conclusion", completed.Actor, completed.Summary)
	}
	stored, err := session.FindConversationThreadByID(srv.rt.SessionDir, owned.ID)
	if err != nil {
		t.Fatalf("reload concluded Task: %v", err)
	}
	if stored.Summary != fullConclusion {
		t.Fatalf("Task lost full conclusion: %q", stored.Summary)
	}
}

func TestConcludeTaskByImmutableLead(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)

	// A promoted Thread carries the same immutable owner and lead.
	task, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID: groupID, AnchorItemID: "anchor-andy", ParentSeq: 1,
		ParentAuthorParticipantID: andy, ThreadOwnerParticipantID: andy,
		Title: "无主的团队任务", CreatedBy: humanReactionParticipantID,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if _, err := session.EscalateConversationThread(srv.rt.SessionDir, task.ID, "user", ""); err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}

	markTaskReadyForConclusionForTest(t, srv, task.ID, mia)
	concluded, err := srv.residentTaskManager(andy).ConcludeTask(context.Background(), task.ID, "全部节点收束完毕")
	if err != nil {
		t.Fatalf("lead conclude: %v", err)
	}
	if concluded.Status != string(session.ConversationThreadResolved) {
		t.Fatalf("status = %q, want resolved", concluded.Status)
	}
}

// need_human is the explicit human-decision point: exec state needs_human
// plus a blocked trace event carrying the reason payload. It wakes nobody.
func TestNeedHumanFlagsTaskForTheHuman(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)

	task, err := createPromotedTaskForTest(context.Background(), srv.residentTaskManager(mia), groupID, "要人拍板的事")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := srv.residentTaskManager(mia).NeedHuman(context.Background(), task.ID, "  "); err == nil {
		t.Fatal("need_human without a reason must be refused")
	}

	view, err := srv.residentTaskManager(mia).NeedHuman(context.Background(), task.ID, "预算超了,要用户拍板")
	if err != nil {
		t.Fatalf("NeedHuman: %v", err)
	}
	if view.ExecState != session.ExecStateNeedsHuman {
		t.Fatalf("exec_state = %q, want needs_human", view.ExecState)
	}
	stored, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID: %v", err)
	}
	if stored.ExecState != session.ExecStateNeedsHuman {
		t.Fatalf("stored exec_state = %q, want needs_human", stored.ExecState)
	}

	events := taskEventKinds(t, srv.rt.SessionDir, task.ID)
	blocked, ok := events[session.TaskEventBlocked]
	if !ok {
		t.Fatalf("trace missing blocked event, have %v", events)
	}
	if !strings.Contains(blocked.Payload, `"reason"`) || !strings.Contains(blocked.Payload, "预算超了,要用户拍板") {
		t.Fatalf("blocked payload should carry the reason JSON, got %q", blocked.Payload)
	}

	// Wakes nobody: the flag is for the human's board, not for teammates.
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, andy, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("need_human must wake nobody, andy has %d pending envelope(s)", len(pending))
	}
}
