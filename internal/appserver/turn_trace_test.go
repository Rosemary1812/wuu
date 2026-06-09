package appserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestPersistTurnTraceWritesSessionArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session-artifacts")
	kit.SetSessionDir(sessionDir)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-old-read",
		Name:      "read_file",
		Arguments: `{"path":"target.txt"}`,
	}); err != nil {
		t.Fatalf("old read_file: %v", err)
	}
	toolRecordStart := len(kit.ToolTelemetry())
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-new-read",
		Name:      "read_file",
		Arguments: `{"path":"target.txt"}`,
	}); err != nil {
		t.Fatalf("new read_file: %v", err)
	}

	startedAt := time.Now().UTC().Add(-time.Second)
	completedAt := time.Now().UTC()
	duration := int64(1000)
	srv := &Server{rt: &runtime.Session{ProviderName: "openai"}}
	runner := &agent.StreamRunner{Model: "gpt-test", APIModel: "gpt-test-api"}
	err = srv.persistTurnTrace(&runtime.ThreadRuntime{Toolkit: kit}, runner, "thread-1", Turn{
		ID:          "turn-1",
		Status:      TurnStatusCompleted,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		DurationMS:  &duration,
	}, agent.LoopResult{
		Content:      "done",
		InputTokens:  10,
		OutputTokens: 20,
	}, nil, toolRecordStart)
	if err != nil {
		t.Fatalf("persistTurnTrace: %v", err)
	}

	data, err := os.ReadFile(sessiontrace.Path(sessionDir))
	if err != nil {
		t.Fatalf("read session trace: %v", err)
	}
	trace := string(data)
	for _, want := range []string{`"type":"turn"`, `"type":"tool_inventory"`, `"type":"tool_records"`, `"type":"final"`, `"provider_name":"openai"`, `"model":"gpt-test"`, `"name":"read_file"`} {
		if !strings.Contains(trace, want) {
			t.Fatalf("session trace missing %s:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, "call-old-read") || !strings.Contains(trace, "call-new-read") {
		t.Fatalf("session trace should include only this turn's tool records:\n%s", trace)
	}
}
