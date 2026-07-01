package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
)

func (t *Toolkit) ContextBlocks() []wuucontext.Block {
	if t == nil {
		return nil
	}
	var blocks []wuucontext.Block
	if block, ok := t.AvailableDeferredToolsContextBlock(); ok {
		blocks = append(blocks, block)
	}
	blocks = append(blocks, t.SessionMemoryContextBlocks()...)
	blocks = append(blocks, t.PlanContextBlocks()...)
	if block, ok := t.ActiveFilesContextBlock(); ok {
		blocks = append(blocks, block)
	}
	if block, ok := t.TestFailureContextBlock(); ok {
		blocks = append(blocks, block)
	}
	if block, ok := t.WebEvidenceContextBlock(); ok {
		blocks = append(blocks, block)
	}
	if block, ok := t.ToolResultSummaryContextBlock(); ok {
		blocks = append(blocks, block)
	}
	return blocks
}

func (t *Toolkit) AvailableDeferredToolsContextBlock() (wuucontext.Block, bool) {
	if t == nil || !t.ToolSearchEnabled() {
		return wuucontext.Block{}, false
	}
	names := t.AvailableDeferredToolNames()
	if len(names) == 0 {
		return wuucontext.Block{}, false
	}
	return wuucontext.Block{
		Kind:        wuucontext.BlockAvailableDeferred,
		Title:       "Deferred tool names",
		Source:      "runtime.tool_surface",
		TokenBudget: 600,
		Content:     "<available-deferred-tools>\n" + strings.Join(names, "\n") + "\n</available-deferred-tools>",
	}, true
}

func (t *Toolkit) AvailableDeferredToolNames() []string {
	if t == nil || !t.ToolSearchEnabled() {
		return nil
	}
	surface := t.activeCompiledSurface()
	names := make([]string, 0)
	seen := map[string]struct{}{}
	for _, tool := range t.allKnownTools() {
		if !activeSurfaceAllowsKnownTool(surface, tool) {
			continue
		}
		name := tool.Name()
		if t.toolExposure(name) != ToolExposureDeferred {
			continue
		}
		if !t.toolSearchCanLoadDeferredTool(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (t *Toolkit) SessionMemoryContextBlocks() []wuucontext.Block {
	if t == nil || t.env == nil {
		return nil
	}
	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return nil
	}
	return sessionmemory.RequestContextBlocks(stateDir, t.env.SessionDir)
}

func (t *Toolkit) ToolPolicyContextBlock() (wuucontext.Block, bool) {
	if t == nil {
		return wuucontext.Block{}, false
	}
	return ToolPolicyContextBlockFor(t.toolPolicy, t.permissionBoundary)
}

func ToolPolicyContextBlockFor(policy ToolPolicy, boundary PermissionBoundary) (wuucontext.Block, bool) {
	if !hasRuntimeToolPolicyContext(policy, boundary) {
		return wuucontext.Block{}, false
	}
	var b strings.Builder
	if profile := strings.TrimSpace(boundary.Profile); profile != "" {
		fmt.Fprintf(&b, "permission_profile: %s\n", profile)
		writePermissionBoundaryContext(&b, profile)
	}
	if policy.Profile != "" {
		fmt.Fprintf(&b, "profile: %s\n", policy.Profile)
	}
	if policy.ApprovalPolicy != "" {
		fmt.Fprintf(&b, "approval_policy: %s\n", policy.ApprovalPolicy)
	}
	if policy.DefaultAction != "" {
		fmt.Fprintf(&b, "default_action: %s\n", policy.DefaultAction)
	}
	writeToolPolicyActions(&b, "risk_actions", toolPolicyRiskActionLines(policy.RiskActions))
	writeToolPolicyActions(&b, "kind_actions", toolPolicyKindActionLines(policy.KindActions))
	writeToolPolicyActions(&b, "tool_actions", toolPolicyToolActionLines(policy.ToolActions))
	b.WriteString("note: permission_boundary is enforced before policy and approval. Boundary denials require changing profile; approval flags cannot bypass them. require_approval means ask the user; auto_classify means let auto mode decide.\n")

	return wuucontext.Block{
		Kind:        wuucontext.BlockToolPolicy,
		Title:       "Runtime tool policy",
		Source:      "runtime.tool_policy",
		TokenBudget: 650,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func hasRuntimeToolPolicyContext(policy ToolPolicy, boundary PermissionBoundary) bool {
	return hasConfiguredToolPolicy(policy) || strings.TrimSpace(boundary.Profile) != ""
}

func hasConfiguredToolPolicy(policy ToolPolicy) bool {
	return policy.Profile != "" ||
		policy.ApprovalPolicy != "" ||
		policy.DefaultAction != "" ||
		len(policy.ToolActions) > 0 ||
		len(policy.KindActions) > 0 ||
		len(policy.RiskActions) > 0
}

func writePermissionBoundaryContext(b *strings.Builder, profile string) {
	switch strings.TrimSpace(profile) {
	case PermissionProfileReadOnly:
		b.WriteString("boundary: read_only permits read-only tools and blocks mutations.\n")
	case PermissionProfileWorkspaceWrite:
		b.WriteString("boundary: workspace_write permits workspace edits but blocks destructive runtime actions and outside-workspace paths.\n")
	case PermissionProfileDangerFullAccess:
		b.WriteString("boundary: danger_full_access removes Wuu workspace, network, sensitive-path, and default command-policy hard guards; explicit policy, tool limits, and OS permissions still apply.\n")
	default:
		fmt.Fprintf(b, "boundary: unknown permission profile %s; stop and report the invalid runtime policy.\n", profile)
	}
}

func writeToolPolicyActions(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, line := range lines {
		fmt.Fprintf(b, "- %s\n", line)
	}
}

func toolPolicyRiskActionLines(actions map[ToolRisk]ToolPolicyAction) []string {
	if len(actions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(actions))
	for risk := range actions {
		keys = append(keys, string(risk))
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+": "+string(actions[ToolRisk(key)]))
	}
	return out
}

func toolPolicyKindActionLines(actions map[ToolKind]ToolPolicyAction) []string {
	if len(actions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(actions))
	for kind := range actions {
		keys = append(keys, string(kind))
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+": "+string(actions[ToolKind(key)]))
	}
	return out
}

func toolPolicyToolActionLines(actions map[string]ToolPolicyAction) []string {
	if len(actions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(actions))
	for name := range actions {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+": "+string(actions[key]))
	}
	return out
}

func (t *Toolkit) ActiveFilesContextBlock() (wuucontext.Block, bool) {
	if t == nil || t.env == nil {
		return wuucontext.Block{}, false
	}
	entries := t.env.ReadEntries()
	if len(entries) == 0 {
		return wuucontext.Block{}, false
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return t.env.NormalizeDisplayPath(paths[i]) < t.env.NormalizeDisplayPath(paths[j])
	})
	const maxFiles = 12
	listed := paths
	if len(listed) > maxFiles {
		listed = listed[:maxFiles]
	}

	var b strings.Builder
	b.WriteString("read_files:\n")
	for _, absPath := range listed {
		entry := entries[absPath]
		status := "current"
		if info, err := os.Stat(absPath); err != nil || info.IsDir() || !readEntryMatchesInfo(entry, info) {
			status = "possibly_stale"
		} else if entry.BaselineOnly {
			status = "current_baseline"
		}
		fmt.Fprintf(&b, "- path=%s status=%s file_sha=%s size_bytes=%d read_range=%s\n",
			compactContextLine(redactToolOutput(t.env.NormalizeDisplayPath(absPath))),
			status,
			formatFileSHA(entry.ContentSHA256),
			entry.Size,
			activeFileReadRange(entry),
		)
	}
	if omitted := len(paths) - len(listed); omitted > 0 {
		fmt.Fprintf(&b, "omitted_files: %d\n", omitted)
	}
	b.WriteString("note: file bodies are omitted; use the previous read_file result as evidence only while status=current. status=current_baseline means this agent wrote the current content but should call read_file if it needs the body. Otherwise read_file again.\n")

	return wuucontext.Block{
		Kind:        wuucontext.BlockActiveFiles,
		Title:       "Files read in this session",
		Source:      "read_file",
		TokenBudget: 700,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func (t *Toolkit) TestFailureContextBlock() (wuucontext.Block, bool) {
	if t == nil || t.env == nil {
		return wuucontext.Block{}, false
	}
	failure, ok := t.env.LatestTestFailure()
	if !ok {
		return wuucontext.Block{}, false
	}
	currentRevision := workspaceRevision(context.Background(), t.env.RootDir)
	status := "current"
	if currentRevision != "" && failure.Revision != "" && currentRevision != failure.Revision {
		status = "possibly_stale"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "status: %s\n", status)
	fmt.Fprintf(&b, "command: %s\n", strings.TrimSpace(failure.Command))
	if strings.TrimSpace(failure.Scope) != "" {
		fmt.Fprintf(&b, "scope: %s\n", strings.TrimSpace(failure.Scope))
	}
	if strings.TrimSpace(failure.Purpose) != "" {
		fmt.Fprintf(&b, "purpose: %s\n", redactToolOutput(strings.TrimSpace(failure.Purpose)))
	}
	fmt.Fprintf(&b, "exit_code: %d\n", failure.ExitCode)
	fmt.Fprintf(&b, "timed_out: %t\n", failure.TimedOut)
	fmt.Fprintf(&b, "duration_ms: %d\n", failure.DurationMS)
	if failure.Revision != "" {
		fmt.Fprintf(&b, "failure_revision: %s\n", failure.Revision)
	}
	if currentRevision != "" {
		fmt.Fprintf(&b, "current_revision: %s\n", currentRevision)
	}
	if failure.FullLogRef != "" {
		fmt.Fprintf(&b, "full_log_ref: %s\n", failure.FullLogRef)
	}
	writeTestFailureSummaryContext(&b, failure.FailureSummary)
	if status == "possibly_stale" {
		b.WriteString("next_suggestion: workspace changed since this failure; rerun targeted verification before trusting it as current.\n")
	} else {
		b.WriteString("next_suggestion: inspect implicated files, form a hypothesis, patch minimally, then rerun targeted verification.\n")
	}

	return wuucontext.Block{
		Kind:        wuucontext.BlockTestFailures,
		Title:       "Latest test failure",
		Source:      "bash",
		TokenBudget: 900,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func activeFileReadRange(entry ReadFileEntry) string {
	start := entry.Offset
	if start <= 0 {
		start = 1
	}
	if entry.Limit > 0 {
		return fmt.Sprintf("%d-%d", start, start+entry.Limit-1)
	}
	return fmt.Sprintf("%d-EOF", start)
}

func (t *Toolkit) WebEvidenceContextBlock() (wuucontext.Block, bool) {
	if t == nil || t.env == nil {
		return wuucontext.Block{}, false
	}
	entries := t.env.WebEvidenceEntries()
	if len(entries) == 0 {
		return wuucontext.Block{}, false
	}
	const maxEntries = 8
	start := 0
	if len(entries) > maxEntries {
		start = len(entries) - maxEntries
	}

	var b strings.Builder
	b.WriteString("recent_web_evidence:\n")
	for i, entry := range entries[start:] {
		evidence := entry.Evidence
		status := "ok"
		if strings.TrimSpace(entry.Error) != "" {
			status = "error"
		}
		fmt.Fprintf(&b, "- #%d id=%s tool=%s kind=%s status=%s source_tier=%s source=%s",
			start+i+1,
			strings.TrimSpace(evidence.ID),
			strings.TrimSpace(entry.ToolName),
			strings.TrimSpace(evidence.Kind),
			status,
			strings.TrimSpace(evidence.SourceTier),
			compactContextLine(redactToolOutput(evidence.Source)),
		)
		if evidence.RetrievedAt != "" {
			fmt.Fprintf(&b, " retrieved_at=%s", strings.TrimSpace(evidence.RetrievedAt))
		}
		if evidence.VersionMatchedToRepo != "" {
			fmt.Fprintf(&b, " version_matched_to_repo=%s", compactContextLine(redactToolOutput(evidence.VersionMatchedToRepo)))
		}
		if entry.ResultCount > 0 {
			fmt.Fprintf(&b, " result_count=%d", entry.ResultCount)
		}
		if entry.StatusCode > 0 {
			fmt.Fprintf(&b, " status_code=%d", entry.StatusCode)
		}
		if strings.TrimSpace(entry.ContentType) != "" {
			fmt.Fprintf(&b, " content_type=%s", compactContextLine(redactToolOutput(entry.ContentType)))
		}
		if entry.Size > 0 {
			fmt.Fprintf(&b, " size_bytes=%d", entry.Size)
		}
		if entry.Truncated {
			b.WriteString(" truncated=true")
		}
		if strings.TrimSpace(entry.Error) != "" {
			fmt.Fprintf(&b, " error=%s", compactContextLine(redactToolOutput(entry.Error)))
		}
		b.WriteString("\n")
	}
	if omitted := len(entries) - (len(entries) - start); omitted > 0 {
		fmt.Fprintf(&b, "omitted_older_evidence: %d\n", omitted)
	}
	b.WriteString("note: web content bodies and search snippets are intentionally omitted; treat web claims as evidence and verify them against repo dependency versions before editing.\n")

	return wuucontext.Block{
		Kind:        wuucontext.BlockWebEvidence,
		Title:       "Recent web evidence",
		Source:      "web_tools",
		TokenBudget: 800,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func (t *Toolkit) ToolResultSummaryContextBlock() (wuucontext.Block, bool) {
	if t == nil || t.env == nil {
		return wuucontext.Block{}, false
	}
	records := t.ToolTelemetry()
	if len(records) == 0 {
		return wuucontext.Block{}, false
	}
	const maxRecords = 8
	start := 0
	if len(records) > maxRecords {
		start = len(records) - maxRecords
	}

	var b strings.Builder
	b.WriteString("recent_tool_calls:\n")
	currentRevision := workspaceRevision(context.Background(), t.env.RootDir)
	for i, record := range records[start:] {
		status := "ok"
		if !record.Success {
			status = "error"
		}
		fmt.Fprintf(&b, "- #%d name=%s status=%s",
			start+i+1,
			strings.TrimSpace(record.Name),
			status,
		)
		if record.PolicyAction != "" && record.PolicyAction != ToolPolicyAllow {
			fmt.Fprintf(&b, " policy=%s", record.PolicyAction)
		}
		if record.ResultAction != "" {
			fmt.Fprintf(&b, " result_action=%s", compactContextLine(redactToolOutput(record.ResultAction)))
		}
		if evidenceStatus := toolEvidenceStatus(record, currentRevision); evidenceStatus != "" {
			fmt.Fprintf(&b, " evidence_status=%s", evidenceStatus)
		}
		if record.ResultBudgeted {
			b.WriteString(" result_budgeted=true")
		}
		if record.ResultRef != "" {
			fmt.Fprintf(&b, " result_ref=%s", compactContextLine(redactToolOutput(contextArtifactRef(t.env, record.ResultRef))))
		}
		if record.ApprovalRef != "" {
			fmt.Fprintf(&b, " approval_ref=%s", compactContextLine(redactToolOutput(contextArtifactRef(t.env, record.ApprovalRef))))
		}
		if record.PatchRiskSummary != nil {
			fmt.Fprintf(&b, " patch_risk=%s", compactToolPatchRisk(*record.PatchRiskSummary))
		}
		if len(record.ArtifactRefs) > 0 {
			fmt.Fprintf(&b, " artifact_refs=%s", strings.Join(redactedContextArtifactRefs(t.env, record.ArtifactRefs, 4), ","))
		}
		if strings.TrimSpace(record.Error) != "" {
			if record.ErrorKind != "" {
				fmt.Fprintf(&b, " error_kind=%s", compactContextLine(redactToolOutput(record.ErrorKind)))
			}
			fmt.Fprintf(&b, " error=%s", compactContextLine(redactToolOutput(record.Error)))
		}
		b.WriteString("\n")
	}
	if repeated := repeatedToolArguments(records[start:]); len(repeated) > 0 {
		b.WriteString("repeated_arguments:\n")
		for _, item := range repeated {
			fmt.Fprintf(&b, "- name=%s args_sha256=%s count=%d\n", item.ToolName, item.ArgumentsSHA256, item.Count)
		}
		b.WriteString("warning: repeated identical tool inputs can indicate a loop; inspect prior evidence before retrying.\n")
	}
	b.WriteString("note: args and bodies omitted; use refs when needed.\n")

	return wuucontext.Block{
		Kind:        wuucontext.BlockToolResultSummary,
		Title:       "Recent tool result summary",
		Source:      "tool_telemetry",
		TokenBudget: 800,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func toolEvidenceStatus(record ToolExecutionRecord, currentRevision string) string {
	currentRevision = strings.TrimSpace(currentRevision)
	revisionAfter := strings.TrimSpace(record.RevisionAfter)
	if currentRevision == "" || revisionAfter == "" {
		return ""
	}
	if revisionAfter == currentRevision {
		return "current"
	}
	return "possibly_stale"
}

type repeatedToolArgument struct {
	ToolName        string
	ArgumentsSHA256 string
	Count           int
}

func repeatedToolArguments(records []ToolExecutionRecord) []repeatedToolArgument {
	counts := map[string]repeatedToolArgument{}
	for _, record := range records {
		toolName := strings.TrimSpace(record.Name)
		argumentsSHA256 := strings.TrimSpace(record.ArgumentsSHA256)
		if toolName == "" || argumentsSHA256 == "" {
			continue
		}
		key := toolName + "\x00" + argumentsSHA256
		item := counts[key]
		if item.Count == 0 {
			item.ToolName = toolName
			item.ArgumentsSHA256 = argumentsSHA256
		}
		item.Count++
		counts[key] = item
	}
	repeated := make([]repeatedToolArgument, 0, len(counts))
	for _, item := range counts {
		if item.Count > 1 {
			repeated = append(repeated, item)
		}
	}
	sort.Slice(repeated, func(i, j int) bool {
		if repeated[i].ToolName != repeated[j].ToolName {
			return repeated[i].ToolName < repeated[j].ToolName
		}
		return repeated[i].ArgumentsSHA256 < repeated[j].ArgumentsSHA256
	})
	return repeated
}

func compactToolPatchRisk(risk ToolPatchRisk) string {
	parts := []string(nil)
	if level := strings.TrimSpace(risk.RiskLevel); level != "" {
		parts = append(parts, "level="+level)
	}
	if risk.FileCount > 0 {
		parts = append(parts, fmt.Sprintf("files=%d", risk.FileCount))
	}
	if risk.HunkCount > 0 {
		parts = append(parts, fmt.Sprintf("hunks=%d", risk.HunkCount))
	}
	if risk.AddedLines > 0 || risk.DeletedLines > 0 {
		parts = append(parts, fmt.Sprintf("+%d/-%d", risk.AddedLines, risk.DeletedLines))
	}
	if risk.MultiFile {
		parts = append(parts, "multi_file=true")
	}
	if risk.ContainsDelete {
		parts = append(parts, "contains_delete=true")
	}
	if risk.ContainsMove {
		parts = append(parts, "contains_move=true")
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ",")
}

func writeTestFailureSummaryContext(b *strings.Builder, summary testFailureSummary) {
	if len(summary.FailingTests) > 0 {
		b.WriteString("failing_tests:\n")
		for _, test := range limitStrings(summary.FailingTests, 8) {
			fmt.Fprintf(b, "- %s\n", test)
		}
	}
	if len(summary.Locations) > 0 {
		b.WriteString("locations:\n")
		for i, loc := range summary.Locations {
			if i >= 8 {
				break
			}
			fmt.Fprintf(b, "- %s", loc.Path)
			if loc.Line > 0 {
				fmt.Fprintf(b, ":%d", loc.Line)
				if loc.Column > 0 {
					fmt.Fprintf(b, ":%d", loc.Column)
				}
			}
			if strings.TrimSpace(loc.Text) != "" {
				fmt.Fprintf(b, " %s", strings.TrimSpace(loc.Text))
			}
			b.WriteString("\n")
		}
	}
	if len(summary.Indicators) > 0 {
		b.WriteString("indicators:\n")
		for _, indicator := range limitStrings(summary.Indicators, 8) {
			fmt.Fprintf(b, "- %s\n", indicator)
		}
	}
	if len(summary.Snippets) > 0 {
		b.WriteString("snippets:\n")
		for _, snippet := range limitStrings(summary.Snippets, 3) {
			fmt.Fprintf(b, "- %s\n", compactContextLine(snippet))
		}
	}
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func compactContextLine(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > 240 {
		return value[:240] + "...[truncated]"
	}
	return value
}

func redactedContextArtifactRefs(env *Env, values []string, limit int) []string {
	values = limitStrings(values, limit)
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, compactContextLine(redactToolOutput(contextArtifactRef(env, value))))
	}
	return out
}

func contextArtifactRef(env *Env, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || env == nil {
		return value
	}
	if rel, ok := relativeContextRef("$SESSION_DIR", env.SessionDir, value); ok {
		return rel
	}
	if rel, ok := relativeContextRef("$STATE_DIR", env.StateDir, value); ok {
		return rel
	}
	if rel, ok := relativeContextRef("$WORKSPACE", env.RootDir, value); ok {
		return rel
	}
	return value
}

func relativeContextRef(prefix, base, value string) (string, bool) {
	base = strings.TrimSpace(base)
	if prefix == "" || base == "" || value == "" {
		return "", false
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", false
	}
	absValue, err := filepath.Abs(value)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absBase, absValue)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return prefix + "/" + filepath.ToSlash(rel), true
}
