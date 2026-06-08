package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ToolExecutionRecord captures benchmark-oriented facts about one tool
// execution. It deliberately excludes arguments and output content.
type ToolExecutionRecord struct {
	Name                 string           `json:"name"`
	CallID               string           `json:"call_id,omitempty"`
	Kind                 ToolKind         `json:"kind"`
	Exposure             ToolExposure     `json:"exposure"`
	Risk                 ToolRisk         `json:"risk"`
	ClassificationReason string           `json:"classification_reason,omitempty"`
	PolicyAction         ToolPolicyAction `json:"policy_action"`
	PolicyReason         string           `json:"policy_reason,omitempty"`
	ReadOnly             bool             `json:"read_only"`
	ConcurrencySafe      bool             `json:"concurrency_safe"`
	StartedAt            time.Time        `json:"started_at"`
	DurationMS           int64            `json:"duration_ms"`
	RevisionBefore       string           `json:"revision_before,omitempty"`
	RevisionAfter        string           `json:"revision_after,omitempty"`
	Success              bool             `json:"success"`
	Error                string           `json:"error,omitempty"`
	RawOutputBytes       int              `json:"raw_output_bytes"`
	ReturnedOutputBytes  int              `json:"returned_output_bytes"`
	ResultBudgeted       bool             `json:"result_budgeted"`
	ResultRef            string           `json:"result_ref,omitempty"`
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
	revisionBefore := workspaceRevision(ctx, t.env.RootDir)

	if err := decision.blockingError(call.Name); err != nil {
		t.recordToolExecution(call, info, decision, startedAt, revisionBefore, revisionBefore, "", "", "", false, err)
		return "", err
	}

	result, err := tool.Execute(ctx, call.Arguments)
	returned := result
	resultRef := ""
	resultBudgeted := false
	if err == nil {
		returned, resultRef, resultBudgeted = MaybePersistResultWithRef(t.env.SessionDir, call.Name, call.ID, result, defaultResultBudget)
	}

	revisionAfter := workspaceRevision(ctx, t.env.RootDir)
	t.recordToolExecution(call, info, decision, startedAt, revisionBefore, revisionAfter, result, returned, resultRef, resultBudgeted, err)

	return returned, err
}

func (t *Toolkit) recordToolExecution(
	call providers.ToolCall,
	info ToolInfo,
	decision ToolPolicyDecision,
	startedAt time.Time,
	revisionBefore string,
	revisionAfter string,
	result string,
	returned string,
	resultRef string,
	resultBudgeted bool,
	err error,
) {
	record := ToolExecutionRecord{
		Name:                 call.Name,
		CallID:               call.ID,
		Kind:                 info.Kind,
		Exposure:             info.Exposure,
		Risk:                 info.Risk,
		ClassificationReason: info.Reason,
		PolicyAction:         decision.Action,
		PolicyReason:         decision.Reason,
		ReadOnly:             info.ReadOnly,
		ConcurrencySafe:      info.ConcurrencySafe,
		StartedAt:            startedAt,
		DurationMS:           time.Since(startedAt).Milliseconds(),
		RevisionBefore:       revisionBefore,
		RevisionAfter:        revisionAfter,
		Success:              err == nil,
		RawOutputBytes:       len(result),
		ReturnedOutputBytes:  len(returned),
		ResultBudgeted:       resultBudgeted,
		ResultRef:            resultRef,
	}
	if err != nil {
		record.Error = err.Error()
	}
	t.env.toolTelemetry.record(record)
}

func workspaceRevision(ctx context.Context, rootDir string) string {
	if rootDir == "" {
		return ""
	}
	revCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	headCmd := exec.CommandContext(revCtx, "git", "rev-parse", "HEAD")
	headCmd.Dir = rootDir
	headOut, err := headCmd.Output()
	if err != nil {
		return ""
	}
	statusCmd := exec.CommandContext(revCtx, "git", "status", "--porcelain=v1", "-z")
	statusCmd.Dir = rootDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(statusOut)
	head := string(headOut)
	if len(head) >= 12 {
		head = head[:12]
	}
	return "git:" + trimRevisionToken(head) + ":worktree:" + hex.EncodeToString(sum[:])[:16]
}

func trimRevisionToken(value string) string {
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'f':
			out = append(out, c)
		case c >= 'A' && c <= 'F':
			out = append(out, c+'a'-'A')
		case c >= '0' && c <= '9':
			out = append(out, c)
		}
	}
	return string(out)
}
