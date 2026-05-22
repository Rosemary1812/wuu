package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
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
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).ThreadID
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
	if params.ThreadID != threadID || params.Content != "done" {
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

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(client.requests))
	}
	messages := client.requests[0].Messages
	if len(messages) < 2 || messages[0].Role != "system" || messages[1].Role != "user" || messages[1].Content != "hello" {
		t.Fatalf("unexpected agent-loop messages: %+v", messages)
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
	if result.ThreadID != sessionID || result.MessageCount != 3 {
		t.Fatalf("unexpected resume result: %+v", result)
	}
	resumed := remarshal[ThreadResumedNotification](t, notificationByMethod(t, msgs, NotificationThreadResumed)["params"])
	if resumed.ThreadID != sessionID || resumed.MessageCount != 3 {
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
		if msg["type"] == EnvelopeResponse && msg["id"] == id {
			return msg
		}
	}
	t.Fatalf("response id %s not found in %+v", id, msgs)
	return nil
}

func notificationByMethod(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["type"] == EnvelopeNotification && msg["method"] == method {
			return msg
		}
	}
	t.Fatalf("notification %s not found in %+v", method, msgs)
	return nil
}

func turnEventByType(t *testing.T, msgs []map[string]any, typ providers.StreamEventType) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["type"] != EnvelopeNotification || msg["method"] != NotificationTurnEvent {
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
