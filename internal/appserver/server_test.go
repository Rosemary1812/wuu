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
	"github.com/blueberrycongee/wuu/internal/tools"
)

type fakeClient struct {
	mu       sync.Mutex
	requests []providers.ChatRequest
	response providers.ChatResponse
}

func (f *fakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	return f.response, nil
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
	_ = New(rt, out)
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
	if updated.Agent.ID != res.AgentID || updated.Agent.TaskName != "check_bridge" {
		t.Fatalf("unexpected agent update: %+v", updated)
	}
	mailbox := remarshal[AgentMailboxNotification](t, notificationByMethod(t, msgs, NotificationAgentMailbox)["params"])
	if mailbox.Message.AgentID != res.AgentID || mailbox.Message.Result != "agent done" || mailbox.Message.Type != "agent_result" {
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
