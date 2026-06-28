package evalharness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTraceEventsSummarizeEvalArtifacts(t *testing.T) {
	events := TraceEvents(Result{
		TaskID:              "task-1",
		TaskName:            "Task One",
		Success:             true,
		DurationMS:          123,
		Turns:               2,
		ToolCalls:           1,
		ToolNames:           []string{"run_shell"},
		ToolSequence:        []string{"read_file", "run_shell", "run_shell"},
		ForbiddenToolsUsed:  []string{"create_workflow"},
		MissingToolCalls:    []string{"checkpoint action=restore"},
		MissingToolSeq:      []string{"apply_patch contains=checkpoint_result.txt"},
		WorkflowIssues:      []string{"run-1:missing_reports=worker-1"},
		InputTokens:         10,
		OutputTokens:        20,
		CacheReadTokens:     6,
		CacheCreationTokens: 4,
		CacheHitRate:        CacheHitRate(10, 6),
		VerificationReason:  "passed",
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
				StepIndex:                0,
				TransientMessages:        1,
				ContentBytes:             512,
				SystemBytes:              2048,
				TurnPrefix:               1,
				TurnPrefixBytes:          2176,
				TurnPrefixHash:           "turn-hash",
				MessageBytes:             2560,
				ToolSchemaBytes:          4096,
				LoadableToolCount:        1,
				LoadableToolSchemaBytes:  512,
				LoadableToolSurfaceHash:  "loadable-hash",
				BlockKinds:               []string{"ENVIRONMENT", "REPO_MAP"},
				BlockKindCounts:          map[string]int{"ENVIRONMENT": 1, "REPO_MAP": 1},
				BlockKindBytes:           map[string]int{"ENVIRONMENT": 128, "REPO_MAP": 384},
				SegmentLifecycleCounts:   map[string]int{"request_only": 1},
				SegmentPlacementCounts:   map[string]int{"after_history": 1},
				SegmentCachePolicyCounts: map[string]int{"volatile": 1},
				InputTokens:              10,
				OutputTokens:             2,
				CacheCreationTokens:      4,
				CacheReadTokens:          6,
			}},
			ToolInventory: []ToolInventoryObservation{{
				Name:     "read_file",
				Kind:     "file",
				Exposure: "direct",
				Risk:     "low",
				ReadOnly: true,
			}},
			ToolRecords: []ToolObservation{{
				Name:           "run_shell",
				Success:        true,
				RawOutputBytes: 42,
			}},
			GoalAttention: []GoalAttentionObservation{{
				Source:  "workflow_agent",
				ID:      "worker-1",
				Status:  "missing_report",
				Message: "workflow run run-1 is missing agent report",
			}},
			WorkflowRuns:   []WorkflowRunObservation{{ID: "run-1", RunDir: "/tmp/state/workflows/run-1", EventLogPath: "/tmp/state/workflows/run-1/events.jsonl", Status: "completed"}},
			HarnessTasks:   []HarnessTaskObservation{{ID: "worker-1", Status: "completed"}},
			HarnessReports: []HarnessReportObservation{{ID: "report-1", Outcome: "completed"}},
		},
	}, time.Unix(100, 0).UTC())

	wantTypes := []string{"task", "observability", "model_profile", "context_blocks", "context_requests", "tool_inventory", "tool_records", "goal_attention", "workflow_runs", "harness_tasks", "harness_reports", "final"}
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
	task, ok := events[0].Data.(TraceTask)
	if !ok {
		t.Fatalf("task event data has wrong type: %#v", events[0].Data)
	}
	if len(task.ForbiddenToolsUsed) != 1 || task.ForbiddenToolsUsed[0] != "create_workflow" {
		t.Fatalf("task event missing forbidden tools: %+v", task)
	}
	if task.InputTokens != 10 || task.OutputTokens != 20 || task.CacheReadTokens != 6 || task.CacheCreationTokens != 4 || task.CacheHitRate != CacheHitRate(10, 6) {
		t.Fatalf("task event missing token/cache metrics: %+v", task)
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
	if len(contextRequests) != 1 ||
		contextRequests[0].StepIndex != 0 ||
		len(contextRequests[0].BlockKinds) != 2 ||
		contextRequests[0].SystemBytes != 2048 ||
		contextRequests[0].TurnPrefix != 1 ||
		contextRequests[0].TurnPrefixBytes != 2176 ||
		contextRequests[0].TurnPrefixHash != "turn-hash" ||
		contextRequests[0].MessageBytes != 2560 ||
		contextRequests[0].ToolSchemaBytes != 4096 ||
		contextRequests[0].LoadableToolCount != 1 ||
		contextRequests[0].LoadableToolSchemaBytes != 512 ||
		contextRequests[0].LoadableToolSurfaceHash != "loadable-hash" ||
		contextRequests[0].SegmentLifecycleCounts["request_only"] != 1 ||
		contextRequests[0].SegmentPlacementCounts["after_history"] != 1 ||
		contextRequests[0].SegmentCachePolicyCounts["volatile"] != 1 ||
		contextRequests[0].InputTokens != 10 ||
		contextRequests[0].OutputTokens != 2 ||
		contextRequests[0].CacheCreationTokens != 4 ||
		contextRequests[0].CacheReadTokens != 6 ||
		contextRequests[0].BlockKindCounts["ENVIRONMENT"] != 1 ||
		contextRequests[0].BlockKindBytes["REPO_MAP"] != 384 {
		t.Fatalf("context_requests event missing request metadata: %+v", contextRequests)
	}
	if len(task.MissingToolCalls) != 1 || task.MissingToolCalls[0] != "checkpoint action=restore" {
		t.Fatalf("task event missing tool call requirements: %+v", task)
	}
	if len(task.MissingToolSeq) != 1 || task.MissingToolSeq[0] != "apply_patch contains=checkpoint_result.txt" {
		t.Fatalf("task event missing tool sequence requirements: %+v", task)
	}
	if len(task.WorkflowIssues) != 1 || task.WorkflowIssues[0] != "run-1:missing_reports=worker-1" {
		t.Fatalf("task event missing workflow issues: %+v", task)
	}
	if len(task.ToolSequence) != 3 || task.ToolSequence[0] != "read_file" || task.ToolSequence[2] != "run_shell" {
		t.Fatalf("task event missing tool sequence: %+v", task)
	}
	if len(task.VerificationEvidence) != 1 || task.VerificationEvidence[0].Command != "go test ./..." {
		t.Fatalf("task event missing verification evidence: %+v", task)
	}
	attention, ok := events[7].Data.([]GoalAttentionObservation)
	if !ok {
		t.Fatalf("goal_attention event data has wrong type: %#v", events[7].Data)
	}
	if len(attention) != 1 || attention[0].Source != "workflow_agent" || attention[0].Status != "missing_report" {
		t.Fatalf("goal_attention event missing attention item: %+v", attention)
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
				Kind:           "TASK",
				Title:          "Task",
				Source:         "system_reminder",
				TokenBudget:    600,
				ContentPreview: "Fix the task.",
			}},
			ToolInventory: []ToolInventoryObservation{{
				Name:     "read_file",
				Kind:     "file",
				Exposure: "direct",
				Risk:     "low",
				ReadOnly: true,
			}},
			ToolRecords: []ToolObservation{{
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("a", 64),
				Kind:            "file",
				Risk:            "low",
				PolicyAction:    "allow",
				Success:         true,
			}, {
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("a", 64),
				Kind:            "file",
				Risk:            "low",
				PolicyAction:    "allow",
				Success:         true,
			}, {
				Name:         "apply_patch",
				ResultAction: "apply",
				Kind:         "file",
				Risk:         "high",
				PolicyAction: "allow",
				Success:      true,
				PatchRiskSummary: &PatchRiskObservation{
					FileCount:    2,
					HunkCount:    2,
					AddedLines:   8,
					DeletedLines: 3,
					MultiFile:    true,
					RiskLevel:    "medium",
				},
			}, {
				Name:            "run_shell",
				ResultAction:    "restore",
				CallID:          "call-shell",
				Kind:            "shell",
				Risk:            "high",
				PolicyAction:    "deny",
				PolicyReason:    "risk policy",
				ErrorKind:       "policy_denied",
				ArgumentsSHA256: strings.Repeat("c", 64),
				RevisionBefore:  "rev-before",
				Success:         false,
			}, {
				Name:            "start_process",
				CallID:          "call-process",
				Kind:            "process",
				Risk:            "high",
				PolicyAction:    "require_approval",
				PolicyReason:    "risk policy",
				ErrorKind:       "approval_required",
				ArgumentsSHA256: strings.Repeat("d", 64),
				ApprovalRef:     "/tmp/state/sessions/eval-task-1/approvals/call-process.json",
				RevisionBefore:  "rev-before",
				Success:         false,
			}},
			GoalAttention: []GoalAttentionObservation{{
				Source:  "workflow_conflict",
				ID:      "run-1",
				Status:  "changed_file_overlap",
				Message: "worker-b, worker-a",
				Path:    "shared.go",
			}},
			WorkflowRuns:   []WorkflowRunObservation{{ID: "run-1", RunDir: "/tmp/state/workflows/run-1", EventLogPath: "/tmp/state/workflows/run-1/events.jsonl", Status: "completed"}},
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
	if !summary.Complete || summary.Mode != "deterministic_trace_replay" || summary.EventCount != 11 {
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
	if len(summary.ContextBlocks) != 1 ||
		summary.ContextBlocks[0].Kind != "TASK" ||
		summary.ContextBlocks[0].Source != "system_reminder" ||
		summary.ContextBlocks[0].TokenBudget != 600 ||
		summary.ContextBlocks[0].ContentPreview != "Fix the task." {
		t.Fatalf("replay missing context block observations: %+v", summary.ContextBlocks)
	}
	if len(summary.ToolInventory) != 1 || summary.ToolInventory[0].Name != "read_file" || summary.ToolInventory[0].Exposure != "direct" {
		t.Fatalf("replay missing tool inventory: %+v", summary.ToolInventory)
	}
	if len(summary.ToolNames) != 5 || summary.ToolNames[0] != "read_file" || summary.ToolNames[1] != "read_file" || summary.ToolNames[2] != "apply_patch" || summary.ToolNames[3] != "run_shell" || summary.ToolNames[4] != "start_process" {
		t.Fatalf("replay missing tool records: %+v", summary.ToolNames)
	}
	if summary.ToolSummary == nil || summary.ToolSummary.Total != 5 || summary.ToolSummary.Succeeded != 3 || summary.ToolSummary.Failed != 2 {
		t.Fatalf("replay missing tool summary: %+v", summary.ToolSummary)
	}
	if summary.ToolSummary.ByKind["file"] != 3 ||
		summary.ToolSummary.ByRisk["high"] != 3 ||
		summary.ToolSummary.ByPolicyAction["deny"] != 1 ||
		summary.ToolSummary.ByPolicyAction["require_approval"] != 1 ||
		summary.ToolSummary.ByResultAction["apply_patch:apply"] != 1 ||
		summary.ToolSummary.ByResultAction["run_shell:restore"] != 1 ||
		summary.ToolSummary.ByErrorKind["policy_denied"] != 1 ||
		summary.ToolSummary.ByErrorKind["approval_required"] != 1 {
		t.Fatalf("replay tool summary missing dimensions: %+v", summary.ToolSummary)
	}
	if len(summary.ToolSummary.PolicyBlocks) != 2 {
		t.Fatalf("replay tool summary missing policy blocks: %+v", summary.ToolSummary.PolicyBlocks)
	}
	if block := summary.ToolSummary.PolicyBlocks[0]; block.ToolName != "run_shell" ||
		block.CallID != "call-shell" ||
		block.PolicyAction != "deny" ||
		block.PolicyReason != "risk policy" ||
		block.ErrorKind != "policy_denied" ||
		block.ArgumentsSHA256 != strings.Repeat("c", 64) ||
		block.RevisionBefore != "rev-before" ||
		!strings.Contains(block.ModelNextAction, "lower-risk tool") {
		t.Fatalf("deny policy block missing replay detail: %+v", block)
	}
	if block := summary.ToolSummary.PolicyBlocks[1]; block.ToolName != "start_process" ||
		block.PolicyAction != "require_approval" ||
		block.ApprovalRef != "/tmp/state/sessions/eval-task-1/approvals/call-process.json" ||
		!strings.Contains(block.ModelNextAction, "approval") {
		t.Fatalf("approval policy block missing replay detail: %+v", block)
	}
	if len(summary.ToolSummary.RepeatedArguments) != 1 ||
		summary.ToolSummary.RepeatedArguments[0].ToolName != "read_file" ||
		summary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("a", 64) ||
		summary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("replay tool summary missing repeated arguments: %+v", summary.ToolSummary.RepeatedArguments)
	}
	if summary.ToolSummary.PatchRisk == nil ||
		summary.ToolSummary.PatchRisk.Total != 1 ||
		summary.ToolSummary.PatchRisk.ByLevel["medium"] != 1 ||
		summary.ToolSummary.PatchRisk.MultiFile != 1 ||
		summary.ToolSummary.PatchRisk.FileCount != 2 ||
		summary.ToolSummary.PatchRisk.HunkCount != 2 ||
		summary.ToolSummary.PatchRisk.AddedLines != 8 ||
		summary.ToolSummary.PatchRisk.DeletedLines != 3 {
		t.Fatalf("replay tool summary missing patch risk: %+v", summary.ToolSummary.PatchRisk)
	}
	if len(summary.WorkflowRunIDs) != 1 || summary.WorkflowRunIDs[0] != "run-1" {
		t.Fatalf("replay missing workflow runs: %+v", summary.WorkflowRunIDs)
	}
	if len(summary.GoalAttention) != 1 ||
		summary.GoalAttention[0].Source != "workflow_conflict" ||
		summary.GoalAttention[0].Path != "shared.go" {
		t.Fatalf("replay missing goal attention: %+v", summary.GoalAttention)
	}
	if len(summary.WorkflowRuns) != 1 ||
		summary.WorkflowRuns[0].RunDir != "/tmp/state/workflows/run-1" ||
		summary.WorkflowRuns[0].EventLogPath != "/tmp/state/workflows/run-1/events.jsonl" {
		t.Fatalf("replay missing workflow artifact paths: %+v", summary.WorkflowRuns)
	}
}

func TestReplayTraceBuildsModelProfileRecommendationsFromEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace", "eval-trace.jsonl")
	start := time.Unix(100, 0).UTC()
	err := WriteTrace(path, Result{
		TaskID:   "task-1",
		TaskName: "Task One",
		Success:  true,
		Observability: &Observability{
			ModelProfile: &ModelProfileObservation{
				ProviderName:          "portable",
				Model:                 "coder",
				AllowParallelReadOnly: false,
			},
			ToolRecords: []ToolObservation{{
				Name:            "read_file",
				ReadOnly:        true,
				ConcurrencySafe: true,
				StartedAt:       start,
				DurationMS:      200,
				Success:         true,
			}, {
				Name:            "grep",
				ReadOnly:        true,
				ConcurrencySafe: true,
				StartedAt:       start.Add(50 * time.Millisecond),
				DurationMS:      100,
				Success:         true,
			}},
		},
	})
	if err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	summary, err := ReplayTrace(path)
	if err != nil {
		t.Fatalf("ReplayTrace: %v", err)
	}
	if len(summary.ModelProfileRecommendations) != 1 {
		t.Fatalf("expected one profile recommendation, got %+v", summary.ModelProfileRecommendations)
	}
	rec := summary.ModelProfileRecommendations[0]
	if rec.Field != "workflow.allow_parallel_read_only" ||
		rec.CurrentValue != "false" ||
		rec.RecommendedValue != "true" ||
		len(rec.Evidence) == 0 ||
		!strings.Contains(rec.Evidence[0], "read_file overlaps grep") {
		t.Fatalf("unexpected profile recommendation: %+v", rec)
	}
}

func TestReplayTraceSummarizesValidationLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace", "eval-trace.jsonl")
	err := WriteTrace(path, Result{
		TaskID:   "task-1",
		TaskName: "Task One",
		Success:  true,
		VerificationEvidence: []VerificationEvidence{{
			Check:    "go tests",
			Passed:   true,
			Command:  "go test ./...",
			Expected: "exit_code=0",
			Observed: "ok",
		}},
		Observability: &Observability{
			ToolRecords: []ToolObservation{{
				Name:                 "bash",
				CallID:               "call-test",
				ResultAction:         "run",
				ClassificationReason: "local verification command",
				Success:              true,
				DurationMS:           1234,
				RevisionBefore:       "rev-before",
				RevisionAfter:        "rev-after",
				ResultRef:            "/tmp/wuu/tool-results/test.log",
				ArtifactRefs:         []string{"/tmp/wuu/tool-results/test.log"},
			}, {
				Name:         "workflow_status",
				CallID:       "call-workflow",
				ResultAction: "status",
				Success:      true,
			}, {
				Name:                 "bash",
				CallID:               "call-shell",
				ClassificationReason: "simple read-only shell command",
				Success:              false,
				ErrorKind:            "policy_denied",
				PolicyAction:         "deny",
			}},
		},
	})
	if err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	summary, err := ReplayTrace(path)
	if err != nil {
		t.Fatalf("ReplayTrace: %v", err)
	}
	if summary.Validation == nil {
		t.Fatal("replay missing validation ledger")
	}
	if summary.Validation.Status != "passed" || len(summary.Validation.Evidence) != 1 || len(summary.Validation.ToolCalls) != 2 {
		t.Fatalf("unexpected validation ledger: %+v", summary.Validation)
	}
	if summary.Validation.Evidence[0].Command != "go test ./..." {
		t.Fatalf("validation evidence command not preserved: %+v", summary.Validation.Evidence)
	}
	if summary.Validation.ToolCalls[0].ToolName != "bash" ||
		summary.Validation.ToolCalls[0].ResultRef != "/tmp/wuu/tool-results/test.log" ||
		summary.Validation.ToolCalls[0].RevisionAfter != "rev-after" {
		t.Fatalf("validation tool call missing metadata: %+v", summary.Validation.ToolCalls)
	}
	for _, call := range summary.Validation.ToolCalls {
		if call.CallID == "call-shell" {
			t.Fatalf("generic bash should not be treated as validation without structured validation metadata: %+v", summary.Validation.ToolCalls)
		}
	}
	if len(summary.Validation.NextActions) == 0 {
		t.Fatalf("validation ledger missing next actions: %+v", summary.Validation)
	}
}

func TestBuildValidationSummaryFromEvalResult(t *testing.T) {
	summary := BuildValidationSummary(Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            false,
		ForbiddenToolsUsed: []string{"run_workflow"},
		WorkflowIssues:     []string{"run-1:missing_reports=worker-1"},
		VerificationEvidence: []VerificationEvidence{{
			Check:    "marker",
			Passed:   false,
			Path:     "marker.txt",
			Observed: "missing",
		}},
		Observability: &Observability{
			ToolRecords: []ToolObservation{{
				Name:                 "bash",
				CallID:               "call-test",
				ResultAction:         "run",
				ClassificationReason: "local verification command",
				Success:              false,
				ErrorKind:            "test_failed",
				ResultRef:            "/tmp/wuu/test.log",
			}},
		},
	})
	if summary == nil {
		t.Fatal("missing validation summary")
	}
	if summary.Status != "incomplete" {
		t.Fatalf("validation status = %q, want incomplete: %+v", summary.Status, summary)
	}
	if len(summary.Missing) != 2 ||
		summary.Missing[0] != "forbidden_tool:run_workflow" ||
		summary.Missing[1] != "workflow_issue:run-1:missing_reports=worker-1" {
		t.Fatalf("validation missing requirements not summarized: %+v", summary.Missing)
	}
	if len(summary.Failures) != 2 ||
		summary.Failures[0] != "bash:test_failed:call_id=call-test" ||
		summary.Failures[1] != "marker" {
		t.Fatalf("validation failures not summarized: %+v", summary.Failures)
	}
	if len(summary.ToolCalls) != 1 || summary.ToolCalls[0].ResultRef != "/tmp/wuu/test.log" {
		t.Fatalf("validation tool call not summarized: %+v", summary.ToolCalls)
	}
}

func TestGoalValidationIssuesSummarizesAttention(t *testing.T) {
	issues := GoalValidationIssues([]GoalAttentionObservation{{
		Source:  "workflow_agent",
		ID:      "agent-missing",
		Status:  "missing_report",
		Message: "workflow run run-1 is missing agent report",
	}, {
		Source:  "workflow_agent",
		ID:      "agent-failed",
		Status:  "failed",
		Message: "workflow run run-1 has failed agent",
	}, {
		Source:  "workflow_conflict",
		ID:      "run-1",
		Status:  "changed_file_overlap",
		Message: "agent-b, agent-a",
		Path:    "shared.go",
	}, {
		Source: "harness",
		ID:     "task-1",
		Status: "failed",
	}}, []WorkflowRunObservation{{
		ID:     "run-2",
		Status: "running",
		TeamArbitration: WorkflowTeamArbitration{
			Status:         "attention_required",
			MissingReports: []string{"agent-c"},
		},
	}})
	want := []string{
		"harness:task-1:status=failed",
		"run-1:failed=agent-failed",
		"run-1:missing_reports=agent-missing",
		"run-1:overlap=shared.go=agent-a+agent-b",
		"run-2:arbitration=attention_required",
		"run-2:missing_reports=agent-c",
		"run-2:status=running",
	}
	if strings.Join(issues, "\n") != strings.Join(want, "\n") {
		t.Fatalf("goal issues = %+v, want %+v", issues, want)
	}
}

func TestWorkflowValidationIssuesSummarizesArbitration(t *testing.T) {
	issues := WorkflowValidationIssues([]WorkflowRunObservation{{
		ID:     "run-1",
		Status: "completed",
		TeamArbitration: WorkflowTeamArbitration{
			Status:          "attention_required",
			OpenAgentRuns:   []string{"agent-open"},
			MissingReports:  []string{"agent-missing"},
			FailedAgentRuns: []string{"agent-failed"},
			ChangedFileOverlaps: []WorkflowChangedFileOverlapObservation{{
				File:        "shared.go",
				AgentRunIDs: []string{"agent-b", "agent-a"},
			}},
		},
	}, {
		ID:     "run-2",
		Status: "running",
	}})
	want := []string{
		"run-1:arbitration=attention_required",
		"run-1:failed=agent-failed",
		"run-1:missing_reports=agent-missing",
		"run-1:open=agent-open",
		"run-1:overlap=shared.go=agent-a+agent-b",
		"run-2:status=running",
	}
	if strings.Join(issues, "\n") != strings.Join(want, "\n") {
		t.Fatalf("workflow issues = %+v, want %+v", issues, want)
	}
	if clear := WorkflowValidationIssues([]WorkflowRunObservation{{
		ID:     "run-clear",
		Status: "completed",
		TeamArbitration: WorkflowTeamArbitration{
			Status: "clear",
		},
	}}); len(clear) != 0 {
		t.Fatalf("clear workflow should not produce issues: %+v", clear)
	}
}
