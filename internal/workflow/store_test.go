package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateTransitions(t *testing.T) {
	if err := ValidateRunTransition(RunStateDraft, RunStateRunning); err != nil {
		t.Fatalf("draft -> running should be valid: %v", err)
	}
	if err := ValidateRunTransition(RunStateCompleted, RunStateRunning); err == nil {
		t.Fatal("completed -> running should be invalid")
	}
	if err := ValidateRunTransition("", RunState("missing")); err == nil {
		t.Fatal("unknown initial run state should be invalid")
	}
	if err := ValidatePhaseTransition(PhaseStateBlocked, PhaseStateRunning); err != nil {
		t.Fatalf("blocked -> running should be valid: %v", err)
	}
	if err := ValidatePhaseTransition(PhaseStateCompleted, PhaseStateRunning); err == nil {
		t.Fatal("completed phase -> running should be invalid")
	}
	if err := ValidatePhaseTransition("", PhaseState("missing")); err == nil {
		t.Fatal("unknown initial phase state should be invalid")
	}
	if err := ValidateAgentRunTransition(AgentRunStateAwaitingReport, AgentRunStateCompleted); err != nil {
		t.Fatalf("awaiting_report -> completed should be valid: %v", err)
	}
	if err := ValidateAgentRunTransition(AgentRunStateFailed, AgentRunStateRunning); err == nil {
		t.Fatal("failed agent -> running should be invalid")
	}
	if err := ValidateAgentRunTransition("", AgentRunState("missing")); err == nil {
		t.Fatal("unknown initial agent run state should be invalid")
	}
}

func TestStoreCreatesAndUpdatesWorkflowRun(t *testing.T) {
	store := NewStore(t.TempDir())
	run, err := store.CreateRun(Run{
		ID:             "run_1",
		DefinitionName: "feature-delivery",
		Arguments:      "build settings search",
		Phases: []Phase{
			{ID: "plan", Name: "Plan", Status: PhaseStatePending},
			{ID: "qa", Name: "QA", Status: PhaseStatePending},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.Status != RunStateDraft {
		t.Fatalf("initial status = %q", run.Status)
	}

	loaded, err := store.LoadRun("run_1")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if loaded.DefinitionName != "feature-delivery" || len(loaded.Phases) != 2 {
		t.Fatalf("loaded run mismatch: %+v", loaded)
	}

	running, err := store.UpdateRunStatus("run_1", RunStateRunning, "")
	if err != nil {
		t.Fatalf("UpdateRunStatus running: %v", err)
	}
	if running.StartedAt.IsZero() {
		t.Fatal("running transition should set StartedAt")
	}

	if _, err := store.UpdatePhaseStatus("run_1", "plan", PhaseStateRunnable, ""); err != nil {
		t.Fatalf("UpdatePhaseStatus runnable: %v", err)
	}
	if _, err := store.UpdatePhaseStatus("run_1", "plan", PhaseStateRunning, ""); err != nil {
		t.Fatalf("UpdatePhaseStatus running: %v", err)
	}
	updated, err := store.UpdatePhaseStatus("run_1", "plan", PhaseStateCompleted, "")
	if err != nil {
		t.Fatalf("UpdatePhaseStatus completed: %v", err)
	}
	if updated.Phases[0].CompletedAt.IsZero() {
		t.Fatal("completed phase should set CompletedAt")
	}

	if err := store.UpsertAgentRun(AgentRun{
		ID:            "agent_run_1",
		WorkflowRunID: "run_1",
		PhaseID:       "qa",
		TaskName:      "qa_check",
		AgentProfile:  "qa_reviewer",
		Prompt:        "verify the feature",
	}); err != nil {
		t.Fatalf("UpsertAgentRun: %v", err)
	}
	if _, err := store.UpdateAgentRunStatus("run_1", "agent_run_1", AgentRunStateStarting, ""); err != nil {
		t.Fatalf("UpdateAgentRunStatus starting: %v", err)
	}
	if _, err := store.UpdateAgentRunStatus("run_1", "agent_run_1", AgentRunStateRunning, ""); err != nil {
		t.Fatalf("UpdateAgentRunStatus running: %v", err)
	}
	awaiting, err := store.UpdateAgentRunStatus("run_1", "agent_run_1", AgentRunStateAwaitingReport, "missing structured handoff")
	if err != nil {
		t.Fatalf("UpdateAgentRunStatus awaiting_report: %v", err)
	}
	if awaiting.Status != AgentRunStateAwaitingReport {
		t.Fatalf("agent status = %q", awaiting.Status)
	}
	completedAgent, err := store.UpdateAgentRunStatus("run_1", "agent_run_1", AgentRunStateCompleted, "")
	if err != nil {
		t.Fatalf("UpdateAgentRunStatus completed: %v", err)
	}
	if completedAgent.CompletedAt.IsZero() {
		t.Fatal("completed agent should set CompletedAt")
	}

	planPath, err := store.WritePlan("run_1", "plan body")
	if err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("plan file not written: %v", err)
	}
	reportPath, err := store.WriteFinalReport("run_1", "final report")
	if err != nil {
		t.Fatalf("WriteFinalReport: %v", err)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("final report not written: %v", err)
	}

	completedRun, err := store.UpdateRunStatus("run_1", RunStateCompleted, "")
	if err != nil {
		t.Fatalf("UpdateRunStatus completed: %v", err)
	}
	if completedRun.CompletedAt.IsZero() {
		t.Fatal("completed run should set CompletedAt")
	}
	if _, err := store.UpdateRunStatus("run_1", RunStateRunning, "resume"); err == nil {
		t.Fatal("completed run should not resume")
	}

	events, err := store.ListEvents("run_1")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) < 8 {
		t.Fatalf("expected lifecycle events, got %d: %+v", len(events), events)
	}
	if events[0].Type != EventRunCreated || events[0].RunID != "run_1" {
		t.Fatalf("first event mismatch: %+v", events[0])
	}
}

func TestStoreListRunsSortsByCreatedAt(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.CreateRun(Run{
		ID:        "later",
		Status:    RunStateDraft,
		CreatedAt: time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateRun later: %v", err)
	}
	if _, err := store.CreateRun(Run{
		ID:        "earlier",
		Status:    RunStateDraft,
		CreatedAt: time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateRun earlier: %v", err)
	}

	runs, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].ID != "earlier" || runs[1].ID != "later" {
		t.Fatalf("runs not sorted by CreatedAt: %+v", runs)
	}
}

func TestStoreRejectsUnsafeAndDuplicateIDs(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.CreateRun(Run{ID: "../escape"}); err == nil {
		t.Fatal("expected unsafe run id to be rejected")
	}
	if _, err := store.CreateRun(Run{ID: "safe-run"}); err != nil {
		t.Fatalf("CreateRun safe-run: %v", err)
	}
	if _, err := store.CreateRun(Run{ID: "safe-run"}); err == nil {
		t.Fatal("expected duplicate run id to be rejected")
	}
	if err := store.UpsertAgentRun(AgentRun{ID: "../agent", WorkflowRunID: "safe-run"}); err == nil {
		t.Fatal("expected unsafe agent run id to be rejected")
	}
	if _, err := store.ListAgentRuns("../escape"); err == nil {
		t.Fatal("expected unsafe list run id to be rejected")
	}
}

func TestStoreAgentRunUpsertMergesAndValidatesTransitions(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.CreateRun(Run{
		ID:     "run-agent",
		Phases: []Phase{{ID: "plan", Name: "Plan", Status: PhaseStatePending}},
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.UpsertAgentRun(AgentRun{
		ID:            "agent-1",
		WorkflowRunID: "run-agent",
		PhaseID:       "plan",
		AgentID:       "agent-1",
		TaskName:      "inspect",
		Status:        AgentRunStateRunning,
		Prompt:        "inspect the code",
	}); err != nil {
		t.Fatalf("UpsertAgentRun initial: %v", err)
	}
	if err := store.UpsertAgentRun(AgentRun{
		ID:            "agent-1",
		WorkflowRunID: "run-agent",
		Status:        AgentRunStateAwaitingReport,
		ReportPath:    "reports/agent-1.md",
		ChangedFiles:  []string{"internal/workflow/store.go"},
	}); err != nil {
		t.Fatalf("UpsertAgentRun update: %v", err)
	}
	agent, err := store.LoadAgentRun("run-agent", "agent-1")
	if err != nil {
		t.Fatalf("LoadAgentRun: %v", err)
	}
	if agent.PhaseID != "plan" || agent.Prompt != "inspect the code" || len(agent.ChangedFiles) != 1 {
		t.Fatalf("agent fields were not merged: %+v", agent)
	}
	if err := store.UpsertAgentRun(AgentRun{
		ID:            "agent-1",
		WorkflowRunID: "run-agent",
		Status:        AgentRunStateRunning,
	}); err == nil {
		t.Fatal("expected invalid agent run transition to be rejected")
	}

	updated, err := store.AttachAgentRunToPhase("run-agent", "plan", "agent-1")
	if err != nil {
		t.Fatalf("AttachAgentRunToPhase: %v", err)
	}
	if len(updated.Phases[0].AgentRunIDs) != 1 || updated.Phases[0].AgentRunIDs[0] != "agent-1" {
		t.Fatalf("agent run was not attached to phase: %+v", updated.Phases[0])
	}
}

func TestStoreUsesExpectedLayout(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	if _, err := store.CreateRun(Run{ID: "layout"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.UpsertAgentRun(AgentRun{ID: "agent_1", WorkflowRunID: "layout"}); err != nil {
		t.Fatalf("UpsertAgentRun: %v", err)
	}
	agents, err := store.ListAgentRuns("layout")
	if err != nil {
		t.Fatalf("ListAgentRuns: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "agent_1" {
		t.Fatalf("unexpected agent runs: %+v", agents)
	}

	for _, path := range []string{
		filepath.Join(root, "workflows", "layout", "run.json"),
		filepath.Join(root, "workflows", "layout", "events.jsonl"),
		filepath.Join(root, "workflows", "layout", "agents", "agent_1.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}
