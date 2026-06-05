package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestBuilder_StaticBeforeDynamic(t *testing.T) {
	var b Builder
	b.AddSection("dynamic1", "DYNAMIC_ONE", false)
	b.AddSection("static1", "STATIC_ONE", true)
	b.AddSection("dynamic2", "DYNAMIC_TWO", false)
	b.AddSection("static2", "STATIC_TWO", true)

	result := b.Build()
	s1 := strings.Index(result, "STATIC_ONE")
	s2 := strings.Index(result, "STATIC_TWO")
	d1 := strings.Index(result, "DYNAMIC_ONE")
	d2 := strings.Index(result, "DYNAMIC_TWO")

	if s1 == -1 || s2 == -1 || d1 == -1 || d2 == -1 {
		t.Fatalf("missing sections in output:\n%s", result)
	}
	if s1 > d1 || s2 > d1 {
		t.Error("static sections should appear before dynamic sections")
	}
	if s1 > s2 || d1 > d2 {
		t.Error("sections within same category should preserve insertion order")
	}
}

func TestBuilder_DeduplicateByKey(t *testing.T) {
	var b Builder
	b.AddSection("key", "first", true)
	b.AddSection("key", "second", true)

	result := b.Build()
	if strings.Contains(result, "first") {
		t.Error("duplicate key should overwrite, not append")
	}
	if !strings.Contains(result, "second") {
		t.Error("latest value should win")
	}
}

func TestBuilder_EmptyContentSkipped(t *testing.T) {
	var b Builder
	b.AddSection("empty", "", true)
	b.AddSection("spaces", "   ", true)
	b.AddSection("real", "content", true)

	result := b.Build()
	if result != "content" {
		t.Errorf("expected only 'content', got %q", result)
	}
}

func TestBuilder_AddMemory_Truncation(t *testing.T) {
	// Create a memory file with 300 lines.
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "line content"
	}
	content := strings.Join(lines, "\n")

	files := []memory.File{
		{Name: "AGENTS.md", Source: "project", Path: "/workspace/AGENTS.md", Content: content},
	}

	var b Builder
	b.AddMemory(files)
	result := b.Build()

	if !strings.Contains(result, "[truncated") {
		t.Error("expected truncation marker for 300-line file")
	}
	if !strings.Contains(result, "AGENTS.md") {
		t.Error("expected file name in output")
	}
}

func TestBuilder_AddMemory_SmallFile(t *testing.T) {
	files := []memory.File{
		{Name: "CLAUDE.md", Source: "user", Path: "~/.claude/CLAUDE.md", Content: "some rules"},
	}

	var b Builder
	b.AddMemory(files)
	result := b.Build()

	if strings.Contains(result, "[truncated") {
		t.Error("small file should not be truncated")
	}
	if !strings.Contains(result, "some rules") {
		t.Error("expected full content in output")
	}
}

func TestBuilder_AddSkills(t *testing.T) {
	sks := []skills.Skill{
		{Name: "commit", Description: "Create a commit", WhenToUse: "When user asks to commit"},
		{Name: "hidden", Description: "Hidden skill", DisableModelInvoke: true},
	}

	var b Builder
	b.AddSkills(sks)
	result := b.Build()

	if !strings.Contains(result, "commit") {
		t.Error("expected visible skill in output")
	}
	if strings.Contains(result, "hidden") {
		t.Error("DisableModelInvoke skills should be excluded")
	}
}

func TestBuilder_AddWorkflows(t *testing.T) {
	workflows := []workflow.Definition{
		{
			Name:         "feature-delivery",
			Description:  "Deliver a feature",
			WhenToUse:    "When feature work spans implementation and QA",
			ArgumentHint: "<feature request>",
			Profiles:     []workflow.ProfileRef{{Name: "frontend_owner"}, {Name: "qa_reviewer", Required: true}},
		},
		{Name: "hidden", Description: "Hidden workflow", DisableModelInvoke: true},
	}

	var b Builder
	b.AddWorkflows(workflows)
	result := b.Build()

	for _, want := range []string{
		"feature-delivery",
		"`create_workflow`",
		"`workflow_control`",
		"frontend_owner",
		"qa_reviewer (required)",
		"memory candidates",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("workflow prompt missing %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "hidden") {
		t.Fatal("DisableModelInvoke workflows should be excluded")
	}
}

func TestBuilder_AddProfileMemoryGuidanceAndSnapshot(t *testing.T) {
	entries := []store.Entry{
		{
			Content:   "User prefers concise Chinese replies",
			Tags:      []string{"target:user", "tone"},
			Source:    store.SourceUser,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Content:   "Project uses make install for local CLI refresh",
			Tags:      []string{"target:memory", "wuu"},
			Source:    store.SourceAssistant,
			CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}

	var b Builder
	b.AddProfileMemory(entries)
	result := b.Build()

	for _, want := range []string{
		"# Persistent Memory",
		"`write_memory`",
		"target=\"user\"",
		"User prefers concise Chinese replies",
		"Project uses make install",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("profile memory prompt missing %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "target:user") || strings.Contains(result, "target:memory") {
		t.Fatalf("internal target tags should not be rendered:\n%s", result)
	}
}

func TestBuilder_AddProfileMemoryGuidanceWithoutEntries(t *testing.T) {
	var b Builder
	b.AddProfileMemory(nil)
	result := b.Build()

	if !strings.Contains(result, "# Persistent Memory") {
		t.Fatalf("expected durable memory guidance, got:\n%s", result)
	}
	if strings.Contains(result, "Current Profile Memory Snapshot") {
		t.Fatalf("empty profile memory should not render snapshot:\n%s", result)
	}
}

func TestBuilder_AddGitContext(t *testing.T) {
	var b Builder
	b.AddGitContext("Branch: main\n\nStatus: clean")
	result := b.Build()

	if !strings.Contains(result, "Branch: main") {
		t.Error("expected git context in output")
	}
	if !strings.Contains(result, "# Git Context") {
		t.Error("expected git context header")
	}
}

func TestTruncateMemory_Lines(t *testing.T) {
	lines := make([]string, 250)
	for i := range lines {
		lines[i] = "x"
	}
	content := strings.Join(lines, "\n")

	result := TruncateMemory(content, 200, 1<<20)
	if !strings.Contains(result, "[truncated") {
		t.Error("expected truncation marker")
	}
	// Should have at most 200 content lines.
	resultLines := strings.Count(result, "\n")
	if resultLines > 205 { // some slack for the marker
		t.Errorf("expected ~200 lines, got %d", resultLines)
	}
}

func TestTruncateMemory_Bytes(t *testing.T) {
	content := strings.Repeat("x", 30*1024) // 30KB
	result := TruncateMemory(content, 1<<20, 25*1024)
	if !strings.Contains(result, "[truncated") {
		t.Error("expected truncation marker for oversized content")
	}
}

func TestTruncateMemory_NoTruncation(t *testing.T) {
	content := "short content\nline two"
	result := TruncateMemory(content, 200, 25*1024)
	if result != content {
		t.Errorf("expected passthrough, got %q", result)
	}
}

func TestBuilder_FullAssembly(t *testing.T) {
	var b Builder
	b.AddSection("base", "You are a coding agent.", true)
	b.AddSection("preamble", "Coordinator preamble.", true)
	b.AddMemory([]memory.File{
		{Name: "AGENTS.md", Source: "project", Path: "/p/AGENTS.md", Content: "project rules"},
	})
	b.AddSkills([]skills.Skill{
		{Name: "test", Description: "Run tests"},
	})
	b.AddWorkflows([]workflow.Definition{
		{Name: "feature", Description: "Feature workflow"},
	})
	b.AddGitContext("Branch: main")

	result := b.Build()

	// Static before dynamic.
	baseIdx := strings.Index(result, "You are a coding agent.")
	memIdx := strings.Index(result, "project rules")
	if baseIdx > memIdx {
		t.Error("static base should come before dynamic memory")
	}

	// All sections present.
	for _, want := range []string{"coding agent", "Coordinator", "project rules", "test", "feature", "Branch: main"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}
