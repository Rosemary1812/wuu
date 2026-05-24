package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type runtimeTestTools struct {
	mu       sync.Mutex
	metadata map[string]ToolMetadata
	results  map[string]string
	delays   map[string]time.Duration
	calls    []providers.ToolCall
}

func (f *runtimeTestTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{
		{Name: "read_file"},
		{Name: "run_shell"},
	}
}

func (f *runtimeTestTools) ToolMetadata(name string) (ToolMetadata, bool) {
	meta, ok := f.metadata[name]
	return meta, ok
}

func (f *runtimeTestTools) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
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

func (f *runtimeTestTools) recordedCalls() []providers.ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]providers.ToolCall, len(f.calls))
	copy(out, f.calls)
	return out
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
