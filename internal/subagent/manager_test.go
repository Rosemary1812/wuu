package subagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// fakeClient is a tiny providers.StreamClient stub for tests. It
// returns the canned response on every Chat / StreamChat call and
// stashes the most recent request payload so tests can assert what
// the runner actually sent (used by the fork-with-history test).
type fakeClient struct {
	response    providers.ChatResponse
	err         error
	calls       atomic.Int32
	delay       time.Duration
	lastRequest atomic.Pointer[providers.ChatRequest]
}

func visibleMessagesForTest(msgs []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Hidden {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func (f *fakeClient) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.calls.Add(1)
	cp := req
	f.lastRequest.Store(&cp)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return providers.ChatResponse{}, ctx.Err()
		}
	}
	if f.err != nil {
		return providers.ChatResponse{}, f.err
	}
	return f.response, nil
}

// StreamChat replays the same canned response as a single content
// delta followed by a terminal Done event. Errors and the delay knob
// behave the same way they do for Chat so existing tests don't need
// to grow a stream-specific code path.
func (f *fakeClient) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	f.calls.Add(1)
	cp := req
	f.lastRequest.Store(&cp)
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan providers.StreamEvent, 4)
	go func() {
		defer close(ch)
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-ctx.Done():
				ch <- providers.StreamEvent{Type: providers.EventError, Error: ctx.Err()}
				return
			}
		}
		if f.response.Content != "" {
			ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: f.response.Content}
		}
		ch <- providers.StreamEvent{
			Type:       providers.EventDone,
			StopReason: f.response.StopReason,
			Truncated:  f.response.Truncated,
		}
	}()
	return ch, nil
}

// fakeToolkit is a no-op ToolExecutor that satisfies the runner contract.
type fakeToolkit struct{}

func (fakeToolkit) Definitions() []providers.ToolDefinition { return nil }
func (fakeToolkit) Execute(_ context.Context, _ providers.ToolCall) (string, error) {
	return "", nil
}

func TestSpawn_HappyPath(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "all done"}}
	mgr := NewManager(client, "fake-model")

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:         "explorer",
		AgentProfile: "qa workflow",
		Prompt:       "find foo",
		Toolkit:      fakeToolkit{},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sa.ID == "" || sa.Type != "explorer" {
		t.Fatalf("unexpected sub-agent: %+v", sa)
	}

	snap, err := mgr.Wait(context.Background(), sa.ID)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if snap.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", snap.Status)
	}
	if snap.Result != "all done" {
		t.Fatalf("got result %q", snap.Result)
	}
	if snap.AgentProfile != "qa workflow" {
		t.Fatalf("AgentProfile = %q, want qa workflow", snap.AgentProfile)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", client.calls.Load())
	}
}

func TestSpawn_UsesManagerDefaultRequestOptions(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "all done"}}
	mgr := NewManagerWithOptions(client, "worker-api-model", ManagerOptions{
		DefaultEffort: "high",
		DefaultProviderOptions: map[string]any{
			"reasoningEffort": "high",
		},
	})

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:    "worker",
		Prompt:  "do work",
		Toolkit: fakeToolkit{},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := mgr.Wait(context.Background(), sa.ID); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	req := client.lastRequest.Load()
	if req == nil {
		t.Fatal("expected a provider request")
	}
	if req.Model != "worker-api-model" || req.Effort != "high" {
		t.Fatalf("request did not use manager defaults: %+v", *req)
	}
	if got := req.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("ProviderOptions = %#v", req.ProviderOptions)
	}
}

func TestSpawn_LLMError(t *testing.T) {
	client := &fakeClient{err: errors.New("boom")}
	mgr := NewManager(client, "fake-model")

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:    "worker",
		Prompt:  "do thing",
		Toolkit: fakeToolkit{},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := mgr.Wait(context.Background(), sa.ID)
	if snap.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", snap.Status)
	}
	if snap.Error == nil {
		t.Fatal("expected error to be set")
	}
}

func TestSpawn_Cancel(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{Content: "ok"},
		delay:    2 * time.Second,
	}
	mgr := NewManager(client, "fake-model")

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:    "worker",
		Prompt:  "slow",
		Toolkit: fakeToolkit{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Give the goroutine a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	if !mgr.Stop(sa.ID) {
		t.Fatal("Stop returned false")
	}

	snap, _ := mgr.Wait(context.Background(), sa.ID)
	if snap.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", snap.Status)
	}
}

func TestStopAll(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{Content: "ok"},
		delay:    2 * time.Second,
	}
	mgr := NewManager(client, "fake-model")

	for i := 0; i < 3; i++ {
		_, err := mgr.Spawn(context.Background(), SpawnOptions{
			Type:    "worker",
			Prompt:  "slow",
			Toolkit: fakeToolkit{},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if mgr.CountRunning() != 3 {
		t.Fatalf("expected 3 running, got %d", mgr.CountRunning())
	}

	mgr.StopAll()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountRunning() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if mgr.CountRunning() != 0 {
		t.Fatalf("expected 0 running after StopAll, got %d", mgr.CountRunning())
	}
}

func TestNotifications(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "ok"}}
	mgr := NewManager(client, "fake-model")

	ch := make(chan Notification, 16)
	mgr.Subscribe(ch)

	sa, _ := mgr.Spawn(context.Background(), SpawnOptions{
		Type:    "explorer",
		Prompt:  "p",
		Toolkit: fakeToolkit{},
	})
	mgr.Wait(context.Background(), sa.ID)

	// Should have received: running + completed.
	statuses := []Status{}
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case n := <-ch:
			statuses = append(statuses, n.Status)
			if n.Status == StatusCompleted {
				break loop
			}
		case <-timeout:
			t.Fatalf("did not receive completed notification, got %v", statuses)
		}
	}
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 notifications, got %d: %v", len(statuses), statuses)
	}
}

func TestBroadcastSnapshotPublishesRunningUsage(t *testing.T) {
	mgr := NewManager(&fakeClient{}, "fake-model")
	ch := make(chan Notification, 1)
	mgr.Subscribe(ch)

	sa := &SubAgent{ID: "worker-1", Type: "worker", Status: StatusRunning, InputTokens: 12, OutputTokens: 7}
	mgr.BroadcastSnapshot(sa)

	select {
	case n := <-ch:
		if n.Status != StatusRunning {
			t.Fatalf("expected running status, got %s", n.Status)
		}
		if n.Snapshot.InputTokens != 12 || n.Snapshot.OutputTokens != 7 {
			t.Fatalf("unexpected usage snapshot: in=%d out=%d", n.Snapshot.InputTokens, n.Snapshot.OutputTokens)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected usage snapshot notification")
	}
}

func TestPersistHistory(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "subagents", "worker.json")

	client := &fakeClient{response: providers.ChatResponse{Content: "ok"}}
	mgr := NewManager(client, "fake-model")

	sa, _ := mgr.Spawn(context.Background(), SpawnOptions{
		Type:        "worker",
		Description: "test task",
		Prompt:      "do it",
		Toolkit:     fakeToolkit{},
		HistoryPath: historyPath,
	})
	mgr.Wait(context.Background(), sa.ID)

	if _, err := os.Stat(historyPath); err != nil {
		t.Fatalf("history file not written: %v", err)
	}
	data, _ := os.ReadFile(historyPath)
	if len(data) < 10 || !contains(string(data), "ok") {
		t.Fatalf("history file content unexpected: %s", data)
	}
}

func TestList(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "ok"}}
	mgr := NewManager(client, "fake-model")

	for i := 0; i < 3; i++ {
		_, _ = mgr.Spawn(context.Background(), SpawnOptions{
			Type:    "worker",
			Prompt:  "p",
			Toolkit: fakeToolkit{},
		})
	}
	if got := len(mgr.List()); got != 3 {
		t.Fatalf("expected 3 sub-agents in list, got %d", got)
	}
}

func TestSpawn_RequiresToolkitAndPrompt(t *testing.T) {
	mgr := NewManager(&fakeClient{}, "m")

	_, err := mgr.Spawn(context.Background(), SpawnOptions{Prompt: "x"})
	if err == nil {
		t.Error("expected error for missing toolkit")
	}
	_, err = mgr.Spawn(context.Background(), SpawnOptions{Toolkit: fakeToolkit{}})
	if err == nil {
		t.Error("expected error for missing prompt")
	}
}

// TestSpawn_WithInitialHistory_PrefixIsParentHistory verifies the
// fork code path in manager.run: when InitialHistory is non-nil, the
// worker's first request to the LLM should start with that exact
// history (preserving prompt-cache compatibility) and end with the
// caller's Prompt as the final user message.
func TestSpawn_WithInitialHistory_PrefixIsParentHistory(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "fork done"}}
	mgr := NewManager(client, "fake-model")

	parentHistory := []providers.ChatMessage{
		{Role: "system", Content: "you are the parent agent"},
		{Role: "user", Content: "fix the bug"},
		{
			Role:      "assistant",
			Content:   "let me read the file",
			ToolCalls: []providers.ToolCall{{ID: "call_read", Name: "read_file", Arguments: `{}`}},
		},
		{Role: "tool", Name: "read_file", ToolCallID: "call_read", Content: "file contents"},
	}

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:           "fork",
		Prompt:         "<system-reminder>do the thing</system-reminder>",
		Toolkit:        fakeToolkit{},
		InitialHistory: parentHistory,
		// SystemPrompt intentionally left empty — the fork path
		// uses the system message in InitialHistory[0] instead.
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	snap, err := mgr.Wait(context.Background(), sa.ID)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if snap.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s (err=%v)", snap.Status, snap.Error)
	}

	last := client.lastRequest.Load()
	if last == nil {
		t.Fatal("client never received a request")
	}
	got := visibleMessagesForTest(last.Messages)
	if len(got) != len(parentHistory)+1 {
		t.Fatalf("expected %d messages (parent history + 1 user), got %d",
			len(parentHistory)+1, len(got))
	}
	for i, want := range parentHistory {
		if got[i].Role != want.Role || got[i].Content != want.Content {
			t.Errorf("message %d: got {%s,%s}, want {%s,%s}",
				i, got[i].Role, got[i].Content, want.Role, want.Content)
		}
	}
	tail := got[len(got)-1]
	if tail.Role != "user" {
		t.Errorf("expected final message to be user, got %s", tail.Role)
	}
	if tail.Content != "<system-reminder>do the thing</system-reminder>" {
		t.Errorf("expected fork prompt as final message, got %q", tail.Content)
	}
}

// TestSpawn_WithoutInitialHistory_UsesSystemPrompt confirms the
// non-fork (regular spawn) code path is unchanged: when
// InitialHistory is nil, the runner builds [system, user] from
// SystemPrompt + Prompt as it always has.
func TestSpawn_WithoutInitialHistory_UsesSystemPrompt(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "spawn done"}}
	mgr := NewManager(client, "fake-model")

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:         "worker",
		Prompt:       "do the task",
		SystemPrompt: "you are a worker",
		Toolkit:      fakeToolkit{},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := mgr.Wait(context.Background(), sa.ID); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	last := client.lastRequest.Load()
	if last == nil {
		t.Fatal("client never received a request")
	}
	visible := visibleMessagesForTest(last.Messages)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible messages [system, user], got %+v", last.Messages)
	}
	if visible[0].Role != "system" || visible[0].Content != "you are a worker" {
		t.Errorf("system message wrong: %+v", visible[0])
	}
	if visible[1].Role != "user" || visible[1].Content != "do the task" {
		t.Errorf("user message wrong: %+v", visible[1])
	}
}

func TestFollowup_CompletedAgentStartsNewTurnWithHistory(t *testing.T) {
	client := &fakeClient{response: providers.ChatResponse{Content: "turn done"}}
	mgr := NewManager(client, "fake-model")

	sa, err := mgr.Spawn(context.Background(), SpawnOptions{
		Type:         "worker",
		Prompt:       "initial task",
		SystemPrompt: "you are a worker",
		Toolkit:      fakeToolkit{},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := mgr.Wait(context.Background(), sa.ID); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if ok := mgr.QueueMessage(sa.ID, "queued mailbox note"); !ok {
		t.Fatal("QueueMessage returned false")
	}

	snap, err := mgr.Followup(context.Background(), sa.ID, "continue task")
	if err != nil {
		t.Fatalf("Followup: %v", err)
	}
	if snap.Status != StatusRunning {
		t.Fatalf("expected follow-up turn running, got %s", snap.Status)
	}
	final, err := mgr.Wait(context.Background(), sa.ID)
	if err != nil {
		t.Fatalf("Wait follow-up: %v", err)
	}
	if final.Status != StatusCompleted {
		t.Fatalf("expected completed follow-up, got %s", final.Status)
	}
	if client.calls.Load() != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", client.calls.Load())
	}

	last := client.lastRequest.Load()
	if last == nil {
		t.Fatal("client never received follow-up request")
	}
	want := []struct {
		role    string
		content string
	}{
		{"system", "you are a worker"},
		{"user", "initial task"},
		{"assistant", "turn done"},
		{"user", "queued mailbox note"},
		{"user", "continue task"},
	}
	visible := visibleMessagesForTest(last.Messages)
	if len(visible) != len(want) {
		t.Fatalf("expected %d visible messages in follow-up request, got %+v", len(want), last.Messages)
	}
	for i, msg := range want {
		if visible[i].Role != msg.role || visible[i].Content != msg.content {
			t.Fatalf("message %d = {%s,%q}, want {%s,%q}", i, visible[i].Role, visible[i].Content, msg.role, msg.content)
		}
	}
}

func TestQueueMessageFIFO(t *testing.T) {
	sa := &SubAgent{}
	sa.pushPendingMessage("first")
	sa.pushPendingMessage("second")

	if got := sa.pendingCount(); got != 2 {
		t.Fatalf("expected pending=2, got %d", got)
	}
	m1, ok := sa.popPendingMessage()
	if !ok || m1 != "first" {
		t.Fatalf("expected first message, got %q ok=%v", m1, ok)
	}
	m2, ok := sa.popPendingMessage()
	if !ok || m2 != "second" {
		t.Fatalf("expected second message, got %q ok=%v", m2, ok)
	}
	if _, ok := sa.popPendingMessage(); ok {
		t.Fatal("expected empty queue after pops")
	}
}

func TestQueueMessageTrimAndDrainToUserMessages(t *testing.T) {
	sa := &SubAgent{}
	sa.pushPendingMessage("  hello  ")
	sa.pushPendingMessage("\t\n") // ignored
	sa.pushPendingMessage("world")

	msgs := sa.popPendingUserMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 drained messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("unexpected first drained message: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "world" {
		t.Fatalf("unexpected second drained message: %+v", msgs[1])
	}
	if got := sa.pendingCount(); got != 0 {
		t.Fatalf("expected queue drained, pending=%d", got)
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
