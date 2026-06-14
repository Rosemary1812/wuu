package loop

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ExternalReport struct {
	Source       string
	SourceID     string
	Outcome      string
	Summary      string
	ReportPath   string
	Blockers     []string
	ChangedFiles []string
	Verification []string
	Artifacts    []string
	NextSteps    []string
	CreatedAt    time.Time
}

func (s *Store) RecordExternalReport(report ExternalReport) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = s.now()
	}
	source := strings.TrimSpace(report.Source)
	if source == "" {
		source = "external_report"
	}
	sourceID := strings.TrimSpace(report.SourceID)
	if sourceID == "" {
		sourceID = strings.TrimSpace(report.ReportPath)
	}
	if sourceID == "" {
		sourceID = report.CreatedAt.Format(time.RFC3339Nano)
	}
	summary := strings.TrimSpace(report.Summary)
	if summary != "" && !progressContainsSource(state.Progress, source, sourceID) {
		state.Progress = append(state.Progress, ProgressEntry{
			Step:      StepExecution,
			Source:    source,
			SourceID:  sourceID,
			Message:   summary,
			CreatedAt: report.CreatedAt,
		})
		state.CurrentStep = StepExecution
		if state.Status == StatusPending {
			state.Status = StatusRunning
		}
	}
	state.ModifiedFiles = appendUniqueStrings(state.ModifiedFiles, report.ChangedFiles)
	state.Artifacts = appendExternalArtifactRefs(state.Artifacts, source, sourceID, "report", []string{report.ReportPath}, report.CreatedAt)
	state.Artifacts = appendExternalArtifactRefs(state.Artifacts, source, sourceID, "evidence", report.Artifacts, report.CreatedAt)
	if len(trimLoopStrings(report.Verification)) > 0 || len(trimLoopStrings(report.Blockers)) > 0 {
		state.TestResults = upsertTestResult(state.TestResults, TestResult{
			Name:      source + ":" + sourceID,
			Passed:    externalReportPassed(report.Outcome, report.Blockers),
			Output:    strings.Join(trimLoopStrings(report.Verification), "\n"),
			Error:     strings.Join(trimLoopStrings(report.Blockers), "\n"),
			CreatedAt: report.CreatedAt,
		})
	}
	state.NextSteps = appendUniqueStrings(state.NextSteps, report.NextSteps)
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{
		Type:     "agent_report_synced",
		LoopID:   state.ID,
		Step:     StepExecution,
		Message:  summary,
		Artifact: strings.TrimSpace(report.ReportPath),
		Data: map[string]any{
			"source":         source,
			"source_id":      sourceID,
			"outcome":        strings.TrimSpace(report.Outcome),
			"changed_files":  len(trimLoopStrings(report.ChangedFiles)),
			"verification":   len(trimLoopStrings(report.Verification)),
			"artifact_paths": len(trimLoopStrings(report.Artifacts)),
		},
	})
}

func progressContainsSource(entries []ProgressEntry, source, sourceID string) bool {
	for _, entry := range entries {
		if entry.Source == source && entry.SourceID == sourceID {
			return true
		}
	}
	return false
}

func appendExternalArtifactRefs(items []Artifact, source, sourceID, kind string, paths []string, createdAt time.Time) []Artifact {
	for i, path := range trimLoopStrings(paths) {
		if artifactPathExists(items, path) {
			continue
		}
		items = append(items, Artifact{
			Name:      externalArtifactName(source, sourceID, kind, path, i),
			Path:      path,
			Kind:      kind,
			Source:    source,
			SourceID:  sourceID,
			CreatedAt: createdAt,
		})
	}
	return items
}

func artifactPathExists(items []Artifact, path string) bool {
	for _, item := range items {
		if item.Path == path {
			return true
		}
	}
	return false
}

func externalArtifactName(source, sourceID, kind, path string, index int) string {
	name := sanitizeFailureKind(source + "_" + sourceID + "_" + kind)
	ext := filepath.Ext(path)
	if ext == "" {
		ext = ".ref"
	}
	if index > 0 {
		return fmt.Sprintf("%s_%d%s", name, index+1, ext)
	}
	return name + ext
}

func externalReportPassed(outcome string, blockers []string) bool {
	if len(trimLoopStrings(blockers)) > 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "", "completed", "complete", "success", "passed", "pass":
		return true
	default:
		return false
	}
}

func upsertTestResult(items []TestResult, next TestResult) []TestResult {
	for i := range items {
		if items[i].Name == next.Name {
			items[i] = next
			return items
		}
	}
	return append(items, next)
}

func appendUniqueStrings(values []string, additions ...[]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	for _, group := range additions {
		for _, value := range group {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}

func trimLoopStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
