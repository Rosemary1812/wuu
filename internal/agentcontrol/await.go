package agentcontrol

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

type AwaitAgentsResult struct {
	TimedOut bool               `json:"timed_out"`
	Results  []AwaitAgentResult `json:"results"`
}

type AwaitAgentResult struct {
	AgentID       string   `json:"agent_id,omitempty"`
	TaskName      string   `json:"task_name,omitempty"`
	AgentPath     string   `json:"agent_path,omitempty"`
	Status        string   `json:"status"`
	Result        string   `json:"result,omitempty"`
	Error         string   `json:"error,omitempty"`
	ReportPath    string   `json:"report_path,omitempty"`
	ReportMissing bool     `json:"report_missing,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	WorktreePath  string   `json:"worktree_path,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
}

type awaitTarget struct {
	Input string
	Meta  agentthread.Metadata
	Found bool
}

func (c *AgentControl) AwaitFrom(currentPath string, ctx context.Context, targets []string) (AwaitAgentsResult, error) {
	if c == nil || c.manager == nil {
		return AwaitAgentsResult{}, errors.New("agent control not configured")
	}
	resolved := c.resolveAwaitTargets(currentPath, targets)
	if len(resolved) == 0 {
		return AwaitAgentsResult{Results: []AwaitAgentResult{}}, nil
	}

	ch := make(chan subagent.Notification, 16)
	c.manager.Subscribe(ch)
	defer c.manager.Unsubscribe(ch)

	for {
		result := c.awaitSnapshot(resolved)
		if awaitComplete(result.Results) {
			return result, nil
		}
		select {
		case <-ch:
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			result.TimedOut = true
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, nil
			}
			return result, ctx.Err()
		}
	}
}

func (c *AgentControl) ActiveTaskReminder(currentPath string) string {
	targets := c.activeDescendantTargets(currentPath)
	if len(targets) == 0 {
		return ""
	}
	results := c.awaitSnapshot(targets).Results
	if len(results) == 0 {
		return ""
	}
	const maxReminderTasks = 8
	var b strings.Builder
	b.WriteString("<subagent_status>\n")
	b.WriteString("Child-agent tasks that are still active or missing a structured report:\n")
	limit := len(results)
	if limit > maxReminderTasks {
		limit = maxReminderTasks
	}
	for i := 0; i < limit; i++ {
		result := results[i]
		label := result.AgentPath
		if label == "" {
			label = result.AgentID
		}
		b.WriteString("- ")
		b.WriteString(label)
		if result.TaskName != "" {
			b.WriteString(" (")
			b.WriteString(result.TaskName)
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(result.Status)
		if result.ReportMissing {
			b.WriteString(", missing agent_report")
		}
		if result.WorktreePath != "" {
			b.WriteString(", worktree=")
			b.WriteString(result.WorktreePath)
		}
		b.WriteString("\n")
	}
	if len(results) > limit {
		b.WriteString("- ... ")
		b.WriteString(strconv.Itoa(len(results) - limit))
		b.WriteString(" more\n")
	}
	b.WriteString("\nUse await_agents with explicit targets when your next step depends on these outputs. ")
	b.WriteString("If a task is awaiting_report, treat its handoff as incomplete until you follow up or verify the raw result.\n")
	b.WriteString("</subagent_status>")
	return b.String()
}

func (c *AgentControl) resolveAwaitTargets(currentPath string, targets []string) []awaitTarget {
	cleaned := trimStringSlice(targets)
	if len(cleaned) == 0 {
		return c.activeDescendantTargets(currentPath)
	}
	out := make([]awaitTarget, 0, len(cleaned))
	seen := map[string]struct{}{}
	for _, target := range cleaned {
		meta, ok := c.threads.ResolveFrom(currentPath, target)
		if !ok {
			if snap := c.snapshotByID(target); snap != nil {
				meta = metadataFromSnapshot(*snap)
				ok = true
			}
		}
		key := strings.TrimSpace(target)
		if ok {
			key = meta.ID
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, awaitTarget{Input: target, Meta: meta, Found: ok})
	}
	return out
}

func (c *AgentControl) activeDescendantTargets(currentPath string) []awaitTarget {
	if c == nil || c.threads == nil {
		return nil
	}
	current := strings.TrimSpace(currentPath)
	if current == "" {
		current = agentthread.RootPath
	}
	out := make([]awaitTarget, 0)
	for _, meta := range c.threads.List() {
		if meta.Path == agentthread.RootPath || !isDescendantAgentPath(current, meta.Path) {
			continue
		}
		result := c.awaitResultForTarget(awaitTarget{Meta: meta, Found: true})
		if isAwaitActiveStatus(result.Status) || result.Status == string(harness.TaskStatusAwaitingReport) {
			out = append(out, awaitTarget{Meta: meta, Found: true})
		}
	}
	return out
}

func isDescendantAgentPath(parent, child string) bool {
	parent = strings.TrimRight(strings.TrimSpace(parent), "/")
	child = strings.TrimRight(strings.TrimSpace(child), "/")
	if parent == "" {
		parent = agentthread.RootPath
	}
	if parent == agentthread.RootPath {
		return child != "" && child != agentthread.RootPath
	}
	return strings.HasPrefix(child, parent+"/")
}

func (c *AgentControl) awaitSnapshot(targets []awaitTarget) AwaitAgentsResult {
	results := make([]AwaitAgentResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, c.awaitResultForTarget(target))
	}
	return AwaitAgentsResult{Results: results}
}

func (c *AgentControl) awaitResultForTarget(target awaitTarget) AwaitAgentResult {
	if !target.Found {
		return AwaitAgentResult{
			AgentID: strings.TrimSpace(target.Input),
			Status:  "not_found",
			Error:   fmt.Sprintf("agent %q not found", strings.TrimSpace(target.Input)),
		}
	}

	meta := target.Meta
	out := AwaitAgentResult{
		AgentID:   meta.ID,
		TaskName:  meta.TaskName,
		AgentPath: meta.Path,
		Status:    string(meta.Status),
	}

	if snap := c.snapshotByID(meta.ID); snap != nil {
		out = awaitResultFromSnapshot(*snap)
	}
	if task, ok := c.harnessTask(meta.ID); ok {
		applyHarnessTaskToAwaitResult(&out, task)
	}
	reportPath, artifacts := c.harnessReportForTask(meta.ID)
	out.ReportPath = reportPath
	out.Artifacts = artifacts
	if out.Status == string(subagent.StatusCompleted) && reportPath == "" {
		out.Status = string(harness.TaskStatusAwaitingReport)
		out.ReportMissing = true
	}
	if out.Status == string(harness.TaskStatusAwaitingReport) {
		out.ReportMissing = true
	}
	return out
}

func (c *AgentControl) snapshotByID(id string) *subagent.SubAgentSnapshot {
	if c == nil || c.manager == nil {
		return nil
	}
	sa := c.manager.Get(strings.TrimSpace(id))
	if sa == nil {
		return nil
	}
	snap := sa.Snapshot()
	return &snap
}

func metadataFromSnapshot(snap subagent.SubAgentSnapshot) agentthread.Metadata {
	return agentthread.Metadata{
		ID:       snap.ID,
		Path:     snap.AgentPath,
		TaskName: snap.TaskName,
		Role:     snap.Type,
		ParentID: snap.ParentID,
		Status:   threadStatusFromSubAgent(snap.Status),
	}
}

func awaitResultFromSnapshot(snap subagent.SubAgentSnapshot) AwaitAgentResult {
	out := AwaitAgentResult{
		AgentID:      snap.ID,
		TaskName:     snap.TaskName,
		AgentPath:    snap.AgentPath,
		Status:       string(snap.Status),
		Result:       snap.Result,
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
	}
	if snap.Error != nil {
		out.Error = snap.Error.Error()
	}
	if !snap.CompletedAt.IsZero() && !snap.StartedAt.IsZero() {
		out.DurationMS = snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
	}
	return out
}

func applyHarnessTaskToAwaitResult(out *AwaitAgentResult, task harness.Task) {
	if out.AgentID == "" {
		out.AgentID = task.ID
	}
	if out.TaskName == "" {
		out.TaskName = task.Name
	}
	if out.AgentPath == "" {
		out.AgentPath = task.Path
	}
	if shouldPreferHarnessStatus(task.Status) {
		out.Status = string(task.Status)
	}
	if out.Error == "" {
		out.Error = task.Error
	}
	if task.Workspace.Mode == harness.WorkspaceWorktree {
		out.WorktreePath = task.Workspace.Root
	}
	if out.InputTokens == 0 {
		out.InputTokens = task.InputTokens
	}
	if out.OutputTokens == 0 {
		out.OutputTokens = task.OutputTokens
	}
	if out.DurationMS == 0 && !task.CompletedAt.IsZero() && !task.StartedAt.IsZero() {
		out.DurationMS = task.CompletedAt.Sub(task.StartedAt).Milliseconds()
	}
}

func shouldPreferHarnessStatus(status harness.TaskStatus) bool {
	switch status {
	case harness.TaskStatusQueued, harness.TaskStatusAwaitingReport, harness.TaskStatusCompleted, harness.TaskStatusFailed, harness.TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func awaitComplete(results []AwaitAgentResult) bool {
	for _, result := range results {
		if isAwaitActiveStatus(result.Status) {
			return false
		}
	}
	return true
}

func isAwaitActiveStatus(status string) bool {
	switch status {
	case string(harness.TaskStatusPending), string(harness.TaskStatusQueued), string(harness.TaskStatusRunning):
		return true
	default:
		return false
	}
}
