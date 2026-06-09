package tools

import (
	"context"
	"encoding/json"
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
	kit.SetSkills([]skills.Skill{{
		Name:         "review",
		Description:  "Review code changes.",
		WhenToUse:    "Use for code review.",
		Content:      "Review ${ARGUMENTS}.",
		Source:       "project",
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
		Action       string   `json:"action"`
		Name         string   `json:"name"`
		Content      string   `json:"content"`
		AllowedTools []string `json:"allowed_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse load_skill response: %v", err)
	}
	if parsed.Action != "load_skill" || parsed.Name != "review" || !strings.Contains(parsed.Content, "the diff") {
		t.Fatalf("unexpected load_skill response: %+v", parsed)
	}
	if len(parsed.AllowedTools) != 1 || parsed.AllowedTools[0] != "read_file" {
		t.Fatalf("allowed_tools missing: %+v", parsed.AllowedTools)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ResultAction != "load_skill" {
		t.Fatalf("load_skill telemetry missing result action: %+v", records)
	}
}
