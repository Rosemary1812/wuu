package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolctx"
)

type runtimeTestTools struct {
	mu         sync.Mutex
	metadata   map[string]ToolMetadata
	results    map[string]string
	delays     map[string]time.Duration
	discovered map[string][]providers.LoadableToolDefinition
	calls      []providers.ToolCall
	steps      []int
}

type cancelAwareRuntimeTools struct {
	started  chan struct{}
	canceled chan struct{}
	once     sync.Once

	mu    sync.Mutex
	calls []providers.ToolCall
}

func (f *runtimeTestTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{
		{Name: "read_file"},
		{Name: "run_shell"},
	}
}

func (f *runtimeTestTools) ToolMetadata(call providers.ToolCall) (ToolMetadata, bool) {
	meta, ok := f.metadata[call.Name]
	return meta, ok
}

func (f *runtimeTestTools) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	if stepIndex, ok := toolctx.StepIndex(ctx); ok {
		f.steps = append(f.steps, stepIndex)
	}
	delay := f.delays[call.ID]
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != nil {
		if result, ok := f.results[call.ID]; ok {
			return result, nil
		}
	}
	return `{"ok":"` + call.ID + `"}`, nil
}

func (f *runtimeTestTools) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.discovered[call.ID]
}

func (f *runtimeTestTools) recordedCalls() []providers.ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]providers.ToolCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *runtimeTestTools) recordedSteps() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, len(f.steps))
	copy(out, f.steps)
	return out
}

func (f *cancelAwareRuntimeTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "read_file"}}
}

func (f *cancelAwareRuntimeTools) ToolMetadata(_ providers.ToolCall) (ToolMetadata, bool) {
	return ToolMetadata{ReadOnly: true, ConcurrencySafe: true}, true
}

func (f *cancelAwareRuntimeTools) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if call.ID != "orphan" {
		return `{"ok":"` + call.ID + `"}`, nil
	}
	f.once.Do(func() { close(f.started) })
	select {
	case <-ctx.Done():
		close(f.canceled)
		return "", ctx.Err()
	case <-time.After(time.Second):
		return "", context.DeadlineExceeded
	}
}

func (f *cancelAwareRuntimeTools) recordedCalls() []providers.ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]providers.ToolCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestTurnToolRuntime_PreservesToolSearchKindOnResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{
		results: map[string]string{"search_1": `{"loadable_tools":[]}`},
	}
	runtime := NewTurnToolRuntime(tools)

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{{
		ID:        "search_1",
		Name:      "tool_search",
		Kind:      providers.ToolCallKindToolSearch,
		Arguments: `{"query":"docs"}`,
	}}, nil)

	calls := tools.recordedCalls()
	if len(calls) != 1 || calls[0].Kind != providers.ToolCallKindToolSearch {
		t.Fatalf("tool call kind not preserved into execution: %+v", calls)
	}
	if len(msgs) != 1 || msgs[0].ToolResultKind != providers.ToolCallKindToolSearch {
		t.Fatalf("tool result kind not preserved: %+v", msgs)
	}
}

func TestTurnToolRuntimeAnnotatesToolExecutionStep(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{}
	runtime := NewTurnToolRuntime(tools)
	runtime.SetStepIndex(3)

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{{
		ID:   "call_1",
		Name: "read_file",
	}}, nil)
	if len(msgs) != 1 || msgs[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected tool result messages: %+v", msgs)
	}
	if steps := tools.recordedSteps(); len(steps) != 1 || steps[0] != 3 {
		t.Fatalf("tool execution steps = %+v, want [3]", steps)
	}
}

func TestTurnToolRuntimeAttachesDiscoveredToolsToToolResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{
		discovered: map[string][]providers.LoadableToolDefinition{
			"spawn_1": {{
				Type:        "function",
				Name:        "await_agents",
				Description: "Wait for subagents",
				InputSchema: map[string]any{"type": "object"},
			}},
		},
	}
	runtime := NewTurnToolRuntime(tools)

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{{
		ID:   "spawn_1",
		Name: "spawn_agent",
	}}, nil)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 tool message, got %+v", msgs)
	}
	if got := msgs[0].DiscoveredTools; len(got) != 1 || got[0].Name != "await_agents" {
		t.Fatalf("unexpected discovered tools on tool result: %+v", got)
	}
	msgs[0].DiscoveredTools[0].Name = "mutated"
	if tools.discovered["spawn_1"][0].Name != "await_agents" {
		t.Fatalf("tool result discovered tools should be cloned before attaching")
	}
}

func TestTurnToolRuntime_ReusesStreamingStartedConcurrentRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{
		metadata: map[string]ToolMetadata{
			"read_file": {ReadOnly: true, ConcurrencySafe: true},
		},
		results: map[string]string{
			"call_1": `{"cached":1}`,
			"call_2": `{"cached":2}`,
		},
		delays: map[string]time.Duration{
			"call_1": 20 * time.Millisecond,
			"call_2": 20 * time.Millisecond,
		},
	}
	runtime := NewTurnToolRuntime(tools)
	runtime.SetStepIndex(5)

	for _, call := range []providers.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		{ID: "call_2", Name: "read_file", Arguments: `{"path":"b.go"}`},
	} {
		runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
			Type:     providers.EventToolUseStart,
			ToolCall: &providers.ToolCall{ID: call.ID, Name: call.Name},
		})
		runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
			Type:     providers.EventToolUseEnd,
			ToolCall: &call,
		})
	}

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		{ID: "call_2", Name: "read_file", Arguments: `{"path":"b.go"}`},
	}, nil)

	calls := tools.recordedCalls()
	if len(calls) != 2 {
		t.Fatalf("expected each streamed call to execute once, got %d calls: %+v", len(calls), calls)
	}
	if steps := tools.recordedSteps(); len(steps) != 2 || steps[0] != 5 || steps[1] != 5 {
		t.Fatalf("streamed tool execution steps = %+v, want [5 5]", steps)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 tool messages, got %d", len(msgs))
	}
	if msgs[0].ToolCallID != "call_1" || msgs[0].Content != `{"cached":1}` {
		t.Fatalf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].ToolCallID != "call_2" || msgs[1].Content != `{"cached":2}` {
		t.Fatalf("unexpected second message: %+v", msgs[1])
	}
}

func TestTurnToolRuntime_DoesNotStreamStartAcrossWriteBarrier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{
		metadata: map[string]ToolMetadata{
			"run_shell": {ReadOnly: false, ConcurrencySafe: false},
			"read_file": {ReadOnly: true, ConcurrencySafe: true},
		},
	}
	runtime := NewTurnToolRuntime(tools)

	for _, call := range []providers.ToolCall{
		{ID: "write_first", Name: "run_shell", Arguments: `{"command":"touch x"}`},
		{ID: "read_second", Name: "read_file", Arguments: `{"path":"x"}`},
	} {
		runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
			Type:     providers.EventToolUseStart,
			ToolCall: &providers.ToolCall{ID: call.ID, Name: call.Name},
		})
		runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
			Type:     providers.EventToolUseEnd,
			ToolCall: &call,
		})
	}

	if calls := tools.recordedCalls(); len(calls) != 0 {
		t.Fatalf("expected no streaming-started calls across write barrier, got %+v", calls)
	}

	_ = runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{
		{ID: "write_first", Name: "run_shell", Arguments: `{"command":"touch x"}`},
		{ID: "read_second", Name: "read_file", Arguments: `{"path":"x"}`},
	}, nil)

	calls := tools.recordedCalls()
	if len(calls) != 2 {
		t.Fatalf("expected final execution of both calls, got %+v", calls)
	}
	if calls[0].ID != "write_first" || calls[1].ID != "read_second" {
		t.Fatalf("expected final call order to respect write barrier, got %+v", calls)
	}
}

func TestTurnToolRuntime_StreamingErrorFallsBackToFinalExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &runtimeTestTools{
		metadata: map[string]ToolMetadata{
			"read_file": {ReadOnly: true, ConcurrencySafe: true},
		},
		results: map[string]string{
			"call_1": `{"ok":true}`,
		},
	}
	runtime := NewTurnToolRuntime(tools)
	runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
		Type:     providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{ID: "call_1", Name: "read_file"},
	})
	runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "call_1",
			Name:      "read_file",
			Arguments: `{"path":"a.go"}`,
		},
	})
	runtime.Cancel()

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
	}, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected one tool message, got %d", len(msgs))
	}
	if msgs[0].Content != `{"ok":true}` {
		t.Fatalf("expected fallback execution result, got %+v", msgs[0])
	}
	if calls := tools.recordedCalls(); len(calls) == 0 {
		t.Fatal("expected the tool to execute at least once")
	}
}

func TestTurnToolRuntime_CancelsStreamingRunsMissingFromFinalCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tools := &cancelAwareRuntimeTools{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
	}
	runtime := NewTurnToolRuntime(tools)
	runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
		Type:     providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{ID: "orphan", Name: "read_file"},
	})
	runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "orphan",
			Name:      "read_file",
			Arguments: `{"path":"orphan.go"}`,
		},
	})

	select {
	case <-tools.started:
	case <-time.After(time.Second):
		t.Fatal("expected orphan streaming run to start")
	}

	msgs := runtime.ExecuteFinalCalls(ctx, []providers.ToolCall{
		{ID: "kept", Name: "read_file", Arguments: `{"path":"kept.go"}`},
	}, nil)
	if len(msgs) != 1 || msgs[0].ToolCallID != "kept" {
		t.Fatalf("expected only final kept result, got %+v", msgs)
	}
	select {
	case <-tools.canceled:
	case <-time.After(time.Second):
		t.Fatal("expected missing streaming run to be canceled")
	}

	runtime.mu.Lock()
	_, stillRegistered := runtime.byID["orphan"]
	runtime.mu.Unlock()
	if stillRegistered {
		t.Fatal("expected missing streaming run to be dropped from runtime registry")
	}
}

func BenchmarkTurnToolRuntimeNoStreamingOverlap(b *testing.B) {
	benchmarkTurnToolRuntimeOverlap(b, false)
}

func BenchmarkTurnToolRuntimeStreamingOverlap(b *testing.B) {
	benchmarkTurnToolRuntimeOverlap(b, true)
}

func benchmarkTurnToolRuntimeOverlap(b *testing.B, streaming bool) {
	calls := []providers.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		{ID: "call_2", Name: "read_file", Arguments: `{"path":"b.go"}`},
		{ID: "call_3", Name: "read_file", Arguments: `{"path":"c.go"}`},
		{ID: "call_4", Name: "read_file", Arguments: `{"path":"d.go"}`},
	}
	const toolDelay = time.Millisecond
	const modelTail = time.Millisecond

	var totalToolExecs int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		tools := &runtimeTestTools{
			metadata: map[string]ToolMetadata{
				"read_file": {ReadOnly: true, ConcurrencySafe: true},
			},
			delays: map[string]time.Duration{
				"call_1": toolDelay,
				"call_2": toolDelay,
				"call_3": toolDelay,
				"call_4": toolDelay,
			},
		}
		runtime := NewTurnToolRuntime(tools)
		if streaming {
			for _, call := range calls {
				runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
					Type:     providers.EventToolUseStart,
					ToolCall: &providers.ToolCall{ID: call.ID, Name: call.Name},
				})
				runtime.ObserveStreamEvent(ctx, providers.StreamEvent{
					Type:     providers.EventToolUseEnd,
					ToolCall: &call,
				})
			}
		}
		time.Sleep(modelTail)
		msgs := runtime.ExecuteFinalCalls(ctx, calls, nil)
		if len(msgs) != len(calls) {
			b.Fatalf("expected %d tool messages, got %d", len(calls), len(msgs))
		}
		totalToolExecs += len(tools.recordedCalls())
	}
	b.StopTimer()
	b.ReportMetric(float64(totalToolExecs)/float64(b.N), "tool_execs/op")
}
