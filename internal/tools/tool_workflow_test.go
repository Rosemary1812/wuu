package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestToolkitWorkflowToolsCreateAndInspectRun(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)
	kit.SetSessionID("thread-workflow")
	kit.SetWorkflows([]workflow.Definition{{
		Name:        "feature-delivery",
		Description: "Deliver a feature with planning and QA.",
		WhenToUse:   "Use for feature work.",
		Source:      "project",
		Path:        "/repo/.claude/workflows/feature-delivery/WORKFLOW.md",
		Dir:         "/repo/.claude/workflows/feature-delivery",
		Content: "## Intent\n\nBuild ${ARGUMENTS}.\n\n## Phases\n\n" +
			"1. Clarify product intent\n" +
			"2. Implement scoped change\n" +
			"3. Review and test\n",
		Profiles: []workflow.ProfileRef{{Name: "frontend_owner"}, {Name: "qa_reviewer"}},
	}})

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_workflows", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_workflows: %v", err)
	}
	var listed struct {
		Count     int `json:"count"`
		Workflows []struct {
			Name string `json:"name"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list response: %v", err)
	}
	if listed.Count != 1 || listed.Workflows[0].Name != "feature-delivery" {
		t.Fatalf("unexpected workflow list: %+v", listed)
	}

	loadResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_workflow",
		Arguments: `{"name":"feature-delivery","arguments":"settings search"}`,
	})
	if err != nil {
		t.Fatalf("load_workflow: %v", err)
	}
	var loaded struct {
		Content             string   `json:"content"`
		SuggestedPhaseNames []string `json:"suggested_phase_names"`
	}
	if err := json.Unmarshal([]byte(loadResp), &loaded); err != nil {
		t.Fatalf("parse load response: %v", err)
	}
	if !strings.Contains(loaded.Content, "Build settings search.") {
		t.Fatalf("workflow arguments were not substituted: %s", loaded.Content)
	}
	if len(loaded.SuggestedPhaseNames) != 3 || loaded.SuggestedPhaseNames[0] != "Clarify product intent" {
		t.Fatalf("unexpected suggested phases: %+v", loaded.SuggestedPhaseNames)
	}

	createResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_workflow",
		Arguments: `{
			"definition_name":"feature-delivery",
			"arguments":"settings search",
			"run_id":"workflow-test-run"
		}`,
	})
	if err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	var created struct {
		RunID    string            `json:"run_id"`
		Status   workflow.RunState `json:"status"`
		PlanPath string            `json:"plan_path"`
		Phases   []workflow.Phase  `json:"phases"`
	}
	if err := json.Unmarshal([]byte(createResp), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	if created.RunID != "workflow-test-run" || created.Status != workflow.RunStateRunning {
		t.Fatalf("unexpected create response: %+v", created)
	}
	if len(created.Phases) != 3 || created.Phases[0].Status != workflow.PhaseStateRunnable {
		t.Fatalf("unexpected phases: %+v", created.Phases)
	}
	if created.PlanPath == "" {
		t.Fatal("expected plan path")
	}
	if _, err := os.Stat(created.PlanPath); err != nil {
		t.Fatalf("expected plan file: %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"set_phase_status","run_id":"workflow-test-run","phase_id":"clarify_product_intent","status":"running"}`,
	}); err != nil {
		t.Fatalf("workflow_control set phase running: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-test-run",
			"phase_id":"clarify_product_intent",
			"agent_id":"agent-1",
			"task_name":"clarify_intent",
			"agent_profile":"product_planner",
			"status":"running",
			"prompt":"Clarify product intent"
		}`,
	}); err != nil {
		t.Fatalf("workflow_control record agent running: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-test-run",
			"agent_id":"agent-1",
			"status":"awaiting_report",
			"report_path":"reports/agent-1.md",
			"changed_files":["docs/plan.md"],
			"artifacts":["reports/agent-1.md"]
		}`,
	}); err != nil {
		t.Fatalf("workflow_control record agent awaiting_report: %v", err)
	}
	memoryResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_memory_candidate",
			"run_id":"workflow-test-run",
			"candidate_id":"candidate-qa",
			"agent_run_id":"agent-1",
			"agent_profile":"product_planner",
			"content":"Settings search requires QA before release.",
			"target":"memory",
			"tags":[" feature ","qa"],
			"source":"agent_report"
		}`,
	})
	if err != nil {
		t.Fatalf("workflow_control record memory candidate: %v", err)
	}
	var memoryResult struct {
		MemoryCandidate workflow.MemoryCandidate `json:"memory_candidate"`
	}
	if err := json.Unmarshal([]byte(memoryResp), &memoryResult); err != nil {
		t.Fatalf("parse memory candidate response: %v", err)
	}
	if memoryResult.MemoryCandidate.Status != workflow.MemoryCandidatePending {
		t.Fatalf("memory candidate should start pending: %+v", memoryResult.MemoryCandidate)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"review_memory_candidate","run_id":"workflow-test-run","candidate_id":"candidate-qa","status":"rejected","message":"temporary task fact"}`,
	}); err != nil {
		t.Fatalf("workflow_control review memory candidate: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"set_phase_status","run_id":"workflow-test-run","phase_id":"clarify_product_intent","status":"completed"}`,
	}); err != nil {
		t.Fatalf("workflow_control set phase completed: %v", err)
	}
	finalResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"write_final_report","run_id":"workflow-test-run","content":"# Final\n\nDone.","complete_run":true}`,
	})
	if err != nil {
		t.Fatalf("workflow_control final report: %v", err)
	}
	var final struct {
		Run             workflow.Run `json:"run"`
		FinalReportPath string       `json:"final_report_path"`
	}
	if err := json.Unmarshal([]byte(finalResp), &final); err != nil {
		t.Fatalf("parse final response: %v", err)
	}
	if final.Run.Status != workflow.RunStateCompleted || final.FinalReportPath == "" {
		t.Fatalf("unexpected final response: %+v", final)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-test-run","include_events":true}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		Run              workflow.Run               `json:"run"`
		AgentRuns        []workflow.AgentRun        `json:"agent_runs"`
		MemoryCandidates []workflow.MemoryCandidate `json:"memory_candidates"`
		Events           []workflow.Event           `json:"events"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if status.Run.ID != "workflow-test-run" || status.Run.PlanPath == "" {
		t.Fatalf("unexpected status: %+v", status.Run)
	}
	if status.Run.Status != workflow.RunStateCompleted || status.Run.FinalReportPath == "" {
		t.Fatalf("workflow should be completed with final report: %+v", status.Run)
	}
	if len(status.Run.Phases[0].AgentRunIDs) != 1 || status.Run.Phases[0].AgentRunIDs[0] != "agent-1" {
		t.Fatalf("agent run should be attached to phase: %+v", status.Run.Phases[0])
	}
	if len(status.AgentRuns) != 1 || status.AgentRuns[0].Status != workflow.AgentRunStateAwaitingReport || len(status.AgentRuns[0].ChangedFiles) != 1 {
		t.Fatalf("unexpected agent runs: %+v", status.AgentRuns)
	}
	if len(status.MemoryCandidates) != 1 || status.MemoryCandidates[0].Status != workflow.MemoryCandidateRejected {
		t.Fatalf("unexpected memory candidates: %+v", status.MemoryCandidates)
	}
	if len(status.Events) < 2 {
		t.Fatalf("expected run and plan events, got %+v", status.Events)
	}
}
