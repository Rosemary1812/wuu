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
	Explanation    string           `json:"explanation,omitempty"`
	Plan           []PlanItem       `json:"plan"`
	Constraints    []PlanConstraint `json:"constraints,omitempty"`
	PreWriteCheck  []string         `json:"pre_write_check,omitempty"`
	PreFinishCheck []string         `json:"pre_finish_check,omitempty"`
}

type PlanConstraint struct {
	ID     string `json:"id,omitempty"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
	Status string `json:"status,omitempty"`
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
		Explanation:    snapshot.Explanation,
		PreWriteCheck:  append([]string(nil), snapshot.PreWriteCheck...),
		PreFinishCheck: append([]string(nil), snapshot.PreFinishCheck...),
	}
	if len(snapshot.Plan) > 0 {
		out.Plan = append([]PlanItem(nil), snapshot.Plan...)
	}
	if len(snapshot.Constraints) > 0 {
		out.Constraints = append([]PlanConstraint(nil), snapshot.Constraints...)
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
	if block, ok := constraintLedgerBlock(snapshot); ok {
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

func constraintLedgerBlock(snapshot PlanSnapshot) (wuucontext.Block, bool) {
	if len(snapshot.Constraints) == 0 && len(snapshot.PreWriteCheck) == 0 && len(snapshot.PreFinishCheck) == 0 {
		return wuucontext.Block{}, false
	}
	var b strings.Builder
	if len(snapshot.Constraints) > 0 {
		b.WriteString("constraints:\n")
		for _, c := range snapshot.Constraints {
			label := strings.TrimSpace(c.ID)
			if label == "" {
				label = "constraint"
			}
			status := strings.TrimSpace(c.Status)
			if status == "" {
				status = "active"
			}
			source := strings.TrimSpace(c.Source)
			if source != "" {
				source = " source=" + source
			}
			fmt.Fprintf(&b, "- %s [%s%s]: %s\n", label, status, source, strings.TrimSpace(c.Text))
		}
	}
	writeChecks := trimStringSlice(snapshot.PreWriteCheck)
	if len(writeChecks) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("pre_write_check:\n")
		for _, item := range writeChecks {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	finishChecks := trimStringSlice(snapshot.PreFinishCheck)
	if len(finishChecks) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("pre_finish_check:\n")
		for _, item := range finishChecks {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	return wuucontext.Block{
		Kind:    wuucontext.BlockConstraintLedger,
		Title:   "Active task constraints",
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
			snapshot, err := decodePlanSnapshot(call.Arguments)
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
			"Provide the full current plan every time. Exactly one item must be in_progress until all plan items are completed.",
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
				"constraints": map[string]any{
					"type":        "array",
					"description": "Optional active constraint ledger for this task. Use it to preserve user constraints, acceptance criteria, and non-goals that must be checked before writing or finishing.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":     map[string]any{"type": "string", "description": "Stable short id such as c1."},
							"text":   map[string]any{"type": "string", "description": "Concrete constraint, acceptance criterion, or non-goal."},
							"source": map[string]any{"type": "string", "description": "Where the constraint came from, such as user, AGENTS.md, workflow, or test."},
							"status": map[string]any{"type": "string", "description": "active, satisfied, superseded, or rejected."},
						},
						"required": []string{"text"},
					},
				},
				"pre_write_check": map[string]any{
					"type":        "array",
					"description": "Optional checklist item ids or text to verify before mutating files, shell state, workflow state, or external systems.",
					"items":       map[string]any{"type": "string"},
				},
				"pre_finish_check": map[string]any{
					"type":        "array",
					"description": "Optional checklist item ids or text to verify before claiming the task is complete.",
					"items":       map[string]any{"type": "string"},
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
	return mustJSON(map[string]string{"action": "update_plan", "status": "updated"})
}

func decodePlanSnapshot(raw string) (PlanSnapshot, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var snapshot PlanSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return PlanSnapshot{}, fmt.Errorf("invalid tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return PlanSnapshot{}, errors.New("invalid tool arguments: multiple JSON values")
	}
	return snapshot, nil
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
	for i, constraint := range snapshot.Constraints {
		if strings.TrimSpace(constraint.Text) == "" {
			return fmt.Errorf("constraint item %d requires text", i)
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
