package appserver

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/kanban"
)

func TestGroupKanbanSpawnCandidates(t *testing.T) {
	mkTask := func(id, parent, title, status string) kanban.Task {
		return kanban.Task{ID: id, ParentID: parent, Title: title, Status: status}
	}
	mkRuns := func(target string) []kanban.Run {
		return []kanban.Run{{TargetID: target}}
	}
	tasks := []kanban.Task{
		mkTask("kt-1", "", "发布官网", kanban.TaskStatusDone),
		mkTask("kt-2", "", "发布 官网!", kanban.TaskStatusReview),
		mkTask("kt-3", "", "发布官网", kanban.TaskStatusDone),
		mkTask("kt-4", "", "写周报", kanban.TaskStatusDone),
		mkTask("kt-5", "", "写周报", kanban.TaskStatusDone),
		mkTask("kt-6", "", "翻译文档", kanban.TaskStatusReady),
		mkTask("kt-7", "", "翻译文档", kanban.TaskStatusDone),
		mkTask("kt-8", "", "翻译文档", kanban.TaskStatusDone),
		mkTask("kt-9", "", "翻译文档", kanban.TaskStatusDone),
		mkTask("kt-parent", "", "容器", kanban.TaskStatusDone),
		mkTask("kt-child", "kt-parent", "容器", kanban.TaskStatusDone),
	}
	runsByTask := map[string][]kanban.Run{
		"kt-1": mkRuns("prt-andy"), "kt-2": mkRuns("prt-andy"), "kt-3": mkRuns("prt-andy"),
		"kt-4": mkRuns("prt-andy"), "kt-5": mkRuns("prt-other"),
		"kt-7": mkRuns("prt-andy"), "kt-8": mkRuns("prt-andy"), "kt-9": mkRuns("prt-andy"),
		"kt-parent": mkRuns("prt-andy"), "kt-child": mkRuns("prt-andy"),
	}
	got := groupKanbanSpawnCandidates(tasks, runsByTask, "prt-andy", 3)
	if len(got) != 2 {
		t.Fatalf("suggestions = %+v", got)
	}
	topics := map[string]kanbanSpawnSuggestion{}
	for _, s := range got {
		topics[s.Topic] = s
	}
	first, ok := topics["发布官网"]
	if !ok || first.TaskCount != 3 {
		t.Fatalf("missing 发布官网 suggestion: %+v", got)
	}
	second, ok := topics["翻译文档"]
	if !ok || second.TaskCount != 3 {
		t.Fatalf("missing 翻译文档 suggestion: %+v", got)
	}
	// "写周报" mixes targets; "容器" groups at 1 (parent excluded as container).
	// Suggestions are sorted by task count desc then topic for a stable board.
	for i := 1; i < len(got); i++ {
		if got[i-1].TaskCount < got[i].TaskCount {
			t.Fatalf("suggestions not sorted by count: %+v", got)
		}
	}
}

func TestNormalizeKanbanTopic(t *testing.T) {
	if got := normalizeKanbanTopic("  发布  官网! "); got != "发布 官网" {
		t.Fatalf("normalize = %q", got)
	}
	if got := normalizeKanbanTopic("Fix: Auth-Bug (v2)"); got != "fix auth bug v2" {
		t.Fatalf("normalize = %q", got)
	}
	if kanbanTopicKey("发布 官网!") != kanbanTopicKey("发布官网") {
		t.Fatal("space/punctuation variants must share a topic key")
	}
}

func TestSlugifyKanbanTopic(t *testing.T) {
	if got := slugifyKanbanTopic("发布 官网!"); got != "发布-官网" {
		t.Fatalf("slug = %q", got)
	}
	if got := slugifyKanbanTopic("  !!!  "); got != "handoff" {
		t.Fatalf("slug = %q", got)
	}
	long := strings.Repeat("a", 100)
	if got := slugifyKanbanTopic(long); len(got) > 48 {
		t.Fatalf("slug not capped: %d", len(got))
	}
}
