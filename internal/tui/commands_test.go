package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
		name  string
		args  string
	}{
		{input: "", ok: false},
		{input: "hello", ok: false},
		{input: "/", ok: false},
		{input: "/resume", ok: true, name: "resume", args: ""},
		{input: " /WORKTREE  ", ok: true, name: "worktree", args: ""},
		{input: "/insight latest", ok: true, name: "insight", args: "latest"},
	}

	for _, tc := range tests {
		cmd, ok := parseSlashCommand(tc.input)
		if ok != tc.ok {
			t.Fatalf("input=%q: expected ok=%v got %v", tc.input, tc.ok, ok)
		}
		if !ok {
			continue
		}
		if cmd.Name != tc.name || cmd.Args != tc.args {
			t.Fatalf("input=%q: unexpected parse result: %#v", tc.input, cmd)
		}
	}
}

func TestHandleSlash(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	msg, handled := m.handleSlash("/skills")
	if !handled {
		t.Fatal("expected /skills to be handled")
	}
	if msg == "" {
		t.Fatal("expected non-empty message")
	}

	msg, handled = m.handleSlash("/unknown")
	if !handled {
		t.Fatal("expected unknown slash command to be handled")
	}
	if msg == "" {
		t.Fatal("expected unknown slash command message")
	}
}

func TestCoordinatorCommandRequiresExperimentalMode(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
	})

	msg, handled := m.handleSlash("/coordinator")
	if !handled {
		t.Fatal("expected slash command to be handled")
	}
	if !strings.Contains(msg, "unknown command") {
		t.Fatalf("expected coordinator command to be hidden by default, got %q", msg)
	}
	if _, ok := findCommandSpec("coordinator"); ok {
		t.Fatal("package-level lookup should hide experimental coordinator by default")
	}

	m = NewModel(Config{
		Provider:                    "test",
		Model:                       "test-model",
		ConfigPath:                  "/tmp/.wuu.json",
		ExperimentalCoordinatorMode: true,
	})
	msg, handled = m.handleSlash("/coordinator")
	if !handled {
		t.Fatal("expected experimental coordinator command to be handled")
	}
	if !strings.Contains(msg, "coordinator mode") {
		t.Fatalf("unexpected coordinator response: %q", msg)
	}
}

func TestInsightsAliasRemainsSupported(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
	})

	msg, handled := m.handleSlash("/insight")
	if !handled {
		t.Fatal("expected /insight alias to be handled")
	}
	if !strings.Contains(msg, "insights: no session directory configured") {
		t.Fatalf("unexpected alias response: %q", msg)
	}

	if _, ok := findCommandSpec("insights"); !ok {
		t.Fatal("expected canonical /insights command")
	}
	if cmd, ok := findCommandSpec("insight"); !ok || cmd.Name != "insights" {
		t.Fatalf("expected /insight alias to resolve to /insights, got %#v ok=%v", cmd, ok)
	}
}

func TestReviewCommandQueuesDefaultCodexPrompt(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	msg, handled := m.handleSlash("/review")
	if !handled {
		t.Fatal("expected /review to be handled")
	}
	if msg != "review queued: current changes" {
		t.Fatalf("unexpected /review response: %q", msg)
	}
	if len(m.messageQueue) != 1 {
		t.Fatalf("expected one queued review prompt, got %d", len(m.messageQueue))
	}
	if m.messageQueue[0].Text != defaultReviewPrompt {
		t.Fatalf("unexpected review prompt: %q", m.messageQueue[0].Text)
	}
}

func TestReviewCommandQueuesCustomInstructions(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	msg, handled := m.handleSlash("/review check regressions")
	if !handled {
		t.Fatal("expected /review with instructions to be handled")
	}
	if msg != "review queued: custom instructions" {
		t.Fatalf("unexpected /review response: %q", msg)
	}
	if len(m.messageQueue) != 1 {
		t.Fatalf("expected one queued review prompt, got %d", len(m.messageQueue))
	}
	prompt := m.messageQueue[0].Text
	if !strings.HasPrefix(prompt, defaultReviewPrompt) {
		t.Fatalf("review prompt should start with Codex default prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Additional review instructions:\ncheck regressions") {
		t.Fatalf("review prompt missing custom instructions: %q", prompt)
	}
}

func TestReviewCommandSpec(t *testing.T) {
	cmd, ok := findCommandSpec("review")
	if !ok {
		t.Fatal("expected /review command to be registered")
	}
	if cmd.ArgMode != slashArgOptional {
		t.Fatalf("expected optional args for /review, got %q", cmd.ArgMode)
	}
	if cmd.Kind != slashCommandKindPrompt {
		t.Fatalf("expected prompt command kind for /review, got %q", cmd.Kind)
	}
	if cmd.AvailableDuringTask {
		t.Fatal("/review should be disabled while a task is in progress")
	}
}

func TestHandleSlashNewResetsChatHistoryButKeepsSystemPrompt(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client:       &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:        "test-model",
			SystemPrompt: "system rules",
		},
	})
	m.chatHistory = []providers.ChatMessage{
		{Role: "system", Content: "system rules"},
		{Role: "user", Content: "old user"},
		{Role: "assistant", Content: "old assistant"},
		{Role: "tool", Content: "old tool"},
	}
	m.entries = []transcriptEntry{{Role: "USER", Content: "visible old entry"}}

	msg, handled := m.handleSlash("/new")
	if !handled {
		t.Fatal("expected /new to be handled")
	}
	if msg == "" {
		t.Fatal("expected /new response message")
	}
	if len(m.entries) != 0 {
		t.Fatalf("expected /new to clear visible entries, got %d", len(m.entries))
	}
	if len(m.chatHistory) != 1 {
		t.Fatalf("expected /new to keep only system prompt in chat history, got %+v", m.chatHistory)
	}
	if m.chatHistory[0].Role != "system" || m.chatHistory[0].Content != "system rules" {
		t.Fatalf("expected /new to preserve system prompt, got %+v", m.chatHistory[0])
	}
}

func TestHandleSlashNewClearsChatHistoryWithoutSystemPrompt(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})
	m.chatHistory = []providers.ChatMessage{
		{Role: "user", Content: "old user"},
		{Role: "assistant", Content: "old assistant"},
	}

	_, handled := m.handleSlash("/new")
	if !handled {
		t.Fatal("expected /new to be handled")
	}
	if len(m.chatHistory) != 0 {
		t.Fatalf("expected /new to clear all chat history without system prompt, got %+v", m.chatHistory)
	}
}

func TestHandleSlashRespectsTaskAvailability(t *testing.T) {
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})
	m.pendingRequest = true
	m.entries = []transcriptEntry{{Role: "USER", Content: "keep visible history"}}

	msg, handled := m.handleSlash("/new")
	if !handled {
		t.Fatal("expected /new to be handled")
	}
	if !strings.Contains(msg, "disabled while a task is in progress") {
		t.Fatalf("expected task availability message, got %q", msg)
	}
	if len(m.entries) != 1 {
		t.Fatalf("expected disabled /new not to clear entries, got %d", len(m.entries))
	}

	msg, handled = m.handleSlash("/status")
	if !handled {
		t.Fatal("expected /status to be handled")
	}
	if !strings.Contains(msg, "provider: test") {
		t.Fatalf("expected /status to run during task, got %q", msg)
	}
}

func TestModelsCommandRejectsUnsupportedProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".wuu.json")
	data := `{
  "default_provider": "test",
  "providers": {
    "test": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key": "sk-test",
      "model": "test-model"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	m := NewModel(Config{
		Provider:      "test",
		Model:         "test-model",
		ConfigPath:    configPath,
		WorkspaceRoot: dir,
	})

	msg, handled := m.handleSlash("/models")
	if !handled {
		t.Fatal("expected /models to be handled")
	}
	if !strings.Contains(msg, "openai-codex only") {
		t.Fatalf("unexpected /models response: %q", msg)
	}
}

func TestCmdUsageUsesLocalSessionStats(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	writeCommandMemoryRecords(t, session.FilePath(dir, "usage-session"), []memoryEntry{
		{Role: "user", Content: "measure usage", At: start},
		{Role: "assistant", Content: "ok", At: start.Add(10 * time.Second)},
		{Role: "user", Content: "continue", At: start.Add(2 * time.Minute)},
		{Role: "assistant", Content: "done", At: start.Add(3 * time.Minute)},
		{Role: "meta", Content: "token_usage", InputTokens: 10, OutputTokens: 4, At: start.Add(3 * time.Minute)},
	})
	m := NewModel(Config{
		Provider:   "test",
		Model:      "test-model",
		ConfigPath: "/tmp/.wuu.json",
		SessionDir: dir,
	})

	out := cmdUsage("", &m)
	if !strings.Contains(out, "Usage") || !strings.Contains(out, "Sessions: 1") {
		t.Fatalf("unexpected usage output: %q", out)
	}
	if !strings.Contains(out, "Tokens: 10 input / 4 output") {
		t.Fatalf("expected token totals in usage output, got %q", out)
	}
}

func writeCommandMemoryRecords(t *testing.T, path string, records []memoryEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create memory records: %v", err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("encode memory record: %v", err)
		}
	}
}

func TestCmdLoopStoresSessionOnlyTask(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	m := NewModel(Config{
		Provider:      "test",
		Model:         "test-model",
		WorkspaceRoot: root,
		ConfigPath:    filepath.Join(root, ".wuu.json"),
		StateDir:      stateDir,
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	out := cmdLoop("5m check deploy", &m)
	if !strings.Contains(out, "in this session only") {
		t.Fatalf("expected session-only message, got %q", out)
	}

	fileTasks, err := cron.NewTaskStore(statepath.ScheduledTasksPath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 0 {
		t.Fatalf("expected no durable tasks, got %d", len(fileTasks))
	}

	sessionTasks, err := cron.NewSessionTaskStore(stateDir).List()
	if err != nil {
		t.Fatalf("session store list: %v", err)
	}
	if len(sessionTasks) != 1 {
		t.Fatalf("expected 1 session task, got %d", len(sessionTasks))
	}
	if len(m.messageQueue) != 1 {
		t.Fatalf("expected 1 queued message, got %d", len(m.messageQueue))
	}
	if m.messageQueue[0].ScheduledTaskID != sessionTasks[0].ID {
		t.Fatalf("expected queued message to track task id %q, got %q", sessionTasks[0].ID, m.messageQueue[0].ScheduledTaskID)
	}
}

func TestCmdTasksShowsSessionOnlyTasks(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	store := cron.NewSessionTaskStore(stateDir)
	if err := store.Add(cron.Task{
		ID:        "abc123",
		Cron:      "*/5 * * * *",
		Prompt:    "check deploy",
		CreatedAt: 1,
		Recurring: true,
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	m := NewModel(Config{
		Provider:      "test",
		Model:         "test-model",
		WorkspaceRoot: root,
		ConfigPath:    filepath.Join(root, ".wuu.json"),
		StateDir:      stateDir,
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	out := cmdTasks("", &m)
	if !strings.Contains(out, "[session-only]") {
		t.Fatalf("expected session-only label, got %q", out)
	}
}

func TestCmdUnloopRemovesTaskAndQueuedRun(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	store := cron.NewSessionTaskStore(stateDir)
	durablePath := statepath.ScheduledTasksPath(stateDir)
	task := cron.Task{
		ID:        "abc123",
		Cron:      "*/5 * * * *",
		Prompt:    "check deploy",
		CreatedAt: 1,
		Recurring: true,
	}
	if err := store.Add(task); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	m := NewModel(Config{
		Provider:      "test",
		Model:         "test-model",
		WorkspaceRoot: root,
		ConfigPath:    filepath.Join(root, ".wuu.json"),
		StateDir:      stateDir,
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})
	m.messageQueue = []queuedMessage{
		{Text: "check deploy", ScheduledTaskID: "abc123"},
		{Text: "keep me"},
	}

	out := cmdUnloop("abc123", &m)
	if !strings.Contains(out, "removed 1 queued run") {
		t.Fatalf("expected queued run cleanup message, got %q", out)
	}

	tasks, err := store.List()
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected task removed, got %d", len(tasks))
	}
	if len(m.messageQueue) != 1 || m.messageQueue[0].Text != "keep me" {
		t.Fatalf("expected unrelated queued message preserved, got %#v", m.messageQueue)
	}
	if _, err := os.Stat(durablePath); !os.IsNotExist(err) {
		t.Fatalf("expected session-only unloop not to create durable task file, got err=%v", err)
	}
}

func TestCmdUnloopRejectsUnknownTask(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	m := NewModel(Config{
		Provider:      "test",
		Model:         "test-model",
		WorkspaceRoot: root,
		ConfigPath:    filepath.Join(root, ".wuu.json"),
		StateDir:      stateDir,
		StreamRunner: &agent.StreamRunner{
			Client: &echoStreamClient{answer: func(_ []providers.ChatMessage) string { return "" }},
			Model:  "test-model",
		},
	})

	out := cmdUnloop("missing", &m)
	if !strings.Contains(out, `no scheduled task with id "missing"`) {
		t.Fatalf("unexpected response: %q", out)
	}
}

func TestCommandCompletionEnterBehavior(t *testing.T) {
	tests := []struct {
		name string
		want slashCompletionEnterBehavior
	}{
		{name: "help", want: slashCompletionExecute},
		{name: "exit", want: slashCompletionExecute},
		{name: "model", want: slashCompletionInsertOnly},
		{name: "resume", want: slashCompletionInsertOnly},
		{name: "worktree", want: slashCompletionInsertOnly},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var found *command
			for i := range commandRegistry {
				if commandRegistry[i].Name == tc.name {
					found = &commandRegistry[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("command %q not found", tc.name)
			}
			if got := found.completionEnterBehavior(); got != tc.want {
				t.Fatalf("completionEnterBehavior() = %v, want %v", got, tc.want)
			}
		})
	}
}
