package appserver

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/automation"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func TestAutomationRequestContextIsHiddenAndRequestOnly(t *testing.T) {
	segments := automationRequestContext(
		automation.Task{ID: "task-1", Cron: "*/5 * * * *", Timezone: "UTC", Mode: string(automation.ModeThreadHeartbeat)},
		automation.Run{ID: "run-1", TriggeredAt: time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)},
	)
	if len(segments) != 1 {
		t.Fatalf("segments = %#v", segments)
	}
	segment := segments[0]
	if segment.Lifecycle != agent.ContextSegmentRequestOnly || segment.Durable || segment.VisibleInUI {
		t.Fatalf("automation context must be hidden and request-only: %#v", segment)
	}
	if len(segment.Messages) != 1 || segment.Messages[0].Role != "user" || !strings.Contains(segment.Messages[0].Content, "task_id: task-1") || !strings.Contains(segment.Messages[0].Content, "run_id: run-1") {
		t.Fatalf("automation context message = %#v", segment.Messages)
	}
}

func TestAutomationRequestContextKeepsProviderMessageOrderValid(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "scheduled task"},
	}
	segments := automationRequestContext(
		automation.Task{ID: "task-1", Cron: "*/5 * * * *", Timezone: "UTC", Mode: string(automation.ModeNewThread)},
		automation.Run{ID: "run-1", TriggeredAt: time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)},
	)
	messages := append(append([]providers.ChatMessage(nil), history...), segments[0].Messages...)
	if err := providers.ValidateToolCallHistory(messages); err != nil {
		t.Fatalf("automation provider message sequence = %#v: %v", messages, err)
	}
}

func TestAutomationNewThreadRunsAsPersistedAutomationThread(t *testing.T) {
	client := &fakeClient{response: providersResponse("done")}
	rt := newTestRuntime(t, client)
	rt.StateDir = filepath.Dir(rt.SessionDir)
	rt.AutomationManager = automation.NewManager(automation.Config{StateDir: rt.StateDir})
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(srv.Close)

	task := automation.Task{
		ID: "new-thread-task", Prompt: "inspect the build", Mode: string(automation.ModeNewThread),
		Cron: "*/5 * * * *", Timezone: "UTC",
	}
	if err := rt.AutomationManager.Fire(context.Background(), task); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	runs, err := rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].ThreadID == "" {
		t.Fatalf("runs after admission = %#v, %v", runs, err)
	}
	threadID := runs[0].ThreadID
	waitForTurnCompletedForThread(t, out, threadID)
	client.mu.Lock()
	requestMessages := append([]providers.ChatMessage(nil), client.requests[0].Messages...)
	client.mu.Unlock()
	if err := providers.ValidateToolCallHistory(requestMessages); err != nil {
		t.Fatalf("automation provider request message sequence = %#v: %v", requestMessages, err)
	}

	persisted, ok, err := session.Find(rt.SessionDir, threadID)
	if err != nil || !ok || persisted.Source != "automation" {
		t.Fatalf("persisted thread = %#v, %t, %v", persisted, ok, err)
	}
	records, err := session.LoadHistoryRecords(rt.SessionDir, threadID, true)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var foundPrompt bool
	for _, record := range records {
		if record.Role == "user" && strings.Contains(record.Content, task.Prompt) {
			foundPrompt = true
		}
		if strings.Contains(record.Content, "task_id: "+task.ID) {
			t.Fatalf("automation metadata was persisted as conversation history: %#v", record)
		}
	}
	if !foundPrompt {
		t.Fatalf("automation prompt missing from persisted history: %#v", records)
	}

	client.mu.Lock()
	requests := append([]providers.ChatRequest(nil), client.requests...)
	client.mu.Unlock()
	var foundHiddenContext bool
	for _, request := range requests {
		for _, message := range request.Messages {
			if message.Hidden && message.Role == "user" && strings.Contains(message.Content, "task_id: "+task.ID) {
				foundHiddenContext = true
			}
		}
	}
	if !foundHiddenContext {
		t.Fatalf("provider request missing hidden automation context: %#v", requests)
	}
	runs, err = rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].Status != automation.RunStatusCompleted || runs[0].TurnID == "" {
		t.Fatalf("completed runs = %#v, %v", runs, err)
	}
}

func TestAutomationHeartbeatQueuesTurnsOnExistingThread(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	secondRelease := make(chan struct{})
	var firstReleaseOnce sync.Once
	var secondReleaseOnce sync.Once
	client := &fakeClient{
		response: providersResponse("done"),
		onChat: func(call int, _ providers.ChatRequest) {
			switch call {
			case 1:
				close(firstStarted)
				<-firstRelease
			case 2:
				close(secondStarted)
				<-secondRelease
			}
		},
	}
	rt := newTestRuntime(t, client)
	rt.StateDir = filepath.Dir(rt.SessionDir)
	rt.AutomationManager = automation.NewManager(automation.Config{StateDir: rt.StateDir})
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(func() {
		firstReleaseOnce.Do(func() { close(firstRelease) })
		secondReleaseOnce.Do(func() { close(secondRelease) })
		srv.Close()
	})

	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	task := automation.Task{
		ID: "heartbeat-task", Prompt: "check cache", Mode: string(automation.ModeThreadHeartbeat),
		HeartbeatThreadID: threadID, Cron: "*/5 * * * *", Timezone: "UTC",
	}
	if err := rt.AutomationManager.Fire(context.Background(), task); err != nil {
		t.Fatalf("first Fire: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first heartbeat turn did not start")
	}
	if err := rt.AutomationManager.Fire(context.Background(), task); err != nil {
		t.Fatalf("second Fire: %v", err)
	}
	runs, err := rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs while busy = %#v, %v", runs, err)
	}
	var queued int
	for _, run := range runs {
		if run.ThreadID != threadID {
			t.Fatalf("heartbeat run used thread %q, want %q", run.ThreadID, threadID)
		}
		if run.Status == automation.RunStatusQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("queued run count = %d, runs = %#v", queued, runs)
	}

	firstReleaseOnce.Do(func() { close(firstRelease) })
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("queued heartbeat turn did not start")
	}
	runs, err = rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs after dequeue = %#v, %v", runs, err)
	}
	var completed, running int
	for _, run := range runs {
		switch run.Status {
		case automation.RunStatusCompleted:
			completed++
		case automation.RunStatusRunning:
			running++
			if run.TurnID == "" || run.QueueID != "" {
				t.Fatalf("running dequeued run = %#v", run)
			}
		}
	}
	if completed != 1 || running != 1 {
		t.Fatalf("dequeued run states = %#v", runs)
	}
	secondReleaseOnce.Do(func() { close(secondRelease) })
	waitForTurnCompletedCountForThread(t, out, threadID, 2)
	runs, err = rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 2 {
		t.Fatalf("completed heartbeat runs = %#v, %v", runs, err)
	}
	for _, run := range runs {
		if run.Status != automation.RunStatusCompleted || run.ThreadID != threadID || run.TurnID == "" {
			t.Fatalf("completed heartbeat run = %#v", run)
		}
	}
}

func TestAutomationManagerRecoversQueuedHeartbeatIntoPersistedThread(t *testing.T) {
	client := &fakeClient{response: providersResponse("recovered")}
	rt := newTestRuntime(t, client)
	rt.StateDir = filepath.Dir(rt.SessionDir)
	threadID := "recovered-heartbeat-thread"
	if _, err := session.CreateWithMetadata(rt.SessionDir, threadID, rt.RootDir); err != nil {
		t.Fatalf("CreateWithMetadata: %v", err)
	}
	task := automation.Task{
		ID: "recovered-heartbeat-task", Prompt: "resume the scheduled check",
		Mode: string(automation.ModeThreadHeartbeat), HeartbeatThreadID: threadID,
		Cron: "*/5 * * * *", Timezone: "UTC",
	}
	runStore := automation.NewRunStore(statepath.AutomationRunsPath(rt.StateDir))
	if err := runStore.Add(automation.Run{
		ID: "recovered-heartbeat-run", TaskID: task.ID, Task: task,
		Mode: task.Mode, Status: automation.RunStatusQueued,
	}); err != nil {
		t.Fatalf("add queued run: %v", err)
	}
	rt.AutomationManager = automation.NewManager(automation.Config{StateDir: rt.StateDir})
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(srv.Close)

	waitForTurnCompletedForThread(t, out, threadID)
	runs, err := rt.AutomationManager.ListRuns()
	if err != nil || len(runs) != 1 {
		t.Fatalf("recovered runs = %#v, %v", runs, err)
	}
	if run := runs[0]; run.Status != automation.RunStatusCompleted || run.ThreadID != threadID || run.TurnID == "" {
		t.Fatalf("recovered run = %#v", run)
	}
	records, err := session.LoadHistoryRecords(rt.SessionDir, threadID, true)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var foundPrompt bool
	for _, record := range records {
		if record.Role == "user" && strings.Contains(record.Content, task.Prompt) {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Fatalf("recovered prompt missing from history: %#v", records)
	}
}
