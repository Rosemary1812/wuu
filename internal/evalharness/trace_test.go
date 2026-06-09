package evalharness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTraceEventsSummarizeEvalArtifacts(t *testing.T) {
	events := TraceEvents(Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		DurationMS:         123,
		Turns:              2,
		ToolCalls:          1,
		ToolNames:          []string{"run_shell"},
		ToolSequence:       []string{"read_file", "run_shell", "run_shell"},
		MissingToolCalls:   []string{"checkpoint action=restore"},
		MissingToolSeq:     []string{"apply_patch contains=checkpoint_result.txt"},
		InputTokens:        10,
		OutputTokens:       20,
		VerificationReason: "passed",
		VerificationEvidence: []VerificationEvidence{{
			Check:    "go tests",
			Passed:   true,
			Command:  "go test ./...",
			Expected: "exit_code=0",
			Observed: "ok",
		}},
		Observability: &Observability{
			SessionID:          "eval-task-1",
			StateDir:           "/tmp/state",
			SessionDir:         "/tmp/state/sessions/eval-task-1",
			TracePath:          "/tmp/state/sessions/eval-task-1/eval-trace.jsonl",
			HarnessDir:         "/tmp/state/sessions/eval-task-1/harness",
			WorkflowDir:        "/tmp/state/workflows",
			TaskWorkdir:        "/tmp/task",
			TaskWorkdirKept:    true,
			FinalAnswerPreview: "done",
			ModelProfile:       &ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
			ContextBlocks: []ContextBlockObservation{{
				Kind:         "ACTIVE_FILES",
				Title:        "Files read in this session",
				Source:       "read_file",
				TokenBudget:  700,
				ContentBytes: 120,
			}},
			ContextRequests: []ContextRequestObservation{{
				StepIndex:         0,
				TransientMessages: 1,
				ContentBytes:      512,
				BlockKinds:        []string{"ENVIRONMENT", "REPO_MAP"},
			}},
			ToolRecords: []ToolObservation{{
				Name:           "run_shell",
				Success:        true,
				RawOutputBytes: 42,
			}},
			WorkflowRuns:   []WorkflowRunObservation{{ID: "run-1", Status: "completed"}},
			HarnessTasks:   []HarnessTaskObservation{{ID: "worker-1", Status: "completed"}},
			HarnessReports: []HarnessReportObservation{{ID: "report-1", Outcome: "completed"}},
		},
	}, time.Unix(100, 0).UTC())

	wantTypes := []string{"task", "observability", "model_profile", "context_blocks", "context_requests", "tool_records", "workflow_runs", "harness_tasks", "harness_reports", "final"}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d: %+v", len(events), len(wantTypes), events)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Fatalf("event %d type = %q, want %q", i, events[i].Type, want)
		}
		if events[i].TaskID != "task-1" || events[i].CreatedAt.IsZero() {
			t.Fatalf("event %d missing stable metadata: %+v", i, events[i])
		}
	}
	obs, ok := events[1].Data.(TraceObservability)
	if !ok {
		t.Fatalf("observability event data has wrong type: %#v", events[1].Data)
	}
	if obs.SessionID != "eval-task-1" || obs.TracePath == "" || !obs.TaskWorkdirKept {
		t.Fatalf("observability event missing artifact pointers: %+v", obs)
	}
	contextBlocks, ok := events[3].Data.([]ContextBlockObservation)
	if !ok {
		t.Fatalf("context_blocks event data has wrong type: %#v", events[3].Data)
	}
	if len(contextBlocks) != 1 || contextBlocks[0].Kind != "ACTIVE_FILES" || contextBlocks[0].ContentBytes == 0 {
		t.Fatalf("context_blocks event missing block metadata: %+v", contextBlocks)
	}
	contextRequests, ok := events[4].Data.([]ContextRequestObservation)
	if !ok {
		t.Fatalf("context_requests event data has wrong type: %#v", events[4].Data)
	}
	if len(contextRequests) != 1 || contextRequests[0].StepIndex != 0 || len(contextRequests[0].BlockKinds) != 2 {
		t.Fatalf("context_requests event missing request metadata: %+v", contextRequests)
	}
	task, ok := events[0].Data.(TraceTask)
	if !ok {
		t.Fatalf("task event data has wrong type: %#v", events[0].Data)
	}
	if len(task.MissingToolCalls) != 1 || task.MissingToolCalls[0] != "checkpoint action=restore" {
		t.Fatalf("task event missing tool call requirements: %+v", task)
	}
	if len(task.MissingToolSeq) != 1 || task.MissingToolSeq[0] != "apply_patch contains=checkpoint_result.txt" {
		t.Fatalf("task event missing tool sequence requirements: %+v", task)
	}
	if len(task.ToolSequence) != 3 || task.ToolSequence[0] != "read_file" || task.ToolSequence[2] != "run_shell" {
		t.Fatalf("task event missing tool sequence: %+v", task)
	}
	if len(task.VerificationEvidence) != 1 || task.VerificationEvidence[0].Command != "go test ./..." {
		t.Fatalf("task event missing verification evidence: %+v", task)
	}
}

func TestWriteTraceWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace", "eval-trace.jsonl")
	err := WriteTrace(path, Result{
		TaskID:   "task-1",
		TaskName: "Task One",
		Observability: &Observability{
			ModelProfile: &ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
		},
	})
	if err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer file.Close()

	var types []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event TraceEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal trace event: %v", err)
		}
		types = append(types, event.Type)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if len(types) != 4 || types[0] != "task" || types[1] != "observability" || types[2] != "model_profile" || types[3] != "final" {
		t.Fatalf("unexpected trace event types: %+v", types)
	}
}

func TestReplayTraceSummarizesRecordedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace", "eval-trace.jsonl")
	err := WriteTrace(path, Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		VerificationReason: "passed",
		Observability: &Observability{
			SessionID:          "eval-task-1",
			SessionDir:         filepath.Dir(path),
			TracePath:          path,
			FinalAnswerPreview: "done",
			ModelProfile:       &ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex", DefaultWriteMode: "patch"},
			ContextBlocks: []ContextBlockObservation{{
				Kind:   "TASK",
				Source: "system_reminder",
			}},
			ToolRecords: []ToolObservation{{
				Name:    "read_file",
				Success: true,
			}},
			WorkflowRuns:   []WorkflowRunObservation{{ID: "run-1", Status: "completed"}},
			HarnessTasks:   []HarnessTaskObservation{{ID: "worker-1", Status: "completed"}},
			HarnessReports: []HarnessReportObservation{{ID: "report-1", Outcome: "completed"}},
		},
	})
	if err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	summary, err := ReplayTrace(path)
	if err != nil {
		t.Fatalf("ReplayTrace: %v", err)
	}
	if !summary.Complete || summary.Mode != "deterministic_trace_replay" || summary.EventCount != 9 {
		t.Fatalf("unexpected replay summary envelope: %+v", summary)
	}
	if summary.Task == nil || summary.Task.ID != "task-1" || summary.Final == nil || !summary.Final.Success {
		t.Fatalf("replay did not preserve task/final outcome: %+v", summary)
	}
	if summary.ModelProfile == nil || summary.ModelProfile.Family != "codex" || summary.ModelProfile.DefaultWriteMode != "patch" {
		t.Fatalf("replay missing model profile: %+v", summary.ModelProfile)
	}
	if len(summary.ContextBlockKinds) != 1 || summary.ContextBlockKinds[0] != "TASK" {
		t.Fatalf("replay missing context block kinds: %+v", summary.ContextBlockKinds)
	}
	if len(summary.ToolNames) != 1 || summary.ToolNames[0] != "read_file" {
		t.Fatalf("replay missing tool records: %+v", summary.ToolNames)
	}
	if len(summary.WorkflowRunIDs) != 1 || summary.WorkflowRunIDs[0] != "run-1" {
		t.Fatalf("replay missing workflow runs: %+v", summary.WorkflowRunIDs)
	}
}
