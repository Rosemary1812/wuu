package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// TestParticipantBusyRegistryLifecycle exercises the decision-five concurrency
// lock registry directly: reserve is atomic (a second reserve fails), bind
// attaches the run id, release is agent-scoped so a stale run can't clear a
// newer lock, and a force release ("") rolls back an unbound reservation.
func TestParticipantBusyRegistryLifecycle(t *testing.T) {
	srv := &Server{participantBusy: make(map[string]participantBusyEntry)}
	const pid = "prt-1"

	if !srv.tryAcquireParticipantBusy(pid, "task") {
		t.Fatal("first reserve should succeed on a free participant")
	}
	if srv.tryAcquireParticipantBusy(pid, "task") {
		t.Fatal("second reserve must fail while the participant is busy")
	}
	if !srv.participantIsBusy(pid) {
		t.Fatal("participant should read busy after reserve")
	}

	// Bind the run id, then a stale (mismatched) release is a no-op.
	srv.bindParticipantBusyAgent(pid, "agt-1")
	if info, ok := srv.participantBusyInfo(pid); !ok || info.AgentID != "agt-1" || info.Reason != "task" {
		t.Fatalf("bind should attach run id: %+v ok=%v", info, ok)
	}
	srv.releaseParticipantBusy(pid, "agt-STALE")
	if !srv.participantIsBusy(pid) {
		t.Fatal("release by a non-matching run id must not clear the lock")
	}

	// The owning run's release clears it; a re-reserve then succeeds.
	srv.releaseParticipantBusy(pid, "agt-1")
	if srv.participantIsBusy(pid) {
		t.Fatal("release by the owning run should clear the lock")
	}
	if !srv.tryAcquireParticipantBusy(pid, "task") {
		t.Fatal("re-reserve should succeed after release")
	}
	// Force release ("") rolls back an unbound reservation (spawn-error path).
	srv.releaseParticipantBusy(pid, "")
	if srv.participantIsBusy(pid) {
		t.Fatal("force release should clear an unbound reservation")
	}

	// markParticipantBusy is the idempotent acquire used by the notification
	// pipe: it creates a lock when free (the workflow-dispatch path) and only
	// fills a missing run id on an existing reservation.
	srv.markParticipantBusy(pid, "task", "agt-2")
	if info, ok := srv.participantBusyInfo(pid); !ok || info.AgentID != "agt-2" {
		t.Fatalf("mark should acquire when free: %+v ok=%v", info, ok)
	}
	srv.markParticipantBusy(pid, "task", "agt-OTHER")
	if info, _ := srv.participantBusyInfo(pid); info.AgentID != "agt-2" {
		t.Fatalf("mark must not overwrite a bound run id, got %q", info.AgentID)
	}
}

// TestParticipantStartRejectsBusyParticipant proves the participant/start gate
// refuses a second task-pull on a named agent that is already running a task.
func TestParticipantStartRejectsBusyParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	pid := saveNamedParticipant(t, rt, "Rina", "reviewer", "")

	// Rina is already executing a run.
	srv.markParticipantBusy(pid, "task", "agt-existing")

	raw := fmt.Sprintf(`{"id":"start","method":"participant/start","params":{"thread_id":%q,"participant_id":%q,"prompt":"do the thing"}}`, threadID, pid)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "start")
	errMsg := fmt.Sprint(resp["error"])
	if !strings.Contains(errMsg, "busy running another task") {
		t.Fatalf("expected busy rejection, got %+v", resp)
	}
}

// TestParticipantStartRejectsChatDrainingParticipant proves the gate also
// refuses a task-pull on a named agent that is mid chat-reply, so chat drains
// and task runs never grab the same resident agent at once.
func TestParticipantStartRejectsChatDrainingParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	pid := saveNamedParticipant(t, rt, "Kenta", "reviewer", "")

	if !srv.beginResidentDrain(pid) {
		t.Fatal("beginResidentDrain should mark the participant draining")
	}
	t.Cleanup(func() { srv.finishResidentDrain(pid) })

	raw := fmt.Sprintf(`{"id":"start","method":"participant/start","params":{"thread_id":%q,"participant_id":%q,"prompt":"do the thing"}}`, threadID, pid)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "start")
	errMsg := fmt.Sprint(resp["error"])
	if !strings.Contains(errMsg, "busy replying in chat") {
		t.Fatalf("expected chat-busy rejection, got %+v", resp)
	}
}

// TestThreadWithGroupMembersOverlaysBusy proves the busy lock surfaces on the
// group member view (for the UI badge and fork decision) and is live overlay,
// not stale cache: it flips with the registry.
func TestThreadWithGroupMembersOverlaysBusy(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	group := startNamedGroupThreadForTest(t, srv, "release")
	pid := saveNamedParticipant(t, rt, "Rina", "reviewer", "")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, pid); err != nil {
		t.Fatalf("add member: %v", err)
	}

	th := srv.thread(group.ID)
	if th == nil {
		t.Fatalf("group thread %q not resident", group.ID)
	}
	snapshot := func() Thread {
		th.mu.Lock()
		defer th.mu.Unlock()
		return th.snapshotLocked()
	}

	before := srv.threadWithGroupMembers(snapshot())
	if got := memberBusy(before, pid); got {
		t.Fatalf("member should be idle before any run, got busy=%v", got)
	}

	srv.markParticipantBusy(pid, "task", "agt-1")
	busy := srv.threadWithGroupMembers(snapshot())
	if got := memberBusy(busy, pid); !got {
		t.Fatalf("member should read busy while a run holds the lock, got busy=%v", got)
	}

	srv.releaseParticipantBusy(pid, "agt-1")
	after := srv.threadWithGroupMembers(snapshot())
	if got := memberBusy(after, pid); got {
		t.Fatalf("member should read idle after release, got busy=%v", got)
	}
}

func memberBusy(thread Thread, pid string) bool {
	for _, m := range thread.Members {
		if m.ID == pid {
			return m.Busy
		}
	}
	return false
}
