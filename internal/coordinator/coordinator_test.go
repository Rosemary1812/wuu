package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// fakeClient returns a canned response on every Chat / StreamChat call.
type fakeClient struct {
	resp providers.ChatResponse
}

func (f *fakeClient) Chat(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	return f.resp, nil
}

// StreamChat replays the canned response as a single content delta
// followed by a terminal Done event so workers — which now run
// through agent.StreamRunner — can be exercised by these tests.
func (f *fakeClient) StreamChat(_ context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent, 2)
	if f.resp.Content != "" {
		ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: f.resp.Content}
	}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

// fakeToolkit is a no-op tool executor.
type fakeToolkit struct{}

func (fakeToolkit) Definitions() []providers.ToolDefinition { return nil }
func (fakeToolkit) Execute(_ context.Context, _ providers.ToolCall) (string, error) {
	return "", nil
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
}

func TestNew_NonGitRepoSucceeds(t *testing.T) {
	dir := t.TempDir() // not a git repo
	c, err := New(Config{
		Client:        &fakeClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatalf("New should succeed for non-git directory, got: %v", err)
	}
	// Worktree manager should be nil for non-git workspaces.
	if c.worktrees != nil {
		t.Fatal("worktrees should be nil for non-git directory")
	}
}

func TestSpawn_SyncHappyPath(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "task done"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-1",
		HistoryDir:    filepath.Join(dir, ".wuu", "sessions", "sess-1", "workers"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "sync_happy",
		Description: "test",
		Prompt:      "do something",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	if res.Result != "task done" {
		t.Fatalf("got result %q", res.Result)
	}
	// Worker now defaults to inplace — additive writes land in the
	// parent repo so users don't have to fish them out of a worktree.
	if res.Isolation != "inplace" {
		t.Fatalf("worker default should be inplace isolation, got %q", res.Isolation)
	}
	if res.WorktreePath != "" {
		t.Fatalf("inplace spawn should not produce a worktree path, got %q", res.WorktreePath)
	}
}

func TestSpawn_RegistersThreadMetadata(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	threadDir := filepath.Join(dir, ".wuu", "sessions", "sess-threads", "threads")
	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "task done"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-threads",
		HistoryDir:    filepath.Join(dir, ".wuu", "sessions", "sess-threads", "workers"),
		ThreadDir:     threadDir,
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "scan_auth_flow",
		Description: "scan auth flow",
		Prompt:      "find auth problems",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.TaskName != "scan_auth_flow" || res.AgentPath != "/root/scan_auth_flow" {
		t.Fatalf("unexpected thread metadata in result: %+v", res)
	}
	snap := c.Manager().Get(res.AgentID).Snapshot()
	if snap.TaskName != res.TaskName || snap.AgentPath != res.AgentPath || snap.ParentID != "sess-threads" {
		t.Fatalf("snapshot missing thread metadata: %+v", snap)
	}

	var threads []agentthread.Metadata
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		threads, err = c.threadStore.ListThreads()
		if err != nil {
			t.Fatalf("ListThreads: %v", err)
		}
		if len(threads) == 2 && threads[1].Status == agentthread.StatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(threads) != 2 {
		t.Fatalf("expected root + child threads, got %+v", threads)
	}
	child := threads[1]
	if child.ID != res.AgentID || child.Path != res.AgentPath || child.Source.Kind != agentthread.SourceThreadSpawn {
		t.Fatalf("unexpected child thread metadata: %+v", child)
	}
	if child.Status != agentthread.StatusCompleted {
		t.Fatalf("expected completed child status, got %s", child.Status)
	}
	events, err := c.threadStore.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("expected root, child, and status events, got %+v", events)
	}
}

func TestSpawn_RegistersNestedThreadPath(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-nested",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     "worker",
		TaskName: "parent",
		Prompt:   "p",
	})
	if err != nil {
		t.Fatalf("parent spawn: %v", err)
	}
	child, err := c.Spawn(context.Background(), SpawnRequest{
		Type:       "worker",
		TaskName:   "child",
		Prompt:     "p",
		ParentID:   parent.AgentID,
		ParentPath: parent.AgentPath,
	})
	if err != nil {
		t.Fatalf("child spawn: %v", err)
	}
	if child.AgentPath != "/root/parent/child" {
		t.Fatalf("unexpected nested path: %+v", child)
	}
	meta, ok := c.threads.ResolveFrom(parent.AgentPath, "child")
	if !ok || meta.ID != child.AgentID || meta.ParentID != parent.AgentID {
		t.Fatalf("nested child did not resolve from parent path: %+v ok=%v", meta, ok)
	}
	if err := c.SendMessageFrom(parent.AgentPath, "child", "queued from parent"); err != nil {
		t.Fatalf("SendMessageFrom parent path: %v", err)
	}
	if got := c.Manager().PendingMessageCount(child.AgentID); got != 1 {
		t.Fatalf("expected child pending message, got %d", got)
	}
	c.StopAll()
}

func TestNestedResultRoutesToParentAgent(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-nested-route",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     "worker",
		TaskName: "parent",
		Prompt:   "p",
	})
	if err != nil {
		t.Fatalf("parent spawn: %v", err)
	}
	delivered := c.deliverNestedResultToParent(context.Background(), subagent.SubAgentSnapshot{
		ID:          "child-1",
		ParentID:    parent.AgentID,
		AgentPath:   parent.AgentPath + "/child",
		TaskName:    "child",
		Type:        "worker",
		Status:      subagent.StatusCompleted,
		Description: "child task",
		Result:      "child done",
	})
	if !delivered {
		t.Fatal("expected nested result to route to parent")
	}
	if got := c.Manager().PendingMessageCount(parent.AgentID); got != 1 {
		t.Fatalf("expected parent pending mailbox message, got %d", got)
	}
	c.StopAll()
}

func TestStopClosesAgentSubtree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-close-tree",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     "worker",
		TaskName: "parent",
		Prompt:   "parent task",
	})
	if err != nil {
		t.Fatalf("parent spawn: %v", err)
	}
	child, err := c.Spawn(context.Background(), SpawnRequest{
		Type:       "worker",
		TaskName:   "child",
		Prompt:     "child task",
		ParentID:   parent.AgentID,
		ParentPath: parent.AgentPath,
	})
	if err != nil {
		t.Fatalf("child spawn: %v", err)
	}

	if !c.Stop(parent.AgentPath) {
		t.Fatal("expected parent subtree stop to succeed")
	}
	if _, err := c.Wait(context.Background(), parent.AgentID); err != nil {
		t.Fatalf("wait parent: %v", err)
	}
	if _, err := c.Wait(context.Background(), child.AgentID); err != nil {
		t.Fatalf("wait child: %v", err)
	}
	for _, id := range []string{parent.AgentID, child.AgentID} {
		meta, ok := c.threads.Resolve(id)
		if !ok {
			t.Fatalf("missing metadata for %s", id)
		}
		if meta.Source.EdgeStatus != agentthread.EdgeClosed {
			t.Fatalf("expected %s edge closed, got %+v", id, meta.Source)
		}
	}
}

func TestWaitForMailboxUpdateFromRootWakesOnChildFinalStatus(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-wait-root",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     "worker",
		TaskName: "child",
		Prompt:   "child task",
	})
	if err != nil {
		t.Fatalf("child spawn: %v", err)
	}

	type waitResult struct {
		completed bool
		err       error
	}
	done := make(chan waitResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		completed, err := c.WaitForMailboxUpdateFrom(agentthread.RootPath, ctx)
		done <- waitResult{completed: completed, err: err}
	}()
	time.Sleep(20 * time.Millisecond)
	if !c.Stop(child.AgentID) {
		t.Fatal("expected stop to succeed")
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("wait mailbox: %v", got.err)
		}
		if !got.completed {
			t.Fatal("expected wait to complete")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mailbox update")
	}
}

func TestWaitForMailboxUpdateFromAgentReturnsAlreadyQueuedMail(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-wait-agent",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     "worker",
		TaskName: "parent",
		Prompt:   "parent task",
	})
	if err != nil {
		t.Fatalf("parent spawn: %v", err)
	}
	if err := c.SendMessage(parent.AgentID, "queued update"); err != nil {
		t.Fatalf("send message: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	completed, err := c.WaitForMailboxUpdateFrom(parent.AgentPath, ctx)
	if err != nil {
		t.Fatalf("wait mailbox: %v", err)
	}
	if !completed {
		t.Fatal("expected already queued mailbox update to complete immediately")
	}
	c.StopAll()
}

func TestSpawn_InplaceSkipsWorktree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "looked at line 42"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-inplace",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Worker defaults to inplace and must not create anything under
	// the worktree root on disk — overlap with TestSpawn_SyncHappyPath
	// on the isolation field, but this one specifically pins the
	// no-disk-side-effect property by reading the worktree dir.
	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "inplace",
		Description: "look",
		Prompt:      "p",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.Isolation != "inplace" {
		t.Fatalf("expected isolation=inplace, got %q", res.Isolation)
	}
	if res.WorktreePath != "" {
		t.Fatalf("expected empty worktree path for inplace spawn, got %q", res.WorktreePath)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".wuu", "worktrees", "sess-inplace")); len(entries) != 0 {
		t.Fatalf("expected no worktrees on disk, got %d entries", len(entries))
	}
}

func TestSpawn_IsolationOverride(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, _ := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "ok"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-override",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})

	// Worker defaults to inplace; explicit isolation="worktree"
	// must override that and put the worker in a fresh worktree.
	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "force_isolated",
		Description: "force-isolated",
		Prompt:      "p",
		Isolation:   "worktree",
		Synchronous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Isolation != "worktree" {
		t.Fatalf("override failed: %q", res.Isolation)
	}

	// And: explicit isolation="inplace" is a no-op (it matches the
	// default) but must still resolve cleanly without touching the
	// worktree directory.
	res2, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "explicit_inplace",
		Description: "explicit-inplace",
		Prompt:      "p",
		Isolation:   "inplace",
		Synchronous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Isolation != "inplace" || res2.WorktreePath != "" {
		t.Fatalf("explicit inplace failed: %+v", res2)
	}
}

func TestSpawn_UnknownIsolationRejected(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, _ := New(Config{
		Client:        &fakeClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	_, err := c.Spawn(context.Background(), SpawnRequest{
		Type: "worker", TaskName: "bad_isolation", Description: "x", Prompt: "p", Isolation: "yolo",
	})
	if err == nil {
		t.Fatal("expected error for unknown isolation")
	}
}

func TestSpawn_PreservesCleanWorktreeForFollowup(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// fakeToolkit doesn't touch the filesystem, so the worker leaves
	// its worktree pristine. The coordinator must still keep it after
	// completion because child tasks can receive follow-up turns.
	//
	// Worker no longer defaults to worktree, so this test explicitly
	// opts in via Isolation: "worktree" — that's the supported way to
	// get an isolated child task now.
	c, _ := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "ok"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-recycle",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "preserve_clean",
		Description: "noop",
		Prompt:      "p",
		Isolation:   "worktree",
		Synchronous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Isolation != "worktree" {
		t.Fatalf("expected worktree isolation, got %q", res.Isolation)
	}
	if res.WorktreePath == "" {
		t.Fatal("clean worktree should be preserved for follow-up turns")
	}
	if _, err := os.Stat(res.WorktreePath); err != nil {
		t.Fatalf("preserved worktree should exist: %v", err)
	}
}

func TestSpawn_KeepDirtyWorktree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// Toolkit that drops a file in the worker's root before returning.
	dirtyKit := func(root string, _ WorkerType, _ agentthread.Metadata) (agent.ToolExecutor, error) {
		if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("x"), 0o644); err != nil {
			return nil, err
		}
		return fakeToolkit{}, nil
	}

	c, _ := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "ok"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-dirty",
		WorkerFactory: dirtyKit,
	})

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "keep_dirty",
		Description: "modifies",
		Prompt:      "p",
		Isolation:   "worktree",
		Synchronous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.WorktreePath == "" {
		t.Fatal("dirty worktree should be preserved and path returned")
	}
	if _, err := os.Stat(res.WorktreePath); err != nil {
		t.Fatalf("dirty worktree should still be on disk: %v", err)
	}
}

func TestSpawn_RequiresPrompt(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, _ := New(Config{
		Client:        &fakeClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})

	_, err := c.Spawn(context.Background(), SpawnRequest{Description: "x"})
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestSpawn_ConcurrencyCap(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// fakeClient with no delay completes instantly, so the cap is hard
	// to hit. Use a slow client.
	slow := &slowClient{}

	c, _ := New(Config{
		Client:        slow,
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
		MaxParallel:   2,
	})

	// Fire 2 async spawns to fill the cap.
	for i := 0; i < 2; i++ {
		_, err := c.Spawn(context.Background(), SpawnRequest{
			Type: "worker", TaskName: fmt.Sprintf("slow_%d", i), Description: "x", Prompt: "p",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// 3rd spawn should be rejected.
	_, err := c.Spawn(context.Background(), SpawnRequest{
		Type: "worker", TaskName: "slow_2", Description: "x", Prompt: "p",
	})
	if err == nil {
		t.Fatal("expected concurrency cap error")
	}
	c.StopAll()
}

// slowClient never returns until context is cancelled.
type slowClient struct{}

func (slowClient) Chat(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	<-ctx.Done()
	return providers.ChatResponse{}, ctx.Err()
}

// StreamChat opens a channel that only emits an error event once the
// caller's context is cancelled. Mirrors Chat's blocking semantics so
// the concurrency-cap test still pins a worker until StopAll fires.
func (slowClient) StreamChat(ctx context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent, 1)
	go func() {
		defer close(ch)
		<-ctx.Done()
		ch <- providers.StreamEvent{Type: providers.EventError, Error: ctx.Err()}
	}()
	return ch, nil
}

func TestSendMessage_QueuesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// Keep worker running so we can enqueue a follow-up.
	slow := &slowClient{}
	c, err := New(Config{
		Client:        slow,
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-send-running",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type: "worker", TaskName: "send_running", Description: "slow", Prompt: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendMessage(res.AgentID, "please also check logs"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got := c.Manager().PendingMessageCount(res.AgentID); got != 1 {
		t.Fatalf("expected pending queue size=1, got %d", got)
	}

	c.StopAll()
}

func TestSendMessage_ResolvesThreadPathAndTaskName(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-send-path",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "review_config",
		Description: "slow",
		Prompt:      "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.AgentPath, "/root/") {
		t.Fatalf("expected canonical path, got %q", res.AgentPath)
	}
	if err := c.SendMessage(res.AgentPath, "check env files too"); err != nil {
		t.Fatalf("SendMessage by path: %v", err)
	}
	if err := c.SendMessage("review_config", "check defaults too"); err != nil {
		t.Fatalf("SendMessage by task name: %v", err)
	}
	if got := c.Manager().PendingMessageCount(res.AgentID); got != 2 {
		t.Fatalf("expected pending queue size=2, got %d", got)
	}

	c.StopAll()
}

func TestSpawn_AsyncDetachedFromParentContext(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-detached-spawn",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	parentCtx, cancelParent := context.WithCancel(context.Background())
	res, err := c.Spawn(parentCtx, SpawnRequest{
		Type: "worker", TaskName: "detached_spawn", Description: "slow", Prompt: "p",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	cancelParent()
	time.Sleep(50 * time.Millisecond)

	sa := c.Manager().Get(res.AgentID)
	if sa == nil {
		t.Fatalf("expected worker %q to exist", res.AgentID)
	}
	if snap := sa.Snapshot(); snap.Status != subagent.StatusRunning {
		t.Fatalf("expected detached async worker to keep running after parent cancel, got %s", snap.Status)
	}

	c.StopAll()
	waitForRunningWorkersToStop(t, c.Manager(), time.Second)
}

func TestFork_AsyncDetachedFromParentContext(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &slowClient{},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-detached-fork",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	parentHistory := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	parentCtx, cancelParent := context.WithCancel(context.Background())
	res, err := c.Fork(parentCtx, ForkRequest{
		TaskName:    "detached_fork",
		Description: "slow fork",
		Prompt:      "continue",
	}, parentHistory)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	cancelParent()
	time.Sleep(50 * time.Millisecond)

	sa := c.Manager().Get(res.AgentID)
	if sa == nil {
		t.Fatalf("expected worker %q to exist", res.AgentID)
	}
	if snap := sa.Snapshot(); snap.Status != subagent.StatusRunning {
		t.Fatalf("expected detached async fork to keep running after parent cancel, got %s", snap.Status)
	}

	c.StopAll()
	waitForRunningWorkersToStop(t, c.Manager(), time.Second)
}

func TestSendMessage_QueuesCompletedWorker(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "done"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-send-complete",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type: "worker", TaskName: "send_complete", Description: "quick", Prompt: "p", Synchronous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed spawn, got %s", res.Status)
	}
	if err := c.SendMessage(res.AgentID, "extra instruction"); err != nil {
		t.Fatalf("SendMessage should queue on completed worker: %v", err)
	}
	if got := c.Manager().PendingMessageCount(res.AgentID); got != 1 {
		t.Fatalf("expected queued message on completed worker, got %d", got)
	}
}

func TestSendMessage_RejectsEmptyFields(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	c, _ := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "ok"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err := c.SendMessage("", "x"); err == nil {
		t.Fatal("expected target required error")
	}
	if err := c.SendMessage("worker-123", ""); err == nil {
		t.Fatal("expected message required error")
	}
}

func TestAgentMailboxChatMessage(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, _ := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "found bug at line 42"}},
		DefaultModel:  "fake",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess",
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})

	res, _ := c.Spawn(context.Background(), SpawnRequest{
		Type:        "worker",
		TaskName:    "find_bug",
		Description: "find the bug",
		Prompt:      "look for it",
		Synchronous: true,
	})

	snap := c.Manager().Get(res.AgentID).Snapshot()
	msg := AgentMailboxChatMessage(snap)
	if msg.Role != "user" || msg.Name != AgentMailboxMessageName {
		t.Fatalf("unexpected mailbox chat message envelope: %+v", msg)
	}
	var payload AgentMailboxMessage
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		t.Fatalf("mailbox payload is not JSON: %v\n%s", err, msg.Content)
	}
	if payload.Type != "agent_result" || payload.AgentID != res.AgentID || payload.Result != "found bug at line 42" {
		t.Fatalf("unexpected mailbox payload: %+v", payload)
	}
	if payload.Description != "find the bug" || payload.Status != "completed" {
		t.Fatalf("missing summary/status in mailbox payload: %+v", payload)
	}
}

func waitForRunningWorkersToStop(t *testing.T, mgr *subagent.Manager, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if mgr.CountRunning() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected workers to stop within %s, still have %d running", timeout, mgr.CountRunning())
}

func TestAgentMailboxMessage_IncludesErrorClass(t *testing.T) {
	snap := subagentSnapshotWithError(&providers.HTTPError{
		StatusCode: 429,
		Body:       "rate limited",
	})
	payload := NewAgentMailboxMessage(snap)
	if payload.ErrorClass != "retryable" {
		t.Fatalf("expected retryable error class, got: %+v", payload)
	}
	if !contains(payload.Error, "rate limited") {
		t.Fatalf("expected error body in mailbox payload, got: %+v", payload)
	}
}

// subagentSnapshotWithError builds a minimal failed-worker snapshot
// for mailbox tests without actually spawning anything.
func subagentSnapshotWithError(err error) subagent.SubAgentSnapshot {
	return subagent.SubAgentSnapshot{
		ID:          "worker-test",
		Type:        "worker",
		Description: "test",
		Status:      subagent.StatusFailed,
		Error:       err,
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
