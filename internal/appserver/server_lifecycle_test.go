package appserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
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

func TestRunStdioWaitsForActiveRootTurnBeforeCallerCleanupOnEOF(t *testing.T) {
	client := newBlockingStreamClient("cancelled root turn")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = client
	manager := attachTestProcessManager(t, rt)
	proc, err := manager.Start(context.Background(), process.StartOptions{
		Command:   "sleep 30",
		OwnerKind: process.OwnerMainAgent,
		OwnerID:   "stdio-shutdown-test",
		Lifecycle: process.LifecycleSession,
	})
	if err != nil {
		t.Fatalf("start session process: %v", err)
	}
	t.Cleanup(func() { _, _ = manager.Stop(proc.ID) })

	const threadID = "stdio-root-turn"
	if _, err := session.CreateWithMetadata(rt.SessionDir, threadID, rt.RootDir); err != nil {
		t.Fatalf("create root thread: %v", err)
	}
	input := strings.Join([]string{
		fmt.Sprintf(`{"jsonrpc":"2.0","id":"resume","method":"thread/resume","params":{"session_id":%q}}`, threadID),
		fmt.Sprintf(`{"jsonrpc":"2.0","id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"stay active until EOF"}}`, threadID),
	}, "\n") + "\n"
	out := newTerminalBlockingWriter()
	t.Cleanup(out.unblock)
	runDone := make(chan error, 1)
	cleanupDone := make(chan struct{})
	go func() {
		runDone <- RunStdio(context.Background(), rt, strings.NewReader(input), out)
		_, _ = rt.Cleanup()
		close(cleanupDone)
	}()

	select {
	case <-out.started:
	case <-time.After(2 * time.Second):
		t.Fatal("root turn did not reach its terminal notification during EOF shutdown")
	}
	select {
	case err := <-runDone:
		t.Fatalf("RunStdio returned before its active root goroutine completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-cleanupDone:
		t.Fatal("caller cleanup ran before RunStdio drained the active root turn")
	default:
	}
	assertProcessStatus(t, manager, proc.ID, process.StatusRunning)

	out.unblock()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("RunStdio: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunStdio did not return after the root turn completed")
	}
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("caller cleanup did not run after RunStdio returned")
	}
	assertProcessStatus(t, manager, proc.ID, process.StatusStopped)
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

type shutdownLateTerminalClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newShutdownLateTerminalClient() *shutdownLateTerminalClient {
	return &shutdownLateTerminalClient{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *shutdownLateTerminalClient) Chat(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	c.calls.Add(1)
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return providers.ChatResponse{Content: "child done"}, nil
	case <-ctx.Done():
		return providers.ChatResponse{}, ctx.Err()
	}
}

func (c *shutdownLateTerminalClient) StreamChat(ctx context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	c.calls.Add(1)
	c.once.Do(func() { close(c.started) })
	events := make(chan providers.StreamEvent, 2)
	go func() {
		defer close(events)
		select {
		case <-c.release:
			events <- providers.StreamEvent{Type: providers.EventContentDelta, Content: "child done"}
			events <- providers.StreamEvent{Type: providers.EventDone}
		case <-ctx.Done():
			events <- providers.StreamEvent{Type: providers.EventError, Error: ctx.Err()}
		}
	}()
	return events, nil
}

func TestServerCloseLateNestedTerminalDoesNotResumeOrNudge(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	artifactDir := filepath.Join(rt.RootDir, ".wuu-state", "sessions", "shutdown-late-terminal")
	parentClient := &fakeClient{response: providers.ChatResponse{Content: "parent done"}}
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(parentClient),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "shutdown-late-terminal",
		HistoryDir:   filepath.Join(artifactDir, "workers"),
		ThreadDir:    filepath.Join(artifactDir, "threads"),
		HarnessDir:   filepath.Join(artifactDir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}

	parent, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type: agentcontrol.DefaultSubagentType, TaskName: "shutdown_parent", Prompt: "finish parent",
	})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	waitForAgentStatus(t, control, parent.AgentID, subagent.StatusCompleted)
	waitForOwnedWorkerExecutions(t, control, 0)

	childClient := newShutdownLateTerminalClient()
	child, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:           agentcontrol.HelpMeRecoveryWorkerType,
		TaskName:       "shutdown_child",
		Prompt:         "finish child without a report",
		ParentID:       parent.AgentID,
		ParentPath:     parent.AgentPath,
		ClientOverride: childClient,
	})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	select {
	case <-childClient.started:
	case <-time.After(time.Second):
		t.Fatal("child provider turn did not start")
	}

	terminalEntered := make(chan struct{})
	releaseTerminal := make(chan struct{})
	var terminalOnce sync.Once
	var releaseTerminalOnce sync.Once
	control.SetWorkerTerminalTransitionHookForTest(func(workerID string) {
		if workerID != child.AgentID {
			return
		}
		terminalOnce.Do(func() { close(terminalEntered) })
		<-releaseTerminal
	})
	close(childClient.release)
	select {
	case <-terminalEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not reach terminal transition")
	}

	srv := New(rt, &lockedBuffer{})
	thread := newThreadState("shutdown-late-terminal", nil, rt.ProviderName, rt.Model, rt.RootDir, true, time.Now().UTC())
	thread.execRuntime = &runtime.ThreadRuntime{AgentControl: control}
	srv.mu.Lock()
	srv.threads[thread.ID] = thread
	srv.mu.Unlock()
	stopWavesDone := make(chan struct{})
	var stopWavesOnce sync.Once
	srv.afterWorkerShutdownStopWavesForTest = func() { stopWavesOnce.Do(func() { close(stopWavesDone) }) }
	t.Cleanup(func() {
		releaseTerminalOnce.Do(func() { close(releaseTerminal) })
		srv.Close()
		control.Close()
	})

	closeDone := make(chan struct{})
	go func() {
		srv.Close()
		close(closeDone)
	}()
	select {
	case <-stopWavesDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Close did not issue both worker StopAll waves")
	}
	select {
	case <-closeDone:
		t.Fatal("Server.Close returned before the late terminal transition finished")
	default:
	}
	releaseTerminalOnce.Do(func() { close(releaseTerminal) })
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Close did not finish after the late terminal transition")
	}

	if got := childClient.calls.Load(); got != 1 {
		t.Fatalf("child model turns = %d, want 1 (no report-closing nudge)", got)
	}
	parentClient.mu.Lock()
	parentCalls := len(parentClient.requests)
	parentClient.mu.Unlock()
	if parentCalls != 1 {
		t.Fatalf("parent model turns = %d, want 1 (no nested completion follow-up)", parentCalls)
	}
	if _, ok, err := control.HarnessStore().ReportForTask(child.AgentID); err != nil || !ok {
		t.Fatalf("late terminal result was not durably finalized: ok=%t err=%v", ok, err)
	}
}

func TestServerCloseRetainsPresenceUntilTerminalWorkerLeaseReleases(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	artifactDir := filepath.Join(rt.RootDir, ".wuu-state", "sessions", "thread-close-observer")
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(&fakeClient{response: providers.ChatResponse{Content: "done"}}),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "thread-close-observer",
		HistoryDir:   filepath.Join(artifactDir, "workers"),
		ThreadDir:    filepath.Join(artifactDir, "threads"),
		HarnessDir:   filepath.Join(artifactDir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	var enteredOnce sync.Once
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseWorker()
		control.StopAll()
		control.Close()
	})
	control.SetWorkerExecutionReleaseHookForTest(func(string) {
		enteredOnce.Do(func() { close(entered) })
		<-release
	})

	srv := New(rt, &lockedBuffer{})
	th := newThreadState("thread-close-observer", nil, rt.ProviderName, rt.Model, rt.RootDir, true, time.Now().UTC())
	th.execRuntime = &runtime.ThreadRuntime{AgentControl: control}
	srv.mu.Lock()
	srv.threads[th.ID] = th
	srv.mu.Unlock()

	if _, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type: agentcontrol.DefaultSubagentType, TaskName: "close_observer", Prompt: "finish",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not reach the terminal lease observer")
	}
	if got := control.Manager().CountRunning(); got != 0 {
		t.Fatalf("manager running count = %d, want terminal observer window", got)
	}
	if got := control.OwnedWorkerExecutionCount(); got != 1 {
		t.Fatalf("owned worker execution count = %d, want 1", got)
	}

	closeDone := make(chan struct{})
	go func() {
		srv.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close returned while the terminal worker lease was still owned")
	case <-time.After(50 * time.Millisecond):
	}
	peer, elected, err := session.AcquireAppServerPresence(rt.SessionDir)
	if err != nil {
		t.Fatalf("acquire peer presence: %v", err)
	}
	if elected {
		_ = peer.Release()
		t.Fatal("Close released presence while the terminal worker lease was still owned")
	}
	if err := peer.FinalizeStartup(); err != nil {
		_ = peer.Release()
		t.Fatalf("finalize peer presence: %v", err)
	}
	if err := peer.Release(); err != nil {
		t.Fatalf("release peer presence: %v", err)
	}

	releaseWorker()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the worker terminal finalizer released its lease")
	}
	waitForOwnedWorkerExecutions(t, control, 0)
	deadline := time.Now().Add(2 * time.Second)
	for {
		probe, first, err := session.AcquireAppServerPresence(rt.SessionDir)
		if err != nil {
			t.Fatalf("probe released presence: %v", err)
		}
		if first {
			_ = probe.Release()
			break
		}
		if err := probe.FinalizeStartup(); err != nil {
			_ = probe.Release()
			t.Fatalf("finalize presence probe: %v", err)
		}
		_ = probe.Release()
		if time.Now().After(deadline) {
			t.Fatal("server presence was not released after the worker lease ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestResidentPruneRetainsTerminalWorkerLeaseObserver(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	artifactDir := filepath.Join(rt.RootDir, ".wuu-state", "sessions", "resident-worker-observer")
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(&fakeClient{response: providers.ChatResponse{Content: "done"}}),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "resident-worker-observer",
		HistoryDir:   filepath.Join(artifactDir, "workers"),
		ThreadDir:    filepath.Join(artifactDir, "threads"),
		HarnessDir:   filepath.Join(artifactDir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	var enteredOnce sync.Once
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseWorker()
		control.StopAll()
		control.Close()
	})
	control.SetWorkerExecutionReleaseHookForTest(func(string) {
		enteredOnce.Do(func() { close(entered) })
		<-release
	})

	srv := New(rt, &lockedBuffer{})
	defer srv.Close()
	now := time.Now().UTC()
	target := newThreadState("resident-worker-observer", nil, rt.ProviderName, rt.Model, rt.RootDir, true, now.Add(-2*time.Hour))
	target.execRuntime = &runtime.ThreadRuntime{AgentControl: control}
	srv.mu.Lock()
	srv.threads[target.ID] = target
	for i := 0; i < residentThreadLimit; i++ {
		id := "resident-idle-" + string(rune('a'+i))
		srv.threads[id] = newThreadState(id, nil, rt.ProviderName, rt.Model, rt.RootDir, true, now.Add(time.Duration(i)*time.Minute))
	}
	srv.mu.Unlock()

	if _, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type: agentcontrol.DefaultSubagentType, TaskName: "resident_observer", Prompt: "finish",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not reach the terminal lease observer")
	}
	if got := control.Manager().CountRunning(); got != 0 {
		t.Fatalf("manager running count = %d, want terminal observer window", got)
	}
	srv.pruneResidentThreads()
	if srv.thread(target.ID) == nil {
		t.Fatal("resident prune evicted a thread with an owned terminal worker lease")
	}

	releaseWorker()
	waitForOwnedWorkerExecutions(t, control, 0)
	extra := newThreadState("resident-extra", nil, rt.ProviderName, rt.Model, rt.RootDir, true, now.Add(time.Hour))
	srv.mu.Lock()
	srv.threads[extra.ID] = extra
	srv.mu.Unlock()
	srv.pruneResidentThreads()
	if srv.thread(target.ID) != nil {
		t.Fatal("resident prune retained the oldest thread after its worker lease released")
	}
}

func TestResidentPruneRetainsThreadDuringExecutionAdmission(t *testing.T) {
	tests := []struct {
		name  string
		mark  func(*threadState)
		clear func(*threadState)
	}{
		{
			name:  "admission reserved",
			mark:  func(th *threadState) { th.admissionReserved = true },
			clear: func(th *threadState) { th.admissionReserved = false },
		},
		{
			name:  "execution lease",
			mark:  func(th *threadState) { th.executionLease = &session.ThreadExecutionLease{} },
			clear: func(th *threadState) { th.executionLease = nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &Server{threads: make(map[string]*threadState)}
			now := time.Now().UTC()
			target := newThreadState("resident-admission-target", nil, "fake-provider", "fake-model", t.TempDir(), true, now.Add(-2*time.Hour))
			tt.mark(target)
			srv.threads[target.ID] = target
			for i := 0; i < residentThreadLimit; i++ {
				id := "resident-admission-idle-" + string(rune('a'+i))
				srv.threads[id] = newThreadState(id, nil, "fake-provider", "fake-model", target.CWD, true, now.Add(time.Duration(i)*time.Minute))
			}

			srv.pruneResidentThreads()
			if srv.thread(target.ID) == nil {
				t.Fatal("resident prune evicted a thread during execution admission")
			}
			target.mu.Lock()
			tt.clear(target)
			target.mu.Unlock()
			extra := newThreadState("resident-admission-extra", nil, "fake-provider", "fake-model", target.CWD, true, now.Add(time.Hour))
			srv.mu.Lock()
			srv.threads[extra.ID] = extra
			srv.mu.Unlock()
			srv.pruneResidentThreads()
			if srv.thread(target.ID) != nil {
				t.Fatal("resident prune retained the oldest thread after execution admission ended")
			}
		})
	}
}

func waitForOwnedWorkerExecutions(t *testing.T, control *agentcontrol.AgentControl, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if control.OwnedWorkerExecutionCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("owned worker execution count = %d, want %d", control.OwnedWorkerExecutionCount(), want)
}

func assertProcessStatus(t *testing.T, manager *process.Manager, processID string, want process.Status) {
	t.Helper()
	processes, err := manager.List()
	if err != nil {
		t.Fatalf("list processes: %v", err)
	}
	for _, proc := range processes {
		if proc.ID == processID {
			if proc.Status != want {
				t.Fatalf("process %s status = %s, want %s", processID, proc.Status, want)
			}
			return
		}
	}
	t.Fatalf("process %s not found", processID)
}
