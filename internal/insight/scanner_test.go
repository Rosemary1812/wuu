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

func TestScanSessionsAggregatesModelBreakdowns(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)

	if _, err := sessionstore.CreateWithMetadata(dir, "sess-anthropic", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateWithMetadata(dir, "sess-openai", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateWithMetadata(dir, "sess-mixed", ""); err != nil {
		t.Fatal(err)
	}

	// Session 1: two token_usage rows on the same provider/model — should
	// collapse into a single bucket with summed tokens.
	writeInsightSessionRecords(t, sessionstore.FilePath(dir, "sess-anthropic"), []memoryRecord{
		{Role: "user", Content: "implement feature a", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(time.Minute)},
		{Role: "user", Content: "continue feature a", At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "anthropic", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 50, CacheCreationTokens: 80, CacheReadTokens: 20, At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "anthropic", Model: "claude-sonnet-4-6", InputTokens: 200, OutputTokens: 80, CacheCreationTokens: 100, CacheReadTokens: 50, At: start.Add(3 * time.Minute)},
	})

	// Session 2: a different provider/model entirely.
	writeInsightSessionRecords(t, sessionstore.FilePath(dir, "sess-openai"), []memoryRecord{
		{Role: "user", Content: "implement feature b", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(time.Minute)},
		{Role: "user", Content: "continue feature b", At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "openai", Model: "gpt-4o", InputTokens: 500, OutputTokens: 200, CacheCreationTokens: 0, CacheReadTokens: 100, At: start.Add(2 * time.Minute)},
	})

	// Session 3: legacy row with no provider/model — should bucket as
	// "(unknown)".
	writeInsightSessionRecords(t, sessionstore.FilePath(dir, "sess-mixed"), []memoryRecord{
		{Role: "user", Content: "legacy session", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(time.Minute)},
		{Role: "user", Content: "more", At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", InputTokens: 50, OutputTokens: 10, CacheCreationTokens: 0, CacheReadTokens: 0, At: start.Add(2 * time.Minute)},
	})

	metas, err := ScanSessions(dir, 0)
	if err != nil {
		t.Fatalf("ScanSessions: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(metas))
	}

	byID := make(map[string]SessionMeta, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
	}

	aMeta := byID["sess-anthropic"]
	if got := len(aMeta.ModelBreakdowns); got != 1 {
		t.Fatalf("anthropic session: expected 1 bucket, got %d (%+v)", got, aMeta.ModelBreakdowns)
	}
	for _, b := range aMeta.ModelBreakdowns {
		if b.Provider != "anthropic" || b.Model != "claude-sonnet-4-6" {
			t.Fatalf("anthropic session: unexpected bucket %+v", b)
		}
		if b.InputTokens != 300 || b.OutputTokens != 130 || b.CacheCreationTokens != 180 || b.CacheReadTokens != 70 {
			t.Fatalf("anthropic session: unexpected bucket totals %+v", b)
		}
	}

	oMeta := byID["sess-openai"]
	if got := len(oMeta.ModelBreakdowns); got != 1 {
		t.Fatalf("openai session: expected 1 bucket, got %d", got)
	}
	for _, b := range oMeta.ModelBreakdowns {
		if b.Provider != "openai" || b.Model != "gpt-4o" {
			t.Fatalf("openai session: unexpected bucket %+v", b)
		}
	}

	mMeta := byID["sess-mixed"]
	if got := len(mMeta.ModelBreakdowns); got != 1 {
		t.Fatalf("mixed session: expected 1 bucket, got %d", got)
	}
	for _, b := range mMeta.ModelBreakdowns {
		if b.Provider != "" || b.Model != "" {
			t.Fatalf("legacy session: bucket should have empty provider/model, got %+v", b)
		}
	}

	// Aggregate: 3 distinct provider/model buckets, sorted by total context
	// tokens desc.
	agg := Aggregate(metas, nil)
	if got := len(agg.ModelBreakdowns); got != 3 {
		t.Fatalf("aggregate: expected 3 buckets, got %d (%+v)", got, agg.ModelBreakdowns)
	}
	// Anthropic total = 300 + 70 + 130 = 500
	// OpenAI total    = 500 + 100 + 200 = 800
	// (unknown) total = 50 + 0 + 10 = 60
	if agg.ModelBreakdowns[0].Provider != "openai" {
		t.Fatalf("aggregate: expected openai first, got %q", agg.ModelBreakdowns[0].Provider)
	}
	if agg.ModelBreakdowns[1].Provider != "anthropic" {
		t.Fatalf("aggregate: expected anthropic second, got %q", agg.ModelBreakdowns[1].Provider)
	}
	if agg.ModelBreakdowns[2].Provider != "" {
		t.Fatalf("aggregate: expected (unknown) last, got %q", agg.ModelBreakdowns[2].Provider)
	}

	for _, b := range agg.ModelBreakdowns {
		if b.Sessions != 1 {
			t.Fatalf("bucket %+v: expected Sessions=1, got %d", b, b.Sessions)
		}
	}

	// Cache hit rate: cacheRead / (input + cacheRead) across all buckets.
	// = (70 + 100 + 0) / ((300 + 500 + 50) + (70 + 100 + 0)) = 170 / 1020.
	expectedRate := float64(170) / float64(1020)
	if diff := agg.OverallCacheHitRate - expectedRate; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("aggregate: OverallCacheHitRate=%f, want %f", agg.OverallCacheHitRate, expectedRate)
	}
}

func TestUsageDataPathsStayUnderUserState(t *testing.T) {
	home := t.TempDir()
	usageDir := UsageDataDir(home)
	if want := filepath.Join(home, ".wuu", "usage-data"); usageDir != want {
		t.Fatalf("UsageDataDir() = %q, want %q", usageDir, want)
	}
	if want := filepath.Join(usageDir, "cache"); CacheDir(usageDir) != want {
		t.Fatalf("CacheDir() = %q, want %q", CacheDir(usageDir), want)
	}

	report := &Report{
		Stats:       AggregatedData{TotalSessions: 1},
		GeneratedAt: time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC),
	}
	path, err := GenerateHTML(usageDir, report)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if path != filepath.Join(usageDir, "report.html") {
		t.Fatalf("GenerateHTML path = %q, want report.html under usage dir", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
}

func writeInsightSessionRecords(t *testing.T, path string, records []memoryRecord) {
	t.Helper()

	if sessDir, id, ok := sessionstore.ParseHistoryPath(path); ok {
		if _, exists, err := sessionstore.Find(sessDir, id); err != nil {
			t.Fatalf("find session: %v", err)
		} else if !exists {
			if _, err := sessionstore.CreateWithMetadata(sessDir, id, ""); err != nil {
				t.Fatalf("create session: %v", err)
			}
		}
		history := make([]sessionstore.HistoryRecord, 0, len(records))
		for _, rec := range records {
			history = append(history, historyRecordFromMemoryRecord(t, rec))
		}
		if err := sessionstore.RewriteHistoryRecords(sessDir, id, history); err != nil {
			t.Fatalf("rewrite session records: %v", err)
		}
		return
	}

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

func historyRecordFromMemoryRecord(t *testing.T, rec memoryRecord) sessionstore.HistoryRecord {
	t.Helper()
	toolCalls, err := json.Marshal(rec.ToolCalls)
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	return sessionstore.HistoryRecord{
		Role:                rec.Role,
		Content:             rec.Content,
		ToolCalls:           toolCalls,
		ToolCallID:          rec.ToolCallID,
		Name:                rec.Name,
		At:                  rec.At,
		InputTokens:         rec.InputTokens,
		OutputTokens:        rec.OutputTokens,
		CacheCreationTokens: rec.CacheCreationTokens,
		CacheReadTokens:     rec.CacheReadTokens,
		Provider:            rec.Provider,
		Model:               rec.Model,
	}
}
