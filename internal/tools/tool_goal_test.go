package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestGoalToolsLifecycle(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}

	startedRaw, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Ship goal tools","task":"wire lifecycle","goal_id":"goal-tools","next_steps":["start workflow"]}`)
	if err != nil {
		t.Fatalf("start_goal: %v", err)
	}
	var started struct {
		GoalID  string `json:"goal_id"`
		GoalDir string `json:"goal_dir"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(startedRaw), &started); err != nil {
		t.Fatalf("parse start_goal: %v\n%s", err, startedRaw)
	}
	if started.GoalID != "goal-tools" || started.Status != string(goalrunner.StatusRunning) || started.GoalDir == "" {
		t.Fatalf("unexpected start_goal result: %+v", started)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("start_goal should not create project .goal directory: %v", err)
	}
	if _, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Duplicate","goal_id":"goal-tools"}`); err == nil {
		t.Fatal("start_goal should reject an existing goal_id")
	}

	updateRaw, err := NewUpdateGoalTool(env).Execute(context.Background(), `{"goal_id":"goal-tools","kind":"decision","message":"Use dedicated goal tools","reason":"Goal is broader than one workflow","step":"plan","next_steps":["bind workflow"]}`)
	if err != nil {
		t.Fatalf("update_goal: %v", err)
	}
	var updated struct {
		GoalID string `json:"goal_id"`
		Step   string `json:"step"`
	}
	if err := json.Unmarshal([]byte(updateRaw), &updated); err != nil {
		t.Fatalf("parse update_goal: %v\n%s", err, updateRaw)
	}
	if updated.GoalID != "goal-tools" || updated.Step != string(goalrunner.StepPlan) {
		t.Fatalf("unexpected update_goal result: %+v", updated)
	}

	completeRaw, err := NewCompleteGoalTool(env).Execute(context.Background(), `{"goal_id":"goal-tools","summary":"Goal tools are wired and tested.","final_artifact":"reports/goal-tools.md"}`)
	if err != nil {
		t.Fatalf("complete_goal: %v", err)
	}
	var completed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(completeRaw), &completed); err != nil {
		t.Fatalf("parse complete_goal: %v\n%s", err, completeRaw)
	}
	if completed.Status != string(goalrunner.StatusCompleted) {
		t.Fatalf("complete_goal status = %q", completed.Status)
	}

	statusRaw, err := NewGoalStatusTool(env).Execute(context.Background(), `{"goal_id":"goal-tools"}`)
	if err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	var status struct {
		Goal struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			FinalArtifact string `json:"final_artifact"`
		} `json:"goal"`
		EventCount int `json:"event_count"`
	}
	if err := json.Unmarshal([]byte(statusRaw), &status); err != nil {
		t.Fatalf("parse goal_status: %v\n%s", err, statusRaw)
	}
	if status.Goal.ID != "goal-tools" || status.Goal.Status != string(goalrunner.StatusCompleted) || status.Goal.FinalArtifact != "reports/goal-tools.md" || status.EventCount == 0 {
		t.Fatalf("unexpected goal_status result: %+v", status)
	}
}

func TestGoalToolDescriptionsDefineDurableBoundary(t *testing.T) {
	desc := NewStartGoalTool(&Env{}).Definition().Description
	for _, want := range []string{
		"durable user-visible Goal",
		"multiple workflow runs",
		"Do not call this for tiny one-shot edits",
		"one self-contained workflow run",
		"start_workflow creates a Goal binding",
		"pass the returned goal_id and goal_dir",
		"complete_goal only when the user-visible outcome is done",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("start_goal description missing %q: %q", want, desc)
		}
	}

	completeDesc := NewCompleteGoalTool(&Env{}).Definition().Description
	for _, want := range []string{
		"independent workflow, subagent, or reviewer evidence",
		"do not self-certify",
	} {
		if !strings.Contains(completeDesc, want) {
			t.Fatalf("complete_goal description missing %q: %q", want, completeDesc)
		}
	}
}

func TestStartWorkflowBindsExistingGoal(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}

	startedRaw, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Coordinate workflow","goal_id":"goal-workflow"}`)
	if err != nil {
		t.Fatalf("start_goal: %v", err)
	}
	var started struct {
		GoalID  string `json:"goal_id"`
		GoalDir string `json:"goal_dir"`
	}
	if err := json.Unmarshal([]byte(startedRaw), &started); err != nil {
		t.Fatalf("parse start_goal: %v\n%s", err, startedRaw)
	}

	workflowArgs, err := mustJSON(map[string]any{
		"driver":   "agent_managed",
		"plan":     "Implement the goal tooling.",
		"goal_id":  started.GoalID,
		"goal_dir": started.GoalDir,
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
	if workflowResult.RunID == "" || workflowResult.GoalID != started.GoalID || workflowResult.GoalDir != started.GoalDir {
		t.Fatalf("workflow did not bind existing goal: %+v start=%+v", workflowResult, started)
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

	startedRaw, err := NewStartGoalTool(env).Execute(context.Background(), `{"goal":"Coordinate multiple workflows","goal_id":"broader-goal"}`)
	if err != nil {
		t.Fatalf("start_goal: %v", err)
	}
	var started struct {
		GoalID  string `json:"goal_id"`
		GoalDir string `json:"goal_dir"`
	}
	if err := json.Unmarshal([]byte(startedRaw), &started); err != nil {
		t.Fatalf("parse start_goal: %v\n%s", err, startedRaw)
	}

	workflowRaw, err := NewStartWorkflowTool(env).Execute(context.Background(), `{
		"driver":"agent_managed",
		"run_id":"child-workflow",
		"goal_id":"broader-goal",
		"goal_dir":`+quoteGoalToolJSON(started.GoalDir)+`,
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
	if workflowResult.GoalID != started.GoalID {
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
	if final.GoalStatus.GoalID != started.GoalID || final.GoalStatus.Status == goalrunner.StatusCompleted {
		t.Fatalf("broader goal should not be auto-completed by child workflow: %+v", final.GoalStatus)
	}
	state, err := goalrunner.NewStore(started.GoalDir).LoadState()
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

func goalProgressContains(state goalrunner.State, source, sourceID string) bool {
	for _, entry := range state.Progress {
		if entry.Source == source && entry.SourceID == sourceID {
			return true
		}
	}
	return false
}
