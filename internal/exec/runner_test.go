package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

type fakeController struct {
	initResult appserver.InitializeResult
	thread     appserver.Thread
	turn       appserver.Turn
	events     []Notification

	startedThread bool
	resumedThread string
	startedPrompt string
	interrupted   string
	shutdown      bool
}

func newFakeController(events ...Notification) *fakeController {
	return &fakeController{
		initResult: appserver.InitializeResult{
			ProtocolVersion: appserver.ProtocolVersion,
			Provider:        "test-provider",
			Model:           "test-model",
			WorkspaceRoot:   "/repo",
			Permissions:     appserver.PermissionSummary{Mode: "default"},
		},
		thread: appserver.Thread{ID: "thread-1", ModelProvider: "test-provider", Model: "test-model", CWD: "/repo"},
		turn:   appserver.Turn{ID: "turn-1"},
		events: events,
	}
}

func (f *fakeController) Initialize(context.Context) (appserver.InitializeResult, error) {
	return f.initResult, nil
}

func (f *fakeController) StartThread(context.Context) (appserver.Thread, error) {
	f.startedThread = true
	return f.thread, nil
}

func (f *fakeController) ResumeThread(_ context.Context, id string) (appserver.Thread, error) {
	f.resumedThread = id
	return f.thread, nil
}

func (f *fakeController) StartTurn(_ context.Context, _ string, prompt string) (appserver.Turn, error) {
	f.startedPrompt = prompt
	return f.turn, nil
}

func (f *fakeController) Interrupt(_ context.Context, threadID string) error {
	f.interrupted = threadID
	return nil
}

func (f *fakeController) Shutdown(context.Context) error {
	f.shutdown = true
	return nil
}

func (f *fakeController) Notifications() <-chan Notification {
	ch := make(chan Notification, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	return ch
}

func TestRunDefaultStdoutOnlyFinalMessage(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{ThreadID: "thread-1", TurnID: "turn-1", Delta: "partial"}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "final answer", TracePath: "/trace.jsonl"}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "do work",
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := stdout.String(); got != "final answer\n" {
		t.Fatalf("stdout = %q", got)
	}
	if strings.Contains(stdout.String(), "provider") || strings.Contains(stdout.String(), "trace_path") {
		t.Fatalf("stdout should contain only final answer, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "provider: test-provider") || !strings.Contains(stderr.String(), "trace_path: /trace.jsonl") {
		t.Fatalf("stderr missing run metadata: %q", stderr.String())
	}
	if !controller.startedThread || controller.startedPrompt != "do work" || !controller.shutdown {
		t.Fatalf("controller did not run expected app-server path: %+v", controller)
	}
}

func TestRunJSONLEmitsStableEvents(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{ThreadID: "thread-1", TurnID: "turn-1", Delta: "hello"}),
		notification(appserver.NotificationTurnUsage, appserver.TurnUsageNotification{ThreadID: "thread-1", TurnID: "turn-1", InputTokens: 3, OutputTokens: 4}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "hello", TracePath: "/trace.jsonl"}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("json mode should not write diagnostics to stderr in fake run, got %q", stderr.String())
	}
	events := parseJSONLines(t, stdout.String())
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event["type"].(string))
	}
	wantTypes := []string{"session_configured", "thread_started", "turn_started", "agent_message_delta", "usage_updated", "turn_completed", "result"}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types:\n got: %#v\nwant: %#v\njsonl:\n%s", gotTypes, wantTypes, stdout.String())
	}
	result := events[len(events)-1]
	if result["status"] != "completed" || result["thread_id"] != "thread-1" || result["turn_id"] != "turn-1" || result["final_message"] != "hello" || result["trace_path"] != "/trace.jsonl" {
		t.Fatalf("unexpected result event: %+v", result)
	}
}

func TestRunResumeLastUsesResumePath(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "continued"}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "continue",
		ResumeLast: true,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.startedThread {
		t.Fatal("resume should not start a new thread")
	}
	if controller.resumedThread != "" {
		t.Fatalf("resume last should pass empty thread id, got %q", controller.resumedThread)
	}
}

func TestRunTurnErrorReturnsExitCodeOne(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{ThreadID: "thread-1", TurnID: "turn-1", Error: "model failed"}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitTurnFailed {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	events := parseJSONLines(t, stdout.String())
	result := events[len(events)-1]
	if result["status"] != "failed" || result["error"] != "model failed" {
		t.Fatalf("unexpected failure result: %+v", result)
	}
}

func notification(method string, params any) Notification {
	data, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return Notification{Method: method, Params: data}
}

func parseJSONLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var events []map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}
