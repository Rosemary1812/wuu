package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sidethread"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func TestServerThreadDeleteRemovesSessionAndArtifacts(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	stateDir, err := srv.workspaceStateDir()
	if err != nil {
		t.Fatalf("workspaceStateDir: %v", err)
	}
	artifactDir := statepath.SessionArtifactDir(stateDir, threadID)
	if err := os.MkdirAll(filepath.Join(artifactDir, "workers"), 0o755); err != nil {
		t.Fatalf("seed artifact dir: %v", err)
	}
	if err := os.WriteFile(statepath.ThreadGoalRuntimePath(stateDir, threadID), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("seed goal runtime: %v", err)
	}

	deletePayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadDelete,
		"params": ThreadDeleteParams{ThreadID: threadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), deletePayload); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	deleteResult := remarshal[ThreadDeleteResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if deleteResult.ThreadID != threadID {
		t.Fatalf("unexpected delete result: %+v", deleteResult)
	}

	if _, ok, err := session.Find(rt.SessionDir, threadID); err != nil {
		t.Fatalf("session.Find after delete: %v", err)
	} else if ok {
		t.Fatalf("session %q should be gone after thread/delete", threadID)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact dir should be removed, stat err = %v", err)
	}
	if srv.thread(threadID) != nil {
		t.Fatalf("thread %q should be dropped from the in-memory registry", threadID)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"3","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	listResult := remarshal[ThreadListResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"])
	if len(listResult.Threads) != 0 {
		t.Fatalf("deleted thread should not be listed, got %+v", listResult.Threads)
	}
}

func TestServerThreadDeleteRejectsRunningThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	th := srv.thread(threadID)
	if th == nil {
		t.Fatalf("thread %q not registered", threadID)
	}
	th.mu.Lock()
	th.running = true
	th.mu.Unlock()

	deletePayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadDelete,
		"params": ThreadDeleteParams{ThreadID: threadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), deletePayload); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "2")
	if resp["error"] == nil {
		t.Fatalf("deleting a running thread must fail, got %+v", resp)
	}
	if _, ok, err := session.Find(rt.SessionDir, threadID); err != nil || !ok {
		t.Fatalf("running thread must survive a rejected delete: ok=%v err=%v", ok, err)
	}
}

func TestServerThreadDeleteRejectsRunningSideThreadAndDoesNotRecreateIt(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	blocking := newBlockingStreamClient("side reply")
	rt.StreamRunner.Client = blocking
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(srv.Close)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"side-delete-start","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "side-delete-start")["result"]).Thread.ID
	if _, err := srv.sendSideThreadMessage(threadID, "status?"); err != nil {
		t.Fatalf("side send: %v", err)
	}
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("side provider request did not start")
	}

	dispatchPayload(t, srv, "side-delete-busy", MethodThreadDelete, ThreadDeleteParams{ThreadID: threadID})
	busy := responseByID(t, parseOutput(t, out.String()), "side-delete-busy")
	if busy["error"] == nil || !strings.Contains(fmt.Sprint(busy["error"]), "side thread is running") {
		t.Fatalf("delete did not report the running side thread: %+v", busy)
	}
	if _, ok, err := session.Find(rt.SessionDir, threadID); err != nil || !ok {
		t.Fatalf("rejected delete removed main thread: ok=%t err=%v", ok, err)
	}

	close(blocking.release)
	waitForSideThreadStatus(t, srv.sideThreadStore, threadID, sidethread.StatusCompleted)
	dispatchPayload(t, srv, "side-delete-idle", MethodThreadDelete, ThreadDeleteParams{ThreadID: threadID})
	idle := responseByID(t, parseOutput(t, out.String()), "side-delete-idle")
	if idle["error"] != nil {
		t.Fatalf("delete after side completion failed: %+v", idle["error"])
	}
	if exists, err := srv.sideThreadStore.Exists(threadID); err != nil || exists {
		t.Fatalf("deleted side thread was recreated: exists=%t err=%v", exists, err)
	}
}

func TestServerThreadDeletePreservesMainThreadWhenSideCleanupFails(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(srv.Close)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"side-cleanup-start","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "side-cleanup-start")["result"]).Thread.ID

	invalidStoreDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidStoreDir, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("seed invalid side store: %v", err)
	}
	srv.sideThreadStore = sidethread.NewStore(invalidStoreDir)
	dispatchPayload(t, srv, "side-cleanup-delete", MethodThreadDelete, ThreadDeleteParams{ThreadID: threadID})
	resp := responseByID(t, parseOutput(t, out.String()), "side-cleanup-delete")
	if resp["error"] == nil || !strings.Contains(fmt.Sprint(resp["error"]), "delete side thread") {
		t.Fatalf("side cleanup failure was not returned: %+v", resp)
	}
	if _, ok, err := session.Find(rt.SessionDir, threadID); err != nil || !ok {
		t.Fatalf("failed side cleanup removed the main thread: ok=%t err=%v", ok, err)
	}
}

func TestServerThreadDeleteRejectsActiveBackgroundAgent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	workerClient := newBlockingStreamClient("done")
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       workerClient,
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    threadID,
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

	th := srv.thread(threadID)
	if th == nil {
		t.Fatalf("thread %q not registered", threadID)
	}
	th.mu.Lock()
	th.execRuntime = &runtime.ThreadRuntime{AgentControl: control}
	th.mu.Unlock()
	if _, err := control.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        agentcontrol.DefaultSubagentType,
		TaskName:    "background_delete_guard",
		Description: "keep running while delete is attempted",
		Prompt:      "wait",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-workerClient.started:
	case <-time.After(time.Second):
		t.Fatal("background agent did not start")
	}

	deletePayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadDelete,
		"params": ThreadDeleteParams{ThreadID: threadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), deletePayload); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "2")
	if resp["error"] == nil {
		t.Fatalf("deleting a thread with an active background agent must fail, got %+v", resp)
	}
	if _, ok, err := session.Find(rt.SessionDir, threadID); err != nil || !ok {
		t.Fatalf("thread with an active agent must survive a rejected delete: ok=%v err=%v", ok, err)
	}
}

func TestServerThreadDeleteStopsRuntimeSubscription(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	th := srv.thread(threadID)
	if th == nil {
		t.Fatalf("thread %q not registered", threadID)
	}
	sub := &threadRuntimeSubscription{done: make(chan struct{})}
	th.mu.Lock()
	th.execRuntime = &runtime.ThreadRuntime{}
	th.runtimeSubscription = sub
	th.mu.Unlock()

	deletePayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadDelete,
		"params": ThreadDeleteParams{ThreadID: threadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), deletePayload); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	if resp := responseByID(t, parseOutput(t, out.String()), "2"); resp["error"] != nil {
		t.Fatalf("thread/delete failed: %+v", resp["error"])
	}
	select {
	case <-sub.done:
	default:
		t.Fatal("thread/delete left its runtime subscription running")
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if th.execRuntime != nil || th.runtimeSubscription != nil {
		t.Fatalf("thread/delete retained runtime ownership: runtime=%p subscription=%p", th.execRuntime, th.runtimeSubscription)
	}
}

func TestServerThreadDeleteCleansForkWorktree(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	initAppserverGitRepo(t, rt.RootDir)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	sourceID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	forkPayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadFork,
		"params": ThreadForkParams{ThreadID: sourceID, Mode: "worktree"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), forkPayload); err != nil {
		t.Fatalf("thread/fork: %v", err)
	}
	forkResult := remarshal[ThreadForkResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if forkResult.Worktree == nil || forkResult.Worktree.Path == "" {
		t.Fatalf("fork should bind a worktree, got %+v", forkResult)
	}
	worktreePath := forkResult.Worktree.Path
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("fork worktree should exist on disk: %v", err)
	}

	deletePayload, err := json.Marshal(map[string]any{
		"id":     "3",
		"method": MethodThreadDelete,
		"params": ThreadDeleteParams{ThreadID: forkResult.Thread.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), deletePayload); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "3")
	if resp["error"] != nil {
		t.Fatalf("thread/delete failed: %+v", resp["error"])
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("fork worktree should be removed with the thread, stat err = %v", err)
	}
	if _, ok, err := session.Find(rt.SessionDir, forkResult.Thread.ID); err != nil {
		t.Fatalf("session.Find after delete: %v", err)
	} else if ok {
		t.Fatalf("fork session should be gone after thread/delete")
	}
}
