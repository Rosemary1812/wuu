package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/skills"
)

func TestToolkit_LoadSkillRecordsResultAction(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	skillDir := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "demo.txt"), []byte("demo"), 0644); err != nil {
		t.Fatalf("write skill resource: %v", err)
	}
	kit.SetSkills([]skills.Skill{{
		Name:         "review",
		Description:  "Review code changes.",
		WhenToUse:    "Use for code review.",
		Content:      "Review ${ARGUMENTS}. Keep `!printf should-not-run` literal.",
		Source:       "project",
		Dir:          skillDir,
		AllowedTools: []string{"read_file"},
	}})

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_skill",
		Arguments: `{"name":"review","arguments":"the diff"}`,
	})
	if err != nil {
		t.Fatalf("load_skill: %v", err)
	}
	var parsed struct {
		Action   string `json:"action"`
		Title    string `json:"title"`
		Output   string `json:"output"`
		Metadata struct {
			Name string `json:"name"`
			Dir  string `json:"dir"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse load_skill response: %v", err)
	}
	if parsed.Action != "load_skill" || parsed.Title != "Loaded skill: review" || parsed.Metadata.Name != "review" || !strings.Contains(parsed.Output, "the diff") {
		t.Fatalf("unexpected load_skill response: %+v", parsed)
	}
	for _, want := range []string{
		`<skill_content name="review">`,
		"Base directory for this skill: file://",
		"Relative paths in this skill",
		"<skill_files>",
		filepath.Join(skillDir, "scripts", "demo.txt"),
		"`!printf should-not-run`",
	} {
		if !strings.Contains(parsed.Output, want) {
			t.Fatalf("load_skill output missing %q:\n%s", want, parsed.Output)
		}
	}
	if parsed.Metadata.Dir != skillDir {
		t.Fatalf("metadata dir = %q, want %q", parsed.Metadata.Dir, skillDir)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ResultAction != "load_skill" {
		t.Fatalf("load_skill telemetry missing result action: %+v", records)
	}
}
