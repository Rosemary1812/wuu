package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

// stubExecutor is a fake ToolExecutor for testing.
type stubExecutor struct {
	result string
	err    error
	calls  []providers.ToolCall
	defs   []providers.ToolDefinition
}

func (s *stubExecutor) Definitions() []providers.ToolDefinition { return s.defs }
func (s *stubExecutor) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	s.calls = append(s.calls, call)
	return s.result, s.err
}

type supportStubExecutor struct {
	stubExecutor
	supported map[string]bool
}

func (s *supportStubExecutor) SupportsTool(name string) bool {
	return s.supported[name]
}

type discoveryStubExecutor struct {
	stubExecutor
	discovered     []providers.LoadableToolDefinition
	discoveryCalls []providers.ToolCall
}

type richStubExecutor struct {
	stubExecutor
	result toolresult.Result
}

func (s *richStubExecutor) ExecuteResult(_ context.Context, call providers.ToolCall) (toolresult.Result, error) {
	s.calls = append(s.calls, call)
	return s.result.Clone(), s.err
}

func (s *discoveryStubExecutor) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	s.discoveryCalls = append(s.discoveryCalls, call)
	return s.discovered
}

func TestHookedExecutor_PassThrough(t *testing.T) {
	inner := &stubExecutor{result: `{"ok":true}`}
	d := NewDispatcher(NewRegistry(nil))
	exec := NewHookedExecutor(inner, d, "sess-1", "/tmp")

	result, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "read_file", Arguments: `{"path":"foo.txt"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"ok":true}` {
		t.Fatalf("unexpected result: %s", result)
	}
	if len(inner.calls) != 1 {
		t.Fatal("inner should be called once")
	}
}

func TestHookedExecutor_PreToolBlock(t *testing.T) {
	inner := &stubExecutor{result: `{"ok":true}`}
	r := NewRegistry(map[Event][]HookConfig{
		PreToolUse: {{Matcher: "run_shell", Command: "exit 2"}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	_, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "run_shell", Arguments: `{"command":"rm -rf /"}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsBlocked(err) {
		t.Fatalf("expected blocked, got: %v", err)
	}
	if len(inner.calls) != 0 {
		t.Fatal("inner should not be called when blocked")
	}
}

func TestHookedExecutor_PreToolBlock_NonMatching(t *testing.T) {
	inner := &stubExecutor{result: `ok`}
	r := NewRegistry(map[Event][]HookConfig{
		PreToolUse: {{Matcher: "run_shell", Command: "exit 2"}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	// read_file should not be blocked.
	_, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "read_file", Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("read_file should not be blocked, got: %v", err)
	}
	if len(inner.calls) != 1 {
		t.Fatal("inner should be called for non-matching tool")
	}
}

func TestHookedExecutor_UpdatedInput(t *testing.T) {
	inner := &stubExecutor{result: `{}`}
	r := NewRegistry(map[Event][]HookConfig{
		PreToolUse: {{Matcher: "*", Command: `echo '{"updated_input":{"command":"echo safe"}}'`}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	_, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "run_shell", Arguments: `{"command":"rm -rf /"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatal("expected inner to be called once")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(inner.calls[0].Arguments), &m); err != nil {
		t.Fatal(err)
	}
	if m["command"] != "echo safe" {
		t.Fatalf("expected updated args, got %v", m)
	}
}

func TestHookedExecutor_PostToolUseFailureFires(t *testing.T) {
	inner := &stubExecutor{err: errors.New("disk full")}
	r := NewRegistry(map[Event][]HookConfig{
		PostToolUseFailure: {{Matcher: "*", Command: "true"}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	_, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "write_file", Arguments: `{"path":"x"}`,
	})
	// Original error should propagate.
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("expected 'disk full' error, got: %v", err)
	}
}

func TestHookedExecutor_PostToolSuccessFires(t *testing.T) {
	inner := &stubExecutor{result: `ok`}
	r := NewRegistry(map[Event][]HookConfig{
		PostToolUse: {{Matcher: "*", Command: "true"}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	result, err := exec.Execute(context.Background(), providers.ToolCall{
		Name: "read_file", Arguments: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("expected ok, got %s", result)
	}
}

func TestHookedExecutorPreservesRichResultAndSendsStableProjectionToHooks(t *testing.T) {
	inputPath := t.TempDir() + "/hook-input.json"
	result := toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: "text", Text: "visible"},
			{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png"},
		},
		StructuredContent: json.RawMessage(`{"answer":42}`),
		Meta:              json.RawMessage(`{"private":"kept"}`),
		Activity:          &toolresult.ActivityRef{ID: "activity-1", Kind: "browser"},
	}
	inner := &richStubExecutor{result: result}
	r := NewRegistry(map[Event][]HookConfig{
		PostToolUse: {{Matcher: "*", Command: fmt.Sprintf("cat > %q", inputPath)}},
	})
	exec := NewHookedExecutor(inner, NewDispatcher(r), "sess-1", "/tmp")

	got, err := exec.ExecuteResult(context.Background(), providers.ToolCall{Name: "browser_observe", Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if got.JSONProjection() != result.JSONProjection() {
		t.Fatalf("rich result changed across hooks:\ngot  %s\nwant %s", got.JSONProjection(), result.JSONProjection())
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read hook input: %v", err)
	}
	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("parse hook input: %v", err)
	}
	if input.ToolResponse != result.HookProjection() {
		t.Fatalf("hook projection = %q, want %q", input.ToolResponse, result.HookProjection())
	}
}

func TestHookedExecutor_DefinitionsDelegates(t *testing.T) {
	inner := &stubExecutor{}
	exec := NewHookedExecutor(inner, NewDispatcher(nil), "", "")
	defs := exec.Definitions()
	if defs != nil {
		t.Fatal("expected nil definitions from stub")
	}
}

func TestHookedExecutor_SupportsToolDelegates(t *testing.T) {
	inner := &supportStubExecutor{supported: map[string]bool{"inception": true}}
	exec := NewHookedExecutor(inner, NewDispatcher(nil), "", "")

	if !exec.SupportsTool("inception") {
		t.Fatal("expected hooked executor to preserve inner deferred tool support")
	}
	if exec.SupportsTool("missing") {
		t.Fatal("missing tool should not be supported")
	}
}

func TestHookedExecutor_SupportsToolFallsBackToDefinitions(t *testing.T) {
	inner := &stubExecutor{defs: []providers.ToolDefinition{{Name: "read_file"}}}
	exec := NewHookedExecutor(inner, NewDispatcher(nil), "", "")

	if !exec.SupportsTool("READ_FILE") {
		t.Fatal("expected hooked executor to support direct definition names")
	}
	if exec.SupportsTool("inception") {
		t.Fatal("unlisted tool should not be supported without inner support provider")
	}
}

func TestHookedExecutor_DiscoveredToolsDelegates(t *testing.T) {
	inner := &discoveryStubExecutor{
		discovered: []providers.LoadableToolDefinition{{
			Name:        "await_agents",
			Description: "Wait for running agents.",
			InputSchema: map[string]any{"type": "object"},
		}},
	}
	exec := NewHookedExecutor(inner, NewDispatcher(nil), "", "")

	call := providers.ToolCall{ID: "call-1", Name: "spawn_agent"}
	discovered := exec.DiscoveredTools(call)
	if len(discovered) != 1 || discovered[0].Name != "await_agents" {
		t.Fatalf("expected discovered await_agents to be forwarded, got %#v", discovered)
	}
	if len(inner.discoveryCalls) != 1 || inner.discoveryCalls[0].ID != "call-1" {
		t.Fatalf("expected discovery call to be forwarded, got %#v", inner.discoveryCalls)
	}

	discovered[0].Name = "mutated"
	if inner.discovered[0].Name != "await_agents" {
		t.Fatal("expected forwarded discovered tools to be cloned")
	}
}
