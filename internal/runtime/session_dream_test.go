package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
)

func TestSessionDreamScheduler_ShouldStartRespectsInterval(t *testing.T) {
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(workspaceState, func() string { return sessionArtifact }, 7)
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

func TestSessionDream_RunWritesProjectMemoryWithSessionMemoryOnlyTool(t *testing.T) {
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
	scheduler := newSessionDreamScheduler(workspaceState, func() string { return sessionArtifact }, 7)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		systemPrompt:       "You are wuu.",
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
	state, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if !state.LastRunAt.Equal(now) {
		t.Fatalf("dream state LastRunAt = %v, want %v", state.LastRunAt, now)
	}
	if len(client.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(client.requests))
	}
	toolNames := make(map[string]bool)
	for _, def := range client.requests[0].Tools {
		toolNames[def.Name] = true
	}
	if len(toolNames) != 1 || !toolNames["session_memory"] {
		t.Fatalf("dream tools = %+v, want only session_memory", toolNames)
	}
	last := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Nothing to dream") {
		t.Fatalf("missing dream prompt in request: %+v", last)
	}
}
