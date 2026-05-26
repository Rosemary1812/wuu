package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
)

type fakeClient struct {
	mu        sync.Mutex
	requests  []providers.ChatRequest
	responses []providers.ChatResponse
	response  providers.ChatResponse
}

func (f *fakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	if len(f.responses) > 0 {
		res := f.responses[0]
		f.responses = f.responses[1:]
		f.mu.Unlock()
		return res, nil
	}
	res := f.response
	f.mu.Unlock()
	return res, nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

type noopToolExecutor struct{}

func (noopToolExecutor) Definitions() []providers.ToolDefinition { return nil }
func (noopToolExecutor) Execute(_ context.Context, _ providers.ToolCall) (string, error) {
	return "", nil
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServerInitializeAndConfigRead(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"initialize"}`)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := srv.handleLine(context.Background(), []byte(`{"id":"2","method":"config/read"}`)); err != nil {
		t.Fatalf("config/read: %v", err)
	}

	msgs := parseOutput(t, out.String())
	initMsg := responseByID(t, msgs, "1")
	initResult := remarshal[InitializeResult](t, initMsg["result"])
	if initResult.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol version: %+v", initResult)
	}
	if initResult.Model != "fake-model" || initResult.Provider != "fake-provider" {
		t.Fatalf("unexpected initialize result: %+v", initResult)
	}

	configMsg := responseByID(t, msgs, "2")
	configResult := remarshal[ConfigReadResult](t, configMsg["result"])
	if configResult.ConfigPath == "" || configResult.SessionDir == "" {
		t.Fatalf("expected config paths, got %+v", configResult)
	}
}

func TestServerConfigModelUpdate(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"model":"new-model"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, msgs, "1")["result"])
	if result.Provider != "fake-provider" || result.Model != "new-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 1 || result.Providers[0].Name != "fake-provider" || result.Providers[0].Model != "new-model" {
		t.Fatalf("unexpected provider summaries: %+v", result.Providers)
	}
	if rt.Model != "new-model" || rt.StreamRunner.Model != "new-model" {
		t.Fatalf("runtime model not updated: runtime=%q stream_runner=%q", rt.Model, rt.StreamRunner.Model)
	}
	if rt.StreamRunner.ContextWindowOverride != providers.ContextWindowFor("new-model") {
		t.Fatalf("context window override not updated: got %d", rt.StreamRunner.ContextWindowOverride)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"model": "new-model"`) {
		t.Fatalf("config model was not persisted: %s", data)
	}
}

func TestServerConfigModelUpdateSwitchesProvider(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    },
    "codex-provider": {
      "type": "openai-codex",
      "base_url": "https://chatgpt.example.test/backend-api/codex",
      "model": "old-codex-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldClient := rt.StreamRunner.Client
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"provider":"codex-provider","model":"new-codex-model"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "codex-provider" || result.Model != "new-codex-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 2 {
		t.Fatalf("expected two provider summaries, got %+v", result.Providers)
	}
	if rt.ProviderName != "codex-provider" || rt.Model != "new-codex-model" || rt.StreamRunner.Model != "new-codex-model" {
		t.Fatalf("runtime provider/model not updated: provider=%q runtime=%q runner=%q", rt.ProviderName, rt.Model, rt.StreamRunner.Model)
	}
	if rt.StreamRunner.Client == oldClient {
		t.Fatal("expected stream runner client to be rebuilt")
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"default_provider": "codex-provider"`) ||
		!strings.Contains(string(data), `"model": "new-codex-model"`) {
		t.Fatalf("provider selection was not persisted: %s", data)
	}
}

func TestServerConfigModelUpdatePersistsEffort(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Effort = "medium"
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "agent": {
    "effort": "medium"
  },
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"model":"new-model","effort":"xhigh"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Model != "new-model" || result.Effort != "xhigh" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if rt.StreamRunner.Effort != "xhigh" {
		t.Fatalf("runtime effort not updated: %q", rt.StreamRunner.Effort)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"effort": "xhigh"`) {
		t.Fatalf("effort was not persisted: %s", data)
	}
}

func TestServerConfigCodexModels(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ProviderName = "openai-codex"
	rt.Model = "gpt-5.5"
	rt.StreamRunner.Model = "gpt-5.5"
	rt.StreamRunner.Effort = "xhigh"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "models": [
		    {"slug":"gpt-hidden","visibility":"hide","supported_in_api":true},
		    {"slug":"spark","display_name":"Spark","supported_in_api":false},
		    {"slug":"gpt-5.4","display_name":"GPT-5.4","priority":20,"supported_in_api":true},
		    {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":9,"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"xhigh"}],"supported_in_api":true}
		  ]
		}`))
	}))
	defer server.Close()

	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "agent": {
    "effort": "xhigh"
  },
  "default_provider": "openai-codex",
  "providers": {
    "openai-codex": {
      "type": "openai-codex",
      "base_url": "`+server.URL+`",
      "api_key": "test-token",
      "model": "gpt-5.5"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/codex/models"}`)); err != nil {
		t.Fatalf("config/codex/models: %v", err)
	}

	result := remarshal[ConfigCodexModelsResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "openai-codex" || result.Model != "gpt-5.5" || result.Effort != "xhigh" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Models) != 3 || result.Models[0].Slug != "gpt-5.5" || result.Models[1].Slug != "gpt-5.4" || result.Models[2].Slug != "spark" {
		t.Fatalf("unexpected models: %+v", result.Models)
	}
	if got := result.Models[0].SupportedReasoning; len(got) != 2 || got[0] != "low" || got[1] != "xhigh" {
		t.Fatalf("unexpected reasoning levels: %+v", got)
	}
}

func TestServerTurnStartRunsAgentLoop(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{
			Content: "done",
			Usage:   &providers.TokenUsage{InputTokens: 10, OutputTokens: 3},
		},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	if strings.TrimSpace(threadID) == "" {
		t.Fatal("expected thread id")
	}

	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "hello"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := notificationByMethod(t, msgs, NotificationTurnCompleted)
	params := remarshal[TurnCompletedNotification](t, completed["params"])
	if params.ThreadID != threadID || params.Turn.ID == "" || params.Turn.Status != TurnStatusCompleted || params.Content != "done" {
		t.Fatalf("unexpected completion: %+v", params)
	}
	if params.Turn.StartedAt == nil || params.Turn.CompletedAt == nil || params.Turn.DurationMS == nil {
		t.Fatalf("completed turn should include timing: %+v", params.Turn)
	}
	if params.InputTokens != 10 || params.OutputTokens != 3 {
		t.Fatalf("unexpected usage: %+v", params)
	}

	event := turnEventByType(t, msgs, providers.EventContentDelta)
	eventParams := remarshal[TurnEventNotification](t, event["params"])
	if eventParams.Event.Type != providers.EventContentDelta || eventParams.Event.Content != "done" {
		t.Fatalf("unexpected turn event: %+v", eventParams)
	}
	delta := notificationByMethod(t, msgs, NotificationAgentMessageDelta)
	deltaParams := remarshal[AgentMessageDeltaNotification](t, delta["params"])
	if deltaParams.ThreadID != threadID || deltaParams.Delta != "done" {
		t.Fatalf("unexpected agent delta: %+v", deltaParams)
	}
	itemCompleted := notificationByMethod(t, msgs, NotificationItemCompleted)
	itemParams := remarshal[ItemCompletedNotification](t, itemCompleted["params"])
	if itemParams.Item.Type != ThreadItemAgentMessage || itemParams.Item.Text != "done" {
		t.Fatalf("unexpected completed item: %+v", itemParams)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(client.requests))
	}
	messages := client.requests[0].Messages
	if len(messages) < 2 || messages[0].Role != "system" || messages[1].Role != "user" || messages[1].Content != "hello" {
		t.Fatalf("unexpected agent-loop messages: %+v", messages)
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(persisted) != 2 || persisted[0].Role != "user" || persisted[0].Content != "hello" || persisted[1].Role != "assistant" || persisted[1].Content != "done" {
		t.Fatalf("unexpected persisted history: %+v", persisted)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != threadID || sessions[0].Entries != 2 || sessions[0].Summary != "hello" {
		t.Fatalf("unexpected session index: %+v", sessions)
	}
}

func TestServerThreadForkAtAssistantItem(t *testing.T) {
	client := &fakeClient{
		responses: []providers.ChatResponse{
			{Content: "first answer"},
			{Content: "second answer"},
		},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	startTurn := func(id, prompt string, completedCount int) Turn {
		t.Helper()
		payload := map[string]any{
			"id":     id,
			"method": MethodTurnStart,
			"params": TurnStartParams{ThreadID: threadID, Prompt: prompt},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal turn request: %v", err)
		}
		if err := srv.handleLine(context.Background(), raw); err != nil {
			t.Fatalf("turn/start: %v", err)
		}
		msgs := waitForNotificationCount(t, out, NotificationTurnCompleted, completedCount)
		completed := notificationsByMethod(msgs, NotificationTurnCompleted)
		return remarshal[TurnCompletedNotification](t, completed[len(completed)-1]["params"]).Turn
	}

	firstTurn := startTurn("2", "first prompt", 1)
	var firstAgentItem ThreadItem
	for _, item := range firstTurn.Items {
		if item.Type == ThreadItemAgentMessage {
			firstAgentItem = item
			break
		}
	}
	if firstAgentItem.ID == "" {
		t.Fatalf("expected first turn to contain assistant item: %+v", firstTurn)
	}
	_ = startTurn("3", "second prompt", 2)

	payload := map[string]any{
		"id":     "4",
		"method": MethodThreadFork,
		"params": ThreadForkParams{
			ThreadID: threadID,
			TurnID:   firstTurn.ID,
			ItemID:   firstAgentItem.ID,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fork request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/fork: %v", err)
	}

	msgs := parseOutput(t, out.String())
	forkResponse := responseByID(t, msgs, "4")
	if forkResponse["error"] != nil {
		t.Fatalf("thread/fork returned error: %+v", forkResponse["error"])
	}
	result := remarshal[ThreadForkResult](t, forkResponse["result"])
	fork := result.Thread
	if fork.ID == "" || fork.ID == threadID {
		t.Fatalf("expected new fork thread id, got %+v", fork)
	}
	if fork.ForkedFromID != threadID || fork.ForkedFromTurnID != firstTurn.ID || fork.ForkedFromItemID != firstAgentItem.ID {
		t.Fatalf("fork metadata not returned: %+v", fork)
	}
	if len(fork.Turns) != 1 || len(fork.Turns[0].Items) != 2 {
		t.Fatalf("expected fork to stop at first assistant item, got %+v", fork.Turns)
	}
	if fork.Turns[0].Items[0].Text != "first prompt" || fork.Turns[0].Items[1].Text != "first answer" {
		t.Fatalf("unexpected fork turn items: %+v", fork.Turns[0].Items)
	}

	forkHistory, err := loadChatMessages(session.FilePath(rt.SessionDir, fork.ID))
	if err != nil {
		t.Fatalf("load fork history: %v", err)
	}
	if len(forkHistory) != 2 || forkHistory[0].Content != "first prompt" || forkHistory[1].Content != "first answer" {
		t.Fatalf("unexpected persisted fork history: %+v", forkHistory)
	}
	sourceHistory, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load source history: %v", err)
	}
	if len(sourceHistory) != 4 {
		t.Fatalf("source history should remain intact, got %+v", sourceHistory)
	}

	metadata, ok, err := session.Find(rt.SessionDir, fork.ID)
	if err != nil {
		t.Fatalf("find fork metadata: %v", err)
	}
	if !ok || metadata.ForkedFromID != threadID || metadata.ForkedFromTurnID != firstTurn.ID || metadata.ForkedFromItemID != firstAgentItem.ID {
		t.Fatalf("fork metadata not persisted: ok=%v metadata=%+v", ok, metadata)
	}
	started := notificationsByMethod(msgs, NotificationThreadStarted)
	if len(started) < 2 {
		t.Fatalf("expected fork to emit thread/started notification, got %+v", msgs)
	}
	forkStarted := remarshal[ThreadStartedNotification](t, started[len(started)-1]["params"])
	if forkStarted.Thread.ID != fork.ID {
		t.Fatalf("unexpected fork started notification: %+v", forkStarted)
	}
}

func TestServerTurnStartAcceptsImageOnlyPrompt(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{Content: "saw it"},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{
			ThreadID: threadID,
			Images: []TurnStartImage{{
				MediaType: "image/png",
				Data:      "ZmFrZS1pbWFnZQ==",
			}},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	started := remarshal[TurnStartResult](t, responseByID(t, msgs, "2")["result"])
	if len(started.Turn.Items) != 1 || len(started.Turn.Items[0].Images) != 1 {
		t.Fatalf("start response missing user image: %+v", started.Turn)
	}
	completed := remarshal[TurnCompletedNotification](t, notificationByMethod(t, msgs, NotificationTurnCompleted)["params"])
	if len(completed.Turn.Items) < 1 || len(completed.Turn.Items[0].Images) != 1 {
		t.Fatalf("completed turn missing user image: %+v", completed.Turn)
	}

	client.mu.Lock()
	requestCount := len(client.requests)
	var messages []providers.ChatMessage
	if requestCount > 0 {
		messages = append([]providers.ChatMessage(nil), client.requests[0].Messages...)
	}
	client.mu.Unlock()
	if requestCount != 1 {
		t.Fatalf("expected one provider request, got %d", requestCount)
	}
	if len(messages) < 2 || messages[1].Role != "user" || messages[1].Content != "" || len(messages[1].Images) != 1 {
		t.Fatalf("unexpected provider messages: %+v", messages)
	}
	if messages[1].Images[0].MediaType != "image/png" || messages[1].Images[0].Data != "ZmFrZS1pbWFnZQ==" {
		t.Fatalf("unexpected provider image: %+v", messages[1].Images[0])
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(persisted) != 2 || len(persisted[0].Images) != 1 {
		t.Fatalf("unexpected persisted history: %+v", persisted)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Summary != "[Image #1]" {
		t.Fatalf("unexpected session index: %+v", sessions)
	}
}

func TestServerThreadListUsesSessionIndexMetadata(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if _, err := session.CreateWithMetadata(rt.SessionDir, "old-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "old-thread", 2, "old summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "new-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "new-thread", 2, "new summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "archived-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if _, err := session.UpdateArchived(rt.SessionDir, "archived-thread", true); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "other-thread", filepath.Join(rt.RootDir, "other")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.UpdatePinned(rt.SessionDir, "old-thread", true); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 2 {
		t.Fatalf("expected two visible workspace threads, got %+v", result.Threads)
	}
	if result.Threads[0].ID != "old-thread" || !result.Threads[0].Pinned || result.Threads[0].Preview != "old summary" {
		t.Fatalf("expected pinned old thread first, got %+v", result.Threads)
	}
	if result.Threads[1].ID != "new-thread" || result.Threads[1].Archived {
		t.Fatalf("unexpected second thread: %+v", result.Threads[1])
	}
}

func TestServerThreadListOrdersSessionsByUpdatedAt(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if _, err := session.CreateWithMetadata(rt.SessionDir, "first-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "first-thread", 2, "first summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "second-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "second-thread", 2, "second summary"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := session.UpdateIndex(rt.SessionDir, "first-thread", 4, "ignored later summary"); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 2 {
		t.Fatalf("expected two visible workspace threads, got %+v", result.Threads)
	}
	if result.Threads[0].ID != "first-thread" || result.Threads[1].ID != "second-thread" {
		t.Fatalf("expected recently updated thread first, got %+v", result.Threads)
	}
	if !result.Threads[0].UpdatedAt.After(result.Threads[1].UpdatedAt) {
		t.Fatalf("expected first thread updated_at to be newer, got %+v", result.Threads)
	}
}

func TestServerThreadListIncludesDirectChildAgents(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu", "state")
	if _, err := session.CreateWithMetadata(rt.SessionDir, "root-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "root-thread", 2, "root summary"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	store := agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "threads"))
	metas := []agentthread.Metadata{
		{
			ID:        "root-thread",
			Path:      agentthread.RootPath,
			Status:    agentthread.StatusRunning,
			CreatedAt: now,
			UpdatedAt: now,
			Source:    agentthread.Source{Kind: agentthread.SourceRoot, Depth: 1},
		},
		{
			ID:        "worker-1",
			SessionID: "root-thread",
			ParentID:  "root-thread",
			Path:      "/root/inspect",
			TaskName:  "inspect",
			Role:      "worker",
			Status:    agentthread.StatusRunning,
			CreatedAt: now.Add(time.Second),
			UpdatedAt: now.Add(time.Second),
			Source: agentthread.Source{
				Kind:           agentthread.SourceThreadSpawn,
				ParentThreadID: "root-thread",
				ParentPath:     agentthread.RootPath,
				Depth:          2,
			},
		},
		{
			ID:        "worker-2",
			SessionID: "root-thread",
			ParentID:  "worker-1",
			Path:      "/root/inspect/deeper",
			TaskName:  "deeper",
			Role:      "worker",
			Status:    agentthread.StatusPending,
			CreatedAt: now.Add(2 * time.Second),
			UpdatedAt: now.Add(2 * time.Second),
			Source: agentthread.Source{
				Kind:           agentthread.SourceThreadSpawn,
				ParentThreadID: "worker-1",
				ParentPath:     "/root/inspect",
				Depth:          3,
			},
		},
	}
	for _, meta := range metas {
		if err := store.UpsertThread(meta); err != nil {
			t.Fatalf("upsert thread %s: %v", meta.ID, err)
		}
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 1 {
		t.Fatalf("expected one root thread, got %+v", result.Threads)
	}
	agents := result.Threads[0].ChildAgents
	if len(agents) != 1 {
		t.Fatalf("expected only the direct child agent, got %+v", agents)
	}
	if agents[0].ID != "worker-1" || agents[0].TaskName != "inspect" || agents[0].NestedCount != 1 || agents[0].NestedRunningCount != 1 {
		t.Fatalf("unexpected child agent summary: %+v", agents[0])
	}
}

func TestServerThreadResumeLoadsChildAgentSession(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu", "state")
	if _, err := session.CreateWithMetadata(rt.SessionDir, "root-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "root-thread", 2, "root summary"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	meta := agentthread.Metadata{
		ID:              "worker-1",
		SessionID:       "root-thread",
		ParentID:        "root-thread",
		Path:            "/root/inspect",
		TaskName:        "inspect",
		Role:            "worker",
		LastTaskMessage: "inspect the UI",
		CWD:             rt.RootDir,
		Model:           "worker-model",
		Status:          agentthread.StatusCompleted,
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
		Source: agentthread.Source{
			Kind:           agentthread.SourceThreadSpawn,
			ParentThreadID: "root-thread",
			ParentPath:     agentthread.RootPath,
			Depth:          2,
		},
	}
	store := agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "threads"))
	if err := store.UpsertThread(meta); err != nil {
		t.Fatalf("upsert worker thread: %v", err)
	}

	workerDir := filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "workers")
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		t.Fatalf("mkdir worker history: %v", err)
	}
	rec := persistedAgentHistory{
		ID:          "worker-1",
		Type:        "worker",
		TaskName:    "inspect",
		AgentPath:   "/root/inspect",
		ParentID:    "root-thread",
		Description: "inspect",
		Status:      "completed",
		StartedAt:   now,
		CompletedAt: now.Add(time.Minute),
		Model:       "worker-model",
		Prompt:      "inspect the UI",
		Result:      "child session result",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "worker system"},
			{Role: "user", Content: "inspect the UI"},
			{Role: "assistant", Content: "child session result"},
		},
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal worker history: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workerDir, "worker-1.json"), data, 0o644); err != nil {
		t.Fatalf("write worker history: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload, err := json.Marshal(map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: "worker-1"},
	})
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), payload); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, msgs, "1")["result"])
	thread := result.Thread
	if thread.ID != "worker-1" || !thread.ReadOnly || thread.ParentID != "root-thread" || thread.AgentPath != "/root/inspect" {
		t.Fatalf("unexpected child thread identity: %+v", thread)
	}
	if thread.Model != "worker-model" || thread.Preview != "inspect" {
		t.Fatalf("unexpected child thread metadata: %+v", thread)
	}
	if len(thread.Turns) != 1 || len(thread.Turns[0].Items) != 2 {
		t.Fatalf("unexpected child thread turns: %+v", thread.Turns)
	}
	if got := thread.Turns[0].Items[1].Text; got != "child session result" {
		t.Fatalf("unexpected child agent message: %q", got)
	}
	resumed := remarshal[ThreadResumedNotification](t, notificationByMethod(t, msgs, NotificationThreadResumed)["params"])
	if resumed.Thread.ID != "worker-1" || !resumed.Thread.ReadOnly {
		t.Fatalf("unexpected resumed notification: %+v", resumed)
	}
}

func TestServerThreadPinAndArchive(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	pinPayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadPin,
		"params": ThreadPinParams{ThreadID: threadID, Pinned: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), pinPayload); err != nil {
		t.Fatalf("thread/pin: %v", err)
	}
	pinResult := remarshal[ThreadPinResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if pinResult.Thread.ID != threadID || !pinResult.Thread.Pinned {
		t.Fatalf("unexpected pin result: %+v", pinResult)
	}
	pinned, ok, err := session.Find(rt.SessionDir, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pinned.PinnedAt == nil {
		t.Fatalf("pin not persisted: ok=%v session=%+v", ok, pinned)
	}

	archivePayload, err := json.Marshal(map[string]any{
		"id":     "3",
		"method": MethodThreadArchive,
		"params": ThreadArchiveParams{ThreadID: threadID, Archived: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), archivePayload); err != nil {
		t.Fatalf("thread/archive: %v", err)
	}
	archiveResult := remarshal[ThreadArchiveResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"])
	if archiveResult.Thread.ID != threadID || !archiveResult.Thread.Archived || archiveResult.Thread.Pinned {
		t.Fatalf("unexpected archive result: %+v", archiveResult)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"4","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	listResult := remarshal[ThreadListResult](t, responseByID(t, parseOutput(t, out.String()), "4")["result"])
	if len(listResult.Threads) != 0 {
		t.Fatalf("archived thread should be hidden, got %+v", listResult.Threads)
	}
}

func TestServerRejectsUnknownTurnParams(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"turn/start","params":{"thread_id":"x","prompt":"p","extra":true}}`)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "1")
	if resp["error"] == nil {
		t.Fatalf("expected response error, got %+v", resp)
	}
}

func TestServerTurnItemsIncludeReasoningAndAgentMessage(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{
			ReasoningContent: "inspect first",
			Content:          "done",
		},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	raw, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "hello"},
	})
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := remarshal[TurnCompletedNotification](t, notificationByMethod(t, msgs, NotificationTurnCompleted)["params"])
	var reasoning, agent int
	for _, item := range completed.Turn.Items {
		switch item.Type {
		case ThreadItemReasoning:
			reasoning++
			if item.Text != "inspect first" {
				t.Fatalf("unexpected reasoning item: %+v", item)
			}
		case ThreadItemAgentMessage:
			agent++
			if item.Text != "done" {
				t.Fatalf("unexpected agent item: %+v", item)
			}
		}
	}
	if reasoning != 1 || agent != 1 {
		t.Fatalf("expected one reasoning and one agent item, got reasoning=%d agent=%d turn=%+v", reasoning, agent, completed.Turn)
	}
}

func TestServerAskUserUsesClientResponse(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = withAskUserThreadID(ctx, "thread-ask")

	done := make(chan tools.AskUserResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := srv.AskUser(ctx, tools.AskUserRequest{
			Questions: []tools.AskUserQuestion{{
				Question: "Continue?",
				Header:   "Continue",
				Options: []tools.AskUserOption{
					{Label: "Yes", Description: "Continue the turn."},
					{Label: "No", Description: "Stop now."},
				},
			}},
		})
		if err != nil {
			errCh <- err
			return
		}
		done <- resp
	}()

	msgs := waitForMethod(t, out, MethodToolRequestUserInput)
	request := requestByMethod(t, msgs, MethodToolRequestUserInput)
	params := remarshal[ToolRequestUserInputParams](t, request["params"])
	if params.ThreadID != "thread-ask" {
		t.Fatalf("ask_user request missing thread id: %+v", params)
	}
	raw, err := json.Marshal(map[string]any{
		"id": request["id"],
		"result": tools.AskUserResponse{
			Answers: map[string]string{"Continue?": "Yes"},
		},
	})
	if err != nil {
		t.Fatalf("marshal client response: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("client response: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("AskUser returned error: %v", err)
	case resp := <-done:
		if resp.Answers["Continue?"] != "Yes" {
			t.Fatalf("unexpected AskUser response: %+v", resp)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for AskUser")
	}
}

func TestServerThreadResumeLoadsSessionHistory(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.MkdirAll(rt.SessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionID := "20260523-000000-test"
	sessionPath := filepath.Join(rt.SessionDir, sessionID+".jsonl")
	history := strings.Join([]string{
		`{"role":"system","content":"system prompt"}`,
		`{"role":"user","content":"hello"}`,
		`{"role":"assistant","content":"done"}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(history), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload := map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sessionID},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, msgs, "1")["result"])
	if result.Thread.ID != sessionID || len(result.Thread.Turns) != 1 {
		t.Fatalf("unexpected resume result: %+v", result)
	}
	if turn := result.Thread.Turns[0]; turn.StartedAt != nil || turn.CompletedAt != nil || turn.DurationMS != nil {
		t.Fatalf("historical turn should leave unknown timing unset: %+v", turn)
	}
	resumed := remarshal[ThreadResumedNotification](t, notificationByMethod(t, msgs, NotificationThreadResumed)["params"])
	if resumed.Thread.ID != sessionID || len(resumed.Thread.Turns) != 1 {
		t.Fatalf("unexpected resume notification: %+v", resumed)
	}

	th := srv.thread(sessionID)
	if th == nil {
		t.Fatal("expected resumed thread")
	}
	if len(th.History) != 3 || th.History[1].Role != "user" || th.History[1].Content != "hello" {
		t.Fatalf("unexpected resumed history: %+v", th.History)
	}
}

func TestServerThreadResumeReturnsLoadedRunningThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	th := srv.thread(threadID)
	if th == nil {
		t.Fatal("expected loaded thread")
	}
	now := time.Now().UTC()
	th.mu.Lock()
	th.startTurnLocked("turn-loaded-running", providers.ChatMessage{Role: "user", Content: "keep running"}, now)
	th.mu.Unlock()

	raw, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: threadID},
	})
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	if srv.thread(threadID) != th {
		t.Fatal("resume should not replace an already loaded thread")
	}
	result := remarshal[ThreadResumeResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if result.Thread.ID != threadID || result.Thread.Status != ThreadStatusInProgress || len(result.Thread.Turns) != 1 {
		t.Fatalf("unexpected loaded resume result: %+v", result.Thread)
	}
}

func TestServerThreadResumeNormalizesToolResultOrder(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.MkdirAll(rt.SessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionID := "20260523-000001-tools"
	sessionPath := filepath.Join(rt.SessionDir, sessionID+".jsonl")
	history := strings.Join([]string{
		`{"role":"system","content":"system prompt"}`,
		`{"role":"user","content":"inspect"}`,
		`{"role":"assistant","tool_calls":[{"id":"call_1","name":"read_file","arguments":"{}"}]}`,
		`{"role":"user","content":"mid-turn context"}`,
		`{"role":"tool","name":"read_file","tool_call_id":"call_1","content":"ok"}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(history), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload := map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sessionID},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	th := srv.thread(sessionID)
	if th == nil {
		t.Fatal("expected resumed thread")
	}
	if err := providers.ValidateMessageSequence(th.History); err != nil {
		t.Fatalf("expected valid resumed history, got %v: %+v", err, th.History)
	}
	roles := make([]string, 0, len(th.History))
	for _, msg := range th.History {
		roles = append(roles, msg.Role)
	}
	if got, want := strings.Join(roles, ","), "system,user,assistant,tool,user"; got != want {
		t.Fatalf("unexpected resumed order: got %s want %s", got, want)
	}

	persisted, err := loadChatMessages(sessionPath)
	if err != nil {
		t.Fatalf("load rewritten session: %v", err)
	}
	roles = roles[:0]
	for _, msg := range persisted {
		roles = append(roles, msg.Role)
	}
	if got, want := strings.Join(roles, ","), "user,assistant,tool,user"; got != want {
		t.Fatalf("unexpected persisted order: got %s want %s", got, want)
	}
}

func TestTurnsFromHistoryRestoresToolCallItems(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "user", Content: "inspect"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: `{"path":"internal/appserver/model.go"}`,
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"path":"internal/appserver/model.go","num_lines":20}`,
		},
		{Role: "assistant", Content: "done"},
	}

	turns := turnsFromHistory("thread", history, time.Unix(0, 0).UTC())
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 3 {
		t.Fatalf("expected user, tool, and assistant items, got %+v", items)
	}
	toolItem := items[1]
	if toolItem.Type != ThreadItemToolCall || toolItem.Name != "read_file" || toolItem.Arguments == "" || toolItem.Result == "" {
		t.Fatalf("unexpected restored tool item: %+v", toolItem)
	}
	if items[2].Type != ThreadItemAgentMessage || items[2].Text != "done" {
		t.Fatalf("unexpected assistant item: %+v", items[2])
	}
}

func TestTurnsFromHistoryRestoresCollabAgentToolItems(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "user", Content: "delegate"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "spawn_agent",
				Arguments: `{"task_name":"inspect","message":"inspect"}`,
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"agent_id":"worker-1","agent_path":"/root/inspect","status":"running"}`,
		},
	}

	turns := turnsFromHistory("thread", history, time.Unix(0, 0).UTC())
	if len(turns) != 1 || len(turns[0].Items) != 2 {
		t.Fatalf("unexpected turns: %+v", turns)
	}
	item := turns[0].Items[1]
	if item.Type != ThreadItemCollabAgentTool || item.Name != "spawn_agent" || item.Result == "" {
		t.Fatalf("unexpected collab agent item: %+v", item)
	}
}

func TestServerForwardsAgentNotifications(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	workerClient := &fakeClient{response: providers.ChatResponse{Content: "agent done"}}
	coord, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(workerClient),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "sess-agents",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	rt.AgentControl = coord

	out := &lockedBuffer{}
	srv := New(rt, out)
	threadID := "sess-agents"
	srv.subscribeThreadRuntime(threadID, &runtime.ThreadRuntime{AgentControl: coord})
	res, err := coord.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "check_bridge",
		Description: "check bridge",
		Prompt:      "do it",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationAgentMailbox)
	updated := remarshal[AgentUpdatedNotification](t, notificationByMethod(t, msgs, NotificationAgentUpdated)["params"])
	if updated.ThreadID != threadID || updated.Agent.ID != res.AgentID || updated.Agent.TaskName != "check_bridge" {
		t.Fatalf("unexpected agent update: %+v", updated)
	}
	mailbox := remarshal[AgentMailboxNotification](t, notificationByMethod(t, msgs, NotificationAgentMailbox)["params"])
	if mailbox.ThreadID != threadID || mailbox.Message.AgentID != res.AgentID || mailbox.Message.Result != "agent done" || mailbox.Message.Type != "agent_result" {
		t.Fatalf("unexpected mailbox notification: %+v", mailbox)
	}
}

func newTestRuntime(t *testing.T, client *fakeClient) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	return &runtime.Session{
		ProviderName: "fake-provider",
		Model:        "fake-model",
		RootDir:      root,
		ConfigPath:   root + "/.wuu.json",
		SessionDir:   root + "/.wuu/sessions",
		StreamRunner: &agent.StreamRunner{
			Client:       providers.AdaptStreamClient(client),
			Model:        "fake-model",
			SystemPrompt: "system prompt",
		},
	}
}

func parseOutput(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var msgs []map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("parse output line %q: %v", line, err)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func waitForMethod(t *testing.T, out *lockedBuffer, method string) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		for _, msg := range msgs {
			if msg["method"] == method {
				return msgs
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; output:\n%s", method, out.String())
	return nil
}

func waitForNotificationCount(t *testing.T, out *lockedBuffer, method string, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		if len(notificationsByMethod(msgs, method)) >= count {
			return msgs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %s notifications; output:\n%s", count, method, out.String())
	return nil
}

func responseByID(t *testing.T, msgs []map[string]any, id string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["id"] == id && msg["method"] == nil {
			return msg
		}
	}
	t.Fatalf("response id %s not found in %+v", id, msgs)
	return nil
}

func notificationByMethod(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] == nil {
			return msg
		}
	}
	t.Fatalf("notification %s not found in %+v", method, msgs)
	return nil
}

func notificationsByMethod(msgs []map[string]any, method string) []map[string]any {
	var out []map[string]any
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] == nil {
			out = append(out, msg)
		}
	}
	return out
}

func requestByMethod(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] != nil {
			return msg
		}
	}
	t.Fatalf("request %s not found in %+v", method, msgs)
	return nil
}

func turnEventByType(t *testing.T, msgs []map[string]any, typ providers.StreamEventType) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["id"] != nil || msg["method"] != NotificationTurnEvent {
			continue
		}
		params := remarshal[TurnEventNotification](t, msg["params"])
		if params.Event.Type == typ {
			return msg
		}
	}
	t.Fatalf("turn event %s not found in %+v", typ, msgs)
	return nil
}

func remarshal[T any](t *testing.T, value any) T {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	return out
}
