package runtime

import (
	"context"
	"time"

	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func startPluginHost(plugins []pluginpkg.Plugin, projectRoot, wuuHome string) *pluginhost.Host {
	host := pluginhost.New()
	for _, item := range plugins {
		if item.Runtime == nil {
			continue
		}
		timeout := time.Duration(item.Runtime.Timeout) * time.Second
		client, err := pluginhost.Start(context.Background(), pluginhost.ProcessConfig{
			ID:          item.ID,
			Command:     item.Runtime.Command,
			Args:        item.Runtime.Args,
			Env:         item.Runtime.Env,
			PluginRoot:  item.Root,
			ProjectRoot: projectRoot,
			WuuHome:     wuuHome,
			Timeout:     timeout,
		})
		if err != nil {
			host.Add(pluginhost.Failed(item.ID, err))
			continue
		}
		host.Add(client)
	}
	return host
}

func pluginRequestInterceptor(host *pluginhost.Host, provider, threadID, cwd string) func(context.Context, *providers.ChatRequest) error {
	if host == nil {
		return nil
	}
	return func(ctx context.Context, request *providers.ChatRequest) error {
		if request == nil {
			return nil
		}
		output := pluginhost.ChatRequestOutput{
			Model:                       request.Model,
			Messages:                    append([]providers.ChatMessage(nil), request.Messages...),
			Tools:                       append([]providers.ToolDefinition(nil), request.Tools...),
			Temperature:                 request.Temperature,
			MaxTokens:                   request.MaxTokens,
			Effort:                      request.Effort,
			NativeDeferredToolDiscovery: request.NativeDeferredToolDiscovery,
			ForceToolName:               request.ForceToolName,
		}
		if request.ProviderOptions != nil {
			output.ProviderOptions = make(map[string]any, len(request.ProviderOptions))
			for key, value := range request.ProviderOptions {
				output.ProviderOptions[key] = value
			}
		}
		if err := host.Run(ctx, pluginhost.HookChatRequest, pluginhost.ChatRequestInput{
			SessionID: threadID,
			ThreadID:  threadID,
			CWD:       cwd,
			Provider:  provider,
			StepIndex: request.StepIndex,
		}, &output); err != nil {
			return err
		}
		request.Model = output.Model
		request.Messages = output.Messages
		request.Tools = output.Tools
		request.Temperature = output.Temperature
		request.MaxTokens = output.MaxTokens
		request.Effort = output.Effort
		request.ProviderOptions = output.ProviderOptions
		request.NativeDeferredToolDiscovery = output.NativeDeferredToolDiscovery
		request.ForceToolName = output.ForceToolName
		return nil
	}
}
