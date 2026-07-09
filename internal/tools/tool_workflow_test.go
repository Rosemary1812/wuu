package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestToolkitWorkflowToolsCreateAndInspectRun(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	kit.SetSessionID("thread-workflow")
	threadStateDir := filepath.Join(stateDir, "sessions", "thread-workflow")
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
		Action    string `json:"action"`
		Count     int    `json:"count"`
		Workflows []struct {
			Name      string   `json:"name"`
			NextSteps []string `json:"next_steps"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list response: %v", err)
	}
	if listed.Action != "list_workflows" {
		t.Fatalf("list action = %q, want list_workflows", listed.Action)
	}
	if listed.Count != 1 || listed.Workflows[0].Name != "feature-delivery" {
		t.Fatalf("unexpected workflow list: %+v", listed)
	}
	if !workflowStepsContain(listed.Workflows[0].NextSteps, "start_workflow") {
		t.Fatalf("markdown workflow list item should point to start_workflow: %+v", listed.Workflows[0].NextSteps)
	}

	loadResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_workflow",
		Arguments: `{"name":"feature-delivery","arguments":"settings search"}`,
	})
	if err != nil {
		t.Fatalf("load_workflow: %v", err)
	}
	var loaded struct {
		Action              string   `json:"action"`
		Content             string   `json:"content"`
		SuggestedPhaseNames []string `json:"suggested_phase_names"`
		NextSteps           []string `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(loadResp), &loaded); err != nil {
		t.Fatalf("parse load response: %v", err)
	}
	if loaded.Action != "load_workflow" {
		t.Fatalf("load action = %q, want load_workflow", loaded.Action)
	}
	if !strings.Contains(loaded.Content, "Build settings search.") {
		t.Fatalf("workflow arguments were not substituted: %s", loaded.Content)
	}
	if len(loaded.SuggestedPhaseNames) != 3 || loaded.SuggestedPhaseNames[0] != "Clarify product intent" {
		t.Fatalf("unexpected suggested phases: %+v", loaded.SuggestedPhaseNames)
	}
	if !workflowStepsContain(loaded.NextSteps, "start_workflow") {
		t.Fatalf("markdown workflow load should point to start_workflow: %+v", loaded.NextSteps)
	}
	records := kit.ToolTelemetry()
	if len(records) != 2 || records[0].ResultAction != "list_workflows" || records[1].ResultAction != "load_workflow" {
		t.Fatalf("workflow definition telemetry actions mismatch: %+v", records)
	}

	startResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "start_workflow",
		Arguments: `{
			"definition_name":"feature-delivery",
			"arguments":"settings search",
			"run_id":"workflow-start-run"
		}`,
	})
	if err != nil {
		t.Fatalf("start_workflow: %v", err)
	}
	var started struct {
		Action     string            `json:"action"`
		Driver     string            `json:"driver"`
		Entrypoint string            `json:"entrypoint"`
		RunID      string            `json:"run_id"`
		Status     workflow.RunState `json:"status"`
		GoalID     string            `json:"goal_id"`
		GoalDir    string            `json:"goal_dir"`
		Phases     []workflow.Phase  `json:"phases"`
	}
	if err := json.Unmarshal([]byte(startResp), &started); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	if started.Action != "create_workflow" || started.Driver != "agent_managed" || started.Entrypoint != "natural_language_agent" || started.RunID != "workflow-start-run" {
		t.Fatalf("unexpected start response: %+v", started)
	}
	if len(started.Phases) != 3 || started.Phases[0].Status != workflow.PhaseStateRunnable {
		t.Fatalf("unexpected start phases: %+v", started.Phases)
	}
	if started.GoalID != started.RunID || started.GoalDir != filepath.Join(threadStateDir, "goals", started.RunID) {
		t.Fatalf("workflow run missing goal binding: %+v", started)
	}
	storedRun, err := workflow.NewStore(threadStateDir).LoadRun(started.RunID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if storedRun.GoalID != started.GoalID || storedRun.GoalDir != started.GoalDir {
		t.Fatalf("stored workflow run missing goal binding: %+v", storedRun)
	}
	goalState, err := goalrunner.NewStore(started.GoalDir).LoadState()
	if err != nil {
		t.Fatalf("Load goal state: %v", err)
	}
	if goalState.ID != started.RunID || goalState.Status != goalrunner.StatusRunning {
		t.Fatalf("unexpected workflow goal state: %+v", goalState)
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
		Action     string            `json:"action"`
		Driver     string            `json:"driver"`
		Entrypoint string            `json:"entrypoint"`
		RunID      string            `json:"run_id"`
		Status     workflow.RunState `json:"status"`
		PlanPath   string            `json:"plan_path"`
		GoalID     string            `json:"goal_id"`
		GoalDir    string            `json:"goal_dir"`
		Phases     []workflow.Phase  `json:"phases"`
	}
	if err := json.Unmarshal([]byte(createResp), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	if created.Action != "create_workflow" || created.Driver != "agent_managed" || created.Entrypoint != "natural_language_agent" {
		t.Fatalf("unexpected workflow driver fields: %+v", created)
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
	if created.GoalID != created.RunID || created.GoalDir != filepath.Join(threadStateDir, "goals", created.RunID) {
		t.Fatalf("create_workflow missing goal binding: %+v", created)
	}
	createdGoalState, err := goalrunner.NewStore(created.GoalDir).LoadState()
	if err != nil {
		t.Fatalf("Load create_workflow goal state: %v", err)
	}
	if !goalStateHasArtifact(createdGoalState, "workflow", created.RunID, "plan", created.PlanPath) {
		t.Fatalf("goal state missing workflow plan artifact ref: %+v", createdGoalState.Artifacts)
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
		GoalStatus      struct {
			GoalID string            `json:"goal_id"`
			Status goalrunner.Status `json:"status"`
		} `json:"goal_status"`
	}
	if err := json.Unmarshal([]byte(finalResp), &final); err != nil {
		t.Fatalf("parse final response: %v", err)
	}
	if final.Run.Status != workflow.RunStateCompleted || final.FinalReportPath == "" {
		t.Fatalf("unexpected final response: %+v", final)
	}
	if final.GoalStatus.GoalID != created.GoalID || final.GoalStatus.Status != goalrunner.StatusCompleted {
		t.Fatalf("workflow-owned goal should complete with workflow run: %+v", final.GoalStatus)
	}
	createdGoalState, err = goalrunner.NewStore(created.GoalDir).LoadState()
	if err != nil {
		t.Fatalf("Load final workflow goal state: %v", err)
	}
	if createdGoalState.Status != goalrunner.StatusCompleted {
		t.Fatalf("workflow-owned goal status = %s, want completed", createdGoalState.Status)
	}
	if !goalStateHasArtifact(createdGoalState, "workflow", created.RunID, "final_report", final.FinalReportPath) {
		t.Fatalf("goal state missing workflow final report artifact ref: %+v", createdGoalState.Artifacts)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-test-run","include_events":true}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		Action           string                     `json:"action"`
		Run              workflow.Run               `json:"run"`
		AgentRuns        []workflow.AgentRun        `json:"agent_runs"`
		MemoryCandidates []workflow.MemoryCandidate `json:"memory_candidates"`
		Events           []workflow.Event           `json:"events"`
		NextSteps        []string                   `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if status.Action != "workflow_status" {
		t.Fatalf("workflow_status action = %q, want workflow_status", status.Action)
	}
	if status.Run.ID != "workflow-test-run" || status.Run.PlanPath == "" {
		t.Fatalf("unexpected status: %+v", status.Run)
	}
	if status.Run.Driver != workflow.RunDriverAgentManaged || status.Run.Entrypoint != workflow.RunEntrypointNaturalLanguageAgent {
		t.Fatalf("workflow_status should retain agent-managed driver fields: %+v", status.Run)
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
	if !workflowStepsContain(status.NextSteps, "final_report_path") {
		t.Fatalf("completed workflow_status should point to final report artifacts: %+v", status.NextSteps)
	}
	statusRecords := kit.ToolTelemetry()
	if len(statusRecords) == 0 || statusRecords[len(statusRecords)-1].ResultAction != "workflow_status" {
		t.Fatalf("workflow_status telemetry missing result action: %+v", statusRecords)
	}
}

func TestToolkitWorkflowToolsFilterByActiveSurface(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(t.TempDir())
	// Keep workflow tools directly testable while applying the no-shell surface
	// to workflow definition filtering. Named/resident model surfaces no longer
	// expose workflow tools, so installing an active profile here would test the
	// surface gate instead of the workflow filtering behavior.
	kit.env.ActiveSurface = modelprofile.DefaultCompiler{}.Compile(modelprofile.Resolve("ollama", "llama-coder"), modelprofile.SurfaceMain)
	kit.markDeferredToolsLoaded("list_workflows", "load_workflow", "save_workflow", "start_workflow", "run_workflow", "create_workflow")
	kit.SetWorkflows([]workflow.Definition{
		{
			Name:        "portable-plan",
			Description: "Plan a portable change.",
			WhenToUse:   "Use for planning.",
			Source:      "project",
			Content:     "## Intent\n\nCreate a scoped implementation plan.\n\n## Phases\n\n1. Inspect files\n2. Draft plan\n",
		},
		{
			Name:        "terminal-release",
			Description: "Git: release workflow.",
			WhenToUse:   "Use when a release needs command checks.",
			Source:      "project",
			Content:     "Run bash, git-status, git_status, and package checks before release.",
		},
	})

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_workflows", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_workflows: %v", err)
	}
	var listed struct {
		Count     int `json:"count"`
		Workflows []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list_workflows: %v", err)
	}
	if listed.Count != 1 || listed.Workflows[0].Name != "portable-plan" {
		t.Fatalf("local/no-shell should only list compatible workflow definitions: %+v", listed)
	}
	if strings.Contains(listResp, "terminal-release") || strings.Contains(listResp, "Git:") || strings.Contains(listResp, "git-status") || strings.Contains(listResp, "git_status") {
		t.Fatalf("local/no-shell list_workflows leaked terminal workflow:\n%s", listResp)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_workflow",
		Arguments: `{"name":"terminal-release"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("local/no-shell must not load terminal workflow, got %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_workflow",
		Arguments: `{"definition_name":"terminal-release","run_id":"blocked"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("local/no-shell must not start terminal workflow, got %v", err)
	}
	for _, call := range []providers.ToolCall{
		{Name: "save_workflow", Arguments: `{"name":"blocked-save","content":"Git: run git-status before release."}`},
		{Name: "run_workflow", Arguments: `{"script":"// Git: run git-status before release."}`},
		{Name: "create_workflow", Arguments: `{"plan":"Git: run git-status before release."}`},
	} {
		_, err = kit.Execute(context.Background(), call)
		if err == nil || !strings.Contains(err.Error(), "terminal-dependent") {
			t.Fatalf("local/no-shell must reject terminal-dependent %s content, got %v", call.Name, err)
		}
	}
}

func TestWorkflowAcceptedMemoryCandidatePersistsToProfileMemory(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	provider, err := memstore.NewFileProvider(filepath.Join(t.TempDir(), "profile-memory"))
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(stateDir)
	kit.SetMemory(provider)

	store := workflow.NewStore(stateDir)
	if _, err := store.CreateRun(workflow.Run{ID: "workflow-memory-run"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_memory_candidate",
			"run_id":"workflow-memory-run",
			"candidate_id":"candidate-1",
			"content":"Release workflow requires visual QA before tagging.",
			"target":"memory",
			"tags":["release","qa"],
			"source":"agent_report",
			"agent_profile":"qa_reviewer"
		}`,
	}); err != nil {
		t.Fatalf("record memory candidate: %v", err)
	}
	reviewResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"review_memory_candidate","run_id":"workflow-memory-run","candidate_id":"candidate-1","status":"accepted","message":"durable workflow fact"}`,
	})
	if err != nil {
		t.Fatalf("review memory candidate: %v", err)
	}
	var review struct {
		MemoryWrite struct {
			Persisted bool           `json:"persisted"`
			Target    string         `json:"target"`
			Result    map[string]any `json:"result"`
		} `json:"memory_write"`
	}
	if err := json.Unmarshal([]byte(reviewResp), &review); err != nil {
		t.Fatalf("parse review response: %v", err)
	}
	if !review.MemoryWrite.Persisted || review.MemoryWrite.Target != "memory" {
		t.Fatalf("memory write not persisted: %s", reviewResp)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_memory",
		Arguments: `{"target":"memory","query":"visual QA","limit":5}`,
	})
	if err != nil {
		t.Fatalf("read_memory: %v", err)
	}
	if !strings.Contains(readResp, "Release workflow requires visual QA before tagging.") ||
		!strings.Contains(readResp, "workflow_run:workflow-memory-run") ||
		!strings.Contains(readResp, "source:agent_report") {
		t.Fatalf("accepted workflow memory not visible in profile memory: %s", readResp)
	}
}

func TestSaveWorkflowWritesProjectDefinitionAndRegistersIt(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
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
		Action string `json:"action"`
		Name   string `json:"name"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(saveResp), &saved); err != nil {
		t.Fatalf("parse save response: %v", err)
	}
	if saved.Action != "save_workflow" {
		t.Fatalf("save action = %q, want save_workflow", saved.Action)
	}
	if saved.Name != "feature-delivery" || saved.Path == "" {
		t.Fatalf("unexpected save response: %+v", saved)
	}
	if !strings.Contains(saved.Path, filepath.Join(".wuu", "workflows", "feature_delivery", "WORKFLOW.md")) {
		t.Fatalf("saved project workflow should use native .wuu path: %+v", saved)
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
	loadWorkflowDriverToolsForTest(t, kit)
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

func TestSaveAndRunScriptWorkflow(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)

	script := `
phase("Plan", () => {
  const current = status();
  if (!current.run || current.run.id.indexOf("workflow-script-") !== 0) {
    throw new Error("status did not expose the current run");
  }
});
phase({id: "qa", name: "QA"}, () => {});
synthesize("# Final\n\nDynamic workflow complete for " + args.feature + ".");
`
	saveArgs, err := mustJSON(map[string]any{
		"name":            "dynamic-review",
		"description":     "Run a script-driven workflow.",
		"kind":            "script",
		"content":         script,
		"max_agents":      8,
		"max_concurrency": 3,
	})
	if err != nil {
		t.Fatalf("build save args: %v", err)
	}
	saveResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "save_workflow",
		Arguments: saveArgs,
	})
	if err != nil {
		t.Fatalf("save_workflow script: %v", err)
	}
	var saved struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(saveResp), &saved); err != nil {
		t.Fatalf("parse save response: %v", err)
	}
	if saved.Kind != workflow.DefinitionKindScript || filepath.Base(saved.Path) != "WORKFLOW.js" {
		t.Fatalf("unexpected saved script workflow: %+v", saved)
	}
	if !strings.Contains(saved.Path, filepath.Join(".wuu", "workflows", "dynamic_review", "WORKFLOW.js")) {
		t.Fatalf("saved script workflow should use native .wuu path: %+v", saved)
	}

	loadResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_workflow",
		Arguments: `{"name":"dynamic-review","arguments":"{\"feature\":\"settings\"}"}`,
	})
	if err != nil {
		t.Fatalf("load_workflow script: %v", err)
	}
	var loaded struct {
		Kind                string   `json:"kind"`
		Content             string   `json:"content"`
		SuggestedPhaseNames []string `json:"suggested_phase_names"`
		NextSteps           []string `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(loadResp), &loaded); err != nil {
		t.Fatalf("parse load response: %v", err)
	}
	if loaded.Kind != workflow.DefinitionKindScript || !strings.Contains(loaded.Content, "synthesize") {
		t.Fatalf("unexpected loaded script workflow: %+v", loaded)
	}
	if loaded.SuggestedPhaseNames != nil {
		t.Fatalf("script workflow should not expose markdown phase suggestions: %+v", loaded.SuggestedPhaseNames)
	}
	if !workflowStepsContain(loaded.NextSteps, "start_workflow") {
		t.Fatalf("script workflow load should point to start_workflow: %+v", loaded.NextSteps)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"definition_name":"dynamic-review","run_id":"wrong-driver"}`,
	}); err == nil {
		t.Fatal("create_workflow should reject script workflow definitions")
	} else if !strings.Contains(err.Error(), "kind=script") || !strings.Contains(err.Error(), "run_workflow") {
		t.Fatalf("create_workflow script rejection should point to run_workflow, got: %v", err)
	}

	startResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "start_workflow",
		Arguments: `{
			"definition_name":"dynamic-review",
			"arguments":"{\"feature\":\"settings\"}",
			"run_id":"workflow-script-start",
			"background":false
		}`,
	})
	if err != nil {
		t.Fatalf("start_workflow script: %v", err)
	}
	var started struct {
		Action     string            `json:"action"`
		Driver     string            `json:"driver"`
		RunID      string            `json:"run_id"`
		Status     workflow.RunState `json:"status"`
		GoalID     string            `json:"goal_id"`
		GoalDir    string            `json:"goal_dir"`
		ScriptPath string            `json:"script_path"`
		Background bool              `json:"background"`
	}
	if err := json.Unmarshal([]byte(startResp), &started); err != nil {
		t.Fatalf("parse script start response: %v", err)
	}
	if started.Action != "run_workflow" || started.Driver != "script" || started.RunID != "workflow-script-start" || started.Status != workflow.RunStateCompleted || started.Background {
		t.Fatalf("unexpected script start response: %+v", started)
	}
	if _, err := os.Stat(started.ScriptPath); err != nil {
		t.Fatalf("script start artifact not written: %v", err)
	}
	if started.GoalID != started.RunID || started.GoalDir != filepath.Join(stateDir, "goals", started.RunID) {
		t.Fatalf("script workflow run missing goal binding: %+v", started)
	}
	scriptGoalState, err := goalrunner.NewStore(started.GoalDir).LoadState()
	if err != nil {
		t.Fatalf("Load script goal state: %v", err)
	}
	if scriptGoalState.ID != started.RunID || scriptGoalState.Status != goalrunner.StatusCompleted {
		t.Fatalf("unexpected script workflow goal state: %+v", scriptGoalState)
	}

	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "run_workflow",
		Arguments: `{
			"definition_name":"dynamic-review",
			"arguments":"{\"feature\":\"settings\"}",
			"run_id":"workflow-script-run",
			"background":false
		}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var ran struct {
		Action     string            `json:"action"`
		RunID      string            `json:"run_id"`
		Status     workflow.RunState `json:"status"`
		ScriptPath string            `json:"script_path"`
		Background bool              `json:"background"`
	}
	if err := json.Unmarshal([]byte(runResp), &ran); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if ran.Action != "run_workflow" || ran.RunID != "workflow-script-run" || ran.Status != workflow.RunStateCompleted || ran.Background {
		t.Fatalf("unexpected run response: %+v", ran)
	}
	if _, err := os.Stat(ran.ScriptPath); err != nil {
		t.Fatalf("script artifact not written: %v", err)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-script-run","include_events":true}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		Run    workflow.Run     `json:"run"`
		Events []workflow.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if status.Run.Status != workflow.RunStateCompleted || len(status.Run.Phases) != 2 {
		t.Fatalf("script run state mismatch: %+v", status.Run)
	}
	if status.Run.Driver != workflow.RunDriverScript || status.Run.Entrypoint != workflow.RunEntrypointNaturalLanguageAgent {
		t.Fatalf("workflow_status should retain script driver fields: %+v", status.Run)
	}
	if status.Run.Phases[0].ID != "plan" || status.Run.Phases[1].ID != "qa" {
		t.Fatalf("script phases not recorded: %+v", status.Run.Phases)
	}
	if status.Run.ScriptPath == "" || status.Run.FinalReportPath == "" {
		t.Fatalf("script/final artifacts missing: %+v", status.Run)
	}
	report, err := os.ReadFile(status.Run.FinalReportPath)
	if err != nil {
		t.Fatalf("read final report: %v", err)
	}
	if !strings.Contains(string(report), "Dynamic workflow complete for settings") {
		t.Fatalf("final report mismatch:\n%s", string(report))
	}
	scriptRunGoalState, err := goalrunner.NewStore(status.Run.GoalDir).LoadState()
	if err != nil {
		t.Fatalf("Load script run goal state: %v", err)
	}
	if !goalStateHasArtifact(scriptRunGoalState, "workflow", status.Run.ID, "script", status.Run.ScriptPath) {
		t.Fatalf("goal state missing workflow script artifact ref: %+v", scriptRunGoalState.Artifacts)
	}
	if !goalStateHasArtifact(scriptRunGoalState, "workflow", status.Run.ID, "final_report", status.Run.FinalReportPath) {
		t.Fatalf("goal state missing script final report artifact ref: %+v", scriptRunGoalState.Artifacts)
	}
	if !workflowEventsContain(status.Events, workflow.EventScriptWritten) {
		t.Fatalf("expected script_written event, got %+v", status.Events)
	}
}

func TestRunScriptWorkflowSpawnsAndAwaitsAgent(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)

	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "agent done"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "workflow-script-session",
		HistoryDir:   filepath.Join(stateDir, "workers"),
		ThreadDir:    filepath.Join(stateDir, "threads"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	script := `
phase("Workers", () => {
  const spawned = spawnAgent({name: "qa", description: "Run QA", prompt: "Run QA.", subagentType: "general-purpose"});
  if (spawned.status !== "completed" || spawned.result !== "agent done") {
    throw new Error("spawnAgent did not return the completed worker result");
  }
  const awaited = awaitAgents();
  if (!awaited.results || awaited.results.length !== 1 || !awaited.results[0].result_consumed || awaited.results[0].consumed_by !== "spawn_agent" || awaited.results[0].result !== "agent done") {
    throw new Error("awaitAgents should report the foreground spawn result as consumed but still carry its text");
  }
  synthesize("# Final\n\n" + spawned.result);
});
`
	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "run_workflow",
		Arguments: `{
			"script":` + strconv.Quote(script) + `,
			"run_id":"workflow-script-spawn-run",
			"background":false
		}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var ran struct {
		Driver     string            `json:"driver"`
		Entrypoint string            `json:"entrypoint"`
		RunID      string            `json:"run_id"`
		GoalID     string            `json:"goal_id"`
		GoalDir    string            `json:"goal_dir"`
		Status     workflow.RunState `json:"status"`
	}
	if err := json.Unmarshal([]byte(runResp), &ran); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if ran.Driver != "script" || ran.Entrypoint != "natural_language_agent" {
		t.Fatalf("unexpected workflow driver fields: %+v", ran)
	}
	if ran.Status != workflow.RunStateCompleted {
		t.Fatalf("workflow should complete: %+v", ran)
	}
	if ran.GoalID != ran.RunID || ran.GoalDir != filepath.Join(stateDir, "goals", ran.RunID) {
		t.Fatalf("script workflow missing goal binding: %+v", ran)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-script-spawn-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		Run          workflow.Run        `json:"run"`
		AgentRuns    []workflow.AgentRun `json:"agent_runs"`
		WorkflowTeam workflow.TeamPlan   `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if len(status.AgentRuns) != 1 || status.AgentRuns[0].TaskName != "qa" || status.AgentRuns[0].Result != "agent done" {
		t.Fatalf("agent run not recorded from script runtime: %+v", status.AgentRuns)
	}
	if len(status.Run.Phases) != 1 || len(status.Run.Phases[0].AgentRunIDs) != 1 {
		t.Fatalf("agent run should attach to current phase: %+v", status.Run.Phases)
	}
	if len(status.WorkflowTeam.Members) != 1 || status.WorkflowTeam.Members[0].TaskName != "qa" || status.WorkflowTeam.Members[0].Mode != workflow.TeamMemberEphemeral {
		t.Fatalf("script driver should record workflow team member: %+v", status.WorkflowTeam)
	}
	tasks, err := control.HarnessStore().ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].GoalID != "" || tasks[0].GoalDir != "" {
		t.Fatalf("workflow-spawned harness task should not bind legacy goal state: %+v", tasks)
	}
}

func TestRunScriptWorkflowSupportsSpawnPrimitive(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)

	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "spawn primitive done"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "workflow-spawn-primitive-session",
		HistoryDir:   filepath.Join(stateDir, "workers"),
		ThreadDir:    filepath.Join(stateDir, "threads"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	script := `
phase("Spawn primitive", () => {
  const spawned = spawn("qa_reviewer", {prompt: "Run QA.", subagentType: "general-purpose"});
  if (spawned.taskName !== "qa_reviewer_1") {
    throw new Error("spawn did not derive a stable task name: " + spawned.taskName);
  }
  if (spawned.agentProfile) {
    throw new Error("spawn role label should not become an Agent Profile");
  }
  if (spawned.status !== "completed" || spawned.result !== "spawn primitive done") {
    throw new Error("spawn did not return the completed worker result");
  }
  synthesize("# Final\n\n" + spawned.result);
});
`
	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "run_workflow",
		Arguments: `{
			"script":` + strconv.Quote(script) + `,
			"run_id":"workflow-spawn-primitive-run",
			"background":false
		}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var ran struct {
		Status workflow.RunState `json:"status"`
	}
	if err := json.Unmarshal([]byte(runResp), &ran); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if ran.Status != workflow.RunStateCompleted {
		t.Fatalf("workflow should complete: %+v", ran)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-spawn-primitive-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		AgentRuns []workflow.AgentRun `json:"agent_runs"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if len(status.AgentRuns) != 1 {
		t.Fatalf("expected one agent run, got %+v", status.AgentRuns)
	}
	run := status.AgentRuns[0]
	if run.TaskName != "qa_reviewer_1" || run.AgentProfile != "" || !strings.Contains(run.Prompt, "Role: qa_reviewer") {
		t.Fatalf("spawn primitive metadata mismatch: %+v", run)
	}
}

func TestRunScriptWorkflowSupportsSpawnBatchAlias(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)

	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "batch worker done"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "workflow-spawn-batch-session",
		HistoryDir:   filepath.Join(stateDir, "workers"),
		ThreadDir:    filepath.Join(stateDir, "threads"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	script := `
phase("Batch", () => {
  const spawned = spawnBatch([
    {name: "qa_1", description: "Run QA 1", prompt: "Run QA 1.", subagentType: "general-purpose"},
    {name: "qa_2", description: "Run QA 2", prompt: "Run QA 2.", subagentType: "general-purpose"}
  ]);
  if (!spawned || spawned.length !== 2) {
    throw new Error("spawnBatch should return two spawn results");
  }
  if (spawned[0].status !== "completed" || spawned[1].status !== "completed") {
    throw new Error("spawnBatch did not return completed foreground results");
  }
  synthesize("# Final\n\n" + spawned.map((item) => item.result).join("\n"));
});
`
	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "run_workflow",
		Arguments: `{
			"script":` + strconv.Quote(script) + `,
			"run_id":"workflow-spawn-batch-run",
			"background":false,
			"max_concurrency":2
		}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var ran struct {
		Status workflow.RunState `json:"status"`
	}
	if err := json.Unmarshal([]byte(runResp), &ran); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if ran.Status != workflow.RunStateCompleted {
		t.Fatalf("workflow should complete: %+v", ran)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-spawn-batch-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		AgentRuns []workflow.AgentRun `json:"agent_runs"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if len(status.AgentRuns) != 2 {
		t.Fatalf("expected two agent runs, got %+v", status.AgentRuns)
	}
}

func TestWorkflowControlRecordsAgentRunsAndGeneratesReport(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
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
	// A completed worker that filed no structured report (empty report_path)
	// must still surface as a missing-report arbitration signal.
	awaitingResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-await-run",
			"phase_id":"qa",
			"agent_id":"agent-qa",
			"task_name":"qa_check",
			"agent_profile":"qa_reviewer",
			"status":"completed",
			"result":"QA finished but skipped the structured handoff.",
			"changed_files":["desktop/src/App.tsx"]
		}`,
	})
	if err != nil {
		t.Fatalf("record_agent_run awaiting: %v", err)
	}
	var awaiting struct {
		AgentRun workflow.AgentRun `json:"agent_run"`
	}
	if err := json.Unmarshal([]byte(awaitingResp), &awaiting); err != nil {
		t.Fatalf("parse awaiting response: %v", err)
	}
	if awaiting.AgentRun.Status != workflow.AgentRunStateCompleted || strings.TrimSpace(awaiting.AgentRun.ReportPath) != "" {
		t.Fatalf("agent run should be completed with no structured report: %+v", awaiting.AgentRun)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"generate_final_report","run_id":"workflow-await-run","complete_run":true}`,
	}); err == nil {
		t.Fatal("expected completion to reject a completed agent that filed no structured report")
	}
	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-await-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status awaiting: %v", err)
	}
	var awaitingStatus struct {
		TeamArbitration workflow.TeamArbitration `json:"team_arbitration"`
		NextSteps       []string                 `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(statusResp), &awaitingStatus); err != nil {
		t.Fatalf("parse awaiting workflow_status: %v", err)
	}
	if awaitingStatus.TeamArbitration.Status != "attention_required" ||
		len(awaitingStatus.TeamArbitration.MissingReports) != 1 ||
		awaitingStatus.TeamArbitration.MissingReports[0] != "agent-qa" {
		t.Fatalf("workflow_status should surface missing report arbitration: %+v", awaitingStatus.TeamArbitration)
	}
	if !workflowStepsContain(awaitingStatus.NextSteps, "agent_report") {
		t.Fatalf("awaiting workflow_status should guide the agent to collect report handoffs: %+v", awaitingStatus.NextSteps)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_agent_run",
			"run_id":"workflow-await-run",
			"phase_id":"qa",
			"agent_id":"agent-qa",
			"task_name":"qa_check",
			"agent_profile":"qa_reviewer",
			"status":"completed",
			"result":"QA passed after adding the structured handoff.",
			"report_path":"reports/agent-qa.md",
			"changed_files":["desktop/src/App.tsx"],
			"artifacts":["reports/agent-qa.md"]
		}`,
	}); err != nil {
		t.Fatalf("record_agent_run completed: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"set_phase_status","run_id":"workflow-await-run","phase_id":"qa","status":"completed"}`,
	}); err != nil {
		t.Fatalf("set phase completed: %v", err)
	}
	statusResp, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-await-run"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status completed: %v", err)
	}
	var completedStatus struct {
		TeamArbitration workflow.TeamArbitration `json:"team_arbitration"`
	}
	if err := json.Unmarshal([]byte(statusResp), &completedStatus); err != nil {
		t.Fatalf("parse completed workflow_status: %v", err)
	}
	if completedStatus.TeamArbitration.Status != "clear" {
		t.Fatalf("workflow_status should clear arbitration after report: %+v", completedStatus.TeamArbitration)
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
	loadWorkflowDriverToolsForTest(t, kit)
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

func TestWorkflowControlResumeStartsInitiallyPausedScriptWorkflow(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	wuuHome := t.TempDir()
	t.Setenv("WUU_HOME", wuuHome)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	kit.SetWorkflows([]workflow.Definition{{
		Name:                 "script-profile-gate",
		Kind:                 workflow.DefinitionKindScript,
		Content:              `phase("Finish", () => { synthesize("# Final\n\nresumed for " + args.feature); });`,
		Profiles:             []workflow.ProfileRef{{Name: "frontend_owner", Required: true}},
		AllowProfileCreation: "ask",
	}})

	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_workflow",
		Arguments: `{"definition_name":"script-profile-gate","arguments":"{\"feature\":\"settings\"}","run_id":"workflow-script-profile-gate"}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var started struct {
		Status     workflow.RunState `json:"status"`
		Background bool              `json:"background"`
	}
	if err := json.Unmarshal([]byte(runResp), &started); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if started.Status != workflow.RunStatePaused || started.Background {
		t.Fatalf("workflow should wait for required profile before starting: %+v", started)
	}

	if _, _, err := workflow.EnsureProfile(workflow.ProfileEnsureOptions{
		WuuHome:      wuuHome,
		Name:         "frontend_owner",
		WorkflowName: "script-profile-gate",
		Role:         "Frontend owner",
	}); err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	resumeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"resume_run","run_id":"workflow-script-profile-gate","message":"profile ready"}`,
	})
	if err != nil {
		t.Fatalf("resume_run: %v", err)
	}
	var resumed struct {
		Run        workflow.Run `json:"run"`
		Background bool         `json:"background"`
	}
	if err := json.Unmarshal([]byte(resumeResp), &resumed); err != nil {
		t.Fatalf("parse resume response: %v", err)
	}
	if resumed.Run.Status != workflow.RunStateRunning || !resumed.Background {
		t.Fatalf("resume should start the saved script in background: %+v", resumed)
	}

	var status struct {
		Run workflow.Run `json:"run"`
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "workflow_status",
			Arguments: `{"run_id":"workflow-script-profile-gate"}`,
		})
		if err != nil {
			t.Fatalf("workflow_status: %v", err)
		}
		if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
			t.Fatalf("parse status response: %v", err)
		}
		if status.Run.Status == workflow.RunStateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Run.Status != workflow.RunStateCompleted || status.Run.FinalReportPath == "" {
		t.Fatalf("resumed script workflow should complete: %+v", status.Run)
	}
	data, err := os.ReadFile(status.Run.FinalReportPath)
	if err != nil {
		t.Fatalf("read final report: %v", err)
	}
	if !strings.Contains(string(data), "resumed for settings") {
		t.Fatalf("final report mismatch:\n%s", string(data))
	}
}

func TestWorkflowControlRecordsWorkflowTeam(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	wuuHome := t.TempDir()
	t.Setenv("WUU_HOME", wuuHome)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	kit.SetWorkflows([]workflow.Definition{{
		Name:      "release-qa",
		Content:   "## Phases\n\n1. QA\n2. Docs\n",
		MaxAgents: 3,
	}})
	if _, _, err := workflow.EnsureProfile(workflow.ProfileEnsureOptions{
		WuuHome: wuuHome,
		Name:    "qa_laowang",
		Role:    "QA reviewer",
	}); err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"definition_name":"release-qa","run_id":"workflow-team-plan"}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	recordResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_workflow_team",
			"run_id":"workflow-team-plan",
			"team":[
				{"role":"QA reviewer","mode":"reuse_profile","agent_profile":"qa_laowang","task_name":"qa_check","phase_id":"qa","reason":"Existing QA profile knows this project."},
				{"role":"Screenshot tester","mode":"create_profile","agent_profile":"screenshot_qa","task_name":"screenshot_check","reason":"Recurring visual QA role."},
				{"role":"Docs checker","mode":"ephemeral","task_name":"docs_check","reason":"One-off docs review."}
			]
		}`,
	})
	if err != nil {
		t.Fatalf("record_workflow_team: %v", err)
	}
	var recorded struct {
		Action       string            `json:"action"`
		WorkflowTeam workflow.TeamPlan `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(recordResp), &recorded); err != nil {
		t.Fatalf("parse record response: %v", err)
	}
	if recorded.Action != "record_workflow_team" || len(recorded.WorkflowTeam.Members) != 3 {
		t.Fatalf("unexpected workflow team response: %+v", recorded)
	}
	if strings.Contains(recordResp, `"team_plan"`) {
		t.Fatalf("record_workflow_team should expose only workflow_team, got: %s", recordResp)
	}
	if !recorded.WorkflowTeam.Members[1].CreatedProfile {
		t.Fatalf("create_profile member should create profile: %+v", recorded.WorkflowTeam.Members[1])
	}
	if _, ok, err := workflow.LoadProfile(wuuHome, "screenshot_qa"); err != nil || !ok {
		t.Fatalf("created profile missing, ok=%t err=%v", ok, err)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"workflow-team-plan"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		WorkflowTeam workflow.TeamPlan `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if len(status.WorkflowTeam.Members) != 3 || status.WorkflowTeam.Members[0].AgentProfile != "qa_laowang" || status.WorkflowTeam.Members[2].AgentProfile != "" {
		t.Fatalf("status workflow team mismatch: %+v", status.WorkflowTeam)
	}
	if strings.Contains(statusResp, `"team_plan"`) {
		t.Fatalf("workflow_status should expose only workflow_team, got: %s", statusResp)
	}
}

// TestWorkflowOrchestrationTaskLeadGate proves the execute-time authorization
// gate: workflow orchestration tools stay statically registered for every named
// agent (no schema churn), and authority is enforced purely at runtime by
// whether the caller leads an active task cth. A lead is allowed and its run
// binds to the reply subthread (so the pool resolves from the cth's parent
// group); a non-lead named member is rejected; a self-contained caller with no
// participant identity is allowed unchanged; and resolving the task reclaims the
// orchestration authority.
func TestWorkflowOrchestrationTaskLeadGate(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	sessDir := t.TempDir()

	// A group thread with a reply subthread escalated to a task led by prt-lead.
	if _, err := session.CreateWithMetadata(sessDir, "grp-1", root); err != nil {
		t.Fatalf("CreateWithMetadata: %v", err)
	}
	cth, err := session.CreateConversationThread(sessDir, session.ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-1",
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if _, err := session.EscalateConversationThread(sessDir, cth.ID, "human", "prt-lead", "Ship it"); err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}

	newKit := func(participant string) *Toolkit {
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		loadWorkflowDriverToolsForTest(t, kit)
		kit.SetStateDir(stateDir)
		kit.SetConversationSessionDir(sessDir)
		if participant != "" {
			kit.SetParticipantIdentity(participant)
		}
		kit.SetGroupManager(&fakeGroupManager{members: map[string][]GroupMember{
			"grp-1": {{ID: "prt-lead", Name: "Lead"}, {ID: "prt-rina", Name: "Rina"}},
		}})
		return kit
	}

	// Lead may create a run; it binds to the task cth.
	leadKit := newKit("prt-lead")
	if _, err := leadKit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"wf-lead","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}],"initial_status":"running"}`,
	}); err != nil {
		t.Fatalf("lead create_workflow: %v", err)
	}
	leadRun, err := workflow.NewStore(stateDir).LoadRun("wf-lead")
	if err != nil {
		t.Fatalf("LoadRun(wf-lead): %v", err)
	}
	if leadRun.ThreadID != cth.ID {
		t.Fatalf("run bound to %q, want cth %q", leadRun.ThreadID, cth.ID)
	}

	// The bound run resolves its named-participant pool from the cth's parent
	// group, so a group member can be enlisted.
	teamResp, err := leadKit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"record_workflow_team","run_id":"wf-lead","team":[{"role":"Reviewer","mode":"named_participant","participant":"Rina","task_name":"review"}]}`,
	})
	if err != nil {
		t.Fatalf("lead record_workflow_team named_participant: %v", err)
	}
	var recorded struct {
		WorkflowTeam workflow.TeamPlan `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(teamResp), &recorded); err != nil {
		t.Fatalf("parse team response: %v", err)
	}
	if len(recorded.WorkflowTeam.Members) != 1 || recorded.WorkflowTeam.Members[0].ParticipantID != "prt-rina" {
		t.Fatalf("named-participant pool did not resolve from parent group: %+v", recorded.WorkflowTeam.Members)
	}

	// A named member that is not the lead is refused on every orchestration entrypoint.
	rinaKit := newKit("prt-rina")
	for _, call := range []providers.ToolCall{
		{Name: "create_workflow", Arguments: `{"run_id":"wf-rina","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}]}`},
		{Name: "run_workflow", Arguments: `{"run_id":"wf-rina2","script":"phase('x');"}`},
		{Name: "start_workflow", Arguments: `{"run_id":"wf-rina3","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}]}`},
	} {
		if _, err := rinaKit.Execute(context.Background(), call); err == nil || !strings.Contains(err.Error(), "task lead") {
			t.Fatalf("%s by non-lead = %v, want task-lead rejection", call.Name, err)
		}
	}
	// A non-lead also cannot drive the lead's existing run.
	if _, err := rinaKit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"pause_run","run_id":"wf-lead"}`,
	}); err == nil || !strings.Contains(err.Error(), "task lead") {
		t.Fatalf("non-lead workflow_control = %v, want task-lead rejection", err)
	}

	// A self-contained caller (no participant identity) is unaffected by the gate.
	selfKit := newKit("")
	if _, err := selfKit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"wf-self","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}]}`,
	}); err != nil {
		t.Fatalf("self-contained create_workflow: %v", err)
	}
	selfRun, err := workflow.NewStore(stateDir).LoadRun("wf-self")
	if err != nil {
		t.Fatalf("LoadRun(wf-self): %v", err)
	}
	if selfRun.ThreadID != "" {
		t.Fatalf("self-contained run should have no thread binding, got %q", selfRun.ThreadID)
	}

	// Lead may drive its own run while the task is live.
	if _, err := leadKit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"pause_run","run_id":"wf-lead"}`,
	}); err != nil {
		t.Fatalf("lead workflow_control pause_run: %v", err)
	}

	// Resolving the task reclaims authority: the former lead can no longer create
	// new runs or drive the bound run.
	if err := session.UpdateConversationThreadStatus(sessDir, cth.ID, session.ConversationThreadResolved); err != nil {
		t.Fatalf("resolve task: %v", err)
	}
	if _, err := leadKit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"wf-lead-after","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}]}`,
	}); err == nil || !strings.Contains(err.Error(), "task lead") {
		t.Fatalf("create after resolve = %v, want task-lead rejection", err)
	}
	if _, err := leadKit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_control",
		Arguments: `{"action":"resume_run","run_id":"wf-lead"}`,
	}); err == nil || !strings.Contains(err.Error(), "task lead") {
		t.Fatalf("workflow_control after resolve = %v, want task-lead rejection", err)
	}
}

// TestWorkflowScriptDriverDispatchesNamedParticipantThroughCthBinding proves the
// full ownership + named-orchestration loop end to end on the SCRIPT driver: a
// task lead runs run_workflow, the run binds to the reply subthread it leads
// (run.ThreadID == cth.ID), the script's named-participant pool resolves from the
// cth's PARENT group (not the cth id itself), and a spawned named participant is
// dispatched under its own durable identity with the result folded back into the
// cth-bound run's team plan and agent run. This is the script-path complement to
// TestWorkflowOrchestrationTaskLeadGate, which exercises the agent-managed
// (record_workflow_team) seam.
func TestWorkflowScriptDriverDispatchesNamedParticipantThroughCthBinding(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	sessDir := t.TempDir()

	// A group thread with a reply subthread escalated to a task led by prt-lead.
	if _, err := session.CreateWithMetadata(sessDir, "grp-1", root); err != nil {
		t.Fatalf("CreateWithMetadata: %v", err)
	}
	cth, err := session.CreateConversationThread(sessDir, session.ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-1",
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if _, err := session.EscalateConversationThread(sessDir, cth.ID, "human", "prt-lead", "Ship it"); err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}

	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	kit.SetConversationSessionDir(sessDir)
	kit.SetParticipantIdentity("prt-lead")
	// The named-participant pool lives on the PARENT group thread; the cth id is
	// never itself a group session, so the run must resolve cth -> parent group.
	kit.SetGroupManager(&fakeGroupManager{members: map[string][]GroupMember{
		"grp-1": {{ID: "prt-lead", Name: "Lead"}, {ID: "prt-rina", Name: "Rina"}},
	}})

	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "review done"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "workflow-named-participant-session",
		HistoryDir:   filepath.Join(stateDir, "workers"),
		ThreadDir:    filepath.Join(stateDir, "threads"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	// The lead's script dispatches to the group member "Rina" by name; the pool
	// resolution + case-insensitive match run inside the script driver.
	script := `
phase("Review", () => {
  const spawned = spawn({participant: "Rina", prompt: "Review the change.", subagentType: "general-purpose"});
  if (spawned.status !== "completed" || spawned.result !== "review done") {
    throw new Error("named participant spawn did not complete: " + JSON.stringify(spawned));
  }
  synthesize("# Final\n\n" + spawned.result);
});
`
	runResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "run_workflow",
		Arguments: `{
			"script":` + strconv.Quote(script) + `,
			"run_id":"wf-named-e2e",
			"background":false
		}`,
	})
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	var ran struct {
		Status workflow.RunState `json:"status"`
	}
	if err := json.Unmarshal([]byte(runResp), &ran); err != nil {
		t.Fatalf("parse run response: %v", err)
	}
	if ran.Status != workflow.RunStateCompleted {
		t.Fatalf("workflow should complete: %+v", ran)
	}

	// The run bound to the reply subthread the lead leads, not the parent group.
	boundRun, err := workflow.NewStore(stateDir).LoadRun("wf-named-e2e")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if boundRun.ThreadID != cth.ID {
		t.Fatalf("run bound to %q, want cth %q", boundRun.ThreadID, cth.ID)
	}

	statusResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"wf-named-e2e"}`,
	})
	if err != nil {
		t.Fatalf("workflow_status: %v", err)
	}
	var status struct {
		AgentRuns    []workflow.AgentRun `json:"agent_runs"`
		WorkflowTeam workflow.TeamPlan   `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(statusResp), &status); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	// The named participant was recorded as a named_participant team member bound
	// to the resolved group identity (prt-rina), never an ephemeral/profile worker.
	if len(status.WorkflowTeam.Members) != 1 {
		t.Fatalf("expected one team member, got %+v", status.WorkflowTeam.Members)
	}
	m := status.WorkflowTeam.Members[0]
	if m.Mode != workflow.TeamMemberNamedParticipant || m.ParticipantID != "prt-rina" || m.ParticipantName != "Rina" || m.AgentProfile != "" {
		t.Fatalf("named participant team member mismatch: %+v", m)
	}
	// The agent run's result folded back into the cth-bound run.
	if len(status.AgentRuns) != 1 || status.AgentRuns[0].Result != "review done" {
		t.Fatalf("named participant agent run not recorded: %+v", status.AgentRuns)
	}

	// Exactly one worker was dispatched (the named participant), not an extra
	// ephemeral fallback.
	tasks, err := control.HarnessStore().ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one dispatched worker task, got %+v", tasks)
	}
}

func TestWorkflowControlRecordsNamedParticipantTeam(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	// Bind the toolkit to a group thread whose members are the named-participant
	// pool. Only these agents may fill named_participant slots.
	kit.SetWorkflowThreadID("thr-group")
	kit.SetGroupManager(&fakeGroupManager{members: map[string][]GroupMember{
		"thr-group": {{ID: "prt-1", Name: "Rina"}, {ID: "prt-2", Name: "Kenta"}},
	}})

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"wf-named","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}],"initial_status":"running"}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}

	recordResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_workflow_team",
			"run_id":"wf-named",
			"team":[
				{"role":"Reviewer","mode":"named_participant","participant":"Rina","task_name":"review","reason":"Rina reviewed this area before."}
			]
		}`,
	})
	if err != nil {
		t.Fatalf("record_workflow_team named_participant: %v", err)
	}
	var recorded struct {
		WorkflowTeam workflow.TeamPlan `json:"workflow_team"`
	}
	if err := json.Unmarshal([]byte(recordResp), &recorded); err != nil {
		t.Fatalf("parse record response: %v", err)
	}
	if len(recorded.WorkflowTeam.Members) != 1 {
		t.Fatalf("member count: %+v", recorded.WorkflowTeam.Members)
	}
	m := recorded.WorkflowTeam.Members[0]
	if m.Mode != workflow.TeamMemberNamedParticipant || m.ParticipantID != "prt-1" || m.ParticipantName != "Rina" || m.AgentProfile != "" {
		t.Fatalf("named participant member mismatch: %+v", m)
	}

	// A participant outside the group pool is rejected.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_workflow_team",
			"run_id":"wf-named",
			"team":[{"role":"Reviewer","mode":"named_participant","participant":"Ghost"}]
		}`,
	}); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("expected out-of-group participant rejection, got %v", err)
	}
}

// TestWorkflowControlRejectsBusyNamedParticipant proves the decision-five
// concurrency lock at the agent-managed team-plan seam: a group member already
// executing another task/workflow run is refused (told busy) rather than being
// enlisted into a second workflow that would race the same resident agent.
func TestWorkflowControlRejectsBusyNamedParticipant(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	kit.SetWorkflowThreadID("thr-group")
	kit.SetGroupManager(&fakeGroupManager{members: map[string][]GroupMember{
		"thr-group": {{ID: "prt-1", Name: "Rina", Busy: true}, {ID: "prt-2", Name: "Kenta"}},
	}})

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"wf-busy","plan":"## Phases\n\n1. Review\n","phases":[{"name":"Review"}],"initial_status":"running"}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}

	// The busy member is refused with a busy message.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_workflow_team",
			"run_id":"wf-busy",
			"team":[{"role":"Reviewer","mode":"named_participant","participant":"Rina"}]
		}`,
	}); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy member rejection, got %v", err)
	}

	// An idle member of the same group is still accepted.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_workflow_team",
			"run_id":"wf-busy",
			"team":[{"role":"Reviewer","mode":"named_participant","participant":"Kenta"}]
		}`,
	}); err != nil {
		t.Fatalf("idle member should be accepted, got %v", err)
	}
}

func TestWorkflowControlDefinitionPrefersWorkflowTeamName(t *testing.T) {
	tool := NewWorkflowControlTool(&Env{})
	data, err := json.Marshal(tool.Definition().InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	schema := string(data)
	if !strings.Contains(schema, "record_workflow_team") {
		t.Fatalf("workflow_control schema should expose record_workflow_team: %s", schema)
	}
	if strings.Contains(schema, "record_team_plan") {
		t.Fatalf("workflow_control schema should not expose deprecated record_team_plan: %s", schema)
	}
	if strings.Contains(schema, "team_plan") {
		t.Fatalf("workflow_control schema should not expose deprecated team_plan field: %s", schema)
	}
}

func TestWorkflowToolDescriptionsPreferStartWorkflow(t *testing.T) {
	env := &Env{}
	startDesc := NewStartWorkflowTool(env).Definition().Description
	if !strings.Contains(startDesc, "Start a workflow run") ||
		!strings.Contains(startDesc, "driver=auto") ||
		!strings.Contains(startDesc, "script path") ||
		!strings.Contains(startDesc, "agent-managed path") ||
		!strings.Contains(startDesc, "creates workflow evidence state") ||
		!strings.Contains(startDesc, "existing workflow evidence state") {
		t.Fatalf("start_workflow description should present the default workflow path: %q", startDesc)
	}
	runDesc := NewRunWorkflowTool(env).Definition().Description
	if !strings.Contains(runDesc, "Prefer start_workflow driver=auto") ||
		!strings.Contains(runDesc, "explicitly requires a saved WORKFLOW.js definition or ad hoc script") {
		t.Fatalf("run_workflow description should point back to start_workflow: %q", runDesc)
	}
	createDesc := NewCreateWorkflowTool(env).Definition().Description
	if !strings.Contains(createDesc, "Prefer start_workflow driver=auto") ||
		!strings.Contains(createDesc, "explicitly requires an agent-managed run") {
		t.Fatalf("create_workflow description should point back to start_workflow: %q", createDesc)
	}
	for _, bad := range []string{"unified natural-language agent entry point", "lower-level script driver override", "lower-level agent-managed driver override", "natural-language workflow entry point"} {
		if strings.Contains(startDesc, bad) || strings.Contains(runDesc, bad) || strings.Contains(createDesc, bad) {
			t.Fatalf("workflow descriptions should avoid awkward wording %q\nstart=%q\nrun=%q\ncreate=%q", bad, startDesc, runDesc, createDesc)
		}
	}
	statusDesc := NewWorkflowStatusTool(env).Definition().Description
	if !strings.Contains(statusDesc, "after start_workflow") || strings.Contains(statusDesc, "after create_workflow") {
		t.Fatalf("workflow_status description should reference start_workflow: %q", statusDesc)
	}
}

func TestWorkflowControlRejectsMissingReuseProfile(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("WUU_HOME", t.TempDir())
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
	kit.SetStateDir(stateDir)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "create_workflow",
		Arguments: `{"run_id":"workflow-missing-reuse","plan":"## Phases\n\n1. QA\n"}`,
	}); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "workflow_control",
		Arguments: `{
			"action":"record_team_plan",
			"run_id":"workflow-missing-reuse",
			"team_plan":[{"role":"QA reviewer","mode":"reuse_profile","agent_profile":"missing_qa"}]
		}`,
	}); err == nil {
		t.Fatal("expected missing reuse profile to fail")
	}
}

func TestWorkflowControlEnforcesMaxAgents(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loadWorkflowDriverToolsForTest(t, kit)
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
	loadWorkflowDriverToolsForTest(t, kit)
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
	loadWorkflowDriverToolsForTest(t, kit)
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

func workflowEventsContain(events []workflow.Event, eventType workflow.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func workflowStepsContain(steps []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, step := range steps {
		if strings.Contains(strings.ToLower(step), needle) {
			return true
		}
	}
	return false
}

func goalStateHasArtifact(state goalrunner.State, source, sourceID, kind, path string) bool {
	for _, artifact := range state.Artifacts {
		if artifact.Source == source &&
			artifact.SourceID == sourceID &&
			artifact.Kind == kind &&
			artifact.Path == path {
			return true
		}
	}
	return false
}

func loadWorkflowDriverToolsForTest(t *testing.T, kit *Toolkit) {
	t.Helper()
	kit.markDeferredToolsLoaded("create_workflow", "run_workflow")
}

type workflowFakeClient struct {
	content string
}

func (f *workflowFakeClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{Content: f.content}, nil
}

func (f *workflowFakeClient) StreamChat(context.Context, providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent, 2)
	if f.content != "" {
		ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: f.content}
	}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

type workflowNoopExecutor struct{}

func (workflowNoopExecutor) Definitions() []providers.ToolDefinition { return nil }

func (workflowNoopExecutor) Execute(context.Context, providers.ToolCall) (string, error) {
	return "", nil
}

func stopWorkflowAgentControl(control *agentcontrol.AgentControl) {
	if control == nil {
		return
	}
	control.StopAll()
	time.Sleep(100 * time.Millisecond)
}
