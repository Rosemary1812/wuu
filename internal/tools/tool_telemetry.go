package tools

import (
	"context"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ToolExecutionRecord captures benchmark-oriented facts about one tool
// execution. It deliberately excludes arguments and output content.
type ToolExecutionRecord struct {
	Name                string           `json:"name"`
	CallID              string           `json:"call_id,omitempty"`
	Kind                ToolKind         `json:"kind"`
	Exposure            ToolExposure     `json:"exposure"`
	Risk                ToolRisk         `json:"risk"`
	PolicyAction        ToolPolicyAction `json:"policy_action"`
	PolicyReason        string           `json:"policy_reason,omitempty"`
	ReadOnly            bool             `json:"read_only"`
	ConcurrencySafe     bool             `json:"concurrency_safe"`
	StartedAt           time.Time        `json:"started_at"`
	DurationMS          int64            `json:"duration_ms"`
	Success             bool             `json:"success"`
	Error               string           `json:"error,omitempty"`
	RawOutputBytes      int              `json:"raw_output_bytes"`
	ReturnedOutputBytes int              `json:"returned_output_bytes"`
	ResultBudgeted      bool             `json:"result_budgeted"`
	ResultRef           string           `json:"result_ref,omitempty"`
}

type toolTelemetry struct {
	mu      sync.RWMutex
	records []ToolExecutionRecord
}

func (t *toolTelemetry) record(record ToolExecutionRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, record)
}

func (t *toolTelemetry) snapshot() []ToolExecutionRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ToolExecutionRecord, len(t.records))
	copy(out, t.records)
	return out
}

// ToolTelemetry returns a snapshot of tool execution records for this toolkit.
func (t *Toolkit) ToolTelemetry() []ToolExecutionRecord {
	return t.env.toolTelemetry.snapshot()
}

func (t *Toolkit) executeKnownTool(ctx context.Context, call providers.ToolCall, tool Tool) (string, error) {
	info := buildToolInfoForArgs(tool, t.toolExposure(call.Name), call.Arguments)
	decision := t.toolPolicy.Decide(info)
	startedAt := time.Now()

	if err := decision.blockingError(call.Name); err != nil {
		t.recordToolExecution(call, info, decision, startedAt, "", "", "", false, err)
		return "", err
	}

	result, err := tool.Execute(ctx, call.Arguments)
	returned := result
	resultRef := ""
	resultBudgeted := false
	if err == nil {
		returned, resultRef, resultBudgeted = MaybePersistResultWithRef(t.env.SessionDir, call.Name, call.ID, result, defaultResultBudget)
	}

	t.recordToolExecution(call, info, decision, startedAt, result, returned, resultRef, resultBudgeted, err)

	return returned, err
}

func (t *Toolkit) recordToolExecution(
	call providers.ToolCall,
	info ToolInfo,
	decision ToolPolicyDecision,
	startedAt time.Time,
	result string,
	returned string,
	resultRef string,
	resultBudgeted bool,
	err error,
) {
	record := ToolExecutionRecord{
		Name:                call.Name,
		CallID:              call.ID,
		Kind:                info.Kind,
		Exposure:            info.Exposure,
		Risk:                info.Risk,
		PolicyAction:        decision.Action,
		PolicyReason:        decision.Reason,
		ReadOnly:            info.ReadOnly,
		ConcurrencySafe:     info.ConcurrencySafe,
		StartedAt:           startedAt,
		DurationMS:          time.Since(startedAt).Milliseconds(),
		Success:             err == nil,
		RawOutputBytes:      len(result),
		ReturnedOutputBytes: len(returned),
		ResultBudgeted:      resultBudgeted,
		ResultRef:           resultRef,
	}
	if err != nil {
		record.Error = err.Error()
	}
	t.env.toolTelemetry.record(record)
}
