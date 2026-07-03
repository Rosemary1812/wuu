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
