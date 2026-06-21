package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
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
