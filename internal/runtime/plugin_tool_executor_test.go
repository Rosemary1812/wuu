package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/blueberrycongee/wuu/internal/pluginhost"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolctx"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

type recordingToolExecutor struct {
	mu    sync.Mutex
	calls []providers.ToolCall
}

func (e *recordingToolExecutor) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "demo", InputSchema: map[string]any{"type": "object"}}}
}
func (e *recordingToolExecutor) Execute(context.Context, providers.ToolCall) (string, error) {
	panic("rich path expected")
}
func (e *recordingToolExecutor) ExecuteResult(_ context.Context, call providers.ToolCall) (toolresult.Result, error) {
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	return toolresult.FromText(call.Arguments), nil
}

type toolPluginClient struct {
	mu     sync.Mutex
	inputs []pluginhost.ToolExecuteInput
}

func (c *toolPluginClient) ID() string { return "tool-plugin" }
func (c *toolPluginClient) Hooks() []pluginhost.Hook {
	return []pluginhost.Hook{pluginhost.HookToolExecuteBefore, pluginhost.HookToolExecuteAfter}
}
func (c *toolPluginClient) Status() pluginhost.Status {
	return pluginhost.Status{ID: c.ID(), State: pluginhost.StateActive, Hooks: c.Hooks()}
}
func (c *toolPluginClient) Close(context.Context) error { return nil }
func (c *toolPluginClient) Invoke(_ context.Context, params pluginhost.InvokeParams) (pluginhost.InvokeResult, error) {
	var input pluginhost.ToolExecuteInput
	if err := json.Unmarshal(params.Input, &input); err != nil {
		return pluginhost.InvokeResult{}, err
	}
	c.mu.Lock()
	c.inputs = append(c.inputs, input)
	c.mu.Unlock()
	switch params.Hook {
	case pluginhost.HookToolExecuteBefore:
		data, _ := json.Marshal(pluginhost.ToolExecuteBeforeOutput{Arguments: json.RawMessage(fmt.Sprintf(`{"value":%q}`, input.CallID))})
		return pluginhost.InvokeResult{Output: data}, nil
	case pluginhost.HookToolExecuteAfter:
		var output pluginhost.ToolExecuteAfterOutput
		if err := json.Unmarshal(params.Output, &output); err != nil {
			return pluginhost.InvokeResult{}, err
		}
		output.Result = toolresult.FromText(output.Result.TextProjection() + "|after:" + input.CallID)
		data, _ := json.Marshal(output)
		return pluginhost.InvokeResult{Output: data}, nil
	default:
		return pluginhost.InvokeResult{}, fmt.Errorf("unexpected hook %s", params.Hook)
	}
}

func TestPluginToolExecutorTransformsArgumentsAndRichResult(t *testing.T) {
	inner := &recordingToolExecutor{}
	plugin := &toolPluginClient{}
	executor := newPluginToolExecutor(inner, pluginhost.New(plugin), "thread-1", "/workspace")
	rich := executor.(interface {
		ExecuteResult(context.Context, providers.ToolCall) (toolresult.Result, error)
	})
	result, err := rich.ExecuteResult(toolctx.WithStepIndex(context.Background(), 4), providers.ToolCall{ID: "call-1", Name: "demo", Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if result.TextProjection() != `{"value":"call-1"}|after:call-1` {
		t.Fatalf("result = %q", result.TextProjection())
	}
	if len(inner.calls) != 1 || inner.calls[0].Arguments != `{"value":"call-1"}` {
		t.Fatalf("calls = %+v", inner.calls)
	}
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	for _, input := range plugin.inputs {
		if input.ThreadID != "thread-1" || input.CallID != "call-1" || input.CWD != "/workspace" || input.StepIndex != 4 {
			t.Fatalf("input = %+v", input)
		}
	}
}

func TestPluginToolExecutorKeepsConcurrentCallsIsolated(t *testing.T) {
	inner := &recordingToolExecutor{}
	plugin := &toolPluginClient{}
	executor := newPluginToolExecutor(inner, pluginhost.New(plugin), "thread", "/workspace")
	rich := executor.(interface {
		ExecuteResult(context.Context, providers.ToolCall) (toolresult.Result, error)
	})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := rich.ExecuteResult(context.Background(), providers.ToolCall{ID: id, Name: "demo", Arguments: `{}`})
			if err != nil {
				errs <- err
				return
			}
			want := fmt.Sprintf(`{"value":%q}|after:%s`, id, id)
			if result.TextProjection() != want {
				errs <- fmt.Errorf("call %s result = %q, want %q", id, result.TextProjection(), want)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
