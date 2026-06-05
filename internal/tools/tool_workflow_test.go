package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
			"status":"completed",
			"report_path":"reports/agent-1.md",
			"changed_files":["docs/plan.md"],
			"artifacts":["reports/agent-1.md"]
		}`,
	}); err != nil {
		t.Fatalf("workflow_control record agent completed: %v", err)
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
	for _, phaseID := range []string{"implement_scoped_change", "review_and_test"} {
		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "workflow_control",
			Arguments: `{"action":"set_phase_status","run_id":"workflow-test-run","phase_id":"` + phaseID + `","status":"skipped"}`,
		}); err != nil {
			t.Fatalf("workflow_control skip phase %s: %v", phaseID, err)
		}
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
	if len(status.AgentRuns) != 1 || status.AgentRuns[0].Status != workflow.AgentRunStateCompleted || len(status.AgentRuns[0].ChangedFiles) != 1 {
		t.Fatalf("unexpected agent runs: %+v", status.AgentRuns)
	}
	if len(status.MemoryCandidates) != 1 || status.MemoryCandidates[0].Status != workflow.MemoryCandidateRejected {
		t.Fatalf("unexpected memory candidates: %+v", status.MemoryCandidates)
	}
	if len(status.Events) < 2 {
		t.Fatalf("expected run and plan events, got %+v", status.Events)
	}
}

func TestSaveWorkflowWritesProjectDefinitionAndRegistersIt(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)

	saveResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "save_workflow",
		Arguments: `{
			"name":"feature-delivery",
			"description":"Deliver a feature through QA.",
			"when_to_use":"Use for feature work.",
			"argument_hint":"<feature request>",
			"content":"## Intent\n\nShip a feature.\n\n## Phases\n\n1. Plan\n2. Implement\n3. QA\n",
			"profiles":[{"name":"frontend_owner","required":true},{"name":"qa_reviewer"}],
			"allow_profile_creation":"ask",
			"memory_policy":"report-candidates-only"
		}`,
	})
	if err != nil {
		t.Fatalf("save_workflow: %v", err)
	}
	var saved struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(saveResp), &saved); err != nil {
		t.Fatalf("parse save response: %v", err)
	}
	if saved.Name != "feature-delivery" || saved.Path == "" {
		t.Fatalf("unexpected save response: %+v", saved)
	}
	if _, err := os.Stat(saved.Path); err != nil {
		t.Fatalf("expected workflow file: %v", err)
	}

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_workflows", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_workflows: %v", err)
	}
	if !strings.Contains(listResp, "feature-delivery") {
		t.Fatalf("saved workflow should be registered: %s", listResp)
	}
	loadResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_workflow",
		Arguments: `{"name":"feature-delivery","arguments":"settings search"}`,
	})
	if err != nil {
		t.Fatalf("load_workflow: %v", err)
	}
	for _, want := range []string{"Ship a feature", "frontend_owner", "report-candidates-only"} {
		if !strings.Contains(loadResp, want) {
			t.Fatalf("loaded workflow missing %q:\n%s", want, loadResp)
		}
	}
}

func TestSaveWorkflowCanUseRunPlan(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_workflow",
		Arguments: `{
			"run_id":"workflow-template-source",
			"arguments":"settings search",
			"phases":[{"id":"plan","name":"Plan"},{"id":"qa","name":"QA"}],
			"plan":"## Intent\n\nBuild settings search.\n\n## Phases\n\n1. Plan\n2. QA\n"
		}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	saveResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "save_workflow",
		Arguments: `{"name":"settings-qa","run_id":"workflow-template-source"}`,
	})
	if err != nil {
		t.Fatalf("save_workflow from run: %v", err)
	}
	var saved struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(saveResp), &saved); err != nil {
		t.Fatalf("parse save response: %v", err)
	}
	data, err := os.ReadFile(saved.Path)
	if err != nil {
		t.Fatalf("read saved workflow: %v", err)
	}
	content := string(data)
	for _, want := range []string{"name: settings-qa", "## Phases", "Plan", "QA"} {
		if !strings.Contains(content, want) {
			t.Fatalf("saved run workflow missing %q:\n%s", want, content)
		}
	}
}

func TestWorkflowControlRecordsAwaitResultsAndGeneratesReport(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_workflow",
		Arguments: `{
			"run_id":"workflow-await-run",
			"plan":"## Phases\n\n1. QA",
			"phases":[{"id":"qa","name":"QA"}]
		}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"set_phase_status","run_id":"workflow-await-run","phase_id":"qa","status":"running"}`,
	}); err != nil {
		t.Fatalf("set phase running: %v", err)
	}
	awaitingResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_await_results",
			"run_id":"workflow-await-run",
			"phase_id":"qa",
			"await_results":[{
				"agent_id":"agent-qa",
				"task_name":"qa_check",
				"agent_profile":"qa_reviewer",
				"agent_path":"/qa_check",
				"status":"completed",
				"result":"QA finished but skipped the structured handoff.",
				"report_missing":true,
				"changed_files":["desktop/src/App.tsx"],
				"input_tokens":12,
				"output_tokens":34,
				"duration_ms":56
			}]
		}`,
	})
	if err != nil {
		t.Fatalf("record_await_results awaiting: %v", err)
	}
	var awaiting struct {
		AgentRuns []workflow.AgentRun `json:"agent_runs"`
	}
	if err := json.Unmarshal([]byte(awaitingResp), &awaiting); err != nil {
		t.Fatalf("parse awaiting response: %v", err)
	}
	if len(awaiting.AgentRuns) != 1 || awaiting.AgentRuns[0].Status != workflow.AgentRunStateAwaitingReport || !awaiting.AgentRuns[0].ReportMissing {
		t.Fatalf("await result should be awaiting_report: %+v", awaiting.AgentRuns)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"generate_final_report","run_id":"workflow-await-run","complete_run":true}`,
	}); err == nil {
		t.Fatal("expected completion to reject awaiting_report agent")
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_await_results",
			"run_id":"workflow-await-run",
			"phase_id":"qa",
			"await_results":[{
				"agent_id":"agent-qa",
				"task_name":"qa_check",
				"agent_profile":"qa_reviewer",
				"agent_path":"/qa_check",
				"status":"completed",
				"result":"QA passed after adding the structured handoff.",
				"report_path":"reports/agent-qa.md",
				"changed_files":["desktop/src/App.tsx"],
				"artifacts":["reports/agent-qa.md"],
				"input_tokens":12,
				"output_tokens":34,
				"duration_ms":56
			}]
		}`,
	}); err != nil {
		t.Fatalf("record_await_results completed: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"set_phase_status","run_id":"workflow-await-run","phase_id":"qa","status":"completed"}`,
	}); err != nil {
		t.Fatalf("set phase completed: %v", err)
	}
	finalResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"generate_final_report","run_id":"workflow-await-run","complete_run":true}`,
	})
	if err != nil {
		t.Fatalf("generate_final_report: %v", err)
	}
	var final struct {
		Run             workflow.Run `json:"run"`
		FinalReportPath string       `json:"final_report_path"`
		Content         string       `json:"content"`
	}
	if err := json.Unmarshal([]byte(finalResp), &final); err != nil {
		t.Fatalf("parse final report response: %v", err)
	}
	if final.Run.Status != workflow.RunStateCompleted || final.FinalReportPath == "" {
		t.Fatalf("workflow should complete: %+v", final)
	}
	for _, want := range []string{"reports/agent-qa.md", "desktop/src/App.tsx", "QA passed after adding"} {
		if !strings.Contains(final.Content, want) {
			t.Fatalf("generated report missing %q:\n%s", want, final.Content)
		}
	}
	data, err := os.ReadFile(final.FinalReportPath)
	if err != nil {
		t.Fatalf("read final report: %v", err)
	}
	if !strings.Contains(string(data), "Workflow Final Report") {
		t.Fatalf("final report file not written correctly:\n%s", string(data))
	}
}

func TestCreateWorkflowPausesForMissingRequiredProfiles(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("WUU_HOME", t.TempDir())
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)
	kit.SetWorkflows([]workflow.Definition{{
		Name:                 "feature-delivery",
		Description:          "Deliver a feature.",
		Content:              "## Phases\n\n1. Plan\n",
		Profiles:             []workflow.ProfileRef{{Name: "frontend_owner", Required: true}},
		AllowProfileCreation: "ask",
	}})

	createResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"definition_name":"feature-delivery","run_id":"workflow-missing-profile"}`,
	})
	if err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	var created struct {
		Status            workflow.RunState            `json:"status"`
		Phases            []workflow.Phase             `json:"phases"`
		ProfileResolution []workflow.ProfileResolution `json:"profile_resolution"`
	}
	if err := json.Unmarshal([]byte(createResp), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	if created.Status != workflow.RunStatePaused {
		t.Fatalf("workflow should pause for missing required profile: %+v", created)
	}
	if len(created.Phases) != 1 || created.Phases[0].Status != workflow.PhaseStatePending {
		t.Fatalf("paused workflow should not mark first phase runnable: %+v", created.Phases)
	}
	if len(created.ProfileResolution) != 1 || created.ProfileResolution[0].Action != "pause_missing_required" {
		t.Fatalf("missing required profile resolution not returned: %+v", created.ProfileResolution)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-missing-profile"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		Run               workflow.Run                 `json:"run"`
		ProfileResolution []workflow.ProfileResolution `json:"profile_resolution"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if status.Run.Status != workflow.RunStatePaused || status.Run.PauseReason == "" || status.Run.ResumeHint == "" {
		t.Fatalf("paused run metadata missing: %+v", status.Run)
	}
	if len(status.ProfileResolution) != 1 || status.ProfileResolution[0].Name != "frontend_owner" {
		t.Fatalf("status should include profile resolution: %+v", status.ProfileResolution)
	}
}

func TestWorkflowControlEnforcesMaxAgents(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)
	kit.SetWorkflows([]workflow.Definition{{
		Name:      "tiny-team",
		Content:   "## Phases\n\n1. Work\n",
		MaxAgents: 1,
	}})

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"definition_name":"tiny-team","run_id":"workflow-agent-cap"}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-agent-cap",
			"agent_id":"agent-1",
			"task_name":"first"
		}`,
	}); err != nil {
		t.Fatalf("record first agent: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-agent-cap",
			"agent_id":"agent-2",
			"task_name":"second"
		}`,
	}); err == nil {
		t.Fatal("expected max agent cap to reject second agent")
	}
}

func TestWorkflowControlPauseResumeAndRetryAgentRun(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_workflow",
		Arguments: `{
			"run_id":"workflow-recovery-run",
			"plan":"## Phases\n\n1. Implement",
			"phases":[{"id":"implement","name":"Implement"}]
		}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	pauseResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"pause_run",
			"run_id":"workflow-recovery-run",
			"pause_reason":"file conflict",
			"resume_hint":"resolve conflict before retrying",
			"rollback_hint":"use the agent worktree checkpoint"
		}`,
	})
	if err != nil {
		t.Fatalf("pause_run: %v", err)
	}
	var paused struct {
		Run workflow.Run `json:"run"`
	}
	if err := json.Unmarshal([]byte(pauseResp), &paused); err != nil {
		t.Fatalf("parse pause response: %v", err)
	}
	if paused.Run.Status != workflow.RunStatePaused || paused.Run.PauseReason != "file conflict" || paused.Run.ResumeHint == "" {
		t.Fatalf("pause metadata missing: %+v", paused.Run)
	}
	resumeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"resume_run","run_id":"workflow-recovery-run","message":"conflict resolved"}`,
	})
	if err != nil {
		t.Fatalf("resume_run: %v", err)
	}
	var resumed struct {
		Run workflow.Run `json:"run"`
	}
	if err := json.Unmarshal([]byte(resumeResp), &resumed); err != nil {
		t.Fatalf("parse resume response: %v", err)
	}
	if resumed.Run.Status != workflow.RunStateRunning || resumed.Run.PauseReason != "" || resumed.Run.ResumeHint != "" {
		t.Fatalf("resume should clear active pause metadata: %+v", resumed.Run)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-recovery-run",
			"phase_id":"implement",
			"agent_id":"agent-impl",
			"task_name":"implementation",
			"agent_profile":"frontend_owner",
			"status":"failed",
			"message":"provider timeout"
		}`,
	}); err != nil {
		t.Fatalf("record failed agent: %v", err)
	}
	retryResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"retry_agent_run",
			"run_id":"workflow-recovery-run",
			"agent_id":"agent-impl",
			"retry_reason":"transient provider failure",
			"max_retries":1,
			"rollback_hint":"reuse the isolated worktree"
		}`,
	})
	if err != nil {
		t.Fatalf("retry_agent_run: %v", err)
	}
	var retry struct {
		AgentRun workflow.AgentRun `json:"agent_run"`
	}
	if err := json.Unmarshal([]byte(retryResp), &retry); err != nil {
		t.Fatalf("parse retry response: %v", err)
	}
	if retry.AgentRun.Status != workflow.AgentRunStateRetrying || retry.AgentRun.RetryCount != 1 || retry.AgentRun.MaxRetries != 1 {
		t.Fatalf("retry metadata missing: %+v", retry.AgentRun)
	}
	if retry.AgentRun.RetryReason != "transient provider failure" || retry.AgentRun.RollbackHint == "" {
		t.Fatalf("retry reason missing: %+v", retry.AgentRun)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"record_agent_run","run_id":"workflow-recovery-run","agent_id":"agent-impl","status":"failed","message":"retry failed"}`,
	}); err != nil {
		t.Fatalf("record retry failed: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"retry_agent_run","run_id":"workflow-recovery-run","agent_id":"agent-impl","max_retries":1}`,
	}); err == nil {
		t.Fatal("expected retry_agent_run to enforce max_retries")
	}
}

func TestWorkflowControlFileCheckpointRestore(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_workflow",
		Arguments: `{
			"run_id":"workflow-checkpoint-run",
			"plan":"## Phases\n\n1. Edit",
			"phases":[{"id":"edit","name":"Edit"}]
		}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	checkpointResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"create_file_checkpoint",
			"run_id":"workflow-checkpoint-run",
			"checkpoint_id":"checkpoint-before-edit",
			"paths":["notes.txt"],
			"message":"before editing notes"
		}`,
	})
	if err != nil {
		t.Fatalf("create_file_checkpoint: %v", err)
	}
	var checkpointResult struct {
		FileCheckpoint workflow.FileCheckpoint `json:"file_checkpoint"`
	}
	if err := json.Unmarshal([]byte(checkpointResp), &checkpointResult); err != nil {
		t.Fatalf("parse checkpoint response: %v", err)
	}
	if len(checkpointResult.FileCheckpoint.Files) != 1 || !checkpointResult.FileCheckpoint.Files[0].Existed {
		t.Fatalf("checkpoint file metadata missing: %+v", checkpointResult.FileCheckpoint)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("modify notes: %v", err)
	}
	restoreResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"restore_file_checkpoint","run_id":"workflow-checkpoint-run","checkpoint_id":"checkpoint-before-edit","message":"rollback failed edit"}`,
	})
	if err != nil {
		t.Fatalf("restore_file_checkpoint: %v", err)
	}
	if !strings.Contains(restoreResp, "notes.txt") {
		t.Fatalf("restore response should include restored file: %s", restoreResp)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("read notes after restore: %v", err)
	}
	if string(data) != "before\n" {
		t.Fatalf("file was not restored: %q", string(data))
	}
	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-checkpoint-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	if !strings.Contains(statusResp, "checkpoint-before-edit") {
		t.Fatalf("workflow_status should include checkpoint: %s", statusResp)
	}
}
