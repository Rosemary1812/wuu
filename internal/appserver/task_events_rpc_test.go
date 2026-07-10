package appserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// The Task panel's "轨迹" timeline reads the trace through the thread/taskEvents
// RPC (plan §T11). Driving a task through its lifecycle — create, plan, finish
// the piece — writes a run of task_events; the RPC must return them in their
// per-task seq order, scoped to the requested subthread, and only for a
// subthread that actually belongs to the named parent thread.

func TestThreadTaskEventsReturnsTraceInSeqOrder(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)

	// Create the task (task_created), plan a single piece for Mia
	// (workflow_planned + the dispatch's node_started), then Mia files it done
	// (node_progress + node_succeeded, and — all pieces done — the lead's
	// lead_invoked wrap-up wake). Every one of these is written synchronously by
	// the manager methods below, so the trace is deterministic.
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "轨迹任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "only", Title: "写调研", Assignee: mia, Prompt: "开工"},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "only", nil); err != nil {
		t.Fatalf("piece_done: %v", err)
	}
	// Let the woken turns (Mia's dispatch, the lead's wrap-up) settle so the
	// trace read is stable — the assertions do not depend on their events, only
	// on the synchronously-written ones, but quiescing avoids a torn read.
	waitForResidentQuiesce(t, srv)

	raw := fmt.Sprintf(`{"id":"tev","method":"thread/taskEvents","params":{"thread_id":%q,"subthread_id":%q}}`, groupID, task.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/taskEvents: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "tev")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/taskEvents returned error: %v", errMsg)
	}
	result := remarshal[ThreadTaskEventsResult](t, resp["result"])
	if len(result.Events) == 0 {
		t.Fatal("thread/taskEvents returned no events for a task that ran")
	}

	// The events come back in per-task seq order: seq is assigned contiguously
	// (max+1) at append time under the store lock, so the timeline is 1..N with
	// no gaps regardless of which writer produced each one.
	kinds := map[string]bool{}
	for i, ev := range result.Events {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d, want the contiguous timeline order %d: %+v", i, ev.Seq, i+1, result.Events)
		}
		kinds[ev.Kind] = true
	}
	for _, want := range []string{
		session.TaskEventTaskCreated,
		session.TaskEventWorkflowPlanned,
		session.TaskEventNodeStarted,
		session.TaskEventNodeSucceeded,
	} {
		if !kinds[want] {
			t.Fatalf("trace timeline missing %q; got kinds %v", want, kinds)
		}
	}

	// task_created is a task-level event (no node); node_succeeded is anchored to
	// the piece it completed.
	for _, ev := range result.Events {
		if ev.Kind == session.TaskEventTaskCreated && ev.NodeID != "" {
			t.Fatalf("task_created must be task-level (empty node), got node %q", ev.NodeID)
		}
		if ev.Kind == session.TaskEventNodeSucceeded && ev.NodeID != "only" {
			t.Fatalf("node_succeeded node = %q, want the piece id %q", ev.NodeID, "only")
		}
	}
}

func TestThreadTaskEventsRejectsForeignSubthreadAndMissingParams(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "隔离任务")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "块", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })

	// A subthread must belong to the named parent thread — a request that names a
	// different (nonexistent) parent is refused, so the trace of a task cannot be
	// read through an unrelated thread.
	raw := fmt.Sprintf(`{"id":"foreign","method":"thread/taskEvents","params":{"thread_id":"cth-not-a-parent","subthread_id":%q}}`, task.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/taskEvents (foreign): %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "foreign")
	if _, ok := resp["error"]; !ok {
		t.Fatalf("thread/taskEvents on a foreign parent must error, got %+v", resp)
	}

	// subthread_id is required.
	raw2 := fmt.Sprintf(`{"id":"missing","method":"thread/taskEvents","params":{"thread_id":%q}}`, groupID)
	if err := srv.handleLine(context.Background(), []byte(raw2)); err != nil {
		t.Fatalf("thread/taskEvents (missing): %v", err)
	}
	resp2 := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "missing")
	if _, ok := resp2["error"]; !ok {
		t.Fatalf("thread/taskEvents with no subthread_id must error, got %+v", resp2)
	}
}
