package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
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
