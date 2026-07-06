package agentcontrol

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// HelpMeCompletionRewrite builds the bounded HelpMe joint-compact history
// rewrite for a just-completed helpme_recovery helper. It is the
// completion-wakeup replacement for the old await_agents-tool delivery: the
// subagent-completion path (forwardAgentNotifications -> drain) calls it when a
// root child finishes, so the main agent's polluted context is replaced by the
// recovery compact without any explicit blocking await.
//
// It is a one-shot: MarkHelpMeRecoveryApplied gates it so a helper is only ever
// rewritten once. It returns nil for any agent that is not a completed
// helpme_recovery helper with a structured report and an unapplied registered
// recovery (the legacy trace-migration and trace-path reference from the old
// await path are intentionally dropped — recovery state is always registered at
// spawn time now).
func (c *AgentControl) HelpMeCompletionRewrite(snap subagent.SubAgentSnapshot) *compact.HelpMeHistoryRewrite {
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
	// Consume the recovery atomically: MarkHelpMeRecoveryApplied returns false
	// when another path already applied it, dropping this rewrite instead of
	// double-firing a wholesale history replacement.
	if !c.MarkHelpMeRecoveryApplied(result.AgentID) {
		return nil
	}
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
