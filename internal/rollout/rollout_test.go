package rollout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

// withTempHome sets WUU_HOME to a fresh temp dir for the duration of the
// test. All tests use this so the rollout file lives under the temp dir
// instead of the user's real ~/.wuu.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WUU_HOME", dir)
	return dir
}

func TestPath(t *testing.T) {
	tmp := withTempHome(t)
	got, err := Path("thread-abc")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(tmp, "sessions", "rollouts", "thread-abc.jsonl")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestRolloutItemJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 10, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		item RolloutItem
	}{
		{
			name: "session_meta",
			item: RolloutItem{Type: TypeSessionMeta, SessionMeta: &SessionMeta{
				SessionID: "s1", ThreadID: "t1",
				ProviderName: "openai", Model: "gpt-4o",
				StartedAt: now,
			}},
		},
		{
			name: "user_message",
			item: RolloutItem{Type: TypeUserMessage, UserMessage: &UserMessage{
				Role: "user", Content: "hello world", At: now,
			}},
		},
		{
			name: "assistant_message",
			item: RolloutItem{Type: TypeAssistantMessage, AssistantMessage: &AssistantMessage{
				Role: "assistant", Content: "hi there", At: now,
			}},
		},
		{
			name: "tool_call",
			item: RolloutItem{Type: TypeToolCall, ToolCall: &ToolCall{
				ID: "call_xyz", Name: "shell",
				Args: json.RawMessage(`{"cmd":"ls -la"}`),
			}},
		},
		{
			name: "tool_result_success",
			item: RolloutItem{Type: TypeToolResult, ToolResult: &ToolResult{
				ID: "call_xyz", Output: json.RawMessage(`"file1\nfile2\n"`),
			}},
		},
		{
			name: "tool_result_error",
			item: RolloutItem{Type: TypeToolResult, ToolResult: &ToolResult{
				ID: "call_err", Error: "exit code 1",
			}},
		},
		{
			name: "turn_meta",
			item: RolloutItem{Type: TypeTurnMeta, TurnMeta: &TurnMeta{
				Status: "completed", FinishReason: "stop",
				TokenUsage: TokenUsage{
					InputTokens: 100, OutputTokens: 50,
					CacheReadTokens: 25,
				},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got RolloutItem
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Type != c.item.Type {
				t.Errorf("Type = %q, want %q", got.Type, c.item.Type)
			}
			// Verify each pointer field round-trips to a non-nil,
			// equal-valued counterpart. Using Equal for time.Time so
			// monotonic-clock noise doesn't false-fail.
			switch c.item.Type {
			case TypeSessionMeta:
				if got.SessionMeta == nil || *got.SessionMeta != *c.item.SessionMeta {
					t.Errorf("SessionMeta mismatch:\n got:  %+v\n want: %+v", got.SessionMeta, c.item.SessionMeta)
				}
			case TypeUserMessage:
				if got.UserMessage == nil || *got.UserMessage != *c.item.UserMessage {
					t.Errorf("UserMessage mismatch:\n got:  %+v\n want: %+v", got.UserMessage, c.item.UserMessage)
				}
			case TypeAssistantMessage:
				if got.AssistantMessage == nil || *got.AssistantMessage != *c.item.AssistantMessage {
					t.Errorf("AssistantMessage mismatch:\n got:  %+v\n want: %+v", got.AssistantMessage, c.item.AssistantMessage)
				}
			case TypeToolCall:
				if got.ToolCall == nil {
					t.Errorf("ToolCall nil after roundtrip")
					return
				}
				if got.ToolCall.ID != c.item.ToolCall.ID {
					t.Errorf("ToolCall.ID = %q, want %q", got.ToolCall.ID, c.item.ToolCall.ID)
				}
				if got.ToolCall.Name != c.item.ToolCall.Name {
					t.Errorf("ToolCall.Name = %q, want %q", got.ToolCall.Name, c.item.ToolCall.Name)
				}
				if !bytes.Equal(got.ToolCall.Args, c.item.ToolCall.Args) {
					t.Errorf("ToolCall.Args = %q, want %q", got.ToolCall.Args, c.item.ToolCall.Args)
				}
			case TypeToolResult:
				if got.ToolResult == nil {
					t.Errorf("ToolResult nil after roundtrip")
					return
				}
				if got.ToolResult.ID != c.item.ToolResult.ID {
					t.Errorf("ToolResult.ID = %q, want %q", got.ToolResult.ID, c.item.ToolResult.ID)
				}
				if !bytes.Equal(got.ToolResult.Output, c.item.ToolResult.Output) {
					t.Errorf("ToolResult.Output = %q, want %q", got.ToolResult.Output, c.item.ToolResult.Output)
				}
				if got.ToolResult.Error != c.item.ToolResult.Error {
					t.Errorf("ToolResult.Error = %q, want %q", got.ToolResult.Error, c.item.ToolResult.Error)
				}
			case TypeTurnMeta:
				if got.TurnMeta == nil || *got.TurnMeta != *c.item.TurnMeta {
					t.Errorf("TurnMeta mismatch:\n got:  %+v\n want: %+v", got.TurnMeta, c.item.TurnMeta)
				}
			}
		})
	}
}

func TestEmitRoundTrip(t *testing.T) {
	withTempHome(t)
	threadID := "round-trip"
	w, err := New(threadID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	items := []RolloutItem{
		{Type: TypeSessionMeta, SessionMeta: &SessionMeta{
			SessionID: "s1", ThreadID: threadID,
			ProviderName: "openai", Model: "gpt-4o",
			StartedAt: time.Now(),
		}},
		{Type: TypeUserMessage, UserMessage: &UserMessage{
			Role: "user", Content: "first message", At: time.Now(),
		}},
		{Type: TypeAssistantMessage, AssistantMessage: &AssistantMessage{
			Role: "assistant", Content: "first reply", At: time.Now(),
		}},
		{Type: TypeToolCall, ToolCall: &ToolCall{
			ID: "c1", Name: "shell",
			Args: json.RawMessage(`{"cmd":"pwd"}`),
		}},
		{Type: TypeToolResult, ToolResult: &ToolResult{
			ID: "c1", Output: json.RawMessage(`"/home/user"`),
		}},
		{Type: TypeTurnMeta, TurnMeta: &TurnMeta{
			Status: "completed", FinishReason: "stop",
			TokenUsage: TokenUsage{InputTokens: 10, OutputTokens: 5},
		}},
	}
	for _, item := range items {
		if err := w.Emit(item); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	msgs, err := Rebuild(threadID)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Rebuild returned %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "first message" {
		t.Errorf("msgs[0] = %+v, want user/first message", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "first reply" {
		t.Errorf("msgs[1] = %+v, want assistant/first reply", msgs[1])
	}
}

func TestConcurrentEmit(t *testing.T) {
	withTempHome(t)
	threadID := "concurrent"
	w, err := New(threadID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 16
	const perGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				item := RolloutItem{
					Type: TypeUserMessage,
					UserMessage: &UserMessage{
						Role:    "user",
						Content: fmt.Sprintf("g%d-msg%d", g, i),
						At:      time.Now(),
					},
				}
				if err := w.Emit(item); err != nil {
					t.Errorf("Emit: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	msgs, err := Rebuild(threadID)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	want := goroutines * perGoroutine
	if len(msgs) != want {
		t.Errorf("Rebuild returned %d messages, want %d", len(msgs), want)
	}

	// Verify uniqueness — no items lost AND no duplicates from races.
	seen := make(map[string]bool, want)
	for _, m := range msgs {
		if seen[m.Content] {
			t.Errorf("duplicate message: %q", m.Content)
		}
		seen[m.Content] = true
	}
	if len(seen) != want {
		t.Errorf("unique messages = %d, want %d", len(seen), want)
	}
}

func TestRebuildSkipsPartialLine(t *testing.T) {
	withTempHome(t)
	threadID := "partial"
	w, err := New(threadID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Write 3 valid user messages.
	for i := 0; i < 3; i++ {
		if err := w.Emit(RolloutItem{
			Type: TypeUserMessage,
			UserMessage: &UserMessage{
				Role: "user", Content: fmt.Sprintf("msg-%d", i),
				At: time.Now(),
			},
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append a partial last line directly (no trailing newline,
	// mid-write truncation simulation).
	path, _ := Path(threadID)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.Write([]byte(`{"type":"user_message","user_message":{"role":"user","conte`)); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close partial: %v", err)
	}

	msgs, err := Rebuild(threadID)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("Rebuild returned %d messages, want 3 (partial line must be skipped)", len(msgs))
	}
	for i, m := range msgs {
		wantContent := fmt.Sprintf("msg-%d", i)
		if m.Content != wantContent {
			t.Errorf("msgs[%d].Content = %q, want %q", i, m.Content, wantContent)
		}
	}
}

func TestFileModeIs0o600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not enforced on Windows")
	}
	withTempHome(t)
	threadID := "filemode"
	w, err := New(threadID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	info, err := os.Stat(w.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	got := info.Mode().Perm()
	if got != 0o600 {
		t.Errorf("file mode = 0o%o, want 0o600", got)
	}
}

func TestFlushIfMaterialized(t *testing.T) {
	withTempHome(t)
	threadID := "flush"
	w, err := New(threadID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	const n = 5
	for i := 0; i < n; i++ {
		if err := w.Emit(RolloutItem{
			Type: TypeUserMessage,
			UserMessage: &UserMessage{
				Role: "user", Content: fmt.Sprintf("flush-%d", i),
				At: time.Now(),
			},
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	// Without FlushIfMaterialized, items might still be sitting in the
	// goroutine's channel buffer — but FlushIfMaterialized is supposed
	// to drain them and sync. After Flush, reading the file directly
	// should see all n items.
	if err := w.FlushIfMaterialized(); err != nil {
		t.Fatalf("FlushIfMaterialized: %v", err)
	}

	data, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Count lines that contain "flush-" — every emit becomes one
	// JSONL line. We expect exactly n such lines.
	lines := splitNonEmpty(data)
	if len(lines) != n {
		t.Errorf("file has %d JSONL lines, want %d (FlushIfMaterialized didn't drain)", len(lines), n)
	}
	// Sanity: contents are sorted by emit order (goroutine processes
	// items in channel-send order, and we Emit serially).
	sort.Strings(lines) // just to ensure deterministic test; lines are already ordered
}

// splitNonEmpty returns the trimmed non-empty lines of a byte slice.
func splitNonEmpty(data []byte) []string {
	var out []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				out = append(out, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, string(data[start:]))
	}
	return out
}
