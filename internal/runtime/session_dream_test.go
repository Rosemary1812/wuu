package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
)

func TestSessionDreamScheduler_ShouldStartRespectsInterval(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7)
	history := makeProfileMemoryReviewHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("missing dream state should start first dream")
	}
	scheduler.finish()

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatalf("SaveDreamState recent: %v", err)
	}
	if scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("recent dream state should not start")
	}

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatalf("SaveDreamState old: %v", err)
	}
	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("old dream state should start")
	}
	scheduler.finish()
}

func TestSessionDreamScheduler_ShouldStartBacksOffRecentFailure(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7)
	history := makeProfileMemoryReviewHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastStatus:     sessionmemory.DreamStatusFailed,
		LastFinishedAt: now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveDreamState recent failure: %v", err)
	}
	if scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("recent failed dream should back off")
	}

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastStatus:     sessionmemory.DreamStatusFailed,
		LastFinishedAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveDreamState old failure: %v", err)
	}
	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("old failed dream should be eligible again")
	}
	scheduler.finish()
}

func TestSessionDream_RunWritesProjectMemoryWithAlignedToolSet(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	client := &profileMemoryReviewFakeClient{
		responses: []providers.ChatResponse{
			{
				ToolCalls: []providers.ToolCall{{
					ID:   "call_1",
					Name: "session_memory",
					Arguments: `{
						"action":"replace",
						"target":"project_memory",
						"content":"# Project Memory\n\n## Discovered Durable Knowledge\n\n- The release workflow requires visual QA before tagging."
					}`,
				}},
			},
			{Content: "Saved."},
		},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		systemPrompt:       "You are wuu.",
		rootDir:            root,
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: sessionArtifact,
		now:                now,
		history: []providers.ChatMessage{
			{Role: "user", Content: "Remember release needs visual QA."},
			{Role: "assistant", Content: "Noted."},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	_, content, exists, err := sessionmemory.ReadTarget(workspaceState, sessionArtifact, sessionmemory.TargetProjectMemory)
	if err != nil {
		t.Fatalf("ReadTarget: %v", err)
	}
	if !exists || !strings.Contains(content, "visual QA before tagging") {
		t.Fatalf("project memory not written: exists=%t content=%q", exists, content)
	}
	eventPath := filepath.Join(workspaceState, "memory", "events.jsonl")
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("read background memory events: %v", err)
	}
	eventText := string(eventData)
	for _, want := range []string{
		`"source":"session_dream"`,
		`"tool":"session_memory"`,
		`"target":"project_memory"`,
		`"path":"` + filepath.Join(workspaceState, "memory", "MEMORY.md") + `"`,
		`"written":true`,
	} {
		if !strings.Contains(eventText, want) {
			t.Fatalf("background memory event missing %q:\n%s", want, eventText)
		}
	}
	state, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if !state.LastRunAt.Equal(now) {
		t.Fatalf("dream state LastRunAt = %v, want %v", state.LastRunAt, now)
	}
	if state.LastStatus != sessionmemory.DreamStatusCompleted || !state.LastFinishedAt.Equal(now) || state.LastError != "" {
		t.Fatalf("dream completion state = %+v", state)
	}
	if len(client.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(client.requests))
	}
	toolNames := make(map[string]bool)
	for _, def := range client.requests[0].Tools {
		toolNames[def.Name] = true
	}
	wantTools := []string{"read_file", "write_file", "list_files", "edit_file", "grep", "glob", "run_shell", "session_memory"}
	if len(toolNames) != len(wantTools) {
		t.Fatalf("dream tools = %+v, want %v", toolNames, wantTools)
	}
	for _, name := range wantTools {
		if !toolNames[name] {
			t.Fatalf("dream tools = %+v, missing %s", toolNames, name)
		}
	}
	last := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "read_file") || !strings.Contains(last.Content, "Nothing to dream") {
		t.Fatalf("missing dream prompt in request: %+v", last)
	}
}

func TestSessionDream_RunRecordsFailureState(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	client := &profileMemoryReviewFakeClient{
		errors: []error{errors.New("provider unavailable")},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7)
	started := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		rootDir:            root,
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: sessionArtifact,
		now:                started,
		history: []providers.ChatMessage{
			{Role: "user", Content: "Remember release needs visual QA."},
			{Role: "assistant", Content: "Noted."},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("run error = %v, want provider unavailable", err)
	}

	state, loadErr := sessionmemory.LoadDreamState(workspaceState)
	if loadErr != nil {
		t.Fatalf("LoadDreamState: %v", loadErr)
	}
	if state.LastStatus != sessionmemory.DreamStatusFailed || !strings.Contains(state.LastError, "provider unavailable") {
		t.Fatalf("dream failure state = %+v", state)
	}
	if !state.LastRunAt.IsZero() {
		t.Fatalf("failed dream should not update LastRunAt: %+v", state)
	}
	if !state.LastStartedAt.Equal(started) {
		t.Fatalf("LastStartedAt = %v, want %v", state.LastStartedAt, started)
	}
}
