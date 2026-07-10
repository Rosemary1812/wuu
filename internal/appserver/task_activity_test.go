package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Activity vs progress (plan §T9): a node's two liveness signals are kept
// strictly apart. Activity ("still alive") is any observable action by the
// assignee — a tool call. Progress ("real headway") is a declared step forward —
// a filed handoff, a public task-thread update. Stall/liveness is a relative
// judgement over the two timestamps, never a fixed lease (red line §4.7). These
// tests pin the wiring: tool-call activity moves only LastActivityAt; piece_done
// and a public update move LastProgressAt (and activity); the final lead summary
// reaches the parent stream, records task_completed, and wakes no teammate.

func reloadPiece(t *testing.T, srv *Server, taskID, pieceID string) session.TaskPiece {
	t.Helper()
	thread, err := session.FindConversationThreadByID(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID %q: %v", taskID, err)
	}
	for _, p := range thread.Plan {
		if p.ID == pieceID {
			return p
		}
	}
	t.Fatalf("task %q has no piece %q", taskID, pieceID)
	return session.TaskPiece{}
}

func hasTaskEvent(t *testing.T, srv *Server, taskID, kind, summarySub string) bool {
	t.Helper()
	events, err := session.TaskEvents(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents %q: %v", taskID, err)
	}
	for _, ev := range events {
		if ev.Kind == kind && (summarySub == "" || strings.Contains(ev.Summary, summarySub)) {
			return true
		}
	}
	return false
}

func pendingCount(t *testing.T, srv *Server, participantID string) int {
	t.Helper()
	pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("PendingResidentEnvelopes %s: %v", participantID, err)
	}
	return len(pending)
}

func mainStreamHasParticipantPost(t *testing.T, srv *Server, threadID, substr string) bool {
	t.Helper()
	recs, err := session.ChatMessagesSince(srv.rt.SessionDir, threadID, 0)
	if err != nil {
		t.Fatalf("ChatMessagesSince %q: %v", threadID, err)
	}
	for _, rec := range recs {
		if strings.TrimSpace(rec.ThreadID) == "" && strings.Contains(rec.Content, substr) {
			return true
		}
	}
	return false
}

// noteNodeActivity moves ONLY LastActivityAt, never LastProgressAt, and records a
// tool_call trace. The executing node is resolved once from a synthesized
// plan-dispatch batch — the same "task plan" system envelope the engine wakes an
// assignee with (reuse of T8's mechanism).
func TestNoteNodeActivityRefreshesActivityNotProgress(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "调研")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "查资料", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	// Dispatch stamped p1 active with a LastActivityAt but no progress. Pin the
	// activity baseline an hour back so the refresh is unambiguously newer.
	old := time.Now().Add(-time.Hour).UTC()
	if _, err := session.UpdateTaskPiece(srv.rt.SessionDir, task.ID, "p1", func(p *session.TaskPiece) {
		p.LastActivityAt = old
	}); err != nil {
		t.Fatalf("pin baseline: %v", err)
	}

	envs := []MessageEnvelope{taskPlanDispatchEnv(t, srv, task.ID, mia)}
	gotTask, gotPiece, ok := srv.resolveExecutingNode(mia, envs)
	if !ok || gotTask != task.ID || gotPiece != "p1" {
		t.Fatalf("resolveExecutingNode = (%q, %q, %v), want (%q, p1, true)", gotTask, gotPiece, ok, task.ID)
	}

	srv.noteNodeActivity(gotTask, gotPiece, session.TaskEventToolCall, "read_file")

	piece := reloadPiece(t, srv, task.ID, "p1")
	if !piece.LastActivityAt.After(old) {
		t.Fatalf("LastActivityAt not advanced: got %v, want after %v", piece.LastActivityAt, old)
	}
	if !piece.LastProgressAt.IsZero() {
		t.Fatalf("LastProgressAt must stay zero after an activity-only refresh, got %v", piece.LastProgressAt)
	}
	if !hasTaskEvent(t, srv, task.ID, session.TaskEventToolCall, "read_file") {
		t.Fatalf("expected a tool_call trace event naming read_file")
	}

	// Guard: a non-node batch (a plain human DM) resolves no executing node, so
	// the activity hook no-ops on ordinary turns.
	if _, _, ok := srv.resolveExecutingNode(mia, []MessageEnvelope{{SenderKind: "user", SenderName: "User"}}); ok {
		t.Fatalf("a non-plan batch must resolve no executing node")
	}
}

// Commentary and tool-result are activity signals too (plan §T9 / §6.8): each
// refreshes ONLY LastActivityAt, never LastProgressAt, and records its own trace
// kind. This runtime fuses a tool call and its result into one stream event and
// streams commentary as a per-message event; the wiring in
// runTurnWithRequestContext maps each to noteNodeActivity, which this pins.
func TestCommentaryAndToolResultRefreshActivityNotProgress(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "调研")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "查资料", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	envs := []MessageEnvelope{taskPlanDispatchEnv(t, srv, task.ID, mia)}
	gotTask, gotPiece, ok := srv.resolveExecutingNode(mia, envs)
	if !ok {
		t.Fatalf("resolveExecutingNode did not resolve the active node")
	}

	for _, tc := range []struct {
		name       string
		kind       string
		summary    string
		summarySub string
	}{
		{"commentary", session.TaskEventCommentary, "先读一下现有实现\n然后动手", "先读一下现有实现"},
		{"tool_result", session.TaskEventToolResult, "read_file", "read_file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := time.Now().Add(-time.Hour).UTC()
			if _, err := session.UpdateTaskPiece(srv.rt.SessionDir, task.ID, "p1", func(p *session.TaskPiece) {
				p.LastActivityAt = old
				p.LastProgressAt = time.Time{}
			}); err != nil {
				t.Fatalf("pin baseline: %v", err)
			}

			summary := tc.summary
			if tc.kind == session.TaskEventCommentary {
				summary = commentaryTraceSummary(tc.summary)
			}
			srv.noteNodeActivity(gotTask, gotPiece, tc.kind, summary)

			piece := reloadPiece(t, srv, task.ID, "p1")
			if !piece.LastActivityAt.After(old) {
				t.Fatalf("%s must refresh LastActivityAt: got %v, want after %v", tc.name, piece.LastActivityAt, old)
			}
			if !piece.LastProgressAt.IsZero() {
				t.Fatalf("%s must not touch LastProgressAt, got %v", tc.name, piece.LastProgressAt)
			}
			if !hasTaskEvent(t, srv, task.ID, tc.kind, tc.summarySub) {
				t.Fatalf("expected a %s trace event containing %q", tc.kind, tc.summarySub)
			}
		})
	}
}

// piece_done refreshes progress (LastProgressAt AND LastActivityAt) and records
// node_progress alongside the existing handoff_created trace.
func TestNoteNodeProgressViaPieceDone(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "两步任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "起草", Assignee: han},
		{ID: "p2", Title: "定稿", Assignee: mia, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	// Pin p1's activity baseline back so both timestamps are unambiguously newer
	// after the progress refresh (its progress is still zero — only activity was
	// stamped on dispatch).
	old := time.Now().Add(-time.Hour).UTC()
	if _, err := session.UpdateTaskPiece(srv.rt.SessionDir, task.ID, "p1", func(p *session.TaskPiece) {
		p.LastActivityAt = old
	}); err != nil {
		t.Fatalf("pin baseline: %v", err)
	}

	if _, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p1", &tools.TaskHandoff{
		Done:       "草稿完成",
		NextGoal:   "定稿",
		Acceptance: "通过评审",
	}); err != nil {
		t.Fatalf("PieceDone: %v", err)
	}

	p1 := reloadPiece(t, srv, task.ID, "p1")
	if p1.LastProgressAt.IsZero() {
		t.Fatalf("LastProgressAt must advance on piece_done, got zero")
	}
	if !p1.LastActivityAt.After(old) {
		t.Fatalf("LastActivityAt must advance with progress: got %v, want after %v", p1.LastActivityAt, old)
	}
	if !hasTaskEvent(t, srv, task.ID, session.TaskEventNodeProgress, "") {
		t.Fatalf("expected a node_progress trace event")
	}
	if !hasTaskEvent(t, srv, task.ID, session.TaskEventHandoffCreated, "") {
		t.Fatalf("expected the existing handoff_created trace event to survive")
	}
}

// A public kind=update the active assignee posts into a task thread is
// user-visible progress: it refreshes the poster's OWN node's LastProgressAt and
// records node_progress, yet wakes no teammate (the T7 invariant holds).
func TestPublicUpdateRefreshesProgressWithoutWakingTeammate(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "并行任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	// p1 (Mia, active) plus p2 (Han, gated on p1). SetPlan pulls both into the
	// task thread, so Han is a teammate who must NOT be woken by Mia's update.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "实现", Assignee: mia},
		{ID: "p2", Title: "验证", Assignee: han, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	if p1 := reloadPiece(t, srv, task.ID, "p1"); !p1.LastProgressAt.IsZero() {
		t.Fatalf("precondition: p1 progress should be zero before the update, got %v", p1.LastProgressAt)
	}
	hanBefore := pendingCount(t, srv, han)

	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		ParticipantID: mia,
		Kind:          "update",
		Text:          "进度:核心链路已跑通",
		ThreadID:      task.ID,
	}); err != nil {
		t.Fatalf("publishParticipantMessage: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	p1 := reloadPiece(t, srv, task.ID, "p1")
	if p1.LastProgressAt.IsZero() {
		t.Fatalf("a public update by the active assignee must refresh LastProgressAt")
	}
	if hanAfter := pendingCount(t, srv, han); hanAfter != hanBefore {
		t.Fatalf("a public update must wake no teammate: Han pending %d -> %d", hanBefore, hanAfter)
	}
	if !hasTaskEvent(t, srv, task.ID, session.TaskEventNodeProgress, "public update") {
		t.Fatalf("expected a node_progress trace event for the public update")
	}
}

// The final lead summary: both pieces done → the engine enters awaiting_lead
// and wakes the lead → the lead verifies and concludes → the summary
// bubbles to the parent MAIN stream, task_completed is recorded, the task
// resolves, and NO other group member gets a pending wake from the conclusion.
func TestFinalSummaryReachesParentWakesNoTeammate(t *testing.T) {
	srv, groupID, andy, mia, han, vera := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "收尾任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	// Two independent pieces so both dispatch at once.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "模块A", Assignee: mia},
		{ID: "p2", Title: "模块B", Assignee: han},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1", nil); err != nil {
		t.Fatalf("Mia piece_done p1: %v", err)
	}
	if _, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p2", nil); err != nil {
		t.Fatalf("Han piece_done p2: %v", err)
	}
	// The engine woke the lead to wrap up.
	waitForResidentDMHistoryContains(t, srv, andy, "Every piece of task")
	waitForResidentQuiesce(t, srv)

	// Snapshot every other member's inbox before the conclusion so we can prove
	// the final summary wakes none of them.
	beforeMia := pendingCount(t, srv, mia)
	beforeHan := pendingCount(t, srv, han)
	beforeVera := pendingCount(t, srv, vera)

	const summary = "全部完成:两个模块已交付并通过验证"
	if _, err := lead.ConcludeTask(context.Background(), task.ID, summary); err != nil {
		t.Fatalf("ConcludeTask: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	if !mainStreamHasParticipantPost(t, srv, groupID, summary) {
		t.Fatalf("final summary did not reach the parent main stream")
	}
	if !hasTaskEvent(t, srv, task.ID, session.TaskEventTaskCompleted, "") {
		t.Fatalf("expected a task_completed trace event")
	}
	if afterMia := pendingCount(t, srv, mia); afterMia != beforeMia {
		t.Fatalf("conclusion woke Mia: pending %d -> %d", beforeMia, afterMia)
	}
	if afterHan := pendingCount(t, srv, han); afterHan != beforeHan {
		t.Fatalf("conclusion woke Han: pending %d -> %d", beforeHan, afterHan)
	}
	if afterVera := pendingCount(t, srv, vera); afterVera != beforeVera {
		t.Fatalf("conclusion woke Vera: pending %d -> %d", beforeVera, afterVera)
	}
	if thread, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID); err != nil {
		t.Fatalf("reload task: %v", err)
	} else if thread.Status != session.ConversationThreadResolved {
		t.Fatalf("task status = %q, want resolved after the conclusion", thread.Status)
	}
}
