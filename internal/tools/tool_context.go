package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
)

func (t *Toolkit) ContextBlocks() []wuucontext.Block {
	if t == nil {
		return nil
	}
	blocks := t.PlanContextBlocks()
	if block, ok := t.ActiveFilesContextBlock(); ok {
		blocks = append(blocks, block)
	}
	if block, ok := t.TestFailureContextBlock(); ok {
		blocks = append(blocks, block)
	}
	if block, ok := t.ToolResultSummaryContextBlock(); ok {
		blocks = append(blocks, block)
	}
	return blocks
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
	b.WriteString("note: file bodies are omitted; use the previous read_file result as evidence only while status=current, otherwise read_file again.\n")

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
		Source:      "run_test",
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
	for i, record := range records[start:] {
		status := "ok"
		if !record.Success {
			status = "error"
		}
		fmt.Fprintf(&b, "- #%d name=%s kind=%s status=%s risk=%s duration_ms=%d",
			start+i+1,
			strings.TrimSpace(record.Name),
			record.Kind,
			status,
			record.Risk,
			record.DurationMS,
		)
		if record.PolicyAction != "" {
			fmt.Fprintf(&b, " policy=%s", record.PolicyAction)
		}
		if record.RevisionBefore != "" {
			fmt.Fprintf(&b, " revision_before=%s", record.RevisionBefore)
		}
		if record.RevisionAfter != "" {
			fmt.Fprintf(&b, " revision_after=%s", record.RevisionAfter)
		}
		fmt.Fprintf(&b, " raw_output_bytes=%d returned_output_bytes=%d", record.RawOutputBytes, record.ReturnedOutputBytes)
		if record.ResultBudgeted {
			b.WriteString(" result_budgeted=true")
		}
		if record.ResultRef != "" {
			fmt.Fprintf(&b, " result_ref=%s", compactContextLine(redactToolOutput(record.ResultRef)))
		}
		if len(record.ArtifactRefs) > 0 {
			fmt.Fprintf(&b, " artifact_refs=%s", strings.Join(redactedCompactStrings(record.ArtifactRefs, 4), ","))
		}
		if strings.TrimSpace(record.Error) != "" {
			fmt.Fprintf(&b, " error=%s", compactContextLine(redactToolOutput(record.Error)))
		}
		b.WriteString("\n")
	}
	b.WriteString("note: tool arguments and output bodies are intentionally omitted; use artifact/result refs when needed.\n")

	return wuucontext.Block{
		Kind:        wuucontext.BlockToolResultSummary,
		Title:       "Recent tool result summary",
		Source:      "tool_telemetry",
		TokenBudget: 800,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
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

func redactedCompactStrings(values []string, limit int) []string {
	values = limitStrings(values, limit)
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, compactContextLine(redactToolOutput(value)))
	}
	return out
}
