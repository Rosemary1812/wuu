package agentcontrol

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// PrepareHelpMeCompletionRewrite builds a bounded HelpMe joint-compact rewrite
// without consuming its one-shot recovery state. A caller that persists the
// rewrite must call MarkHelpMeRecoveryApplied only after that persistence
// succeeds; a failed write can then retry without losing the recovery.
func (c *AgentControl) PrepareHelpMeCompletionRewrite(snap subagent.SubAgentSnapshot) *compact.HelpMeHistoryRewrite {
	if c == nil {
		return nil
	}
	// Cheap snapshot-only guard first: the overwhelming majority of completing
	// agents are ordinary children, and this method runs on every completion
	// wakeup. Skip all harness/store I/O unless the snapshot itself already
	// looks like a HelpMe recovery helper.
	if !isHelpMeRecoveryResult(snap.TaskName, snap.AgentPath) {
		return nil
	}
	if !c.isRootChildSnapshot(snap) {
		return nil
	}
	result := c.awaitResultForTarget(awaitTarget{Meta: metadataFromSnapshot(snap), Found: true})
	if !isHelpMeRecoveryResult(result.TaskName, result.AgentPath) {
		return nil
	}
	if strings.TrimSpace(result.Status) != string(subagent.StatusCompleted) || result.ReportMissing {
		return nil
	}
	recovery, ok := c.HelpMeRecoveryForHelper(result.AgentID)
	if !ok || recovery.Applied {
		return nil
	}
	report, reportOK := c.AgentReportDetailsForTask(result.AgentID)
	if !reportOK {
		return nil
	}

	brief := recovery.Brief
	reason := strings.TrimSpace(brief.Reason)
	if reason == "" {
		reason = "The main agent requested a fresh-context recovery."
	}
	originalGoal := helpMeFirstNonEmpty(brief.OriginalGoal, "Continue the user's current coding task.")
	ask := helpMeFirstNonEmpty(brief.Ask, originalGoal)
	content := compact.BuildHelpMeJointCompactContent(compact.HelpMeJointCompactInput{
		OriginalGoal:           originalGoal,
		ParentExecutionJournal: recovery.ParentExecutionJournal,
		CurrentUnderstanding:   brief.CurrentUnderstanding,
		Ask:                    ask,
		Reason:                 reason,
		Constraints:            brief.Constraints,
		FailedAttempts:         brief.FailedAttempts,
		Evidence:               trimStringSlice(brief.Evidence),
		HelperStatus:           result.Status,
		HelperAgentID:          result.AgentID,
		HelperAgentPath:        result.AgentPath,
		HelperResult:           result.Result,
		HelperResultPath:       result.ResultPath,
		HelperReportPath:       report.ReportPath,
		HelperError:            result.Error,
		ReportOutcome:          report.Outcome,
		ReportSummary:          report.Summary,
		ChangedFiles:           report.ChangedFiles,
		WorkDone:               report.WorkDone,
		Blockers:               report.Blockers,
		Risks:                  report.Risks,
		Verification:           report.Verification,
		ReportEvidence:         helpMeEvidenceStrings(report.Evidence),
		NextSteps:              report.NextSteps,
		Artifacts:              report.Artifacts,
	})
	return &compact.HelpMeHistoryRewrite{
		Kind:         compact.HelpMeHistoryRewriteKind,
		Content:      content,
		AgentID:      result.AgentID,
		AgentPath:    result.AgentPath,
		ResultPath:   result.ResultPath,
		ReportPath:   report.ReportPath,
		TraceSummary: "Main history was replaced by a bounded HelpMe compact built from the helper report and result references; raw main/helper transcripts were not merged.",
	}
}

// isHelpMeRecoveryResult reports whether an agent identity belongs to a HelpMe
// recovery helper (spawned by the helpme tool with the helpme_recovery worker
// type and task-name prefix).
func isHelpMeRecoveryResult(taskName, agentPath string) bool {
	taskName = strings.TrimSpace(taskName)
	agentPath = strings.TrimSpace(agentPath)
	return strings.HasPrefix(taskName, "helpme_recovery_") || strings.Contains(agentPath, "/helpme_recovery")
}

func helpMeFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func helpMeEvidenceStrings(evidence []ReportEvidence) []string {
	out := make([]string, 0, len(evidence))
	for _, ref := range evidence {
		var parts []string
		if ref.Type != "" {
			parts = append(parts, ref.Type)
		}
		if ref.Path != "" {
			path := ref.Path
			if ref.Line > 0 {
				path = fmt.Sprintf("%s:%d", path, ref.Line)
			}
			parts = append(parts, path)
		}
		if ref.Command != "" {
			parts = append(parts, ref.Command)
		}
		if ref.Output != "" {
			parts = append(parts, ref.Output)
		}
		if ref.Note != "" {
			parts = append(parts, ref.Note)
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, " - "))
		}
	}
	return out
}
