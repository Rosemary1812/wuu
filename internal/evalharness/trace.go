package evalharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type TraceEvent struct {
	Type      string    `json:"type"`
	TaskID    string    `json:"task_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Data      any       `json:"data,omitempty"`
}

type TraceTask struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Success              bool                   `json:"success"`
	DurationMS           int64                  `json:"duration_ms"`
	Turns                int                    `json:"turns"`
	ToolCalls            int                    `json:"tool_calls"`
	ToolNames            []string               `json:"tool_names,omitempty"`
	ToolSequence         []string               `json:"tool_sequence,omitempty"`
	MissingTools         []string               `json:"missing_tools,omitempty"`
	MissingToolCalls     []string               `json:"missing_tool_calls,omitempty"`
	MissingToolSeq       []string               `json:"missing_tool_sequence,omitempty"`
	MissingErrors        []string               `json:"missing_errors,omitempty"`
	InputTokens          int                    `json:"input_tokens"`
	OutputTokens         int                    `json:"output_tokens"`
	VerificationReason   string                 `json:"verification_reason,omitempty"`
	VerificationEvidence []VerificationEvidence `json:"verification_evidence,omitempty"`
	Error                string                 `json:"error,omitempty"`
	Workdir              string                 `json:"workdir,omitempty"`
}

type TraceObservability struct {
	SessionID       string   `json:"session_id,omitempty"`
	StateDir        string   `json:"state_dir,omitempty"`
	SessionDir      string   `json:"session_dir,omitempty"`
	TracePath       string   `json:"trace_path,omitempty"`
	HarnessDir      string   `json:"harness_dir,omitempty"`
	WorkflowDir     string   `json:"workflow_dir,omitempty"`
	TaskWorkdir     string   `json:"task_workdir,omitempty"`
	TaskWorkdirKept bool     `json:"task_workdir_kept,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

func TraceEvents(result Result, createdAt time.Time) []TraceEvent {
	taskID := result.TaskID
	events := []TraceEvent{{
		Type:      "task",
		TaskID:    taskID,
		CreatedAt: createdAt,
		Data: TraceTask{
			ID:                   result.TaskID,
			Name:                 result.TaskName,
			Success:              result.Success,
			DurationMS:           result.DurationMS,
			Turns:                result.Turns,
			ToolCalls:            result.ToolCalls,
			ToolNames:            append([]string(nil), result.ToolNames...),
			ToolSequence:         append([]string(nil), result.ToolSequence...),
			MissingTools:         append([]string(nil), result.MissingTools...),
			MissingToolCalls:     append([]string(nil), result.MissingToolCalls...),
			MissingToolSeq:       append([]string(nil), result.MissingToolSeq...),
			MissingErrors:        append([]string(nil), result.MissingErrors...),
			InputTokens:          result.InputTokens,
			OutputTokens:         result.OutputTokens,
			VerificationReason:   result.VerificationReason,
			VerificationEvidence: append([]VerificationEvidence(nil), result.VerificationEvidence...),
			Error:                result.Error,
			Workdir:              result.Workdir,
		},
	}}
	if result.Observability == nil {
		return events
	}
	obs := result.Observability
	events = append(events, TraceEvent{Type: "observability", TaskID: taskID, CreatedAt: createdAt, Data: TraceObservability{
		SessionID:       obs.SessionID,
		StateDir:        obs.StateDir,
		SessionDir:      obs.SessionDir,
		TracePath:       obs.TracePath,
		HarnessDir:      obs.HarnessDir,
		WorkflowDir:     obs.WorkflowDir,
		TaskWorkdir:     obs.TaskWorkdir,
		TaskWorkdirKept: obs.TaskWorkdirKept,
		Warnings:        append([]string(nil), obs.Warnings...),
	}})
	if obs.ModelProfile != nil {
		events = append(events, TraceEvent{Type: "model_profile", TaskID: taskID, CreatedAt: createdAt, Data: obs.ModelProfile})
	}
	if len(obs.ContextBlocks) > 0 {
		events = append(events, TraceEvent{Type: "context_blocks", TaskID: taskID, CreatedAt: createdAt, Data: obs.ContextBlocks})
	}
	if len(obs.ToolRecords) > 0 {
		events = append(events, TraceEvent{Type: "tool_records", TaskID: taskID, CreatedAt: createdAt, Data: obs.ToolRecords})
	}
	if len(obs.WorkflowRuns) > 0 {
		events = append(events, TraceEvent{Type: "workflow_runs", TaskID: taskID, CreatedAt: createdAt, Data: obs.WorkflowRuns})
	}
	if len(obs.HarnessTasks) > 0 {
		events = append(events, TraceEvent{Type: "harness_tasks", TaskID: taskID, CreatedAt: createdAt, Data: obs.HarnessTasks})
	}
	if len(obs.HarnessReports) > 0 {
		events = append(events, TraceEvent{Type: "harness_reports", TaskID: taskID, CreatedAt: createdAt, Data: obs.HarnessReports})
	}
	events = append(events, TraceEvent{Type: "final", TaskID: taskID, CreatedAt: createdAt, Data: map[string]any{
		"success":               result.Success,
		"verification_reason":   result.VerificationReason,
		"verification_evidence": append([]VerificationEvidence(nil), result.VerificationEvidence...),
		"error":                 result.Error,
		"final_answer_preview":  obs.FinalAnswerPreview,
		"warnings":              append([]string(nil), obs.Warnings...),
	}})
	return events
}

func WriteTrace(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	createdAt := time.Now().UTC()
	for _, event := range TraceEvents(result, createdAt) {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}
