package insight

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sessionstore "github.com/blueberrycongee/wuu/internal/session"
)

func TestScanSessionsHandleLargeToolRecords(t *testing.T) {
	dir := t.TempDir()
	sessionID := "20260413-101416-cd82"
	largeToolResult := strings.Repeat("x", 2100*1024)
	start := time.Date(2026, time.April, 13, 10, 14, 16, 0, time.UTC)

	if _, err := sessionstore.CreateWithMetadata(dir, sessionID, ""); err != nil {
		t.Fatal(err)
	}
	writeInsightSessionRecords(t, dir, sessionID, []memoryRecord{
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
	// The 2MB tool payload only contributes to the token estimate; it must
	// not break scanning or leak into the extracted first-user-message.
	if !strings.Contains(meta.FirstUserMsg, "restore this session") {
		t.Fatalf("unexpected first user message: %q", meta.FirstUserMsg)
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
	writeInsightSessionRecords(t, dir, "sess-anthropic", []memoryRecord{
		{Role: "user", Content: "implement feature a", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(time.Minute)},
		{Role: "user", Content: "continue feature a", At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "anthropic", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 50, CacheCreationTokens: 80, CacheReadTokens: 20, At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "anthropic", Model: "claude-sonnet-4-6", InputTokens: 200, OutputTokens: 80, CacheCreationTokens: 100, CacheReadTokens: 50, At: start.Add(3 * time.Minute)},
	})

	// Session 2: a different provider/model entirely.
	writeInsightSessionRecords(t, dir, "sess-openai", []memoryRecord{
		{Role: "user", Content: "implement feature b", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(time.Minute)},
		{Role: "user", Content: "continue feature b", At: start.Add(2 * time.Minute)},
		{Role: "meta", Content: "token_usage", Provider: "openai", Model: "gpt-4o", InputTokens: 500, OutputTokens: 200, CacheCreationTokens: 0, CacheReadTokens: 100, At: start.Add(2 * time.Minute)},
	})

	// Session 3: legacy row with no provider/model — should bucket as
	// "(unknown)".
	writeInsightSessionRecords(t, dir, "sess-mixed", []memoryRecord{
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

}

func TestCollectUsageScanCountsSkillCallsAcrossPersistedShapes(t *testing.T) {
	dir := t.TempDir()
	sessionID := "usage-skills"
	if _, err := sessionstore.CreateWithMetadata(dir, sessionID, ""); err != nil {
		t.Fatal(err)
	}
	writeInsightSessionRecords(t, dir, sessionID, []memoryRecord{
		{
			Role: "assistant",
			ToolCalls: []toolCallRec{
				{ID: "call-review", Name: "functions.load_skill", Arguments: `{"name":"review"}`},
			},
		},
		{
			Role:       "tool",
			Name:       "load_skill",
			ToolCallID: "call-review",
			Content:    `{"metadata":{"name":"review"}}`,
		},
		{
			// Provider checkpoints can retain the tool result without the
			// assistant call that originally carried its arguments.
			Role:       "tool",
			Name:       "functions/load_skill",
			ToolCallID: "call-commit",
			Content:    `{"metadata":{"name":"commit"}}`,
		},
		{
			Role:       "tool",
			Name:       "load_skill",
			ToolCallID: "call-malformed",
			Content:    `not-json`,
		},
	})

	scan, err := CollectUsageScan(dir)
	if err != nil {
		t.Fatalf("CollectUsageScan: %v", err)
	}
	if len(scan.Skills) != 2 {
		t.Fatalf("expected two skill buckets, got %+v", scan.Skills)
	}
	if scan.Skills[0] != (SkillUsage{Name: "commit", Count: 1}) ||
		scan.Skills[1] != (SkillUsage{Name: "review", Count: 1}) {
		t.Fatalf("unexpected skill usage: %+v", scan.Skills)
	}
}

func writeInsightSessionRecords(t *testing.T, sessDir, id string, records []memoryRecord) {
	t.Helper()

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
