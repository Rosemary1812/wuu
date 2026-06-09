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
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			Status:       "completed",
			ProviderName: "openai",
			Model:        "gpt-test",
			InputTokens:  12,
			OutputTokens: 34,
		},
		FinalRecord{
			Status:             "completed",
			FinalAnswerPreview: "done API_KEY=secret-value-1234567890",
		},
		[]tools.ToolInfo{{Name: "read_file", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{Name: "read_file", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow, Success: true, RawOutputBytes: 100}},
	)
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	events := readTraceEvents(t, path)
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	wantTypes := []string{"turn", "tool_inventory", "tool_records", "final"}
	for i, want := range wantTypes {
		if events[i].Type != want || events[i].ThreadID != "thread-1" || events[i].TurnID != "turn-1" {
			t.Fatalf("unexpected event %d: %+v", i, events[i])
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if strings.Contains(string(raw), "secret-value") || !strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("trace should redact secret-like final previews:\n%s", raw)
	}
}

func TestReplayTraceSummarizesSessionEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := AppendTurn(path,
		TurnRecord{ThreadID: "thread-1", TurnID: "turn-1", Status: "completed", Model: "gpt-test"},
		FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		[]tools.ToolInfo{{Name: "semantic_search", Kind: tools.ToolKindSearch, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{Name: "semantic_search", Kind: tools.ToolKindSearch, Success: true}},
	); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	summary, err := ReplayTrace(path)
	if err != nil {
		t.Fatalf("ReplayTrace: %v", err)
	}
	if summary.Mode != "session_trace_replay" || !summary.Complete || summary.EventCount != 4 {
		t.Fatalf("unexpected replay summary: %+v", summary)
	}
	if summary.LatestTurn == nil || summary.LatestTurn.ThreadID != "thread-1" || summary.LatestTurn.Model != "gpt-test" {
		t.Fatalf("latest turn missing: %+v", summary.LatestTurn)
	}
	if len(summary.ToolInventory) != 1 || summary.ToolInventory[0].Name != "semantic_search" {
		t.Fatalf("tool inventory missing: %+v", summary.ToolInventory)
	}
	if len(summary.ToolNames) != 1 || summary.ToolNames[0] != "semantic_search" {
		t.Fatalf("tool records missing: %+v", summary.ToolNames)
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
