package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type PlanItem struct {
	Step   string     `json:"step"`
	Status PlanStatus `json:"status"`
}

type PlanSnapshot struct {
	Explanation string     `json:"explanation,omitempty"`
	Plan        []PlanItem `json:"plan"`
}

type PlanStatus string

const (
	PlanStatusPending    PlanStatus = "pending"
	PlanStatusInProgress PlanStatus = "in_progress"
	PlanStatusCompleted  PlanStatus = "completed"
)

type planState struct {
	mu       sync.RWMutex
	snapshot PlanSnapshot
}

func (s *planState) set(snapshot PlanSnapshot) PlanSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = clonePlanSnapshot(snapshot)
	return clonePlanSnapshot(s.snapshot)
}

func (s *planState) get() (PlanSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.snapshot.Plan) == 0 {
		return PlanSnapshot{}, false
	}
	return clonePlanSnapshot(s.snapshot), true
}

func clonePlanSnapshot(snapshot PlanSnapshot) PlanSnapshot {
	out := PlanSnapshot{
		Explanation: snapshot.Explanation,
	}
	if len(snapshot.Plan) > 0 {
		out.Plan = append([]PlanItem(nil), snapshot.Plan...)
	}
	return out
}

func (t *Toolkit) PlanContextBlocks() []wuucontext.Block {
	if t == nil || t.env == nil {
		return nil
	}
	snapshot, ok := t.env.planState.get()
	if !ok {
		return nil
	}
	return PlanSnapshotContextBlocks(snapshot)
}

func PlanSnapshotContextBlocks(snapshot PlanSnapshot) []wuucontext.Block {
	var blocks []wuucontext.Block
	if block, ok := planStateBlock(snapshot); ok {
		blocks = append(blocks, block)
	}
	return blocks
}

func planStateBlock(snapshot PlanSnapshot) (wuucontext.Block, bool) {
	if len(snapshot.Plan) == 0 {
		return wuucontext.Block{}, false
	}
	var b strings.Builder
	if strings.TrimSpace(snapshot.Explanation) != "" {
		b.WriteString("explanation: ")
		b.WriteString(strings.TrimSpace(snapshot.Explanation))
		b.WriteString("\n\n")
	}
	b.WriteString("plan:\n")
	for _, item := range snapshot.Plan {
		fmt.Fprintf(&b, "- [%s] %s\n", item.Status, strings.TrimSpace(item.Step))
	}
	return wuucontext.Block{
		Kind:    wuucontext.BlockTaskState,
		Title:   "Current visible task plan",
		Source:  "update_plan",
		Content: strings.TrimRight(b.String(), "\n"),
	}, true
}

// RestorePlanFromHistory restores the latest valid update_plan snapshot from
// persisted assistant tool calls. It does not call OnPlanUpdated because
// recovery is state hydration, not a fresh tool update.
func (t *Toolkit) RestorePlanFromHistory(history []providers.ChatMessage) (bool, error) {
	if t == nil || t.env == nil {
		return false, nil
	}
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role != "assistant" {
			continue
		}
		for j := len(msg.ToolCalls) - 1; j >= 0; j-- {
			call := msg.ToolCalls[j]
			if call.Name != "update_plan" {
				continue
			}
			snapshot, err := decodeLegacyPlanSnapshot(call.Arguments)
			if err != nil {
				return false, fmt.Errorf("restore update_plan arguments: %w", err)
			}
			if err := validatePlan(snapshot); err != nil {
				return false, fmt.Errorf("restore update_plan snapshot: %w", err)
			}
			t.env.planState.set(snapshot)
			return true, nil
		}
	}
	return false, nil
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
			"Provide the full current plan every time. Exactly one item must be in_progress until all plan items are completed. " +
			"After finishing a step, call update_plan again to mark it completed and the next step in_progress before moving on. " +
			"When every step is done, call update_plan to mark all items completed.",
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
	args, err := decodePlanSnapshot(argsJSON)
	if err != nil {
		return "", err
	}
	if err := validatePlan(args); err != nil {
		return "", err
	}
	snapshot := t.env.planState.set(args)
	if t.env.OnPlanUpdated != nil {
		t.env.OnPlanUpdated(snapshot)
	}
	// Echo the stored snapshot so the transcript tail always carries a fresh
	// copy of the plan; the derived TASK_STATE ledger no longer re-states it
	// on every request.
	return mustJSON(map[string]any{
		"action": "update_plan",
		"status": "updated",
		"plan":   snapshot.Plan,
	})
}

func decodePlanSnapshot(raw string) (PlanSnapshot, error) {
	var snapshot PlanSnapshot
	if err := decodeJSONStrict(raw, &snapshot); err != nil {
		return PlanSnapshot{}, err
	}
	return snapshot, nil
}

func decodeLegacyPlanSnapshot(raw string) (PlanSnapshot, error) {
	var legacy struct {
		Explanation    string          `json:"explanation,omitempty"`
		Plan           []PlanItem      `json:"plan"`
		Constraints    json.RawMessage `json:"constraints,omitempty"`
		PreWriteCheck  json.RawMessage `json:"pre_write_check,omitempty"`
		PreFinishCheck json.RawMessage `json:"pre_finish_check,omitempty"`
	}
	if err := decodeJSONStrict(raw, &legacy); err != nil {
		return PlanSnapshot{}, err
	}
	return PlanSnapshot{
		Explanation: legacy.Explanation,
		Plan:        legacy.Plan,
	}, nil
}

func decodeJSONStrict(raw string, out any) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid tool arguments: multiple JSON values")
	}
	return nil
}

func validatePlan(snapshot PlanSnapshot) error {
	if len(snapshot.Plan) == 0 {
		return errors.New("update_plan requires at least one plan item")
	}
	inProgress := 0
	completed := 0
	for i, item := range snapshot.Plan {
		if strings.TrimSpace(item.Step) == "" {
			return fmt.Errorf("plan item %d requires step", i)
		}
		switch item.Status {
		case PlanStatusPending:
		case PlanStatusCompleted:
			completed++
		case PlanStatusInProgress:
			inProgress++
		default:
			return fmt.Errorf("plan item %d has invalid status %q", i, item.Status)
		}
	}
	if inProgress > 1 {
		return errors.New("only one plan item may be in_progress")
	}
	if completed < len(snapshot.Plan) && inProgress == 0 {
		return errors.New("one plan item must be in_progress until all items are completed")
	}
	return nil
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
