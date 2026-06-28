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
	if _, ok := props["timeout_ms"]; ok {
		t.Fatalf("helpme schema should not expose long synchronous timeout: %#v", def.InputSchema)
	}
	if _, ok := props["wait_ms"]; !ok {
		t.Fatalf("helpme schema should expose optional short wait_ms: %#v", def.InputSchema)
	}
	for _, want := range []string{"returns immediately", "await_agents", "inception"} {
		if !strings.Contains(def.Description, want) {
			t.Fatalf("helpme description missing %q:\n%s", want, def.Description)
		}
	}
	for _, unwanted := range []string{"waits for it to finish", "joint compact marker"} {
		if strings.Contains(def.Description, unwanted) {
			t.Fatalf("helpme description should not promise synchronous compact path %q:\n%s", unwanted, def.Description)
		}
	}
}

func TestDecodeHelpMeArgsAcceptsSingleStringLists(t *testing.T) {
	var args helpMeArgs
	if err := decodeArgs(`{
		"reason": "stuck",
		"failed_attempts": "CSS visibility changed but the rail still did not render",
		"constraints": "preserve existing sidebar behavior",
		"evidence": "screenshot shows the rail missing"
		}`, &args); err != nil {
		t.Fatalf("decode helpme args: %v", err)
	}
	if got := args.FailedAttempts; len(got) != 1 || got[0] != "CSS visibility changed but the rail still did not render" {
		t.Fatalf("failed_attempts = %#v", got)
	}
	if got := args.Constraints; len(got) != 1 || got[0] != "preserve existing sidebar behavior" {
		t.Fatalf("constraints = %#v", got)
	}
	if got := args.Evidence; len(got) != 1 || got[0] != "screenshot shows the rail missing" {
		t.Fatalf("evidence = %#v", got)
	}
	if args.WaitMS != 0 {
		t.Fatalf("wait_ms default = %d, want 0", args.WaitMS)
	}
}

func TestDecodeHelpMeArgsRejectsObjectListField(t *testing.T) {
	var args helpMeArgs
	err := decodeArgs(`{"failed_attempts":{"summary":"still wrong"}}`, &args)
	if err == nil {
		t.Fatal("expected invalid failed_attempts type")
	}
	if !strings.Contains(err.Error(), "failed_attempts must be a string or string array") {
		t.Fatalf("unexpected error: %v", err)
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
	for _, want := range []string{"HelpMe Handoff Brief", "Why this handoff is needed", "User goal", "Task to complete"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("HelpMe prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"wrong assumption", "polluted context", "do not inherit", "fresh general-purpose helper agent"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("HelpMe prompt should not expose internal recovery diagnosis %q:\n%s", unwanted, prompt)
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
	}, nil, true)
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
