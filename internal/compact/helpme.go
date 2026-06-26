package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	HelpMeToolName               = "helpme"
	HelpMeHistoryRewriteKind     = "helpme_joint_compact"
	HelpMeJointCompactPrefix     = "[HelpMe joint compact]"
	helpMeCompactMaxSectionBytes = 6000
	helpMeCompactMaxItemBytes    = 1200
)

// HelpMeHistoryRewrite is the compact marker embedded in helpme tool output.
// The agent loop consumes it only after the tool result has been appended.
type HelpMeHistoryRewrite struct {
	Kind         string `json:"kind"`
	Content      string `json:"content"`
	AgentID      string `json:"agent_id,omitempty"`
	AgentPath    string `json:"agent_path,omitempty"`
	ResultPath   string `json:"result_path,omitempty"`
	ReportPath   string `json:"report_path,omitempty"`
	TraceSummary string `json:"trace_summary,omitempty"`
}

type helpMeToolEnvelope struct {
	Action         string                `json:"action"`
	Status         string                `json:"status"`
	HistoryRewrite *HelpMeHistoryRewrite `json:"history_rewrite,omitempty"`
}

// HelpMeJointCompactInput is the structured material used to build the
// replacement context after a fresh helper agent finishes.
type HelpMeJointCompactInput struct {
	OriginalGoal         string
	CurrentUnderstanding string
	Ask                  string
	Reason               string
	Constraints          []string
	FailedAttempts       []string
	Evidence             []string
	HelperStatus         string
	HelperAgentID        string
	HelperAgentPath      string
	HelperResult         string
	HelperResultPath     string
	HelperReportPath     string
	HelperError          string
	ReportOutcome        string
	ReportSummary        string
	ChangedFiles         []string
	WorkDone             []string
	Blockers             []string
	Risks                []string
	Verification         []string
	ReportEvidence       []string
	NextSteps            []string
	Artifacts            []string
}

func BuildHelpMeJointCompactContent(input HelpMeJointCompactInput) string {
	var b strings.Builder
	b.WriteString(HelpMeJointCompactPrefix)
	b.WriteString("\nThis context was rewritten after a HelpMe recovery. Treat this as the current task state. Do not repeat the ruled-out parent-agent paths unless new evidence changes them.\n\n")

	writeHelpMeField(&b, "Original user goal", input.OriginalGoal)
	writeHelpMeField(&b, "Parent rescue reason", input.Reason)
	writeHelpMeField(&b, "Parent understanding before recovery", input.CurrentUnderstanding)
	writeHelpMeField(&b, "Recovery ask given to helper", input.Ask)
	writeHelpMeList(&b, "User/project constraints to preserve", input.Constraints)
	writeHelpMeList(&b, "Failed or low-confidence parent paths", input.FailedAttempts)
	writeHelpMeList(&b, "Parent evidence handed to helper", input.Evidence)

	b.WriteString("## Fresh helper result\n")
	writeHelpMeField(&b, "Status", input.HelperStatus)
	writeHelpMeField(&b, "Agent id", input.HelperAgentID)
	writeHelpMeField(&b, "Agent path", input.HelperAgentPath)
	writeHelpMeField(&b, "Result path", input.HelperResultPath)
	writeHelpMeField(&b, "Report path", input.HelperReportPath)
	writeHelpMeField(&b, "Error", input.HelperError)
	writeHelpMeField(&b, "Result summary", input.HelperResult)
	writeHelpMeField(&b, "Report outcome", input.ReportOutcome)
	writeHelpMeField(&b, "Report summary", input.ReportSummary)
	writeHelpMeList(&b, "Changed files", input.ChangedFiles)
	writeHelpMeList(&b, "Work done", input.WorkDone)
	writeHelpMeList(&b, "Verification", input.Verification)
	writeHelpMeList(&b, "Helper evidence", input.ReportEvidence)
	writeHelpMeList(&b, "Blockers", input.Blockers)
	writeHelpMeList(&b, "Risks", input.Risks)
	writeHelpMeList(&b, "Artifacts", input.Artifacts)

	b.WriteString("## Correct continuation path\n")
	nextSteps := trimStringSlice(input.NextSteps)
	if len(nextSteps) > 0 {
		for _, item := range nextSteps {
			fmt.Fprintf(&b, "- %s\n", truncateHelpMeItem(item))
		}
	} else {
		b.WriteString("- Continue from the helper's verified findings and workspace state.\n")
	}
	b.WriteString("- Use the helper report/result paths for raw trace when details are needed.\n")
	b.WriteString("- Keep the old parent-agent failed paths as negative evidence, not as the default plan.\n")

	return strings.TrimSpace(b.String())
}

func RewriteHistoryFromHelpMeToolMessages(messages []providers.ChatMessage, toolMessages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	rewrite, ok, err := HelpMeRewriteFromToolMessages(toolMessages)
	if err != nil || !ok {
		return nil, false, err
	}
	return RewriteHistoryWithHelpMeCompact(messages, rewrite.Content), true, nil
}

func RewriteHistoryFromHelpMeToolMessagesWithContext(_ context.Context, messages []providers.ChatMessage, toolMessages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	return RewriteHistoryFromHelpMeToolMessages(messages, toolMessages)
}

func HelpMeRewriteFromToolMessages(toolMessages []providers.ChatMessage) (HelpMeHistoryRewrite, bool, error) {
	for _, msg := range toolMessages {
		if !strings.EqualFold(strings.TrimSpace(msg.Name), HelpMeToolName) {
			continue
		}
		rewrite, ok, err := HelpMeRewriteFromToolMessage(msg)
		if err != nil || ok {
			return rewrite, ok, err
		}
	}
	return HelpMeHistoryRewrite{}, false, nil
}

func HelpMeRewriteFromToolMessage(msg providers.ChatMessage) (HelpMeHistoryRewrite, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(msg.Name), HelpMeToolName) {
		return HelpMeHistoryRewrite{}, false, nil
	}
	var envelope helpMeToolEnvelope
	if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
		return HelpMeHistoryRewrite{}, false, fmt.Errorf("helpme history rewrite: parse tool result: %w", err)
	}
	if envelope.HistoryRewrite == nil {
		return HelpMeHistoryRewrite{}, false, nil
	}
	rewrite := *envelope.HistoryRewrite
	if strings.TrimSpace(rewrite.Kind) != HelpMeHistoryRewriteKind {
		return HelpMeHistoryRewrite{}, false, nil
	}
	rewrite.Content = strings.TrimSpace(rewrite.Content)
	if rewrite.Content == "" {
		return HelpMeHistoryRewrite{}, false, nil
	}
	return rewrite, true, nil
}

func RewriteHistoryWithHelpMeCompact(messages []providers.ChatMessage, content string) []providers.ChatMessage {
	content = strings.TrimSpace(content)
	if content == "" {
		return providers.CloneChatMessages(messages)
	}
	systemPrefix, previousSummary, previousSummaryDiscoveredTools, conversation := splitLeadingSystemMessages(messages)
	if previousSummary != "" {
		content = content + "\n\n## Previous compact summary before HelpMe\n" + truncateHelpMeSection(previousSummary)
	}
	discovered := providers.MergeLoadableToolDefinitions(
		previousSummaryDiscoveredTools,
		providers.DiscoveredToolsFromMessages(conversation),
	)
	rewritten := providers.CloneChatMessages(systemPrefix)
	rewritten = append(rewritten, providers.ChatMessage{
		Role:            "system",
		Content:         content,
		DiscoveredTools: discovered,
	})
	return rewritten
}

func writeHelpMeField(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(b, "## %s\n%s\n\n", label, truncateHelpMeSection(value))
}

func writeHelpMeList(b *strings.Builder, label string, values []string) {
	values = trimStringSlice(values)
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n", label)
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", truncateHelpMeItem(value))
	}
	b.WriteByte('\n')
}

func truncateHelpMeSection(value string) string {
	return stringutil.Truncate(strings.TrimSpace(value), helpMeCompactMaxSectionBytes, "\n[truncated; inspect trace paths for full content]")
}

func truncateHelpMeItem(value string) string {
	return stringutil.Truncate(strings.TrimSpace(value), helpMeCompactMaxItemBytes, " [truncated]")
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
