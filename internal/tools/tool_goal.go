package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

type CreateGoalTool struct{ env *Env }

func NewCreateGoalTool(env *Env) *CreateGoalTool { return &CreateGoalTool{env: env} }

func (t *CreateGoalTool) Name() string            { return "create_goal" }
func (t *CreateGoalTool) IsReadOnly() bool        { return false }
func (t *CreateGoalTool) IsConcurrencySafe() bool { return false }

func (t *CreateGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "create_goal",
		Description: "Create a goal only when explicitly requested by the user or system/developer instructions; do not infer goals from ordinary tasks. " +
			"This starts a new active goal when no unfinished goal exists. Fails if an unfinished goal exists; use update_goal only for status.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"objective": map[string]any{
					"type":        "string",
					"description": "Required. The concrete objective to start pursuing. State the outcome, not implementation details.",
				},
			},
			"required": []string{"objective"},
		},
	}
}

func (t *CreateGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Objective string `json:"objective"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	objective := strings.TrimSpace(args.Objective)
	if objective == "" {
		return "", errors.New("create_goal requires objective")
	}
	goalID := "goal-" + session.NewID()
	if err := validateGoalToolID(goalID); err != nil {
		return "", err
	}
	runtimeGoal, err := t.startRuntimeGoal(goalID, objective)
	if err != nil {
		return "", err
	}
	return goalRuntimeToolResult(runtimeGoal)
}

type UpdateGoalTool struct{ env *Env }

func NewUpdateGoalTool(env *Env) *UpdateGoalTool { return &UpdateGoalTool{env: env} }

func (t *UpdateGoalTool) Name() string            { return "update_goal" }
func (t *UpdateGoalTool) IsReadOnly() bool        { return false }
func (t *UpdateGoalTool) IsConcurrencySafe() bool { return false }

func (t *UpdateGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "update_goal",
		Description: "Update the existing goal. Use this tool only to mark the goal achieved or genuinely blocked. " +
			"Set status=complete only when the objective has actually been achieved and no required work remains. " +
			"Set status=blocked only when the same blocking condition has repeated for at least three consecutive goal turns and the agent cannot make meaningful progress without user input or an external-state change. " +
			"Do not use blocked merely because the work is hard, slow, uncertain, incomplete, or would benefit from clarification. " +
			"You cannot use this tool to pause, resume, or usage-limit a goal; those status changes are controlled by the user or system.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"complete", "blocked"},
					"description": "Required. Set to complete only when the objective is achieved; set to blocked only after the repeated-blocker audit is satisfied.",
				},
			},
			"required": []string{"status"},
		},
	}
}

func (t *UpdateGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Status string `json:"status"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	status := goalruntime.Status(strings.TrimSpace(args.Status))
	switch status {
	case goalruntime.StatusComplete, goalruntime.StatusBlocked:
	default:
		return "", errors.New("update_goal requires status=complete or status=blocked")
	}
	if _, err := currentActiveRuntimeGoalForTool(t.env); err != nil {
		return "", err
	}
	runtimeGoal, err := t.env.GoalRuntime.SetModelStatus(status, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return goalRuntimeToolResult(runtimeGoal)
}

type GetGoalTool struct{ env *Env }

func NewGetGoalTool(env *Env) *GetGoalTool { return &GetGoalTool{env: env} }

func (t *GetGoalTool) Name() string            { return "get_goal" }
func (t *GetGoalTool) IsReadOnly() bool        { return true }
func (t *GetGoalTool) IsConcurrencySafe() bool { return true }

func (t *GetGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "get_goal",
		Description: "Get the current goal for this thread, including status and elapsed usage.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *GetGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct{}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	result := map[string]any{
		"goal": nil,
	}
	if runtimeGoal, ok, err := currentGoalRuntime(t.env); err != nil {
		return "", err
	} else if ok {
		result["goal"] = runtimeGoal
	}
	return mustJSON(result)
}

func (e *Env) GoalStore(goalID string) (*goalrunner.Store, error) {
	goalID = strings.TrimSpace(goalID)
	if err := validateGoalToolID(goalID); err != nil {
		return nil, err
	}
	stateDir, err := e.OrchestrationStateDir()
	if err != nil {
		return nil, err
	}
	return goalrunner.NewStore(statepath.GoalDir(stateDir, goalID)), nil
}

func (e *Env) ResolveGoalStore(goalID, goalDir string) (*goalrunner.Store, goalrunner.State, error) {
	goalID = strings.TrimSpace(goalID)
	goalDir = strings.TrimSpace(goalDir)
	if goalID == "" && goalDir == "" {
		return nil, goalrunner.State{}, errors.New("goal_id or goal_dir is required")
	}
	var store *goalrunner.Store
	var err error
	if goalDir != "" {
		store = goalrunner.NewStore(goalDir)
	} else {
		store, err = e.GoalStore(goalID)
		if err != nil {
			return nil, goalrunner.State{}, err
		}
	}
	state, err := store.LoadState()
	if err != nil {
		return nil, goalrunner.State{}, err
	}
	if goalID != "" && state.ID != goalID {
		return nil, goalrunner.State{}, fmt.Errorf("goal_id %q does not match state id %q", goalID, state.ID)
	}
	return store, state, nil
}

func (t *CreateGoalTool) startRuntimeGoal(goalID, objective string) (goalruntime.Goal, error) {
	if t == nil || t.env == nil || t.env.GoalRuntime == nil {
		return goalruntime.Goal{}, errors.New("create_goal requires a thread runtime goal store")
	}
	threadID := strings.TrimSpace(t.env.SessionID)
	if threadID == "" {
		return goalruntime.Goal{}, errors.New("thread session_id is required for runtime goal")
	}
	goal, err := t.env.GoalRuntime.Create(goalruntime.Spec{
		ThreadID:  threadID,
		GoalID:    goalID,
		Objective: objective,
	})
	if err != nil {
		return goalruntime.Goal{}, err
	}
	return goal, nil
}

func currentActiveRuntimeGoalForTool(env *Env) (goalruntime.Goal, error) {
	goal, ok, err := currentGoalRuntime(env)
	if err != nil {
		return goalruntime.Goal{}, err
	}
	if !ok {
		return goalruntime.Goal{}, errors.New("active runtime goal not found")
	}
	if goalruntime.IsTerminalStatus(goal.Status) {
		return goalruntime.Goal{}, fmt.Errorf("active runtime goal %q is already %s", goal.GoalID, goal.Status)
	}
	return goal, nil
}

func currentGoalRuntime(env *Env) (goalruntime.Goal, bool, error) {
	if env == nil || env.GoalRuntime == nil {
		return goalruntime.Goal{}, false, nil
	}
	goal, err := env.GoalRuntime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return goalruntime.Goal{}, false, nil
	}
	if err != nil {
		return goalruntime.Goal{}, false, err
	}
	return goal, true, nil
}

func validateGoalToolID(goalID string) error {
	if strings.TrimSpace(goalID) == "" {
		return errors.New("goal_id is required")
	}
	if goalID == "." || goalID == ".." || filepath.Base(goalID) != goalID || strings.ContainsAny(goalID, `/\`) {
		return fmt.Errorf("goal_id must be an id, not a path: %q", goalID)
	}
	return nil
}

func goalRuntimeToolResult(goal goalruntime.Goal) (string, error) {
	result := map[string]any{
		"goal": goal,
	}
	return mustJSON(result)
}
