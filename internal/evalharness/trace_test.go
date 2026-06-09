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
		InputTokens:        10,
		OutputTokens:       20,
		VerificationReason: "passed",
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

	wantTypes := []string{"task", "observability", "model_profile", "tool_records", "workflow_runs", "harness_tasks", "harness_reports", "final"}
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
