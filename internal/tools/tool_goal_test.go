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

	createdRaw, err := NewCreateGoalTool(env).Execute(context.Background(), `{"objective":"Ship goal tools"}`)
	if err != nil {
		t.Fatalf("create_goal: %v", err)
	}
	var created struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(createdRaw), &created); err != nil {
		t.Fatalf("parse create_goal: %v\n%s", err, createdRaw)
	}
	if !strings.HasPrefix(created.Goal.GoalID, "goal-") ||
		created.Goal.Objective != "Ship goal tools" ||
		created.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected create_goal result: %+v", created)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("create_goal should not create project .goal directory: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(env.StateDir, "goals")); err == nil && len(entries) > 0 {
		t.Fatalf("create_goal must not create legacy goal ledger entries: %+v", entries)
	}
	if _, err := NewCreateGoalTool(env).Execute(context.Background(), `{"objective":"Duplicate"}`); err == nil {
		t.Fatal("create_goal should reject an unfinished runtime goal")
	}

	if _, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"status":"active"}`); err == nil {
		t.Fatal("update_goal should reject unsupported statuses")
	}

	statusRaw, err := NewGetGoalTool(env).Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("get_goal: %v", err)
	}
	var status struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(statusRaw), &status); err != nil {
		t.Fatalf("parse get_goal: %v\n%s", err, statusRaw)
	}
	if status.Goal.GoalID != created.Goal.GoalID || status.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected get_goal result: %+v", status)
	}

	completeRaw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"status":"complete"}`)
	if err != nil {
		t.Fatalf("update_goal: %v", err)
	}
	var completed struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(completeRaw), &completed); err != nil {
		t.Fatalf("parse complete update_goal: %v\n%s", err, completeRaw)
	}
	if completed.Goal.GoalID != created.Goal.GoalID || completed.Goal.Status != goalruntime.StatusComplete {
		t.Fatalf("unexpected complete result: %+v", completed)
	}

	replacementRaw, err := NewCreateGoalTool(env).Execute(context.Background(), `{"objective":"Handle a blocker"}`)
	if err != nil {
		t.Fatalf("create replacement goal: %v", err)
	}
	var replacement struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(replacementRaw), &replacement); err != nil {
		t.Fatalf("parse replacement create_goal: %v\n%s", err, replacementRaw)
	}
	if replacement.Goal.GoalID == created.Goal.GoalID || replacement.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected replacement goal: %+v", replacement)
	}

	blockRaw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"status":"blocked"}`)
	if err != nil {
		t.Fatalf("blocked update_goal: %v", err)
	}
	var blocked struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(blockRaw), &blocked); err != nil {
		t.Fatalf("parse blocked update_goal: %v\n%s", err, blockRaw)
	}
	if blocked.Goal.GoalID != replacement.Goal.GoalID || blocked.Goal.Status != goalruntime.StatusBlocked {
		t.Fatalf("unexpected blocked result: %+v", blocked)
	}
}

func TestCreateGoalRequiresRuntime(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}
	if _, err := NewCreateGoalTool(env).Execute(context.Background(), `{"objective":"No runtime"}`); err == nil {
		t.Fatal("create_goal should require GoalRuntime")
	}
}

func TestGetGoalWithoutRuntimeGoal(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	raw, err := NewGetGoalTool(env).Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("get_goal: %v", err)
	}
	var status struct {
		Goal *goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("parse get_goal: %v\n%s", err, raw)
	}
	if status.Goal != nil {
		t.Fatalf("expected nil goal, got %+v", status.Goal)
	}
}

func TestUpdateGoalBlocksDirectly(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-blocked",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	if _, err := NewCreateGoalTool(env).Execute(context.Background(), `{"objective":"Handle repeated blocker"}`); err != nil {
		t.Fatalf("create_goal: %v", err)
	}

	var updated struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	raw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"status":"blocked"}`)
	if err != nil {
		t.Fatalf("update_goal blocker: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &updated); err != nil {
		t.Fatalf("parse update_goal: %v\n%s", err, raw)
	}
	if updated.Goal.Status != goalruntime.StatusBlocked {
		t.Fatalf("runtime goal should block directly: %+v", updated.Goal)
	}
}

func TestGoalToolDescriptionsDefineDurableBoundary(t *testing.T) {
	createDef := NewCreateGoalTool(&Env{}).Definition()
	desc := createDef.Description
	for _, want := range []string{
		"explicitly requested",
		"unfinished goal exists",
		"use update_goal only for status",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("create_goal description missing %q: %q", want, desc)
		}
	}
	assertToolSchemaOmits(t, createDef, "goal", "goal_id", "task", "trigger_type", "trigger_source", "next_steps", "goal_dir", "token_budget")

	updateDef := NewUpdateGoalTool(&Env{}).Definition()
	updateDesc := updateDef.Description
	for _, want := range []string{
		"status=complete",
		"status=blocked",
		"pause or resume",
	} {
		if !strings.Contains(updateDesc, want) {
			t.Fatalf("update_goal description missing %q: %q", want, updateDesc)
		}
	}
	assertToolSchemaOmits(t, updateDef, "goal_id", "kind", "message", "summary", "final_artifact", "goal_dir", "step", "reason", "next_steps")

	getDef := NewGetGoalTool(&Env{}).Definition()
	assertToolSchemaOmits(t, getDef, "goal_id", "goal_dir")
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
