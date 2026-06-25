package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestGoalToolsLifecycle(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-tools",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}

	startedRaw, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Ship goal tools","goal_id":"goal-tools"}`)
	if err != nil {
		t.Fatalf("start_goal: %v", err)
	}
	var started struct {
		GoalID      string           `json:"goal_id"`
		Status      string           `json:"status"`
		RuntimeGoal goalruntime.Goal `json:"runtime_goal"`
	}
	if err := json.Unmarshal([]byte(startedRaw), &started); err != nil {
		t.Fatalf("parse start_goal: %v\n%s", err, startedRaw)
	}
	if started.GoalID != "goal-tools" || started.Status != string(goalruntime.StatusActive) {
		t.Fatalf("unexpected start_goal result: %+v", started)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("start_goal should not create project .goal directory: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(env.StateDir, "goals")); err == nil && len(entries) > 0 {
		t.Fatalf("start_goal must not create legacy goal ledger entries: %+v", entries)
	}
	if _, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Duplicate","goal_id":"goal-tools"}`); err == nil {
		t.Fatal("start_goal should reject an unfinished runtime goal")
	}

	if _, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"goal_id":"goal-tools","kind":"decision","message":"old progress path"}`); err == nil {
		t.Fatal("update_goal should reject legacy progress/decision updates")
	}
	updateRaw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"goal_id":"goal-tools","kind":"status","status":"blocked","message":"needs credentials"}`)
	if err != nil {
		t.Fatalf("update_goal: %v", err)
	}
	var updated struct {
		RuntimeGoal goalruntime.Goal `json:"runtime_goal"`
		Blocked     bool             `json:"blocked"`
	}
	if err := json.Unmarshal([]byte(updateRaw), &updated); err != nil {
		t.Fatalf("parse update_goal: %v\n%s", err, updateRaw)
	}
	if updated.RuntimeGoal.BlockerAudit.ConsecutiveTurns != 1 || updated.Blocked {
		t.Fatalf("unexpected update_goal result: %+v", updated)
	}

	completeRaw, err := NewCompleteGoalTool(env).Execute(context.Background(), `{"goal_id":"goal-tools","summary":"Goal tools are wired and tested.","final_artifact":"reports/goal-tools.md"}`)
	if err != nil {
		t.Fatalf("complete_goal: %v", err)
	}
	var completed struct {
		Status        string           `json:"status"`
		RuntimeGoal   goalruntime.Goal `json:"runtime_goal"`
		FinalArtifact string           `json:"final_artifact"`
	}
	if err := json.Unmarshal([]byte(completeRaw), &completed); err != nil {
		t.Fatalf("parse complete_goal: %v\n%s", err, completeRaw)
	}
	if completed.Status != string(goalruntime.StatusComplete) || completed.RuntimeGoal.Status != goalruntime.StatusComplete || completed.FinalArtifact != "reports/goal-tools.md" {
		t.Fatalf("unexpected complete_goal result: %+v", completed)
	}

	statusRaw, err := NewGoalStatusTool(env).Execute(context.Background(), `{"goal_id":"goal-tools"}`)
	if err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	var status struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(statusRaw), &status); err != nil {
		t.Fatalf("parse goal_status: %v\n%s", err, statusRaw)
	}
	if status.Goal.GoalID != "goal-tools" || status.Goal.Status != goalruntime.StatusComplete {
		t.Fatalf("unexpected goal_status result: %+v", status)
	}
}

func TestStartGoalRequiresRuntime(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}
	if _, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"No runtime"}`); err == nil {
		t.Fatal("start_goal should require GoalRuntime")
	}
}

func TestGoalStatusWithoutRuntimeGoal(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	raw, err := NewGoalStatusTool(env).Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	var status struct {
		Goal *goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("parse goal_status: %v\n%s", err, raw)
	}
	if status.Goal != nil {
		t.Fatalf("expected nil goal, got %+v", status.Goal)
	}
}

func TestUpdateGoalRecordsRuntimeBlockerThreshold(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-blocked",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	if _, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Handle repeated blocker","goal_id":"blocked-runtime-goal"}`); err != nil {
		t.Fatalf("start_goal: %v", err)
	}

	var updated struct {
		Status      string           `json:"status"`
		RuntimeGoal goalruntime.Goal `json:"runtime_goal"`
	}
	for i := 0; i < goalruntime.RequiredBlockerTurns; i++ {
		raw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"kind":"status","status":"blocked","message":"needs credentials"}`)
		if err != nil {
			t.Fatalf("update_goal blocker %d: %v", i, err)
		}
		if err := json.Unmarshal([]byte(raw), &updated); err != nil {
			t.Fatalf("parse update_goal %d: %v\n%s", i, err, raw)
		}
	}
	if updated.RuntimeGoal.Status != goalruntime.StatusBlocked ||
		updated.RuntimeGoal.BlockerAudit.ConsecutiveTurns != goalruntime.RequiredBlockerTurns {
		t.Fatalf("runtime goal should block after repeated blocker: %+v", updated.RuntimeGoal)
	}
}

func TestGoalToolDescriptionsDefineDurableBoundary(t *testing.T) {
	startDef := NewStartGoalTool(&Env{}).Definition()
	desc := startDef.Description
	for _, want := range []string{
		"thread's active runtime Goal",
		"GoalRuntime state",
		"auto-continue while active",
		"Workflow and subagent reports are evidence",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("start_goal description missing %q: %q", want, desc)
		}
	}
	assertToolSchemaOmits(t, startDef, "task", "trigger_type", "trigger_source", "next_steps", "goal_dir", "token_budget")

	completeDef := NewCompleteGoalTool(&Env{}).Definition()
	completeDesc := completeDef.Description
	for _, want := range []string{
		"active runtime Goal",
		"workflow/subagent evidence",
		"user-visible goal is closed",
	} {
		if !strings.Contains(completeDesc, want) {
			t.Fatalf("complete_goal description missing %q: %q", want, completeDesc)
		}
	}
	assertToolSchemaOmits(t, completeDef, "goal_dir")

	updateDef := NewUpdateGoalTool(&Env{}).Definition()
	updateDesc := updateDef.Description
	for _, want := range []string{
		"Report a real blocker",
		"repeated-blocker threshold",
		"Do not use this for progress notes",
	} {
		if !strings.Contains(updateDesc, want) {
			t.Fatalf("update_goal description missing %q: %q", want, updateDesc)
		}
	}
	assertToolSchemaOmits(t, updateDef, "goal_dir", "step", "reason", "next_steps")

	statusDef := NewGoalStatusTool(&Env{}).Definition()
	assertToolSchemaOmits(t, statusDef, "goal_dir")
}

func TestStartWorkflowBindsExistingGoal(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}

	goalID, goalDir := createLegacyGoalForWorkflow(t, env, "goal-workflow", "Coordinate workflow")

	workflowArgs, err := mustJSON(map[string]any{
		"driver":   "agent_managed",
		"plan":     "Implement the goal tooling.",
		"goal_id":  goalID,
		"goal_dir": goalDir,
		"phases": []map[string]string{{
			"name": "Implement",
		}},
	})
	if err != nil {
		t.Fatalf("workflow args: %v", err)
	}
	workflowRaw, err := NewStartWorkflowTool(env).Execute(context.Background(), workflowArgs)
	if err != nil {
		t.Fatalf("start_workflow: %v", err)
	}
	var workflowResult struct {
		RunID   string `json:"run_id"`
		GoalID  string `json:"goal_id"`
		GoalDir string `json:"goal_dir"`
	}
	if err := json.Unmarshal([]byte(workflowRaw), &workflowResult); err != nil {
		t.Fatalf("parse start_workflow: %v\n%s", err, workflowRaw)
	}
	if workflowResult.RunID == "" || workflowResult.GoalID != goalID || workflowResult.GoalDir != goalDir {
		t.Fatalf("workflow did not bind existing goal: %+v goal_id=%q goal_dir=%q", workflowResult, goalID, goalDir)
	}

	entries, err := os.ReadDir(filepath.Join(env.StateDir, "goals"))
	if err != nil {
		t.Fatalf("read goals dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "goal-workflow" {
		t.Fatalf("workflow should not create a second goal, got %+v", entries)
	}
}

func TestWorkflowCompletionDoesNotCompleteBroaderGoal(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}

	goalID, goalDir := createLegacyGoalForWorkflow(t, env, "broader-goal", "Coordinate multiple workflows")

	workflowRaw, err := NewStartWorkflowTool(env).Execute(context.Background(), `{
		"driver":"agent_managed",
		"run_id":"child-workflow",
		"goal_id":"broader-goal",
		"goal_dir":`+quoteGoalToolJSON(goalDir)+`,
		"plan":"Run one slice of the broader goal.",
		"phases":[{"id":"slice","name":"Slice"}]
	}`)
	if err != nil {
		t.Fatalf("start_workflow: %v", err)
	}
	var workflowResult struct {
		GoalID string `json:"goal_id"`
	}
	if err := json.Unmarshal([]byte(workflowRaw), &workflowResult); err != nil {
		t.Fatalf("parse start_workflow: %v\n%s", err, workflowRaw)
	}
	if workflowResult.GoalID != goalID {
		t.Fatalf("workflow should bind broader goal: %+v", workflowResult)
	}
	if _, err := NewWorkflowControlTool(env).Execute(context.Background(), `{"action":"set_phase_status","run_id":"child-workflow","phase_id":"slice","status":"completed"}`); err != nil {
		t.Fatalf("complete phase: %v", err)
	}
	finalRaw, err := NewWorkflowControlTool(env).Execute(context.Background(), `{"action":"write_final_report","run_id":"child-workflow","content":"# Final\n\nSlice done.","complete_run":true}`)
	if err != nil {
		t.Fatalf("write final report: %v", err)
	}
	var final struct {
		Run        workflow.Run `json:"run"`
		GoalStatus struct {
			GoalID string            `json:"goal_id"`
			Status goalrunner.Status `json:"status"`
		} `json:"goal_status"`
	}
	if err := json.Unmarshal([]byte(finalRaw), &final); err != nil {
		t.Fatalf("parse final report: %v\n%s", err, finalRaw)
	}
	if final.Run.Status != workflow.RunStateCompleted {
		t.Fatalf("workflow run should complete: %+v", final.Run)
	}
	if final.GoalStatus.GoalID != goalID || final.GoalStatus.Status == goalrunner.StatusCompleted {
		t.Fatalf("broader goal should not be auto-completed by child workflow: %+v", final.GoalStatus)
	}
	state, err := goalrunner.NewStore(goalDir).LoadState()
	if err != nil {
		t.Fatalf("load broader goal: %v", err)
	}
	if state.Status == goalrunner.StatusCompleted {
		t.Fatalf("broader goal was auto-completed: %+v", state)
	}
	if !goalProgressContains(state, "workflow", "child-workflow:completed") {
		t.Fatalf("broader goal should record workflow completion evidence: %+v", state.Progress)
	}
}

func quoteGoalToolJSON(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func createLegacyGoalForWorkflow(t *testing.T, env *Env, goalID, goalText string) (string, string) {
	t.Helper()
	store, err := env.GoalStore(goalID)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	state, err := store.Init(goalrunner.Spec{
		ID:            goalID,
		Goal:          goalText,
		AssignedAgent: "workflow",
	})
	if err != nil {
		t.Fatalf("init goal: %v", err)
	}
	if _, err := store.SetStatus(goalrunner.StatusRunning, goalrunner.StepInit, "Goal started."); err != nil {
		t.Fatalf("mark goal running: %v", err)
	}
	return state.ID, store.Dir()
}

func assertToolSchemaOmits(t *testing.T, def providers.ToolDefinition, names ...string) {
	t.Helper()
	schema := def.InputSchema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s schema properties have unexpected type %T", def.Name, schema["properties"])
	}
	for _, name := range names {
		if _, ok := properties[name]; ok {
			t.Fatalf("%s schema should not expose %q", def.Name, name)
		}
	}
}

func goalProgressContains(state goalrunner.State, source, sourceID string) bool {
	for _, entry := range state.Progress {
		if entry.Source == source && entry.SourceID == sourceID {
			return true
		}
	}
	return false
}
