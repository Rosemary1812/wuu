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

type StartGoalTool struct{ env *Env }

func NewStartGoalTool(env *Env) *StartGoalTool { return &StartGoalTool{env: env} }

func (t *StartGoalTool) Name() string            { return "start_goal" }
func (t *StartGoalTool) IsReadOnly() bool        { return false }
func (t *StartGoalTool) IsConcurrencySafe() bool { return false }

func (t *StartGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "start_goal",
		Description: "Start the thread's active runtime Goal. This creates the GoalRuntime state that can auto-continue while active. " +
			"Use this only when the user explicitly asks for an ongoing objective; do not call it for ordinary one-shot edits or local scratch planning. " +
			"Workflow and subagent reports are evidence, not the active Goal lifecycle owner.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal": map[string]any{
					"type":        "string",
					"description": "User-visible objective. State the outcome, not implementation details.",
				},
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional stable id. Omit unless a caller needs a predictable id.",
				},
				"token_budget": map[string]any{
					"type":        "integer",
					"description": "Optional runtime token budget for automatic continuation. Omit for no explicit token budget.",
				},
			},
			"required": []string{"goal"},
		},
	}
}

func (t *StartGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Goal        string `json:"goal"`
		GoalID      string `json:"goal_id"`
		TokenBudget int    `json:"token_budget"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	objective := strings.TrimSpace(args.Goal)
	if objective == "" {
		return "", errors.New("start_goal requires goal")
	}
	if args.TokenBudget < 0 {
		return "", errors.New("token_budget cannot be negative")
	}
	goalID := strings.TrimSpace(args.GoalID)
	if goalID == "" {
		goalID = "goal-" + session.NewID()
	}
	if err := validateGoalToolID(goalID); err != nil {
		return "", err
	}
	runtimeGoal, err := t.startRuntimeGoal(goalID, objective, args.TokenBudget)
	if err != nil {
		return "", err
	}
	return runtimeGoalToolResult("start_goal", runtimeGoal)
}

type UpdateGoalTool struct{ env *Env }

func NewUpdateGoalTool(env *Env) *UpdateGoalTool { return &UpdateGoalTool{env: env} }

func (t *UpdateGoalTool) Name() string            { return "update_goal" }
func (t *UpdateGoalTool) IsReadOnly() bool        { return false }
func (t *UpdateGoalTool) IsConcurrencySafe() bool { return false }

func (t *UpdateGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "update_goal",
		Description: "Report a real blocker for the active runtime Goal. The runtime applies the repeated-blocker threshold before the Goal becomes blocked. " +
			"Do not use this for progress notes, decisions, failures, pause, cancel, budget, or completion; use complete_goal only when the objective is actually done.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional active runtime goal id. Omit to update the current active thread Goal.",
				},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"status"},
					"description": "Optional. Blocker reporting is the only supported update.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Concise blocker message.",
				},
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"blocked"},
					"description": "Required. The model can only report status=blocked; user/system owns pause, cancel, and limit states. Use complete_goal for completed.",
				},
			},
			"required": []string{"status", "message"},
		},
	}
}

func (t *UpdateGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID  string `json:"goal_id"`
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	message := strings.TrimSpace(args.Message)
	if message == "" {
		return "", errors.New("update_goal requires message")
	}
	if kind := strings.TrimSpace(args.Kind); kind != "" && kind != "status" {
		return "", errors.New("update_goal only supports reporting status=blocked for the active runtime goal")
	}
	if strings.TrimSpace(args.Status) != string(goalruntime.StatusBlocked) {
		return "", errors.New("update_goal requires status=blocked; user/system owns pause, cancel, limit, and other terminal states")
	}
	runtimeGoal, err := currentActiveRuntimeGoalForTool(t.env, args.GoalID)
	if err != nil {
		return "", err
	}
	runtimeGoal, blocked, err := t.env.GoalRuntime.RecordBlocker(message, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return runtimeGoalToolResult("update_goal", runtimeGoal, "blocked", blocked)
}

type CompleteGoalTool struct{ env *Env }

func NewCompleteGoalTool(env *Env) *CompleteGoalTool { return &CompleteGoalTool{env: env} }

func (t *CompleteGoalTool) Name() string            { return "complete_goal" }
func (t *CompleteGoalTool) IsReadOnly() bool        { return false }
func (t *CompleteGoalTool) IsConcurrencySafe() bool { return false }

func (t *CompleteGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "complete_goal",
		Description: "Mark the active runtime Goal complete after the requested outcome is actually done and any needed workflow/subagent evidence has been integrated. " +
			"Do not call this just because a subagent or workflow finished; complete only when the user-visible goal is closed.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional active runtime goal id. Omit to complete the current active thread Goal.",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "Concise completion summary. Include the user-visible outcome and key evidence.",
				},
				"final_artifact": map[string]any{
					"type":        "string",
					"description": "Optional final artifact path or report path.",
				},
			},
			"required": []string{"summary"},
		},
	}
}

func (t *CompleteGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID        string `json:"goal_id"`
		Summary       string `json:"summary"`
		FinalArtifact string `json:"final_artifact"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		return "", errors.New("complete_goal requires summary")
	}
	if _, err := currentActiveRuntimeGoalForTool(t.env, args.GoalID); err != nil {
		return "", err
	}
	runtimeGoal, err := t.env.GoalRuntime.Complete(time.Now().UTC())
	if err != nil {
		return "", err
	}
	return runtimeGoalToolResult("complete_goal", runtimeGoal, "summary", summary, "final_artifact", strings.TrimSpace(args.FinalArtifact))
}

type GoalStatusTool struct{ env *Env }

func NewGoalStatusTool(env *Env) *GoalStatusTool { return &GoalStatusTool{env: env} }

func (t *GoalStatusTool) Name() string            { return "goal_status" }
func (t *GoalStatusTool) IsReadOnly() bool        { return true }
func (t *GoalStatusTool) IsConcurrencySafe() bool { return true }

func (t *GoalStatusTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "goal_status",
		Description: "Read the current thread runtime Goal, including status, usage, blocker audit, and budget.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional active runtime goal id to validate.",
				},
			},
		},
	}
}

func (t *GoalStatusTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID string `json:"goal_id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	result := map[string]any{
		"action": "goal_status",
		"goal":   nil,
	}
	if runtimeGoal, ok, err := currentGoalRuntime(t.env); err != nil {
		return "", err
	} else if ok {
		if goalID := strings.TrimSpace(args.GoalID); goalID != "" && runtimeGoal.GoalID != goalID {
			return "", fmt.Errorf("active thread goal %q does not match requested goal %q", runtimeGoal.GoalID, goalID)
		}
		result["goal"] = runtimeGoal
		result["goal_id"] = runtimeGoal.GoalID
		result["status"] = runtimeGoal.Status
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

func (t *StartGoalTool) startRuntimeGoal(goalID, objective string, tokenBudget int) (goalruntime.Goal, error) {
	if t == nil || t.env == nil || t.env.GoalRuntime == nil {
		return goalruntime.Goal{}, errors.New("start_goal requires a thread runtime goal store")
	}
	threadID := strings.TrimSpace(t.env.SessionID)
	if threadID == "" {
		return goalruntime.Goal{}, errors.New("thread session_id is required for runtime goal")
	}
	goal, err := t.env.GoalRuntime.Create(goalruntime.Spec{
		ThreadID:    threadID,
		GoalID:      goalID,
		Objective:   objective,
		TokenBudget: tokenBudget,
	})
	if err != nil {
		return goalruntime.Goal{}, err
	}
	return goal, nil
}

func currentActiveRuntimeGoalForTool(env *Env, goalID string) (goalruntime.Goal, error) {
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
	goalID = strings.TrimSpace(goalID)
	if goalID != "" && goal.GoalID != goalID {
		return goalruntime.Goal{}, fmt.Errorf("active thread goal %q does not match requested goal %q", goal.GoalID, goalID)
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

func runtimeGoalToolResult(action string, goal goalruntime.Goal, fields ...any) (string, error) {
	result := map[string]any{
		"action":       action,
		"goal_id":      goal.GoalID,
		"status":       goal.Status,
		"runtime_goal": goal,
	}
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		result[key] = fields[i+1]
	}
	return mustJSON(result)
}
