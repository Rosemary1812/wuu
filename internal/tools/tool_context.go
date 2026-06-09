package tools

import (
	"context"
	"fmt"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
)

func (t *Toolkit) ContextBlocks() []wuucontext.Block {
	if t == nil {
		return nil
	}
	blocks := t.PlanContextBlocks()
	if block, ok := t.TestFailureContextBlock(); ok {
		blocks = append(blocks, block)
	}
	return blocks
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
