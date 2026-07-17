package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolctx"
	"github.com/blueberrycongee/wuu/internal/toolerrors"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

type pluginToolExecutor struct {
	inner    agent.ToolExecutor
	host     *pluginhost.Host
	threadID string
	cwd      string
}

func newPluginToolExecutor(inner agent.ToolExecutor, host *pluginhost.Host, threadID, cwd string) agent.ToolExecutor {
	if inner == nil || host == nil {
		return inner
	}
	return &pluginToolExecutor{inner: inner, host: host, threadID: threadID, cwd: cwd}
}

func (e *pluginToolExecutor) Definitions() []providers.ToolDefinition {
	return e.inner.Definitions()
}

func (e *pluginToolExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	result, err := e.ExecuteResult(ctx, call)
	return result.TextProjection(), err
}

func (e *pluginToolExecutor) ExecuteResult(ctx context.Context, call providers.ToolCall) (toolresult.Result, error) {
	input, err := e.toolInput(ctx, call)
	if err != nil {
		return toolresult.Result{}, err
	}
	before := pluginhost.ToolExecuteBeforeOutput{Arguments: append(json.RawMessage(nil), input.Arguments...)}
	if err := e.host.Run(ctx, pluginhost.HookToolExecuteBefore, input, &before); err != nil {
		return toolresult.Result{}, err
	}
	if len(before.Arguments) == 0 || !json.Valid(before.Arguments) {
		return toolresult.Result{}, fmt.Errorf("plugin tool.execute.before returned invalid arguments for %q", call.Name)
	}
	call.Arguments = string(before.Arguments)
	input.Arguments = append(input.Arguments[:0], before.Arguments...)

	var result toolresult.Result
	var executeErr error
	if rich, ok := e.inner.(agent.RichToolExecutor); ok {
		result, executeErr = rich.ExecuteResult(ctx, call)
	} else {
		var text string
		text, executeErr = e.inner.Execute(ctx, call)
		result = toolresult.FromText(text)
	}
	if executeErr != nil {
		result.IsError = true
	}
	after := pluginhost.ToolExecuteAfterOutput{Result: result.Clone()}
	if executeErr != nil {
		after.Error = executeErr.Error()
	}
	if err := e.host.Run(ctx, pluginhost.HookToolExecuteAfter, input, &after); err != nil {
		return result, err
	}
	if err := after.Result.Validate(); err != nil {
		return toolresult.Result{}, fmt.Errorf("plugin tool.execute.after returned invalid result for %q: %w", call.Name, err)
	}
	if strings.TrimSpace(after.Error) != "" {
		return after.Result, errors.New(after.Error)
	}
	return after.Result, nil
}

func (e *pluginToolExecutor) toolInput(ctx context.Context, call providers.ToolCall) (pluginhost.ToolExecuteInput, error) {
	arguments := strings.TrimSpace(call.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	if !json.Valid([]byte(arguments)) {
		return pluginhost.ToolExecuteInput{}, toolerrors.New(
			toolerrors.InvalidArguments,
			fmt.Sprintf("tool %q arguments are invalid JSON", call.Name),
		)
	}
	stepIndex, _ := toolctx.StepIndex(ctx)
	return pluginhost.ToolExecuteInput{
		SessionID: e.threadID,
		ThreadID:  e.threadID,
		CWD:       e.cwd,
		StepIndex: stepIndex,
		CallID:    call.ID,
		Tool:      call.Name,
		Arguments: json.RawMessage(arguments),
	}, nil
}

func (e *pluginToolExecutor) SupportsTool(name string) bool {
	provider, ok := e.inner.(agent.ToolSupportProvider)
	return ok && provider.SupportsTool(name)
}

func (e *pluginToolExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	provider, ok := e.inner.(agent.ToolMetadataProvider)
	if !ok {
		return agent.ToolMetadata{}, false
	}
	return provider.ToolMetadata(call)
}

func (e *pluginToolExecutor) ToolDisplay(call providers.ToolCall) (providers.ToolCallDisplay, bool) {
	provider, ok := e.inner.(agent.ToolDisplayProvider)
	if !ok {
		return providers.ToolCallDisplay{}, false
	}
	return provider.ToolDisplay(call)
}

func (e *pluginToolExecutor) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	provider, ok := e.inner.(agent.ToolDiscoveryProvider)
	if !ok {
		return nil
	}
	return provider.DiscoveredTools(call)
}
