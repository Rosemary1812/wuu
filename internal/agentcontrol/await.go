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
	Action    string             `json:"action"`
	TimedOut  bool               `json:"timed_out"`
	Warnings  []string           `json:"warnings,omitempty"`
	Results   []AwaitAgentResult `json:"results"`
	NextSteps []string           `json:"next_steps,omitempty"`
}

type AwaitAgentResult struct {
	ResultID        string   `json:"result_id,omitempty"`
	AgentID         string   `json:"agent_id,omitempty"`
	TaskName        string   `json:"task_name,omitempty"`
	AgentProfile    string   `json:"agent_profile,omitempty"`
	AgentPath       string   `json:"agent_path,omitempty"`
	Status          string   `json:"status"`
	Result          string   `json:"result,omitempty"`
	ResultPath      string   `json:"result_path,omitempty"`
	ResultBytes     int      `json:"result_bytes,omitempty"`
	ResultTruncated bool     `json:"result_truncated,omitempty"`
	Error           string   `json:"error,omitempty"`
	ChangedFiles    []string `json:"changed_files,omitempty"`
	ReportPath      string   `json:"report_path,omitempty"`
	ReportMissing   bool     `json:"report_missing,omitempty"`
	Artifacts       []string `json:"artifacts,omitempty"`
	WorktreePath    string   `json:"worktree_path,omitempty"`
	InputTokens     int      `json:"input_tokens,omitempty"`
	OutputTokens    int      `json:"output_tokens,omitempty"`
	DurationMS      int64    `json:"duration_ms,omitempty"`
	ResultConsumed  bool     `json:"result_consumed,omitempty"`
	ConsumedBy      string   `json:"consumed_by,omitempty"`
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
		result := AwaitAgentsResult{Action: "await_agents", Results: []AwaitAgentResult{}}
		result.NextSteps = awaitAgentsNextSteps(result)
		return result, nil
	}

	ch := make(chan subagent.Notification, 16)
	c.manager.Subscribe(ch)
	defer c.manager.Unsubscribe(ch)

	for {
		result := c.awaitSnapshot(resolved)
		if awaitComplete(result.Results) {
			c.claimAwaitedAgentResults(result.Results)
			result.NextSteps = awaitAgentsNextSteps(result)
			return result, nil
		}
		select {
		case <-ch:
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			result.TimedOut = true
			c.claimAwaitedAgentResults(result.Results)
			result.NextSteps = awaitAgentsNextSteps(result)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, nil
			}
			return result, ctx.Err()
		}
	}
}

func (c *AgentControl) claimAwaitedAgentResults(results []AwaitAgentResult) {
	if c == nil || len(results) == 0 {
		return
	}
	for i := range results {
		result := &results[i]
		if !awaitResultSuppressesCompletion(*result) {
			continue
		}
		resultID := strings.TrimSpace(result.ResultID)
		if resultID == "" {
			continue
		}
		claimed, consumedBy := c.ClaimAgentResultDeliveryID(resultID, agentResultConsumerAwaitAgents)
		if claimed {
			continue
		}
		result.ResultConsumed = true
		result.ConsumedBy = consumedBy
		result.Result = ""
	}
}

func awaitResultSuppressesCompletion(result AwaitAgentResult) bool {
	switch strings.TrimSpace(result.Status) {
	case string(harness.TaskStatusCompleted), string(harness.TaskStatusFailed), string(harness.TaskStatusCancelled), string(harness.TaskStatusAwaitingReport):
		return true
	default:
		return false
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
	return AwaitAgentsResult{Action: "await_agents", Warnings: changedFileOverlapWarnings(results), Results: results}
}

func awaitAgentsNextSteps(result AwaitAgentsResult) []string {
	if len(result.Results) == 0 {
		return []string{"No active or matching child agents were found; continue local work or spawn_agent only if delegation is still needed."}
	}
	if result.TimedOut {
		return []string{
			"Some child agents are still active; continue non-overlapping local work when possible.",
			"Call await_agents again with explicit targets only when synthesis or integration is blocked on their output.",
		}
	}

	steps := make([]string, 0, 4)
	if awaitResultsHaveStatus(result.Results, string(harness.TaskStatusAwaitingReport)) {
		steps = append(steps, "Follow up with agents in awaiting_report or verify their raw result before relying on the handoff; require agent_report for durable evidence.")
	}
	if awaitResultsHaveStatus(result.Results, string(harness.TaskStatusFailed)) || awaitResultsHaveStatus(result.Results, string(subagent.StatusFailed)) {
		steps = append(steps, "Inspect failed agent errors and artifacts, then decide whether to retry with a narrower brief, rollback, or ask the user.")
	}
	if len(result.Warnings) > 0 {
		steps = append(steps, "Resolve await_agents warnings, especially overlapping changed files, before synthesis or merge decisions.")
	}
	if len(steps) == 0 {
		steps = append(steps, "Use agent reports, changed_files, artifacts, and results to synthesize the parent answer.")
	}
	return steps
}

func awaitResultsHaveStatus(results []AwaitAgentResult, status string) bool {
	for _, result := range results {
		if result.Status == status {
			return true
		}
	}
	return false
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
		if delivery := c.ensureAgentResultDelivery(*snap); delivery.ResultID != "" {
			out.ResultID = delivery.ResultID
		}
		ref := c.AgentResultReference(*snap)
		out.Result = ref.Preview
		out.ResultPath = ref.Path
		out.ResultBytes = ref.Bytes
		out.ResultTruncated = ref.Truncated
	}
	if task, ok := c.harnessTask(meta.ID); ok {
		applyHarnessTaskToAwaitResult(&out, task)
	}
	reportPath, artifacts := c.harnessReportForTask(meta.ID)
	out.ReportPath = c.sessionArtifactRef(reportPath)
	out.Artifacts = c.sessionArtifactRefs(artifacts)
	if report, ok := c.harnessReportDetailsForTask(meta.ID); ok {
		out.ChangedFiles = trimStringSlice(report.ChangedFiles)
	}
	if out.Status == string(subagent.StatusCompleted) && reportPath == "" {
		out.Status = string(harness.TaskStatusAwaitingReport)
		out.ReportMissing = true
	}
	if out.Status == string(harness.TaskStatusAwaitingReport) {
		out.ReportMissing = true
	}
	return out
}

func (c *AgentControl) harnessReportDetailsForTask(taskID string) (harness.Report, bool) {
	if c == nil || c.harnessStore == nil {
		return harness.Report{}, false
	}
	report, ok, err := c.harnessStore.ReportForTask(taskID)
	if err != nil || !ok {
		return harness.Report{}, false
	}
	return report, true
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
		ID:           snap.ID,
		Path:         snap.AgentPath,
		TaskName:     snap.TaskName,
		AgentProfile: snap.AgentProfile,
		Role:         snap.Type,
		ParentID:     snap.ParentID,
		Status:       threadStatusFromSubAgent(snap.Status),
	}
}

func awaitResultFromSnapshot(snap subagent.SubAgentSnapshot) AwaitAgentResult {
	out := AwaitAgentResult{
		AgentID:      snap.ID,
		TaskName:     snap.TaskName,
		AgentProfile: snap.AgentProfile,
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

func changedFileOverlapWarnings(results []AwaitAgentResult) []string {
	owners := map[string][]string{}
	for _, result := range results {
		label := result.AgentPath
		if label == "" {
			label = result.AgentID
		}
		for _, file := range trimStringSlice(result.ChangedFiles) {
			owners[file] = append(owners[file], label)
		}
	}
	warnings := make([]string, 0)
	for file, labels := range owners {
		if len(labels) < 2 {
			continue
		}
		warnings = append(warnings, "changed_file_overlap: "+file+" touched by "+strings.Join(labels, ", "))
	}
	return warnings
}

func isAwaitActiveStatus(status string) bool {
	switch status {
	case string(harness.TaskStatusPending), string(harness.TaskStatusQueued), string(harness.TaskStatusRunning):
		return true
	default:
		return false
	}
}
