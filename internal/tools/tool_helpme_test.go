package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestHelpMeDefinitionDoesNotExposeMode(t *testing.T) {
	def := NewHelpMeTool(&Env{}).Definition()
	props, _ := def.InputSchema["properties"].(map[string]any)
	if _, ok := props["mode"]; ok {
		t.Fatalf("helpme schema should not expose mode: %#v", def.InputSchema)
	}
}

func TestBuildHelpMePromptDoesNotEmitModeSection(t *testing.T) {
	prompt := buildHelpMePrompt(helpMePromptInput{
		Reason:       "parent may be stuck",
		OriginalGoal: "finish the design",
		Ask:          "re-evaluate the handoff",
	})
	if strings.Contains(prompt, "## Mode") {
		t.Fatalf("HelpMe prompt should not emit a Mode section:\n%s", prompt)
	}
	for _, want := range []string{"HelpMe Recovery Brief", "Why recovery was triggered", "Original user goal", "Your task"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("HelpMe prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestWriteHelpMeMainTraceArchivesParentHistory(t *testing.T) {
	sessionDir := t.TempDir()
	ref, err := writeHelpMeMainTrace(&Env{
		SessionDir: sessionDir,
		AgentID:    "root-agent",
		AgentPath:  "/root",
	}, []providers.ChatMessage{
		{Role: "user", Content: "original task"},
		{Role: "assistant", Content: "wrong direction"},
	}, helpMeArgs{
		Reason:       "stuck",
		OriginalGoal: "original task",
	}, &agentcontrol.SpawnResult{
		AgentID:   "helper-1",
		AgentPath: "/root/helpme_recovery",
		Status:    "completed",
	}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "$SESSION_DIR/helpme/") {
		t.Fatalf("expected session ref, got %q", ref)
	}
	rel := strings.TrimPrefix(ref, "$SESSION_DIR/")
	data, err := os.ReadFile(filepath.Join(sessionDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var payload struct {
		SchemaVersion string                  `json:"schema_version"`
		MainHistory   []providers.ChatMessage `json:"main_history"`
		ReportMissing bool                    `json:"report_missing"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if payload.SchemaVersion != "wuu/helpme-main-trace/v0.1" {
		t.Fatalf("schema = %q", payload.SchemaVersion)
	}
	if len(payload.MainHistory) != 2 || payload.MainHistory[0].Content != "original task" {
		t.Fatalf("main history not archived: %+v", payload.MainHistory)
	}
	if !payload.ReportMissing {
		t.Fatal("expected missing report to be recorded")
	}
}
