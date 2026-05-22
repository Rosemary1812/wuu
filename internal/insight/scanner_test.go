package insight

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionstore "github.com/blueberrycongee/wuu/internal/session"
)

func TestScanSessionsAndFormatTranscriptHandleLargeToolRecords(t *testing.T) {
	dir := t.TempDir()
	sessionID := "20260413-101416-cd82"
	path := filepath.Join(dir, sessionID+".jsonl")
	largeToolResult := strings.Repeat("x", 2100*1024)
	start := time.Date(2026, time.April, 13, 10, 14, 16, 0, time.UTC)

	writeInsightSessionRecords(t, path, []memoryRecord{
		{Role: "user", Content: "restore this session", At: start},
		{
			Role:    "assistant",
			Content: "",
			At:      start.Add(10 * time.Second),
			ToolCalls: []toolCallRec{
				{ID: "call_big", Name: "write_file", Arguments: `{"file_path":"main.go"}`},
			},
		},
		{
			Role:       "tool",
			Content:    largeToolResult,
			ToolCallID: "call_big",
			Name:       "write_file",
			At:         start.Add(20 * time.Second),
		},
		{Role: "user", Content: "and keep it visible", At: start.Add(2 * time.Minute)},
		{Role: "assistant", Content: "done", At: start.Add(3 * time.Minute)},
	})

	metas, err := ScanSessions(dir, 0)
	if err != nil {
		t.Fatalf("ScanSessions: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("expected 1 session meta, got %d", len(metas))
	}
	meta := metas[0]
	if meta.ID != sessionID {
		t.Fatalf("unexpected session id: %q", meta.ID)
	}
	if meta.UserMessages != 2 {
		t.Fatalf("expected 2 user messages, got %d", meta.UserMessages)
	}
	if meta.AssistantMsgs != 2 {
		t.Fatalf("expected 2 assistant messages, got %d", meta.AssistantMsgs)
	}
	if meta.ToolCounts["write_file"] != 1 {
		t.Fatalf("expected write_file count 1, got %d", meta.ToolCounts["write_file"])
	}
	if meta.FilesModified != 1 {
		t.Fatalf("expected 1 modified file, got %d", meta.FilesModified)
	}

	transcript, err := FormatTranscript(dir, sessionID)
	if err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	if !strings.Contains(transcript, "[User]: restore this session") {
		t.Fatalf("expected first user message in transcript, got %q", transcript)
	}
	if !strings.Contains(transcript, "[User]: and keep it visible") {
		t.Fatalf("expected follow-up user message in transcript, got %q", transcript)
	}
	if !strings.Contains(transcript, "[Tool: write_file]") {
		t.Fatalf("expected tool call marker in transcript, got %q", transcript)
	}
	if strings.Contains(transcript, largeToolResult[:256]) {
		t.Fatal("expected large tool payload to stay out of transcript output")
	}
}

func TestScanSessionsForCWDFiltersBySessionIndex(t *testing.T) {
	dir := t.TempDir()
	cwdA := filepath.Join(t.TempDir(), "project-a")
	cwdB := filepath.Join(t.TempDir(), "project-b")
	start := time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC)

	if _, err := sessionstore.CreateWithMetadata(dir, "sess-a", cwdA); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateWithMetadata(dir, "sess-b", cwdB); err != nil {
		t.Fatal(err)
	}
	writeInsightSessionRecords(t, sessionstore.FilePath(dir, "sess-a"), []memoryRecord{
		{Role: "user", Content: "work in project a", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(10 * time.Second)},
		{Role: "user", Content: "continue project a", At: start.Add(2 * time.Minute)},
	})
	writeInsightSessionRecords(t, sessionstore.FilePath(dir, "sess-b"), []memoryRecord{
		{Role: "user", Content: "work in project b", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(10 * time.Second)},
		{Role: "user", Content: "continue project b", At: start.Add(2 * time.Minute)},
	})

	metas, err := ScanSessionsForCWD(dir, cwdA, 0)
	if err != nil {
		t.Fatalf("ScanSessionsForCWD: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "sess-a" {
		t.Fatalf("unexpected scoped metas: %+v", metas)
	}
}

func TestBuildUsageReportAggregatesLocalSessions(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, time.April, 15, 9, 0, 0, 0, time.UTC)

	writeInsightSessionRecords(t, filepath.Join(dir, "usage-a.jsonl"), []memoryRecord{
		{Role: "user", Content: "implement usage stats", At: start},
		{
			Role: "assistant",
			At:   start.Add(10 * time.Second),
			ToolCalls: []toolCallRec{
				{ID: "call_1", Name: "write_file", Arguments: `{"file_path":"internal/usage.go"}`},
			},
		},
		{Role: "tool", Content: `{"diff":{"hunks":[{"lines":[{"op":"insert"},{"op":"delete"}]}]}}`, ToolCallID: "call_1", Name: "write_file", At: start.Add(20 * time.Second)},
		{Role: "user", Content: "show the totals", At: start.Add(2 * time.Minute)},
		{Role: "assistant", Content: "done", At: start.Add(3 * time.Minute)},
		{Role: "meta", Content: "token_usage", InputTokens: 1200, OutputTokens: 300, At: start.Add(3 * time.Minute)},
	})

	report, err := BuildUsageReport(dir, 0)
	if err != nil {
		t.Fatalf("BuildUsageReport: %v", err)
	}
	if report.Stats.TotalSessions != 1 {
		t.Fatalf("expected 1 session, got %d", report.Stats.TotalSessions)
	}
	if report.Stats.TotalInputTokens != 1200 || report.Stats.TotalOutputTokens != 300 {
		t.Fatalf("unexpected token totals: in=%d out=%d", report.Stats.TotalInputTokens, report.Stats.TotalOutputTokens)
	}
	if report.Stats.ToolCounts["write_file"] != 1 {
		t.Fatalf("expected write_file count, got %+v", report.Stats.ToolCounts)
	}

	out := FormatUsageReport(report)
	for _, want := range []string{"Usage", "Sessions: 1", "Tokens: 1.2k input / 300 output", "Top tools", "Languages"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage output missing %q: %s", want, out)
		}
	}
}

func writeInsightSessionRecords(t *testing.T, path string, records []memoryRecord) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create session file: %v", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("encode session record: %v", err)
		}
	}
}
