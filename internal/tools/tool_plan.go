package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type PlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type PlanSnapshot struct {
	Explanation string     `json:"explanation,omitempty"`
	Plan        []PlanItem `json:"plan"`
}

type planState struct {
	mu       sync.RWMutex
	snapshot PlanSnapshot
}

func (s *planState) set(snapshot PlanSnapshot) PlanSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = snapshot
	return s.snapshot
}

// ---------------------------------------------------------------------------
// update_plan
// ---------------------------------------------------------------------------

type UpdatePlanTool struct{ env *Env }

func NewUpdatePlanTool(env *Env) *UpdatePlanTool { return &UpdatePlanTool{env: env} }

func (t *UpdatePlanTool) Name() string            { return "update_plan" }
func (t *UpdatePlanTool) IsReadOnly() bool        { return false }
func (t *UpdatePlanTool) IsConcurrencySafe() bool { return false }

func (t *UpdatePlanTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "update_plan",
		Description: "Update the current task plan. Use this for multi-step work so the user can see what is pending, in progress, and completed. " +
			"Provide the full current plan every time. Exactly zero or one item may be in_progress.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"explanation": map[string]any{
					"type":        "string",
					"description": "Optional short explanation for why the plan changed.",
				},
				"plan": map[string]any{
					"type":        "array",
					"minItems":    1,
					"description": "Full current plan.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"step": map[string]any{
								"type":        "string",
								"description": "Concrete task step.",
							},
							"status": map[string]any{
								"type":        "string",
								"enum":        []string{"pending", "in_progress", "completed"},
								"description": "Current step status.",
							},
						},
						"required": []string{"step", "status"},
					},
				},
			},
			"required": []string{"plan"},
		},
	}
}

func (t *UpdatePlanTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args PlanSnapshot
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if err := validatePlan(args); err != nil {
		return "", err
	}
	snapshot := t.env.planState.set(args)
	result := map[string]any{
		"status": "updated",
		"plan":   snapshot.Plan,
	}
	if snapshot.Explanation != "" {
		result["explanation"] = snapshot.Explanation
	}
	return mustJSON(result)
}

func validatePlan(snapshot PlanSnapshot) error {
	if len(snapshot.Plan) == 0 {
		return errors.New("update_plan requires at least one plan item")
	}
	inProgress := 0
	for i, item := range snapshot.Plan {
		if strings.TrimSpace(item.Step) == "" {
			return fmt.Errorf("plan item %d requires step", i)
		}
		switch item.Status {
		case "pending", "completed":
		case "in_progress":
			inProgress++
		default:
			return fmt.Errorf("plan item %d has invalid status %q", i, item.Status)
		}
	}
	if inProgress > 1 {
		return errors.New("only one plan item may be in_progress")
	}
	return nil
}
