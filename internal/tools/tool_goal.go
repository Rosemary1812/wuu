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
	"github.com/blueberrycongee/wuu/internal/harness"
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
		Description: "Start a durable user-visible Goal for work that needs explicit progress tracking, multiple workflow runs, subagents, approvals, retries, or later resumption. " +
			"Do not call this for tiny one-shot edits, ordinary local investigation, or one self-contained workflow run; start_workflow creates a Goal binding for that run. " +
			"If the user-level Goal is broader than one workflow or child task, pass the returned goal_id and goal_dir to start_workflow and workflow-bound spawn_agent calls, then call complete_goal only when the user-visible outcome is done.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal": map[string]any{
					"type":        "string",
					"description": "User-visible objective. State the outcome, not implementation details.",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "Optional concrete task detail or current slice of the objective.",
				},
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional stable id. Omit unless you need a predictable id to bind a workflow or subagent.",
				},
				"trigger_type": map[string]any{
					"type":        "string",
					"description": "Optional trigger type such as manual, workflow, scheduled, or recovery. Defaults to manual.",
				},
				"trigger_source": map[string]any{
					"type":        "string",
					"description": "Optional trigger source. Defaults to start_goal.",
				},
				"next_steps": map[string]any{
					"type":        "array",
					"description": "Optional immediate next steps for resumption.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"goal"},
		},
	}
}

func (t *StartGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Goal          string   `json:"goal"`
		Task          string   `json:"task"`
		GoalID        string   `json:"goal_id"`
		TriggerType   string   `json:"trigger_type"`
		TriggerSource string   `json:"trigger_source"`
		NextSteps     []string `json:"next_steps"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	objective := strings.TrimSpace(args.Goal)
	if objective == "" {
		return "", errors.New("start_goal requires goal")
	}
	goalID := strings.TrimSpace(args.GoalID)
	if goalID == "" {
		goalID = "goal-" + session.NewID()
	}
	if err := validateGoalToolID(goalID); err != nil {
		return "", err
	}
	store, err := t.env.GoalStore(goalID)
	if err != nil {
		return "", err
	}
	if _, err := store.LoadState(); err == nil {
		return "", fmt.Errorf("goal %q already exists; use goal_status or update_goal", goalID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	triggerType := strings.TrimSpace(args.TriggerType)
	if triggerType == "" {
		triggerType = "manual"
	}
	triggerSource := strings.TrimSpace(args.TriggerSource)
	if triggerSource == "" {
		triggerSource = "start_goal"
	}
	state, err := store.Init(goalrunner.Spec{
		ID:   goalID,
		Goal: objective,
		Task: strings.TrimSpace(args.Task),
		Trigger: goalrunner.Trigger{
			Type:   triggerType,
			Source: triggerSource,
		},
		AssignedAgent: "lead",
	})
	if err != nil {
		return "", err
	}
	state, err = store.SetStatus(goalrunner.StatusRunning, goalrunner.StepInit, "Goal started.")
	if err != nil {
		return "", err
	}
	state, err = setGoalNextSteps(store, state, args.NextSteps)
	if err != nil {
		return "", err
	}
	return goalToolStateResult("start_goal", store, state)
}

type UpdateGoalTool struct{ env *Env }

func NewUpdateGoalTool(env *Env) *UpdateGoalTool { return &UpdateGoalTool{env: env} }

func (t *UpdateGoalTool) Name() string            { return "update_goal" }
func (t *UpdateGoalTool) IsReadOnly() bool        { return false }
func (t *UpdateGoalTool) IsConcurrencySafe() bool { return false }

func (t *UpdateGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "update_goal",
		Description: "Record meaningful Goal progress, a decision, a blocker, or a status change. Use this when the update should survive context compaction or coordinate workflow/subagent work. " +
			"Do not use it as a scratchpad for every small thought; use update_plan for short local task lists.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Goal id returned by start_goal or start_workflow.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional goal_dir returned by start_goal or start_workflow. If omitted, goal_id is resolved from workspace state.",
				},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"progress", "decision", "failure", "status"},
					"description": "Update kind. Defaults to progress.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Concise update, decision, failure, or status message.",
				},
				"step": map[string]any{
					"type":        "string",
					"enum":        goalStepNames(),
					"description": "Goal step this update belongs to. Defaults to the current step or execution.",
				},
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"pending", "running", "blocked", "needs_human", "failed", "cancelled"},
					"description": "Required when kind=status; ignored for progress and decision. Use complete_goal for completed.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Optional decision reason or failure detail.",
				},
				"next_steps": map[string]any{
					"type":        "array",
					"description": "Optional next steps to store for resumption.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"goal_id", "message"},
		},
	}
}

func (t *UpdateGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID    string   `json:"goal_id"`
		GoalDir   string   `json:"goal_dir"`
		Kind      string   `json:"kind"`
		Message   string   `json:"message"`
		Step      string   `json:"step"`
		Status    string   `json:"status"`
		Reason    string   `json:"reason"`
		NextSteps []string `json:"next_steps"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	message := strings.TrimSpace(args.Message)
	if message == "" {
		return "", errors.New("update_goal requires message")
	}
	store, state, err := t.env.ResolveGoalStore(args.GoalID, args.GoalDir)
	if err != nil {
		return "", err
	}
	step := parseGoalStep(args.Step, state.CurrentStep)
	kind := strings.TrimSpace(args.Kind)
	if kind == "" {
		kind = "progress"
	}
	switch kind {
	case "progress":
		state, err = store.AddProgress(step, message)
	case "decision":
		state, err = store.AddDecision(step, message, strings.TrimSpace(args.Reason))
		if err == nil {
			state.CurrentStep = step
			err = store.SaveState(state)
		}
	case "failure":
		state, err = store.AddFailure(goalrunner.Failure{
			Step:    step,
			Kind:    "goal_update",
			Source:  "update_goal",
			Message: firstGoalToolText(args.Reason, message),
		})
	case "status":
		status, ok := parseGoalStatus(args.Status)
		if !ok || status == goalrunner.StatusCompleted {
			return "", errors.New("update_goal kind=status requires status pending, running, blocked, needs_human, failed, or cancelled")
		}
		state, err = store.SetStatus(status, step, message)
	default:
		return "", fmt.Errorf("update_goal kind must be progress, decision, failure, or status")
	}
	if err != nil {
		return "", err
	}
	state, err = setGoalNextSteps(store, state, args.NextSteps)
	if err != nil {
		return "", err
	}
	return goalToolStateResult("update_goal", store, state)
}

type CompleteGoalTool struct{ env *Env }

func NewCompleteGoalTool(env *Env) *CompleteGoalTool { return &CompleteGoalTool{env: env} }

func (t *CompleteGoalTool) Name() string            { return "complete_goal" }
func (t *CompleteGoalTool) IsReadOnly() bool        { return false }
func (t *CompleteGoalTool) IsConcurrencySafe() bool { return false }

func (t *CompleteGoalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "complete_goal",
		Description: "Mark a Goal complete after the requested outcome is actually done and any needed workflow/subagent results have been integrated. " +
			"For delegated or multi-agent work, inspect goal_status and require independent workflow, subagent, or reviewer evidence before completion; do not self-certify from the lead agent's own claim. " +
			"Do not call this just because a subagent or workflow finished; complete only when the user-visible goal is closed.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Goal id returned by start_goal or start_workflow.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional goal_dir returned by start_goal or start_workflow. If omitted, goal_id is resolved from workspace state.",
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
			"required": []string{"goal_id", "summary"},
		},
	}
}

func (t *CompleteGoalTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID        string `json:"goal_id"`
		GoalDir       string `json:"goal_dir"`
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
	store, state, err := t.env.ResolveGoalStore(args.GoalID, args.GoalDir)
	if err != nil {
		return "", err
	}
	state, err = store.SetStatus(goalrunner.StatusCompleted, goalrunner.StepSummary, summary)
	if err != nil {
		return "", err
	}
	state.FinalArtifact = strings.TrimSpace(args.FinalArtifact)
	state.CurrentBlocker = ""
	state.NeedsHuman = false
	state.NextSteps = nil
	state.CompletedSteps = appendUniqueGoalStrings(state.CompletedSteps, goalrunner.StepSummary)
	state.Progress = append(state.Progress, goalrunner.ProgressEntry{
		Step:      goalrunner.StepSummary,
		Source:    "complete_goal",
		Message:   summary,
		CreatedAt: time.Now().UTC(),
	})
	if err := store.SaveState(state); err != nil {
		return "", err
	}
	return goalToolStateResult("complete_goal", store, state)
}

type GoalStatusTool struct{ env *Env }

func NewGoalStatusTool(env *Env) *GoalStatusTool { return &GoalStatusTool{env: env} }

func (t *GoalStatusTool) Name() string            { return "goal_status" }
func (t *GoalStatusTool) IsReadOnly() bool        { return true }
func (t *GoalStatusTool) IsConcurrencySafe() bool { return true }

func (t *GoalStatusTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "goal_status",
		Description: "Read durable Goal state. Use this before resuming, updating, completing, or binding workflow/subagent work to an existing Goal.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional goal id. Omit to list workspace goal, workflow, and agent status.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional goal_dir returned by start_goal or start_workflow.",
				},
			},
		},
	}
}

func (t *GoalStatusTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		GoalID  string `json:"goal_id"`
		GoalDir string `json:"goal_dir"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.GoalID) != "" || strings.TrimSpace(args.GoalDir) != "" {
		store, state, err := t.env.ResolveGoalStore(args.GoalID, args.GoalDir)
		if err != nil {
			return "", err
		}
		events, err := store.Events()
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"action":      "goal_status",
			"goal":        state,
			"goal_id":     state.ID,
			"goal_dir":    store.Dir(),
			"state_path":  filepath.Join(store.Dir(), "state.json"),
			"event_count": len(events),
			"next_steps":  goalToolNextSteps(state),
		})
	}
	stateDir, err := t.env.OrchestrationStateDir()
	if err != nil {
		return "", err
	}
	workflowStore, _ := t.env.WorkflowStore()
	var harnessStore *harness.Store
	if t.env.AgentControl != nil {
		harnessStore = t.env.AgentControl.HarnessStore()
	}
	snapshot := goalrunner.SnapshotSystem(goalrunner.SnapshotOptions{
		GoalRoot:      statepath.GoalRoot(stateDir),
		WorkflowStore: workflowStore,
		HarnessStore:  harnessStore,
	})
	return mustJSON(map[string]any{
		"action":     "goal_status",
		"snapshot":   snapshot,
		"next_steps": []string{"Use start_goal for a new durable objective, update_goal for progress or blockers, complete_goal when the user-visible goal is done."},
	})
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

func validateGoalToolID(goalID string) error {
	if strings.TrimSpace(goalID) == "" {
		return errors.New("goal_id is required")
	}
	if goalID == "." || goalID == ".." || filepath.Base(goalID) != goalID || strings.ContainsAny(goalID, `/\`) {
		return fmt.Errorf("goal_id must be an id, not a path: %q", goalID)
	}
	return nil
}

func parseGoalStep(raw string, fallback goalrunner.Step) goalrunner.Step {
	value := strings.TrimSpace(raw)
	if value == "" {
		if fallback != "" {
			return fallback
		}
		return goalrunner.StepExecution
	}
	for _, step := range goalSteps() {
		if string(step) == value {
			return step
		}
	}
	return goalrunner.StepExecution
}

func parseGoalStatus(raw string) (goalrunner.Status, bool) {
	switch goalrunner.Status(strings.TrimSpace(raw)) {
	case goalrunner.StatusPending, goalrunner.StatusRunning, goalrunner.StatusBlocked, goalrunner.StatusNeedsHuman, goalrunner.StatusCompleted, goalrunner.StatusFailed, goalrunner.StatusCancelled:
		return goalrunner.Status(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

func goalSteps() []goalrunner.Step {
	return []goalrunner.Step{
		goalrunner.StepInit,
		goalrunner.StepResearch,
		goalrunner.StepPlan,
		goalrunner.StepApproval,
		goalrunner.StepExecution,
		goalrunner.StepVerification,
		goalrunner.StepReview,
		goalrunner.StepIntegration,
		goalrunner.StepSummary,
	}
}

func goalStepNames() []string {
	steps := goalSteps()
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, string(step))
	}
	return out
}

func setGoalNextSteps(store *goalrunner.Store, state goalrunner.State, steps []string) (goalrunner.State, error) {
	clean := trimGoalToolStrings(steps)
	if len(clean) == 0 {
		return state, nil
	}
	state.NextSteps = appendUniqueStringsLocal(state.NextSteps, clean)
	if err := store.SaveState(state); err != nil {
		return goalrunner.State{}, err
	}
	return state, nil
}

func goalToolStateResult(action string, store *goalrunner.Store, state goalrunner.State) (string, error) {
	return mustJSON(map[string]any{
		"action":     action,
		"goal_id":    state.ID,
		"goal_dir":   store.Dir(),
		"state_path": filepath.Join(store.Dir(), "state.json"),
		"status":     state.Status,
		"step":       state.CurrentStep,
		"next_steps": goalToolNextSteps(state),
	})
}

func goalToolNextSteps(state goalrunner.State) []string {
	if len(state.NextSteps) > 0 {
		return state.NextSteps
	}
	return []string{"Use goal_status to inspect the goal later; pass goal_id and goal_dir to start_workflow or spawn_agent when binding delegated work."}
}

func trimGoalToolStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func appendUniqueStringsLocal(existing, values []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(values))
	out := make([]string, 0, len(existing)+len(values))
	for _, value := range existing {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendUniqueGoalStrings(existing []goalrunner.Step, values ...goalrunner.Step) []goalrunner.Step {
	seen := make(map[goalrunner.Step]struct{}, len(existing)+len(values))
	out := make([]goalrunner.Step, 0, len(existing)+len(values))
	for _, value := range existing {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstGoalToolText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
