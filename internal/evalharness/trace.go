package evalharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

type TraceReplaySummary struct {
	Path              string                     `json:"path,omitempty"`
	Mode              string                     `json:"mode"`
	EventCount        int                        `json:"event_count"`
	EventTypes        map[string]int             `json:"event_types,omitempty"`
	Task              *TraceTask                 `json:"task,omitempty"`
	Observability     *TraceObservability        `json:"observability,omitempty"`
	ModelProfile      *ModelProfileObservation   `json:"model_profile,omitempty"`
	ContextBlockKinds []string                   `json:"context_block_kinds,omitempty"`
	ToolInventory     []ToolInventoryObservation `json:"tool_inventory,omitempty"`
	ToolNames         []string                   `json:"tool_names,omitempty"`
	ToolSummary       *ToolReplaySummary         `json:"tool_summary,omitempty"`
	WorkflowRunIDs    []string                   `json:"workflow_run_ids,omitempty"`
	HarnessTaskIDs    []string                   `json:"harness_task_ids,omitempty"`
	HarnessReportIDs  []string                   `json:"harness_report_ids,omitempty"`
	Final             *TraceReplayFinal          `json:"final,omitempty"`
	Complete          bool                       `json:"complete"`
	Warnings          []string                   `json:"warnings,omitempty"`
}

type ToolReplaySummary struct {
	Total             int                           `json:"total"`
	Succeeded         int                           `json:"succeeded"`
	Failed            int                           `json:"failed"`
	ByKind            map[string]int                `json:"by_kind,omitempty"`
	ByRisk            map[string]int                `json:"by_risk,omitempty"`
	ByPolicyAction    map[string]int                `json:"by_policy_action,omitempty"`
	ByErrorKind       map[string]int                `json:"by_error_kind,omitempty"`
	RepeatedArguments []ToolRepeatedArgumentSummary `json:"repeated_arguments,omitempty"`
	PatchRisk         *PatchRiskReplaySummary       `json:"patch_risk,omitempty"`
	argumentCounts    map[string]ToolRepeatedArgumentSummary
}

type ToolRepeatedArgumentSummary struct {
	ToolName        string `json:"tool_name"`
	ArgumentsSHA256 string `json:"arguments_sha256"`
	Count           int    `json:"count"`
}

type PatchRiskReplaySummary struct {
	Total          int            `json:"total"`
	ByLevel        map[string]int `json:"by_level,omitempty"`
	MultiFile      int            `json:"multi_file,omitempty"`
	ContainsDelete int            `json:"contains_delete,omitempty"`
	ContainsMove   int            `json:"contains_move,omitempty"`
	FileCount      int            `json:"file_count"`
	HunkCount      int            `json:"hunk_count"`
	AddedLines     int            `json:"added_lines"`
	DeletedLines   int            `json:"deleted_lines"`
}

type TraceReplayFinal struct {
	Success              bool                   `json:"success"`
	VerificationReason   string                 `json:"verification_reason,omitempty"`
	VerificationEvidence []VerificationEvidence `json:"verification_evidence,omitempty"`
	Error                string                 `json:"error,omitempty"`
	FinalAnswerPreview   string                 `json:"final_answer_preview,omitempty"`
	Warnings             []string               `json:"warnings,omitempty"`
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
	if len(obs.ContextRequests) > 0 {
		events = append(events, TraceEvent{Type: "context_requests", TaskID: taskID, CreatedAt: createdAt, Data: obs.ContextRequests})
	}
	if len(obs.ToolInventory) > 0 {
		events = append(events, TraceEvent{Type: "tool_inventory", TaskID: taskID, CreatedAt: createdAt, Data: obs.ToolInventory})
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

func ReplayTrace(path string) (TraceReplaySummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return TraceReplaySummary{}, err
	}
	defer file.Close()

	summary := TraceReplaySummary{
		Path:       path,
		Mode:       "deterministic_trace_replay",
		EventTypes: make(map[string]int),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event struct {
			Type      string          `json:"type"`
			TaskID    string          `json:"task_id,omitempty"`
			CreatedAt time.Time       `json:"created_at"`
			Data      json.RawMessage `json:"data,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return TraceReplaySummary{}, fmt.Errorf("decode trace line %d: %w", line, err)
		}
		summary.EventCount++
		summary.EventTypes[event.Type]++
		if err := replayTraceEvent(&summary, event.Type, event.Data); err != nil {
			return TraceReplaySummary{}, fmt.Errorf("replay trace line %d type %q: %w", line, event.Type, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return TraceReplaySummary{}, err
	}
	if summary.Task == nil {
		return TraceReplaySummary{}, fmt.Errorf("trace %s has no task event", path)
	}
	if summary.Final == nil {
		summary.Warnings = append(summary.Warnings, "trace has no final event; using task success as replay outcome")
		summary.Final = &TraceReplayFinal{Success: summary.Task.Success, VerificationReason: summary.Task.VerificationReason, Error: summary.Task.Error}
	}
	summary.finalizeToolSummary()
	summary.Complete = summary.EventTypes["task"] > 0 && summary.EventTypes["final"] > 0
	return summary, nil
}

func replayTraceEvent(summary *TraceReplaySummary, eventType string, data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	switch eventType {
	case "task":
		var task TraceTask
		if err := json.Unmarshal(data, &task); err != nil {
			return err
		}
		summary.Task = &task
	case "observability":
		var obs TraceObservability
		if err := json.Unmarshal(data, &obs); err != nil {
			return err
		}
		summary.Observability = &obs
	case "model_profile":
		var profile ModelProfileObservation
		if err := json.Unmarshal(data, &profile); err != nil {
			return err
		}
		summary.ModelProfile = &profile
	case "context_blocks":
		var blocks []ContextBlockObservation
		if err := json.Unmarshal(data, &blocks); err != nil {
			return err
		}
		for _, block := range blocks {
			if block.Kind != "" {
				summary.ContextBlockKinds = append(summary.ContextBlockKinds, block.Kind)
			}
		}
	case "tool_records":
		var records []ToolObservation
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		for _, record := range records {
			if record.Name != "" {
				summary.ToolNames = append(summary.ToolNames, record.Name)
			}
			summary.addToolObservation(record)
		}
	case "tool_inventory":
		var inventory []ToolInventoryObservation
		if err := json.Unmarshal(data, &inventory); err != nil {
			return err
		}
		summary.ToolInventory = append(summary.ToolInventory, inventory...)
	case "workflow_runs":
		var runs []WorkflowRunObservation
		if err := json.Unmarshal(data, &runs); err != nil {
			return err
		}
		for _, run := range runs {
			if run.ID != "" {
				summary.WorkflowRunIDs = append(summary.WorkflowRunIDs, run.ID)
			}
		}
	case "harness_tasks":
		var tasks []HarnessTaskObservation
		if err := json.Unmarshal(data, &tasks); err != nil {
			return err
		}
		for _, task := range tasks {
			if task.ID != "" {
				summary.HarnessTaskIDs = append(summary.HarnessTaskIDs, task.ID)
			}
		}
	case "harness_reports":
		var reports []HarnessReportObservation
		if err := json.Unmarshal(data, &reports); err != nil {
			return err
		}
		for _, report := range reports {
			if report.ID != "" {
				summary.HarnessReportIDs = append(summary.HarnessReportIDs, report.ID)
			}
		}
	case "final":
		var final TraceReplayFinal
		if err := json.Unmarshal(data, &final); err != nil {
			return err
		}
		summary.Final = &final
	}
	return nil
}

func (summary *TraceReplaySummary) addToolObservation(record ToolObservation) {
	summary.ensureToolSummary()
	summary.ToolSummary.Total++
	if record.Success {
		summary.ToolSummary.Succeeded++
	} else {
		summary.ToolSummary.Failed++
	}
	if kind := strings.TrimSpace(record.Kind); kind != "" {
		summary.ToolSummary.ByKind[kind]++
	}
	if risk := strings.TrimSpace(record.Risk); risk != "" {
		summary.ToolSummary.ByRisk[risk]++
	}
	if action := strings.TrimSpace(record.PolicyAction); action != "" {
		summary.ToolSummary.ByPolicyAction[action]++
	}
	if errorKind := strings.TrimSpace(record.ErrorKind); errorKind != "" {
		summary.ToolSummary.ByErrorKind[errorKind]++
	}
	summary.addRepeatedToolArguments(record.Name, record.ArgumentsSHA256)
	if record.PatchRiskSummary != nil {
		summary.addPatchRiskObservation(*record.PatchRiskSummary)
	}
}

func (summary *TraceReplaySummary) ensureToolSummary() {
	if summary.ToolSummary != nil {
		return
	}
	summary.ToolSummary = &ToolReplaySummary{
		ByKind:         map[string]int{},
		ByRisk:         map[string]int{},
		ByPolicyAction: map[string]int{},
		ByErrorKind:    map[string]int{},
	}
}

func (summary *TraceReplaySummary) addRepeatedToolArguments(toolName, argumentsSHA256 string) {
	toolName = strings.TrimSpace(toolName)
	argumentsSHA256 = strings.TrimSpace(argumentsSHA256)
	if toolName == "" || argumentsSHA256 == "" {
		return
	}
	summary.ensureToolSummary()
	if summary.ToolSummary.argumentCounts == nil {
		summary.ToolSummary.argumentCounts = map[string]ToolRepeatedArgumentSummary{}
	}
	key := toolName + "\x00" + argumentsSHA256
	entry := summary.ToolSummary.argumentCounts[key]
	if entry.Count == 0 {
		entry.ToolName = toolName
		entry.ArgumentsSHA256 = argumentsSHA256
	}
	entry.Count++
	summary.ToolSummary.argumentCounts[key] = entry
}

func (summary *TraceReplaySummary) finalizeToolSummary() {
	if summary.ToolSummary == nil || len(summary.ToolSummary.argumentCounts) == 0 {
		return
	}
	summary.ToolSummary.RepeatedArguments = summary.ToolSummary.RepeatedArguments[:0]
	for _, entry := range summary.ToolSummary.argumentCounts {
		if entry.Count > 1 {
			summary.ToolSummary.RepeatedArguments = append(summary.ToolSummary.RepeatedArguments, entry)
		}
	}
	sort.Slice(summary.ToolSummary.RepeatedArguments, func(i, j int) bool {
		left := summary.ToolSummary.RepeatedArguments[i]
		right := summary.ToolSummary.RepeatedArguments[j]
		if left.ToolName != right.ToolName {
			return left.ToolName < right.ToolName
		}
		return left.ArgumentsSHA256 < right.ArgumentsSHA256
	})
	summary.ToolSummary.argumentCounts = nil
}

func (summary *TraceReplaySummary) addPatchRiskObservation(risk PatchRiskObservation) {
	summary.ensureToolSummary()
	if summary.ToolSummary.PatchRisk == nil {
		summary.ToolSummary.PatchRisk = &PatchRiskReplaySummary{ByLevel: map[string]int{}}
	}
	patchRisk := summary.ToolSummary.PatchRisk
	patchRisk.Total++
	if level := strings.TrimSpace(risk.RiskLevel); level != "" {
		patchRisk.ByLevel[level]++
	}
	if risk.MultiFile {
		patchRisk.MultiFile++
	}
	if risk.ContainsDelete {
		patchRisk.ContainsDelete++
	}
	if risk.ContainsMove {
		patchRisk.ContainsMove++
	}
	patchRisk.FileCount += risk.FileCount
	patchRisk.HunkCount += risk.HunkCount
	patchRisk.AddedLines += risk.AddedLines
	patchRisk.DeletedLines += risk.DeletedLines
}
