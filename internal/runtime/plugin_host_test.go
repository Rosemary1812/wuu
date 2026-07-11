package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type runtimePluginClient struct {
	input pluginhost.ChatRequestInput
}

func (c *runtimePluginClient) ID() string { return "runtime-test" }
func (c *runtimePluginClient) Hooks() []pluginhost.Hook {
	return []pluginhost.Hook{pluginhost.HookChatRequest}
}
func (c *runtimePluginClient) Status() pluginhost.Status {
	return pluginhost.Status{ID: c.ID(), State: pluginhost.StateActive}
}
func (c *runtimePluginClient) Close(context.Context) error { return nil }
func (c *runtimePluginClient) Invoke(_ context.Context, params pluginhost.InvokeParams) (pluginhost.InvokeResult, error) {
	if err := json.Unmarshal(params.Input, &c.input); err != nil {
		return pluginhost.InvokeResult{}, err
	}
	var output pluginhost.ChatRequestOutput
	if err := json.Unmarshal(params.Output, &output); err != nil {
		return pluginhost.InvokeResult{}, err
	}
	output.Model = "plugin-model"
	output.ProviderOptions = map[string]any{"plugin": true}
	data, err := json.Marshal(output)
	return pluginhost.InvokeResult{Output: data}, err
}

func TestPluginRequestInterceptorCarriesThreadContextAndTransformsRequest(t *testing.T) {
	client := &runtimePluginClient{}
	host := pluginhost.New(client)
	intercept := pluginRequestInterceptor(host, "openai", "thread-1", "/workspace")
	request := providers.ChatRequest{
		Model:           "original",
		Messages:        []providers.ChatMessage{{Role: "user", Content: "hello"}},
		StepIndex:       2,
		ProviderOptions: map[string]any{"original": true},
	}
	if err := intercept(context.Background(), &request); err != nil {
		t.Fatal(err)
	}
	if request.Model != "plugin-model" || request.ProviderOptions["plugin"] != true {
		t.Fatalf("request = %+v", request)
	}
	if client.input.ThreadID != "thread-1" || client.input.SessionID != "thread-1" || client.input.CWD != "/workspace" || client.input.Provider != "openai" || client.input.StepIndex != 2 {
		t.Fatalf("input = %+v", client.input)
	}
}

func TestStartPluginHostPreservesRuntimeFailure(t *testing.T) {
	host := startPluginHost([]pluginpkg.Plugin{{
		Manifest: pluginpkg.Manifest{ID: "broken", Runtime: &pluginpkg.RuntimeSpec{
			Protocol: pluginhost.ProtocolName,
			Command:  "/definitely/not/a/wuu-plugin",
		}},
		Root: t.TempDir(),
	}}, t.TempDir(), t.TempDir())
	statuses := host.Statuses()
	if len(statuses) != 1 || statuses[0].State != pluginhost.StateFailed || !strings.Contains(statuses[0].Error, "start plugin") {
		t.Fatalf("statuses = %+v", statuses)
	}
}
