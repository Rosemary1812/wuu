package appserver

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/kanban"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestKanbanRunRequestPromptExecute(t *testing.T) {
	task := kanban.Task{
		ID: "kt-1", Title: "Build the landing page", Brief: "goal: ship it\ndone when: deployed",
		SourceThreadID: "cth-src",
	}
	prompt := kanbanRunRequestPrompt(task, kanban.Run{Kind: kanban.RunKindExecute}, []kanban.Artifact{
		{Path: "design/mock.png"},
	}, "/tmp/out/kt-1")
	for _, want := range []string{
		"# Task: Build the landing page",
		"goal: ship it",
		"- design/mock.png",
		"cth-src",
		"/tmp/out/kt-1",
		"final message must be a short summary",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "second pair of eyes") {
		t.Error("execute prompt must not contain review framing")
	}
}

func TestKanbanRunRequestPromptReview(t *testing.T) {
	task := kanban.Task{ID: "kt-1", Title: "Build the landing page", Brief: "done when: deployed"}
	prompt := kanbanRunRequestPrompt(task, kanban.Run{Kind: kanban.RunKindReview}, nil, "/tmp/out/kt-1")
	for _, want := range []string{
		"# Verification task: Build the landing page",
		"second pair of eyes",
		"No prior artifacts",
		"Do not",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("review prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "# Task:") {
		t.Error("review prompt must not use the execute header")
	}
}

func TestKanbanRunRequestPromptNoSourceThread(t *testing.T) {
	task := kanban.Task{ID: "kt-1", Title: "T"}
	prompt := kanbanRunRequestPrompt(task, kanban.Run{Kind: kanban.RunKindExecute}, nil, "/tmp/out")
	if strings.Contains(prompt, "crystallized from conversation") {
		t.Error("source-thread note must be omitted when there is no source thread")
	}
}

func TestKanbanTranscript(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "persona"},
		{Role: "user", Content: "  我想做个官网  "},
		{Role: "assistant", Content: "好，先确认风格"},
		{Role: "user", Content: ""},
	}
	got := kanbanTranscript(history)
	if strings.Contains(got, "persona") {
		t.Error("system messages must be dropped")
	}
	if !strings.Contains(got, "user: 我想做个官网") || !strings.Contains(got, "assistant: 好，先确认风格") {
		t.Fatalf("transcript = %q", got)
	}
}

func TestKanbanTranscriptTailKeptUnderCap(t *testing.T) {
	var history []providers.ChatMessage
	for i := 0; i < 60; i++ {
		history = append(history, providers.ChatMessage{Role: "user", Content: strings.Repeat("x", 2000)})
	}
	got := kanbanTranscript(history)
	if len(got) > 62000 {
		t.Fatalf("transcript exceeded cap: %d", len(got))
	}
	if got == "" {
		t.Fatal("transcript must keep the tail")
	}
}

func TestParseKanbanCrystallizedPlan(t *testing.T) {
	raw := "Here you go:\n{\"title\": \"官网\", \"brief\": \"goal\", \"subtasks\": [{\"title\": \"设计\", \"brief\": \"b\", \"suggested_target\": \"小克\"}]}\nDone."
	plan, err := parseKanbanCrystallizedPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Title != "官网" || len(plan.Subtasks) != 1 || plan.Subtasks[0].SuggestedTarget != "小克" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := parseKanbanCrystallizedPlan("no json here"); err == nil {
		t.Fatal("expected error for missing JSON")
	}
	if _, err := parseKanbanCrystallizedPlan("{\"brief\": \"no title\"}"); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestKanbanStandingInstructionsPrompt(t *testing.T) {
	got := kanbanStandingInstructionsPrompt("  回复要短  ", []string{"commit", "review-pr"})
	for _, want := range []string{"## Standing instructions", "回复要短", "commit, review-pr"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestKanbanAutoDispatchEligible(t *testing.T) {
	tasks := []kanban.Task{
		{ID: "kt-root", Status: kanban.TaskStatusReady},                                   // container: has children
		{ID: "kt-a", ParentID: "kt-root", Status: kanban.TaskStatusReady, SortIndex: 1},   // eligible
		{ID: "kt-b", ParentID: "kt-root", Status: kanban.TaskStatusRunning, SortIndex: 2}, // not ready
		{ID: "kt-draft", Status: kanban.TaskStatusDraft},                                  // not ready
		{ID: "kt-c", ParentID: "kt-draft", Status: kanban.TaskStatusReady},                // parent still draft
		{ID: "kt-solo", Status: kanban.TaskStatusReady, SortIndex: 3},                     // eligible root leaf
		{ID: "kt-orphan", ParentID: "kt-missing", Status: kanban.TaskStatusReady},         // parent unknown
	}
	eligible := kanbanAutoDispatchEligible(tasks)
	var ids []string
	for _, task := range eligible {
		ids = append(ids, task.ID)
	}
	want := []string{"kt-a", "kt-solo"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("eligible = %v, want %v", ids, want)
	}
}
