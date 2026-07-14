package appserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

func TestServerCloseReleasesConnectionOwnedResources(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ActivityRegistry = activity.NewRegistry()
	out := &lockedBuffer{}
	srv := New(rt, out)

	cancelled := make(chan struct{})
	th := newThreadState("thread-close", nil, rt.ProviderName, rt.Model, rt.RootDir, false, time.Now().UTC())
	th.running = true
	th.cancel = func() { close(cancelled) }
	sub := &threadRuntimeSubscription{done: make(chan struct{})}
	th.execRuntime = &runtime.ThreadRuntime{}
	th.runtimeSubscription = sub
	srv.mu.Lock()
	srv.threads[th.ID] = th
	srv.mu.Unlock()

	srv.idleUnreadWakeMu.Lock()
	srv.idleUnreadWakeTimers[th.ID] = time.AfterFunc(time.Hour, func() {})
	srv.idleUnreadWakeMu.Unlock()
	srv.queuedTurnMu.Lock()
	srv.pendingQueuedTurns[th.ID] = []queuedTurn{{id: "queued-close"}}
	srv.drainingQueuedTurns[th.ID] = true
	srv.queuedTurnMu.Unlock()

	srv.Close()
	srv.Close()

	select {
	case <-cancelled:
	default:
		t.Fatal("Server.Close did not cancel the active turn")
	}
	select {
	case <-sub.done:
	default:
		t.Fatal("Server.Close left the thread runtime subscription running")
	}
	th.mu.Lock()
	if th.execRuntime != nil || th.runtimeSubscription != nil {
		t.Fatalf("Server.Close retained runtime ownership: runtime=%p subscription=%p", th.execRuntime, th.runtimeSubscription)
	}
	th.mu.Unlock()
	if srv.thread(th.ID) != nil {
		t.Fatal("Server.Close retained the thread registry")
	}

	if _, _, err := rt.ActivityRegistry.Start(activity.StartOptions{
		ThreadID: th.ID,
		Workdir:  rt.RootDir,
		Kind:     activity.KindBrowser,
	}); err != nil {
		t.Fatalf("start activity after close: %v", err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("closed server still received activity notifications: %s", got)
	}
	srv.idleUnreadWakeMu.Lock()
	remainingTimers := len(srv.idleUnreadWakeTimers)
	srv.idleUnreadWakeMu.Unlock()
	if remainingTimers != 0 {
		t.Fatalf("Server.Close retained %d idle wake timer(s)", remainingTimers)
	}
	srv.queuedTurnMu.Lock()
	remainingQueued := len(srv.pendingQueuedTurns) + len(srv.drainingQueuedTurns)
	srv.queuedTurnMu.Unlock()
	if remainingQueued != 0 {
		t.Fatalf("Server.Close retained %d queued-turn entries", remainingQueued)
	}

	srv.scheduleIdleUnreadWake(th.ID, "participant-after-close")
	srv.idleUnreadWakeMu.Lock()
	remainingTimers = len(srv.idleUnreadWakeTimers)
	srv.idleUnreadWakeMu.Unlock()
	if remainingTimers != 0 {
		t.Fatalf("closed server accepted a new idle wake timer: %d", remainingTimers)
	}
	if srv.beginResidentDrain("participant-after-close") {
		t.Fatal("closed server accepted a new resident drain")
	}
}

func TestRunStdioClosesServerResourcesOnEOF(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ActivityRegistry = activity.NewRegistry()
	out := &lockedBuffer{}
	if err := RunStdio(context.Background(), rt, strings.NewReader(""), out); err != nil {
		t.Fatalf("RunStdio: %v", err)
	}

	if _, _, err := rt.ActivityRegistry.Start(activity.StartOptions{
		ThreadID: "after-eof",
		Workdir:  rt.RootDir,
		Kind:     activity.KindBrowser,
	}); err != nil {
		t.Fatalf("start activity after EOF: %v", err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("RunStdio retained its activity subscription after EOF: %s", got)
	}
}

func TestServerCloseCancelsBackgroundAgents(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	workerClient := newBlockingStreamClient("done")
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       workerClient,
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "thread-close-agent",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(func() {
		control.StopAll()
		control.Close()
	})

	srv := New(rt, &lockedBuffer{})
	th := newThreadState("thread-close-agent", nil, rt.ProviderName, rt.Model, rt.RootDir, false, time.Now().UTC())
	threadRuntime := &runtime.ThreadRuntime{AgentControl: control}
	th.execRuntime = threadRuntime
	th.runtimeSubscription = srv.subscribeThreadRuntime(th.ID, threadRuntime)
	srv.mu.Lock()
	srv.threads[th.ID] = th
	srv.mu.Unlock()

	result, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        agentcontrol.DefaultSubagentType,
		TaskName:    "close_background_agent",
		Description: "stay active until the server closes",
		Prompt:      "wait",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-workerClient.started:
	case <-time.After(time.Second):
		t.Fatal("background agent did not start")
	}

	srv.Close()
	waitForAgentStatus(t, control, result.AgentID, subagent.StatusCancelled)
}
