package agentcontrol

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// TestAwaitAlwaysCarriesResultTextAfterConsumption verifies the delivery
// ledger dedupes injection only: once a worker's result has been claimed, an
// explicit re-await still returns the full result text so a parent that asks
// again is never handed an empty row.
func TestAwaitAlwaysCarriesResultTextAfterConsumption(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "durable worker answer"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-await-carry",
		ThreadDir:     filepath.Join(dir, ".wuu-state", "sessions", "sess-await-carry", "threads"),
		HarnessDir:    filepath.Join(dir, ".wuu-state", "sessions", "sess-await-carry", "harness"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        DefaultSubagentType,
		TaskName:    "carry_result",
		Prompt:      "finish the task",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	first, err := c.AwaitFrom(agentthread.RootPath, context.Background(), []string{res.AgentID})
	if err != nil {
		t.Fatalf("first AwaitFrom: %v", err)
	}
	if len(first.Results) != 1 || strings.TrimSpace(first.Results[0].Result) == "" {
		t.Fatalf("first await should carry the result text, got %+v", first.Results)
	}

	second, err := c.AwaitFrom(agentthread.RootPath, context.Background(), []string{res.AgentID})
	if err != nil {
		t.Fatalf("second AwaitFrom: %v", err)
	}
	if len(second.Results) != 1 {
		t.Fatalf("explicit re-await should still return the task, got %+v", second.Results)
	}
	if strings.TrimSpace(second.Results[0].Result) == "" {
		t.Fatalf("explicit re-await must still carry the result text after the delivery was consumed, got %+v", second.Results[0])
	}
}

// TestAwaitFromRehydratesAcrossRestart simulates a process restart: a first
// AgentControl runs a worker to completion (with a submitted report), and a
// fresh instance pointed at the same state dirs must let await_agents see
// that dormant run — the same lazy rehydration send_message/followup_task
// already get — instead of reporting not_found. Targetless awaits must not
// rehydrate anything.
func TestAwaitFromRehydratesAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	historyDir := filepath.Join(dir, ".wuu-state", "sessions", "sess-await-restart", "workers")
	threadDir := filepath.Join(dir, ".wuu-state", "sessions", "sess-await-restart", "threads")
	harnessDir := filepath.Join(dir, ".wuu-state", "sessions", "sess-await-restart", "harness")
	config := func(client providers.StreamClient) Config {
		return Config{
			Client:        client,
			DefaultModel:  "fake-model",
			ParentRepo:    dir,
			WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
			SessionID:     "sess-await-restart",
			HistoryDir:    historyDir,
			ThreadDir:     threadDir,
			HarnessDir:    harnessDir,
			WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
		}
	}

	first, err := New(config(&fakeClient{resp: providers.ChatResponse{Content: "done before restart"}}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := first.Spawn(context.Background(), SpawnRequest{
		Type:        DefaultSubagentType,
		TaskName:    "await_restart",
		Prompt:      "finish before the restart",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("expected completed first run, got %s", res.Status)
	}
	report, err := first.RecordAgentReport(res.AgentID, res.AgentPath, AgentReportRequest{
		Outcome: "completed",
		Summary: "Finished before the restart.",
	})
	if err != nil {
		t.Fatalf("RecordAgentReport: %v", err)
	}
	waitForHarnessEvent(t, first.HarnessStore(), harness.EventRunCompleted, res.AgentID)
	first.Close()

	// Fresh AgentControl over the same state dirs stands in for a restart.
	second, err := New(config(&fakeClient{resp: providers.ChatResponse{Content: "unused after restart"}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if second.Manager().Get(res.AgentID) != nil {
		t.Fatal("restarted manager should not track the dead run before rehydration")
	}

	// A targetless await never scans history: the dormant run stays dormant.
	blank, err := second.AwaitFrom(agentthread.RootPath, context.Background(), nil)
	if err != nil {
		t.Fatalf("targetless AwaitFrom: %v", err)
	}
	if len(blank.Results) != 0 {
		t.Fatalf("targetless await after restart should see no active children, got %+v", blank.Results)
	}
	if second.Manager().Get(res.AgentID) != nil {
		t.Fatal("targetless await must not rehydrate dormant runs")
	}

	awaited, err := second.AwaitFrom(agentthread.RootPath, context.Background(), []string{res.AgentID})
	if err != nil {
		t.Fatalf("AwaitFrom across restart: %v", err)
	}
	if len(awaited.Results) != 1 {
		t.Fatalf("expected one await result, got %+v", awaited.Results)
	}
	got := awaited.Results[0]
	if got.Status == "not_found" {
		t.Fatalf("explicit await should rehydrate the dormant run, got not_found: %+v", got)
	}
	if got.Status != string(harness.TaskStatusCompleted) {
		t.Fatalf("await status = %q, want %q (result %+v)", got.Status, harness.TaskStatusCompleted, got)
	}
	if got.AgentID != res.AgentID {
		t.Fatalf("await agent id = %q, want %q", got.AgentID, res.AgentID)
	}
	if got.ReportPath != report.ReportPath {
		t.Fatalf("await report path = %q, want %q", got.ReportPath, report.ReportPath)
	}
	if second.Manager().Get(res.AgentID) == nil {
		t.Fatal("explicit await should leave the rehydrated run addressable for follow-ups")
	}
}

func TestAwaitAgentsNextStepsDoNotMentionWorkflowForPlainResults(t *testing.T) {
	result := AwaitAgentsResult{
		Results: []AwaitAgentResult{{Status: string(harness.TaskStatusCompleted)}},
	}
	steps := strings.Join(awaitAgentsNextSteps(result), "\n")
	if strings.Contains(steps, "workflow_control") || strings.Contains(steps, "Workflow Run") {
		t.Fatalf("plain await next steps should not mention workflow binding:\n%s", steps)
	}
	if !strings.Contains(steps, "agent reports") {
		t.Fatalf("plain await next steps should still guide synthesis:\n%s", steps)
	}
}

// TestNoTargetAwaitDoesNotRejoinDeliveredCompletedWithoutReport reproduces the
// polling trap observed with live models: a worker that finishes without a
// structured report is completed with its report missing, and a no-target
// await used to re-join it on every call, returning an already-consumed empty
// row plus guidance the parent can never satisfy.
func TestNoTargetAwaitDoesNotRejoinDeliveredCompletedWithoutReport(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "raw worker answer"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-await-loop",
		ThreadDir:     filepath.Join(dir, ".wuu-state", "sessions", "sess-await-loop", "threads"),
		HarnessDir:    filepath.Join(dir, ".wuu-state", "sessions", "sess-await-loop", "harness"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)

	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:     DefaultSubagentType,
		TaskName: "await_report_loop",
		Prompt:   "finish without report",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	first, err := c.AwaitFrom(agentthread.RootPath, context.Background(), nil)
	if err != nil {
		t.Fatalf("first AwaitFrom: %v", err)
	}
	if len(first.Results) != 1 || first.Results[0].Status != string(harness.TaskStatusCompleted) || !first.Results[0].ReportMissing {
		t.Fatalf("first no-target await should deliver the completed (report-missing) result, got %+v", first)
	}
	if first.Results[0].Result == "" {
		t.Fatalf("first delivery should carry the raw result, got %+v", first.Results[0])
	}

	second, err := c.AwaitFrom(agentthread.RootPath, context.Background(), nil)
	if err != nil {
		t.Fatalf("second AwaitFrom: %v", err)
	}
	if len(second.Results) != 0 {
		t.Fatalf("no-target await must not re-join a terminal task whose result was delivered, got %+v", second.Results)
	}

	explicit, err := c.AwaitFrom(agentthread.RootPath, context.Background(), []string{res.AgentID})
	if err != nil {
		t.Fatalf("explicit AwaitFrom: %v", err)
	}
	if len(explicit.Results) != 1 || explicit.Results[0].Status != string(harness.TaskStatusCompleted) || !explicit.Results[0].ReportMissing {
		t.Fatalf("explicit-target await should still report the completed task, got %+v", explicit)
	}
}
