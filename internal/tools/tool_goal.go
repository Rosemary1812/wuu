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

// GoalTool is the single read/write surface for the thread's one runtime goal.
// A thread has at most one active goal at a time (goalruntime rejects a second
// unfinished goal), so the domain is a single instance addressed by action
// rather than a list of goals. action=get reads the current goal, action=create
// starts a new one, action=update changes only its status.
type GoalTool struct{ env *Env }

func NewGoalTool(env *Env) *GoalTool { return &GoalTool{env: env} }

func (t *GoalTool) Name() string            { return "goal" }
func (t *GoalTool) IsReadOnly() bool        { return false }
func (t *GoalTool) IsConcurrencySafe() bool { return false }

func (t *GoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "goal",
		Description: "Read or change the single runtime goal for this thread. " +
			"action=get returns the current goal with its status and elapsed usage. " +
			"action=create starts a new active goal and requires objective; create a goal only when explicitly requested by the user or system/developer instructions, do not infer goals from ordinary tasks, and note that it fails if an unfinished goal exists. " +
			"action=update changes only the goal status and requires status. " +
			"Set status=complete only when the objective has actually been achieved and no required work remains. " +
			"Set status=blocked only when the same blocking condition has repeated for at least three consecutive goal turns and the agent cannot make meaningful progress without user input or an external-state change. " +
			"Do not use blocked merely because the work is hard, slow, uncertain, incomplete, or would benefit from clarification. " +
			"You cannot pause or resume a goal; those status changes are controlled by the user or system.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"get", "create", "update"},
					"description": "Required. get reads the current goal; create starts a new goal (requires objective); update changes the goal status (requires status).",
				},
				"objective": map[string]any{
					"type":        "string",
					"description": "Used by action=create. The concrete objective to start pursuing. State the outcome, not implementation details.",
				},
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"complete", "blocked"},
					"description": "Used by action=update. Set to complete only when the objective is achieved; set to blocked only after the repeated-blocker audit is satisfied.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *GoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Action    string `json:"action"`
		Objective string `json:"objective"`
		Status    string `json:"status"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "get":
		return t.executeGet()
	case "create":
		return t.executeCreate(args.Objective)
	case "update":
		return t.executeUpdate(args.Status)
	default:
		return "", errors.New("goal requires action=get, create, or update")
	}
}

func (t *GoalTool) executeGet() (string, error) {
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

func (t *GoalTool) executeCreate(objective string) (string, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return "", errors.New("goal action=create requires objective")
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

func (t *GoalTool) executeUpdate(statusArg string) (string, error) {
	status := goalruntime.Status(strings.TrimSpace(statusArg))
	switch status {
	case goalruntime.StatusComplete, goalruntime.StatusBlocked:
	default:
		return "", errors.New("goal action=update requires status=complete or status=blocked")
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

func (t *GoalTool) startRuntimeGoal(goalID, objective string) (goalruntime.Goal, error) {
	if t == nil || t.env == nil || t.env.GoalRuntime == nil {
		return goalruntime.Goal{}, errors.New("goal action=create requires a thread runtime goal store")
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
	if goal.Status != goalruntime.StatusActive {
		return goalruntime.Goal{}, fmt.Errorf("active runtime goal %q is currently %s; ask the user to resume the goal before updating it", goal.GoalID, goal.Status)
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
