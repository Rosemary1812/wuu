package sessiontrace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestAppendTurnWritesAgentFriendlyEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-trace.jsonl")
	err := AppendTurn(path,
		TurnRecord{
			ThreadID:            "thread-1",
			TurnID:              "turn-1",
			Status:              "completed",
			ProviderName:        "openai",
			Model:               "gpt-test",
			APIModel:            "gpt-5-codex",
			ModelProfile:        NewModelProfileRecord("openai", "gpt-test", "gpt-5-codex"),
			InputTokens:         12,
			OutputTokens:        34,
			CacheCreationTokens: 8,
			CacheReadTokens:     5,
		},
		FinalRecord{
			Status:              "completed",
			CacheCreationTokens: 8,
			CacheReadTokens:     5,
			FinalAnswerPreview:  "done API_KEY=secret-value-1234567890",
		},
		[]tools.ToolInfo{{Name: "read_file", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{Name: "read_file", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow, Success: true, RawOutputBytes: 100}},
		[]RequestContextRecord{{
			StepIndex:         0,
			TransientMessages: 1,
			ContentBytes:      100,
			BlockKinds:        []string{"ENVIRONMENT", "TASK"},
		}},
	)
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	events := readTraceEvents(t, path)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}
	wantTypes := []string{"turn", "context_requests", "tool_inventory", "tool_records", "final"}
	for i, want := range wantTypes {
		if events[i].Type != want || events[i].ThreadID != "thread-1" || events[i].TurnID != "turn-1" {
			t.Fatalf("unexpected event %d: %+v", i, events[i])
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !strings.Contains(string(raw), `"model_profile"`) ||
		!strings.Contains(string(raw), `"family":"codex"`) ||
		!strings.Contains(string(raw), `"default_write_mode":"patch"`) {
		t.Fatalf("trace should include model profile metadata:\n%s", raw)
	}
	if strings.Contains(string(raw), "secret-value") || !strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("trace should redact secret-like final previews:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"cache_creation_tokens":8`) || !strings.Contains(string(raw), `"cache_read_tokens":5`) {
		t.Fatalf("trace should include prompt cache usage:\n%s", raw)
	}
}

func TestReplayTraceSummarizesSessionEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := AppendTurn(path,
		TurnRecord{ThreadID: "thread-1", TurnID: "turn-1", Status: "completed", ProviderName: "openai", Model: "gpt-test", APIModel: "gpt-5-codex", ModelProfile: NewModelProfileRecord("openai", "gpt-test", "gpt-5-codex")},
		FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		[]tools.ToolInfo{{Name: "semantic_search", Kind: tools.ToolKindSearch, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("b", 64),
			Kind:            tools.ToolKindSearch,
			Risk:            tools.ToolRiskLow,
			PolicyAction:    tools.ToolPolicyAllow,
			Success:         true,
		}, {
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("b", 64),
			Kind:            tools.ToolKindSearch,
			Risk:            tools.ToolRiskLow,
			PolicyAction:    tools.ToolPolicyAllow,
			Success:         true,
		}, {
			Name:            "run_shell",
			ResultAction:    "restore",
			CallID:          "call-shell",
			Kind:            tools.ToolKindShell,
			Risk:            tools.ToolRiskHigh,
			PolicyAction:    tools.ToolPolicyDeny,
			PolicyReason:    "risk policy",
			ErrorKind:       "policy_denied",
			ArgumentsSHA256: strings.Repeat("c", 64),
			RevisionBefore:  "rev-before",
			Success:         false,
		}, {
			Name:            "start_process",
			CallID:          "call-process",
			Kind:            tools.ToolKindProcess,
			Risk:            tools.ToolRiskHigh,
			PolicyAction:    tools.ToolPolicyRequireApproval,
			PolicyReason:    "risk policy",
			ErrorKind:       "approval_required",
			ArgumentsSHA256: strings.Repeat("d", 64),
			ApprovalRef:     "/tmp/state/sessions/thread-1/approvals/call-process.json",
			RevisionBefore:  "rev-before",
			Success:         false,
		}},
		[]RequestContextRecord{{
			StepIndex:         0,
			TransientMessages: 2,
			ContentBytes:      240,
			BlockKinds:        []string{"ENVIRONMENT", "TOOL_POLICY"},
		}},
	); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	summary, err := ReplayTrace(path)
	if err != nil {
		t.Fatalf("ReplayTrace: %v", err)
	}
	if summary.Mode != "session_trace_replay" || !summary.Complete || summary.EventCount != 5 {
		t.Fatalf("unexpected replay summary: %+v", summary)
	}
	if summary.LatestTurn == nil || summary.LatestTurn.ThreadID != "thread-1" || summary.LatestTurn.Model != "gpt-test" {
		t.Fatalf("latest turn missing: %+v", summary.LatestTurn)
	}
	if summary.LatestTurn.ModelProfile == nil ||
		summary.LatestTurn.ModelProfile.Family != "codex" ||
		summary.LatestTurn.ModelProfile.DefaultWriteMode != "patch" {
		t.Fatalf("model profile missing from replay: %+v", summary.LatestTurn.ModelProfile)
	}
	if len(summary.ToolInventory) != 1 || summary.ToolInventory[0].Name != "semantic_search" {
		t.Fatalf("tool inventory missing: %+v", summary.ToolInventory)
	}
	if len(summary.ContextRequests) != 1 ||
		summary.ContextRequests[0].TransientMessages != 2 ||
		!containsString(summary.ContextBlockKinds, "TOOL_POLICY") {
		t.Fatalf("context requests missing: %+v", summary)
	}
	if len(summary.ToolNames) != 4 || summary.ToolNames[0] != "semantic_search" || summary.ToolNames[1] != "semantic_search" || summary.ToolNames[2] != "run_shell" || summary.ToolNames[3] != "start_process" {
		t.Fatalf("tool records missing: %+v", summary.ToolNames)
	}
	if summary.ToolSummary == nil || summary.ToolSummary.Total != 4 || summary.ToolSummary.Succeeded != 2 || summary.ToolSummary.Failed != 2 {
		t.Fatalf("tool summary missing: %+v", summary.ToolSummary)
	}
	if summary.ToolSummary.ByKind[string(tools.ToolKindShell)] != 1 ||
		summary.ToolSummary.ByKind[string(tools.ToolKindProcess)] != 1 ||
		summary.ToolSummary.ByRisk[string(tools.ToolRiskHigh)] != 2 ||
		summary.ToolSummary.ByPolicyAction[string(tools.ToolPolicyDeny)] != 1 ||
		summary.ToolSummary.ByPolicyAction[string(tools.ToolPolicyRequireApproval)] != 1 ||
		summary.ToolSummary.ByResultAction["run_shell:restore"] != 1 ||
		summary.ToolSummary.ByErrorKind["policy_denied"] != 1 ||
		summary.ToolSummary.ByErrorKind["approval_required"] != 1 {
		t.Fatalf("tool summary dimensions missing: %+v", summary.ToolSummary)
	}
	if len(summary.ToolSummary.PolicyBlocks) != 2 {
		t.Fatalf("tool summary missing policy blocks: %+v", summary.ToolSummary.PolicyBlocks)
	}
	if block := summary.ToolSummary.PolicyBlocks[0]; block.ToolName != "run_shell" ||
		block.CallID != "call-shell" ||
		block.PolicyAction != string(tools.ToolPolicyDeny) ||
		block.PolicyReason != "risk policy" ||
		block.ErrorKind != "policy_denied" ||
		block.ArgumentsSHA256 != strings.Repeat("c", 64) ||
		block.RevisionBefore != "rev-before" ||
		!strings.Contains(block.ModelNextAction, "lower-risk tool") {
		t.Fatalf("deny policy block missing replay detail: %+v", block)
	}
	if block := summary.ToolSummary.PolicyBlocks[1]; block.ToolName != "start_process" ||
		block.PolicyAction != string(tools.ToolPolicyRequireApproval) ||
		block.ApprovalRef != "/tmp/state/sessions/thread-1/approvals/call-process.json" ||
		!strings.Contains(block.ModelNextAction, "approval") {
		t.Fatalf("approval policy block missing replay detail: %+v", block)
	}
	if len(summary.ToolSummary.RepeatedArguments) != 1 ||
		summary.ToolSummary.RepeatedArguments[0].ToolName != "semantic_search" ||
		summary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("b", 64) ||
		summary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("tool summary missing repeated arguments: %+v", summary.ToolSummary.RepeatedArguments)
	}
	if summary.Final == nil || summary.Final.Status != "completed" || summary.Final.FinalAnswerPreview != "done" {
		t.Fatalf("final summary missing: %+v", summary.Final)
	}
}

func readTraceEvents(t *testing.T, path string) []Event {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer file.Close()
	var events []Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode trace event: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return events
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
