package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
)

type fakeClient struct {
	mu        sync.Mutex
	requests  []providers.ChatRequest
	responses []providers.ChatResponse
	response  providers.ChatResponse
}

func (f *fakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	if len(f.responses) > 0 {
		res := f.responses[0]
		f.responses = f.responses[1:]
		f.mu.Unlock()
		return res, nil
	}
	res := f.response
	f.mu.Unlock()
	return res, nil
}

type blockingStreamClient struct {
	started chan struct{}
	release chan struct{}
	content string
	once    sync.Once
}

func newBlockingStreamClient(content string) *blockingStreamClient {
	return &blockingStreamClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		content: content,
	}
}

func (c *blockingStreamClient) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	<-c.started
	select {
	case <-c.release:
	case <-ctx.Done():
		return providers.ChatResponse{}, ctx.Err()
	}
	return providers.ChatResponse{Content: c.content}, nil
}

func (c *blockingStreamClient) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent, 4)
	c.once.Do(func() { close(c.started) })
	go func() {
		defer close(ch)
		select {
		case <-c.release:
		case <-ctx.Done():
			ch <- providers.StreamEvent{Type: providers.EventError, Error: ctx.Err()}
			return
		}
		if c.content != "" {
			ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: c.content}
		}
		ch <- providers.StreamEvent{Type: providers.EventDone}
	}()
	return ch, nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

type noopToolExecutor struct{}

func (noopToolExecutor) Definitions() []providers.ToolDefinition { return nil }
func (noopToolExecutor) Execute(_ context.Context, _ providers.ToolCall) (string, error) {
	return "", nil
}

type blockingToolExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingToolExecutor() *blockingToolExecutor {
	return &blockingToolExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingToolExecutor) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{
		Name:        "wait_for_steer",
		Description: "wait for a steer test signal",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}}
}

func (b *blockingToolExecutor) Execute(ctx context.Context, _ providers.ToolCall) (string, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return `{"ok":true}`, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServerInitializeAndConfigRead(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ToolPolicy = config.ToolPolicyConfig{
		Profile:       "balanced",
		DefaultAction: "allow",
		Tools:         map[string]string{"run_shell": "require_approval"},
		Risks:         map[string]string{"high": "deny"},
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"initialize"}`)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := srv.handleLine(context.Background(), []byte(`{"id":"2","method":"config/read"}`)); err != nil {
		t.Fatalf("config/read: %v", err)
	}

	msgs := parseOutput(t, out.String())
	initMsg := responseByID(t, msgs, "1")
	initResult := remarshal[InitializeResult](t, initMsg["result"])
	if initResult.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol version: %+v", initResult)
	}
	if initResult.Core.Version == "" {
		t.Fatalf("expected core.version in initialize result, got %+v", initResult)
	}
	if initResult.Core.Commit == "" {
		t.Fatalf("expected core.commit in initialize result, got %+v", initResult)
	}
	if initResult.Model != "fake-model" || initResult.Provider != "fake-provider" {
		t.Fatalf("unexpected initialize result: %+v", initResult)
	}
	if initResult.ToolPolicy.Profile != "balanced" || initResult.ToolPolicy.Tools["run_shell"] != "require_approval" {
		t.Fatalf("initialize missing tool policy summary: %+v", initResult.ToolPolicy)
	}

	configMsg := responseByID(t, msgs, "2")
	configResult := remarshal[ConfigReadResult](t, configMsg["result"])
	if configResult.ConfigPath == "" || configResult.SessionDir == "" {
		t.Fatalf("expected config paths, got %+v", configResult)
	}
	if configResult.ToolPolicy.Profile != "balanced" || configResult.ToolPolicy.Risks["high"] != "deny" {
		t.Fatalf("config/read missing tool policy summary: %+v", configResult.ToolPolicy)
	}
}

func TestProviderSummariesExposeOpenCodeStyleVariants(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "xiaomi",
		Providers: map[string]config.ProviderConfig{
			"xiaomi": {
				Type:    "openai-compatible",
				BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
				Model:   "mimo-v2.5-pro",
			},
			"anthropic": {
				Type:    "anthropic",
				BaseURL: "https://anthropic.example.test",
				Model:   "claude-opus-4-6",
			},
			"openai": {
				Type:  "openai",
				Model: "gpt-5.5",
			},
			"openrouter": {
				Type:    "openai-compatible",
				BaseURL: "https://openrouter.ai/api/v1",
				Model:   "openai/gpt-5.5",
			},
		},
	}

	summaries := providerSummariesFromConfig(cfg, t.TempDir())
	var xiaomi, anthropic, openai, openrouter ProviderSummary
	for _, summary := range summaries {
		switch summary.Name {
		case "xiaomi":
			xiaomi = summary
		case "anthropic":
			anthropic = summary
		case "openai":
			openai = summary
		case "openrouter":
			openrouter = summary
		}
	}
	if len(xiaomi.Models) < 2 {
		t.Fatalf("expected xiaomi catalog models, got %+v", xiaomi)
	}
	xiaomiModel := providerModelByID(t, xiaomi, "mimo-v2.5-pro")
	if xiaomiModel.DisplayName != "MiMo-V2.5-Pro" || xiaomiModel.Source != "models.dev" {
		t.Fatalf("unexpected xiaomi model summary: %+v", xiaomiModel)
	}
	if got := variantIDs(xiaomiModel.Variants); strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("xiaomi variants = %+v", got)
	}
	if got := xiaomiModel.Variants[0].Options["reasoningEffort"]; got != "low" {
		t.Fatalf("xiaomi low variant options = %#v", xiaomiModel.Variants[0].Options)
	}
	anthropicModel := providerModelByID(t, anthropic, "claude-opus-4-6")
	if got := variantIDs(anthropicModel.Variants); strings.Join(got, ",") != "low,medium,high,max" {
		t.Fatalf("anthropic variants = %+v", got)
	}
	openaiModel := providerModelByID(t, openai, "gpt-5.5")
	if got := variantIDs(openaiModel.Variants); strings.Join(got, ",") != "none,low,medium,high,xhigh" {
		t.Fatalf("openai variants = %+v", got)
	}
	if got := openaiModel.Variants[0].Options["reasoningSummary"]; got != "auto" {
		t.Fatalf("openai variant options = %#v", openaiModel.Variants[0].Options)
	}
	openrouterModel := providerModelByID(t, openrouter, "openai/gpt-5.5")
	if got := variantIDs(openrouterModel.Variants); strings.Join(got, ",") != "none,low,medium,high,xhigh" {
		t.Fatalf("openrouter variants = %+v", got)
	}
	reasoning, ok := openrouterModel.Variants[0].Options["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "none" {
		t.Fatalf("openrouter variant options = %#v", openrouterModel.Variants[0].Options)
	}
}

func TestProviderSummariesMergeConfiguredVariants(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "custom",
		Providers: map[string]config.ProviderConfig{
			"custom": {
				Type:    "openai-compatible",
				BaseURL: "https://example.test/v1",
				Model:   "custom-model",
				Models: map[string]config.ProviderModelConfig{
					"custom-model": {
						DefaultVariant: "deep",
						Variants: map[string]map[string]any{
							"deep":     {"reasoningEffort": "high"},
							"disabled": {"disabled": true, "reasoningEffort": "low"},
						},
					},
				},
			},
		},
	}

	summaries := providerSummariesFromConfig(cfg, t.TempDir())
	if len(summaries) != 1 || len(summaries[0].Models) != 1 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
	model := summaries[0].Models[0]
	if model.DefaultVariant != "deep" {
		t.Fatalf("DefaultVariant = %q, want deep", model.DefaultVariant)
	}
	if got := variantIDs(model.Variants); strings.Join(got, ",") != "deep" {
		t.Fatalf("variants = %+v", got)
	}
}

func TestProviderSummariesPreferConfiguredVariantsOverCatalog(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:  "openai",
				Model: "gpt-5.5",
				Models: map[string]config.ProviderModelConfig{
					"gpt-5.5": {
						DefaultVariant: "deep",
						Variants: map[string]map[string]any{
							"deep": {"reasoningEffort": "high"},
						},
					},
				},
			},
		},
	}

	summaries := providerSummariesFromConfig(cfg, t.TempDir())
	model := providerModelByID(t, summaries[0], "gpt-5.5")
	if model.Source != "config" || model.DisplayName != "GPT-5.5" {
		t.Fatalf("unexpected configured model summary: %+v", model)
	}
	if got := variantIDs(model.Variants); strings.Join(got, ",") != "deep" {
		t.Fatalf("variants = %+v", got)
	}
}

func TestProviderSummariesDoNotShowOfficialModelsForCustomAnthropicEndpoint(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "zhipu2",
		Providers: map[string]config.ProviderConfig{
			"zhipu2": {
				Type:    "anthropic",
				BaseURL: "https://open.bigmodel.cn/api/anthropic",
				Model:   "glm-5.1",
			},
		},
	}

	summaries := providerSummariesFromConfig(cfg, t.TempDir())
	if len(summaries) != 1 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
	if len(summaries[0].Models) != 1 || summaries[0].Models[0].ID != "glm-5.1" {
		t.Fatalf("custom endpoint should only expose configured model, got %+v", summaries[0].Models)
	}
}

func TestProviderSummariesDoNotShowOpenCodeModelsForCodexSubscription(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai-codex",
		Providers: map[string]config.ProviderConfig{
			"openai-codex": {
				Type:    "openai-codex",
				BaseURL: "https://chatgpt.com/backend-api/codex",
				Model:   "gpt-5.5",
			},
		},
	}

	summaries := providerSummariesFromConfig(cfg, t.TempDir())
	if len(summaries) != 1 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
	if len(summaries[0].Models) != 1 || summaries[0].Models[0].ID != "gpt-5.5" {
		t.Fatalf("Codex subscription should only expose selected model before live load, got %+v", summaries[0].Models)
	}
}

func providerModelByID(t *testing.T, provider ProviderSummary, id string) ProviderModelSummary {
	t.Helper()
	for _, model := range provider.Models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("model %s not found in provider %+v", id, provider)
	return ProviderModelSummary{}
}

func variantIDs(variants []ProviderModelVariantSummary) []string {
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		out = append(out, variant.ID)
	}
	return out
}

func TestServerConfigModelUpdate(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"model":"new-model"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, msgs, "1")["result"])
	if result.Provider != "fake-provider" || result.Model != "new-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 1 || result.Providers[0].Name != "fake-provider" || result.Providers[0].Model != "new-model" {
		t.Fatalf("unexpected provider summaries: %+v", result.Providers)
	}
	if rt.Model != "new-model" || rt.StreamRunner.Model != "new-model" {
		t.Fatalf("runtime model not updated: runtime=%q stream_runner=%q", rt.Model, rt.StreamRunner.Model)
	}
	if rt.StreamRunner.ContextWindowOverride != providers.ContextWindowFor("new-model") {
		t.Fatalf("context window override not updated: got %d", rt.StreamRunner.ContextWindowOverride)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"model": "new-model"`) {
		t.Fatalf("config model was not persisted: %s", data)
	}
}

func TestServerConfigModelUpdateReconfiguresEditTools(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("new runtime toolkit: %v", err)
	}
	threadKit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("new thread toolkit: %v", err)
	}
	rt.Toolkit = kit
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)
	thread := newThreadState("thread-1", nil, rt.ProviderName, rt.Model, rt.RootDir, "", time.Now().UTC())
	thread.execRuntime = &runtime.ThreadRuntime{
		StreamRunner: &agent.StreamRunner{Model: "fake-model", APIModel: "fake-model"},
		Toolkit:      threadKit,
	}
	srv.threads[thread.ID] = thread

	if defs := toolDefinitionNames(rt.Toolkit.Definitions()); defs["apply_patch"] || !defs["edit_file"] {
		t.Fatalf("fixture should start in text edit mode: %+v", defs)
	}
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"model":"gpt-5.5"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	if rt.StreamRunner.APIModel != "gpt-5.5" {
		t.Fatalf("runtime APIModel not updated: %q", rt.StreamRunner.APIModel)
	}
	if defs := toolDefinitionNames(rt.Toolkit.Definitions()); !defs["apply_patch"] || defs["edit_file"] || defs["write_file"] {
		t.Fatalf("runtime toolkit should switch to patch edit mode: %+v", defs)
	}
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.ModelProvider != "fake-provider" || thread.Model != "gpt-5.5" {
		t.Fatalf("idle thread model metadata not updated: provider=%q model=%q", thread.ModelProvider, thread.Model)
	}
	if thread.execRuntime.StreamRunner.APIModel != "gpt-5.5" {
		t.Fatalf("idle thread APIModel not updated: %q", thread.execRuntime.StreamRunner.APIModel)
	}
	if defs := toolDefinitionNames(thread.execRuntime.Toolkit.Definitions()); !defs["apply_patch"] || defs["edit_file"] || defs["write_file"] {
		t.Fatalf("idle thread toolkit should switch to patch edit mode: %+v", defs)
	}
}

func TestServerConfigModelUpdateSwitchesProvider(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    },
    "codex-provider": {
      "type": "openai-codex",
      "base_url": "https://chatgpt.example.test/backend-api/codex",
      "model": "old-codex-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldClient := rt.StreamRunner.Client
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"provider":"codex-provider","model":"new-codex-model"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "codex-provider" || result.Model != "new-codex-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 2 {
		t.Fatalf("expected two provider summaries, got %+v", result.Providers)
	}
	var codexSummary ProviderSummary
	for _, summary := range result.Providers {
		if summary.Name == "codex-provider" {
			codexSummary = summary
			break
		}
	}
	if !codexSummary.ConnectionLocked {
		t.Fatalf("expected codex provider connection to be locked: %+v", result.Providers)
	}
	if rt.ProviderName != "codex-provider" || rt.Model != "new-codex-model" || rt.StreamRunner.Model != "new-codex-model" {
		t.Fatalf("runtime provider/model not updated: provider=%q runtime=%q runner=%q", rt.ProviderName, rt.Model, rt.StreamRunner.Model)
	}
	if rt.StreamRunner.Client == oldClient {
		t.Fatal("expected stream runner client to be rebuilt")
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"default_provider": "codex-provider"`) ||
		!strings.Contains(string(data), `"model": "new-codex-model"`) {
		t.Fatalf("provider selection was not persisted: %s", data)
	}
}

func TestServerConfigModelUpdatePersistsProviderConnection(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "api_key": "old-key",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldClient := rt.StreamRunner.Client
	out := &lockedBuffer{}
	srv := New(rt, out)

	req := `{"id":"1","method":"config/model/update","params":{"provider":"fake-provider","model":"new-model","base_url":"https://custom.example.test/v1","api_key":"new-key"}}`
	if err := srv.handleLine(context.Background(), []byte(req)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "fake-provider" || result.Model != "new-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 1 ||
		result.Providers[0].BaseURL != "https://custom.example.test/v1" ||
		!result.Providers[0].APIKeyConfigured {
		t.Fatalf("unexpected provider summaries: %+v", result.Providers)
	}
	if rt.StreamRunner.Client == oldClient {
		t.Fatal("expected stream runner client to be rebuilt")
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"base_url": "https://custom.example.test/v1"`) ||
		strings.Contains(string(data), `"api_key": "new-key"`) ||
		!strings.Contains(string(data), `"model": "new-model"`) {
		t.Fatalf("provider connection was not persisted: %s", data)
	}
	key, err := config.LoadAuthKey(os.Getenv("HOME"), "fake-provider")
	if err != nil || key != "new-key" {
		t.Fatalf("provider key was not saved to auth store: key=%q err=%v", key, err)
	}
}

func TestServerConfigModelUpdateCreatesProvider(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "api_key": "old-key",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldClient := rt.StreamRunner.Client
	out := &lockedBuffer{}
	srv := New(rt, out)

	req := `{"id":"1","method":"config/model/update","params":{"provider":"custom-1","model":"custom-model","base_url":"https://custom.example.test/v1","api_key":"new-key","create_provider":true}}`
	if err := srv.handleLine(context.Background(), []byte(req)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "custom-1" || result.Model != "custom-model" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if len(result.Providers) != 2 {
		t.Fatalf("expected two provider summaries, got %+v", result.Providers)
	}
	var customSummary ProviderSummary
	for _, summary := range result.Providers {
		if summary.Name == "custom-1" {
			customSummary = summary
			break
		}
	}
	if customSummary.BaseURL != "https://custom.example.test/v1" || !customSummary.APIKeyConfigured {
		t.Fatalf("unexpected custom provider summary: %+v", result.Providers)
	}
	if rt.ProviderName != "custom-1" || rt.Model != "custom-model" || rt.StreamRunner.Model != "custom-model" {
		t.Fatalf("runtime provider/model not updated: provider=%q runtime=%q runner=%q", rt.ProviderName, rt.Model, rt.StreamRunner.Model)
	}
	if rt.StreamRunner.Client == oldClient {
		t.Fatal("expected stream runner client to be rebuilt")
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"custom-1"`) ||
		!strings.Contains(string(data), `"base_url": "https://custom.example.test/v1"`) ||
		strings.Contains(string(data), `"api_key": "new-key"`) ||
		!strings.Contains(string(data), `"default_provider": "custom-1"`) {
		t.Fatalf("new provider was not persisted: %s", data)
	}
	key, err := config.LoadAuthKey(os.Getenv("HOME"), "custom-1")
	if err != nil || key != "new-key" {
		t.Fatalf("provider key was not saved to auth store: key=%q err=%v", key, err)
	}
}

func TestServerConfigModelUpdateRejectsOAuthConnectionChanges(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "codex-provider",
  "providers": {
    "codex-provider": {
      "type": "openai-codex",
      "base_url": "https://chatgpt.example.test/backend-api/codex",
      "model": "old-codex-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	req := `{"id":"1","method":"config/model/update","params":{"provider":"codex-provider","model":"new-codex-model","base_url":"https://custom.example.test/v1","api_key":"new-key"}}`
	if err := srv.handleLine(context.Background(), []byte(req)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	response := responseByID(t, parseOutput(t, out.String()), "1")
	if response["error"] == nil || !strings.Contains(fmt.Sprint(response["error"]), "OpenAI OAuth") {
		t.Fatalf("expected OAuth connection error, got %+v", response)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"api_key": "new-key"`) ||
		strings.Contains(string(data), "https://custom.example.test/v1") ||
		strings.Contains(string(data), `"model": "new-codex-model"`) {
		t.Fatalf("OAuth provider connection should not be persisted: %s", data)
	}
}

func TestServerConfigModelUpdatePersistsEffort(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Effort = "medium"
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "agent": {
    "effort": "medium"
  },
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "model": "fake-model"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"model":"new-model","effort":"xhigh"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Model != "new-model" || result.Effort != "xhigh" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if rt.StreamRunner.Effort != "xhigh" {
		t.Fatalf("runtime effort not updated: %q", rt.StreamRunner.Effort)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"effort": "xhigh"`) {
		t.Fatalf("effort was not persisted: %s", data)
	}
}

func TestServerConfigModelUpdatePersistsVariantOptions(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ProviderName = "xiaomi"
	rt.Model = "mimo-v2.5-pro"
	rt.StreamRunner.Model = "mimo-v2.5-pro"
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "agent": {
    "effort": "low"
  },
  "default_provider": "xiaomi",
  "providers": {
    "xiaomi": {
      "type": "openai-compatible",
      "base_url": "https://token-plan-cn.xiaomimimo.com/v1",
      "model": "mimo-v2.5-pro"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"provider":"xiaomi","model":"mimo-v2.5-pro","variant":"high"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Model != "mimo-v2.5-pro" || result.Variant != "high" || result.Effort != "high" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if rt.StreamRunner.Variant != "high" {
		t.Fatalf("runtime variant not updated: %q", rt.StreamRunner.Variant)
	}
	if rt.StreamRunner.Effort != "" {
		t.Fatalf("legacy effort should be empty when variant options are active, got %q", rt.StreamRunner.Effort)
	}
	if got := rt.StreamRunner.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("runtime provider options not updated: %#v", rt.StreamRunner.ProviderOptions)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"variant": "high"`) {
		t.Fatalf("variant was not persisted: %s", text)
	}
	if strings.Contains(text, `"effort"`) {
		t.Fatalf("legacy effort should be removed after variant migration: %s", text)
	}
}

func TestServerConfigModelUpdateUsesCatalogFastModelRuntime(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ProviderName = "openai"
	rt.Model = "gpt-5.5"
	rt.StreamRunner.Model = "gpt-5.5"
	t.Setenv("OPENAI_API_KEY", "abc")
	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "default_provider": "openai",
  "providers": {
    "openai": {
      "type": "openai",
      "base_url": "https://api.openai.com/v1",
      "model": "gpt-5.5"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/model/update","params":{"provider":"openai","model":"gpt-5.5-fast"}}`)); err != nil {
		t.Fatalf("config/model/update: %v", err)
	}

	result := remarshal[ConfigModelUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Model != "gpt-5.5-fast" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if rt.StreamRunner.Model != "gpt-5.5-fast" || rt.StreamRunner.APIModel != "gpt-5.5" {
		t.Fatalf("runtime model mismatch: model=%q api=%q", rt.StreamRunner.Model, rt.StreamRunner.APIModel)
	}
	if got := rt.StreamRunner.ProviderOptions["serviceTier"]; got != "priority" {
		t.Fatalf("runtime provider options not updated: %#v", rt.StreamRunner.ProviderOptions)
	}
}

func TestServerConfigCodexModels(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ProviderName = "openai-codex"
	rt.Model = "gpt-5.5"
	rt.StreamRunner.Model = "gpt-5.5"
	rt.StreamRunner.Effort = "xhigh"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "models": [
		    {"slug":"gpt-hidden","visibility":"hide","supported_in_api":true},
		    {"slug":"spark","display_name":"Spark","supported_in_api":false},
		    {"slug":"gpt-5.4","display_name":"GPT-5.4","priority":20,"supported_in_api":true},
		    {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":9,"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"xhigh"}],"supported_in_api":true}
		  ]
		}`))
	}))
	defer server.Close()

	if err := os.WriteFile(rt.ConfigPath, []byte(`{
  "agent": {
    "effort": "xhigh"
  },
  "default_provider": "openai-codex",
  "providers": {
    "openai-codex": {
      "type": "openai-codex",
      "base_url": "`+server.URL+`",
      "api_key": "test-token",
      "model": "gpt-5.5"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"config/codex/models"}`)); err != nil {
		t.Fatalf("config/codex/models: %v", err)
	}

	result := remarshal[ConfigCodexModelsResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if result.Provider != "openai-codex" || result.Model != "gpt-5.5" || result.Effort != "xhigh" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Models) != 3 || result.Models[0].Slug != "gpt-5.5" || result.Models[1].Slug != "gpt-5.4" || result.Models[2].Slug != "spark" {
		t.Fatalf("unexpected models: %+v", result.Models)
	}
	if got := result.Models[0].SupportedReasoning; len(got) != 2 || got[0] != "low" || got[1] != "xhigh" {
		t.Fatalf("unexpected reasoning levels: %+v", got)
	}
}

func TestServerSkillList(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.Skills = []skills.Skill{{
		Name:          "slides",
		Description:   "Create slide decks",
		WhenToUse:     "When the user asks for a presentation",
		Source:        "bundled",
		ArgumentHint:  "topic",
		UserInvocable: true,
		AllowedTools:  []string{"read_file"},
		Paths:         []string{"**/*.pptx"},
	}}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"skill/list"}`)); err != nil {
		t.Fatalf("skill/list: %v", err)
	}

	result := remarshal[SkillListResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"])
	if len(result.Skills) != 1 {
		t.Fatalf("expected one skill, got %+v", result)
	}
	got := result.Skills[0]
	if got.Name != "slides" || got.Description != "Create slide decks" || got.Source != "bundled" || !got.UserInvocable {
		t.Fatalf("unexpected skill summary: %+v", got)
	}
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "read_file" || len(got.Paths) != 1 || got.Paths[0] != "**/*.pptx" {
		t.Fatalf("skill metadata missing: %+v", got)
	}
}

func TestServerTurnStartRunsAgentLoop(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{
			Content: "done",
			Usage:   &providers.TokenUsage{InputTokens: 10, OutputTokens: 3},
		},
	}
	rt := newTestRuntime(t, client)
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	if strings.TrimSpace(threadID) == "" {
		t.Fatal("expected thread id")
	}

	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "hello"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := notificationByMethod(t, msgs, NotificationTurnCompleted)
	params := remarshal[TurnCompletedNotification](t, completed["params"])
	if params.ThreadID != threadID || params.Turn.ID == "" || params.Turn.Status != TurnStatusCompleted || params.Content != "done" {
		t.Fatalf("unexpected completion: %+v", params)
	}
	if params.Turn.StartedAt == nil || params.Turn.CompletedAt == nil || params.Turn.DurationMS == nil {
		t.Fatalf("completed turn should include timing: %+v", params.Turn)
	}
	if params.InputTokens != 10 || params.OutputTokens != 3 {
		t.Fatalf("unexpected usage: %+v", params)
	}
	if params.TracePath == "" {
		t.Fatalf("completed turn should include trace path: %+v", params)
	}
	if _, err := os.Stat(params.TracePath); err != nil {
		t.Fatalf("turn trace path should exist: %v", err)
	}

	event := turnEventByType(t, msgs, providers.EventContentDelta)
	eventParams := remarshal[TurnEventNotification](t, event["params"])
	if eventParams.Event.Type != providers.EventContentDelta || eventParams.Event.Content != "done" {
		t.Fatalf("unexpected turn event: %+v", eventParams)
	}
	contextEvent := turnEventByType(t, msgs, providers.EventRequestContext)
	contextParams := remarshal[TurnEventNotification](t, contextEvent["params"])
	if contextParams.Event.RequestContext == nil {
		t.Fatalf("request context missing from turn event: %+v", contextParams.Event)
	}
	if contextParams.Event.RequestContext.TransientMessages != 2 || contextParams.Event.RequestContext.ContentBytes == 0 {
		t.Fatalf("unexpected request context metadata: %+v", contextParams.Event.RequestContext)
	}
	for _, want := range []string{"ENVIRONMENT", "TASK", "CONSTRAINT_LEDGER"} {
		if !testStringSliceContains(contextParams.Event.RequestContext.BlockKinds, want) {
			t.Fatalf("request context missing block kind %s: %+v", want, contextParams.Event.RequestContext)
		}
	}
	delta := notificationByMethod(t, msgs, NotificationAgentMessageDelta)
	deltaParams := remarshal[AgentMessageDeltaNotification](t, delta["params"])
	if deltaParams.ThreadID != threadID || deltaParams.Delta != "done" {
		t.Fatalf("unexpected agent delta: %+v", deltaParams)
	}
	itemCompleted := notificationByMethod(t, msgs, NotificationItemCompleted)
	itemParams := remarshal[ItemCompletedNotification](t, itemCompleted["params"])
	if itemParams.Item.Type != ThreadItemAgentMessage || itemParams.Item.Text != "done" {
		t.Fatalf("unexpected completed item: %+v", itemParams)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(client.requests))
	}
	messages := client.requests[0].Messages
	if len(messages) < 2 || messages[0].Role != "system" || messages[1].Role != "user" || messages[1].Content != "hello" {
		t.Fatalf("unexpected agent-loop messages: %+v", messages)
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(persisted) != 2 || persisted[0].Role != "user" || persisted[0].Content != "hello" || persisted[1].Role != "assistant" || persisted[1].Content != "done" {
		t.Fatalf("unexpected persisted history: %+v", persisted)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != threadID || sessions[0].Entries != 2 || sessions[0].Summary != "hello" {
		t.Fatalf("unexpected session index: %+v", sessions)
	}
}

func TestServerQueuesUserTurnWhileThreadIsRunning(t *testing.T) {
	client := newBlockingStreamClient("done")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = client
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	startReq := fmt.Sprintf(`{"id":"2","method":"turn/start","params":{"thread_id":%q,"prompt":"first"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(startReq)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	<-client.started

	queueReq := fmt.Sprintf(`{"id":"3","method":"turn/queue","params":{"thread_id":%q,"prompt":"queued follow-up","client_id":"queued-1"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(queueReq)); err != nil {
		t.Fatalf("turn/queue: %v", err)
	}
	queueResult := remarshal[TurnQueueResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"])
	if queueResult.Queued.ID != "queued-1" || queueResult.Queued.ThreadID != threadID {
		t.Fatalf("unexpected queue result: %+v", queueResult)
	}

	close(client.release)
	msgs := waitForTurnCompletedCountForThread(t, out, threadID, 2)
	var queuedStarted bool
	for _, msg := range msgs {
		if msg["method"] != NotificationTurnStarted {
			continue
		}
		params := msg["params"].(map[string]any)
		if params["thread_id"] == threadID && params["queue_id"] == "queued-1" {
			queuedStarted = true
			break
		}
	}
	if !queuedStarted {
		t.Fatalf("queued turn did not publish queue_id; output:\n%s", out.String())
	}

	th := srv.thread(threadID)
	th.mu.Lock()
	history := append([]providers.ChatMessage(nil), th.History...)
	th.mu.Unlock()
	var found bool
	for _, msg := range history {
		if msg.Role == "user" && msg.Content == "queued follow-up" && msg.ClientID == "queued-1" && !msg.Steered {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("history missing queued user turn: %+v", history)
	}
}

func TestServerSteersActiveTurnBeforeNextModelStep(t *testing.T) {
	client := &fakeClient{
		responses: []providers.ChatResponse{
			{
				ToolCalls: []providers.ToolCall{{
					ID:        "call_1",
					Name:      "wait_for_steer",
					Arguments: `{}`,
				}},
			},
			{Content: "done after steer"},
		},
	}
	rt := newTestRuntime(t, client)
	blockingTool := newBlockingToolExecutor()
	rt.StreamRunner.Tools = blockingTool
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	startReq := fmt.Sprintf(`{"id":"2","method":"turn/start","params":{"thread_id":%q,"prompt":"start"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(startReq)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	started := remarshal[TurnStartResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])

	<-blockingTool.started
	badSteerReq := fmt.Sprintf(`{"id":"bad-steer","method":"turn/steer","params":{"thread_id":%q,"expected_turn_id":"wrong-turn","prompt":"wrong"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(badSteerReq)); err != nil {
		t.Fatalf("bad turn/steer: %v", err)
	}
	badSteerResp := responseByID(t, parseOutput(t, out.String()), "bad-steer")
	if badSteerResp["error"] == nil {
		t.Fatalf("expected steer mismatch error, got %+v", badSteerResp)
	}

	steerReq := fmt.Sprintf(`{"id":"3","method":"turn/steer","params":{"thread_id":%q,"expected_turn_id":%q,"prompt":"steer now","client_id":"steer-1"}}`, threadID, started.Turn.ID)
	if err := srv.handleLine(context.Background(), []byte(steerReq)); err != nil {
		t.Fatalf("turn/steer: %v", err)
	}
	steerResult := remarshal[TurnSteerResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"])
	if steerResult.TurnID != started.Turn.ID {
		t.Fatalf("unexpected steer result: %+v", steerResult)
	}

	close(blockingTool.release)
	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := remarshal[TurnCompletedNotification](t, notificationByMethodForThread(t, msgs, NotificationTurnCompleted, threadID)["params"])
	var foundItem bool
	for _, item := range completed.Turn.Items {
		if item.Type == ThreadItemUserMessage && item.Text == "steer now" && item.SourceID == "steer-1" {
			foundItem = true
			break
		}
	}
	if !foundItem {
		t.Fatalf("completed turn missing steer item: %+v", completed.Turn.Items)
	}

	client.mu.Lock()
	requests := append([]providers.ChatRequest(nil), client.requests...)
	client.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected two provider requests, got %d", len(requests))
	}
	var foundSteerInSecondRequest bool
	for _, msg := range requests[1].Messages {
		if msg.Role == "user" && msg.Content == "steer now" && msg.ClientID == "steer-1" && msg.Steered {
			foundSteerInSecondRequest = true
			break
		}
	}
	if !foundSteerInSecondRequest {
		t.Fatalf("second provider request missing steer: %+v", requests[1].Messages)
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	var foundPersisted bool
	for _, msg := range persisted {
		if msg.Role == "user" && msg.Content == "steer now" && msg.ClientID == "steer-1" && msg.Steered {
			foundPersisted = true
			break
		}
	}
	if !foundPersisted {
		t.Fatalf("persisted history missing steered input: %+v", persisted)
	}
}

func TestServerGeneratesThreadTitle(t *testing.T) {
	mainClient := &fakeClient{response: providers.ChatResponse{Content: "done"}}
	titleClient := &fakeClient{response: providers.ChatResponse{Content: "<think>ignore</think>\nFix login crash"}}
	rt := newTestRuntime(t, mainClient)
	rt.TitleClient = titleClient
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "please help me fix the login crash in auth.ts"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationThreadUpdated)
	updated := remarshal[ThreadUpdatedNotification](t, notificationByMethod(t, msgs, NotificationThreadUpdated)["params"])
	if updated.Thread.ID != threadID || updated.Thread.Preview != "Fix login crash" {
		t.Fatalf("unexpected title update: %+v", updated)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != "Fix login crash" || sessions[0].Summary != "please help me fix the login crash in auth.ts" {
		t.Fatalf("unexpected persisted title: %+v", sessions)
	}

	mainClient.mu.Lock()
	mainRequests := len(mainClient.requests)
	mainClient.mu.Unlock()
	titleClient.mu.Lock()
	titleRequests := len(titleClient.requests)
	titleClient.mu.Unlock()
	if mainRequests != 1 || titleRequests != 1 {
		t.Fatalf("unexpected request counts: main=%d title=%d", mainRequests, titleRequests)
	}
}

func TestServerGeneratesThreadTitleFromFirstTurnSnapshot(t *testing.T) {
	titleClient := &fakeClient{response: providers.ChatResponse{Content: "First task title"}}
	rt := newTestRuntime(t, &fakeClient{})
	rt.TitleClient = titleClient
	out := &lockedBuffer{}
	srv := New(rt, out)

	sess, err := session.CreateWithMetadata(rt.SessionDir, "snapshot-title-thread", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	firstTurnHistory := []providers.ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "first task"},
		{Role: "assistant", Content: "done"},
	}
	currentHistory := append(cloneHistory(firstTurnHistory), providers.ChatMessage{Role: "user", Content: "second task"})
	th := newThreadState(sess.ID, currentHistory, rt.ProviderName, rt.Model, rt.RootDir, session.FilePath(rt.SessionDir, sess.ID), time.Now().UTC())
	srv.mu.Lock()
	srv.threads[th.ID] = th
	srv.mu.Unlock()

	srv.generateThreadTitle(th.ID, firstTurnHistory)

	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Title != "First task title" {
		t.Fatalf("unexpected generated title: %+v", sessions)
	}
	titleClient.mu.Lock()
	defer titleClient.mu.Unlock()
	if len(titleClient.requests) != 1 {
		t.Fatalf("expected one title request, got %d", len(titleClient.requests))
	}
	prompt := titleClient.requests[0].Messages[len(titleClient.requests[0].Messages)-1].Content
	if !strings.Contains(prompt, "first task") || strings.Contains(prompt, "second task") {
		t.Fatalf("title prompt should use first-turn snapshot, got %q", prompt)
	}
}

// TestServerGeneratesThreadTitleEndToEndWithStreaming exercises the full
// pipeline with a real streaming title client (not a fakeClient wrapped by
// AdaptStreamClient). It mirrors what production looks like for kimi-k2.6 —
// a provider that REQUIRES streaming and has a pinned temperature — and
// verifies:
//
//   - the title request actually went through StreamChat (not Chat)
//   - thinking deltas were ignored, content deltas were aggregated
//   - temperature matches the per-model mapping
//   - the persisted title and the thread/updated notification Preview carry
//     the cleaned title
//   - the main client received exactly one non-stream chat for the agent
//     loop and the title client received exactly one stream chat
func TestServerGeneratesThreadTitleEndToEndWithStreaming(t *testing.T) {
	t.Parallel()
	mainClient := &scriptedStreamClient{chunks: []string{"d", "one"}}
	titleClient := &scriptedStreamClient{
		prefix: "let me think about a good title\n",
		chunks: []string{"Fix ", "login ", "crash"},
	}
	rt := &runtime.Session{
		ProviderName: "fake-provider",
		Model:        "kimi-k2.6",
		RootDir:      t.TempDir(),
		ConfigPath:   "/tmp/.wuu.json",
		SessionDir:   t.TempDir() + "/.wuu/sessions",
		StreamRunner: &agent.StreamRunner{
			Client:       providers.AdaptStreamClient(mainClient),
			Model:        "kimi-k2.6",
			SystemPrompt: "system prompt",
		},
		TitleClient: titleClient,
	}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "please help me fix the login crash in auth.ts"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationThreadUpdated)
	updated := remarshal[ThreadUpdatedNotification](t, notificationByMethod(t, msgs, NotificationThreadUpdated)["params"])
	if updated.Thread.ID != threadID {
		t.Fatalf("notification thread id = %q; want %q", updated.Thread.ID, threadID)
	}
	if updated.Thread.Preview != "Fix login crash" {
		t.Fatalf("notification Preview = %q; want %q", updated.Thread.Preview, "Fix login crash")
	}
	// Summary (the raw first user prompt) must be preserved — the title
	// model only writes to Title, never to Summary.
	if updated.Thread.Preview == "please help me fix the login crash in auth.ts" {
		t.Fatal("Preview should have been replaced by the LLM title, not the raw user prompt")
	}

	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Title != "Fix login crash" {
		t.Fatalf("persisted Title = %q; want %q", sessions[0].Title, "Fix login crash")
	}
	if sessions[0].Summary != "please help me fix the login crash in auth.ts" {
		t.Fatalf("persisted Summary = %q; want raw user prompt preserved", sessions[0].Summary)
	}

	titleClient.mu.Lock()
	defer titleClient.mu.Unlock()
	if len(titleClient.requests) != 1 {
		t.Fatalf("expected exactly 1 title request, got %d", len(titleClient.requests))
	}
	req := titleClient.requests[0]
	if req.Model != "kimi-k2.6" {
		t.Errorf("title request model = %q; want kimi-k2.6", req.Model)
	}
	if req.Temperature != 1.0 {
		t.Errorf("title request Temperature = %v; want 1.0 for kimi-k2.6", req.Temperature)
	}
	if len(req.Messages) < 2 {
		t.Fatalf("title request must have at least system+user messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || !strings.Contains(req.Messages[0].Content, "title generator") {
		t.Errorf("title system prompt not aligned with opencode: %q", req.Messages[0].Content)
	}
	if req.Messages[1].Role != "user" || !strings.Contains(req.Messages[1].Content, "please help me fix the login crash") {
		t.Errorf("title user message wrong: %q", req.Messages[1].Content)
	}
}

// TestServerRegenerateTitle exercises the thread/regenerate-title JSON-RPC
// method end-to-end: a thread that already has multiple turns (so the
// first-turn auto title gen is skipped) can still be re-titled by hand.
// Verifies:
//
//   - dry-run=true returns the cleaned title but does not persist or notify
//   - dry-run=false persists the title and fires a thread/updated
//     notification with the new Preview
//   - the response surfaces every TitleGenerationResult field
func TestServerRegenerateTitle(t *testing.T) {
	mainClient := &fakeClient{response: providers.ChatResponse{Content: "ok"}}
	titleClient := &scriptedStreamClient{
		prefix: "let me think…\n",
		chunks: []string{"Refactor ", "user ", "service"},
	}
	rt := newTestRuntime(t, mainClient)
	rt.TitleClient = titleClient
	srv := New(rt, &lockedBuffer{})

	// Seed an existing thread that is BEYOND its first turn. The first-turn
	// auto title gen would skip this because history has > 1 user message;
	// the explicit regenerate method must still work.
	sess, err := session.CreateWithMetadata(rt.SessionDir, "regen-thread-1", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	history := []providers.ChatMessage{
		{Role: "user", Content: "first task prompt"},
		{Role: "assistant", Content: "first task answer"},
		{Role: "user", Content: "second task prompt"},
		{Role: "assistant", Content: "second task answer"},
	}
	if err := session.UpdateIndex(rt.SessionDir, sess.ID, len(history), "first task prompt"); err != nil {
		t.Fatal(err)
	}
	if err := rewriteChatHistory(session.FilePath(rt.SessionDir, sess.ID), history); err != nil {
		t.Fatal(err)
	}

	// dry-run path: returns the cleaned title without persisting or notifying.
	dryParams, _ := json.Marshal(ThreadRegenerateTitleParams{
		ThreadID: sess.ID,
		DryRun:   true,
	})
	dryReq := []byte(fmt.Sprintf(`{"id":"reg-1","method":%q,"params":%s}`, MethodThreadRegenerateTitle, dryParams))
	if err := srv.handleLine(context.Background(), dryReq); err != nil {
		t.Fatalf("regenerate-title dry-run: %v", err)
	}
	// Verify title was NOT persisted.
	if _, ok, _ := session.Find(rt.SessionDir, sess.ID); !ok {
		t.Fatal("session should still exist")
	}
	persisted, _ := session.List(rt.SessionDir, 100)
	for _, p := range persisted {
		if p.ID == sess.ID && p.Title != "" {
			t.Fatalf("dry-run must not persist, got Title=%q", p.Title)
		}
	}

	// Persist path: re-issue without dry-run.
	persistParams, _ := json.Marshal(ThreadRegenerateTitleParams{
		ThreadID: sess.ID,
		DryRun:   false,
	})
	persistReq := []byte(fmt.Sprintf(`{"id":"reg-2","method":%q,"params":%s}`, MethodThreadRegenerateTitle, persistParams))
	if err := srv.handleLine(context.Background(), persistReq); err != nil {
		t.Fatalf("regenerate-title persist: %v", err)
	}
	persisted2, _ := session.List(rt.SessionDir, 100)
	var updated *session.Session
	for i := range persisted2 {
		if persisted2[i].ID == sess.ID {
			updated = &persisted2[i]
		}
	}
	if updated == nil {
		t.Fatal("session missing after persist")
	}
	if updated.Title != "Refactor user service" {
		t.Fatalf("persisted Title = %q; want %q", updated.Title, "Refactor user service")
	}
	if updated.Summary != "first task prompt" {
		t.Fatalf("Summary must be preserved, got %q", updated.Summary)
	}
}

func TestCleanGeneratedThreadTitle(t *testing.T) {
	got := cleanGeneratedThreadTitle("<think>hidden</think>\nTitle: \"调试登录崩溃并修复认证流程\"")
	if got != "调试登录崩溃并修复认证流程" {
		t.Fatalf("cleanGeneratedThreadTitle() = %q", got)
	}
}

func TestServerThreadForkAtAssistantItem(t *testing.T) {
	client := &fakeClient{
		responses: []providers.ChatResponse{
			{Content: "first answer"},
			{Content: "second answer"},
		},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	startTurn := func(id, prompt string, completedCount int) Turn {
		t.Helper()
		payload := map[string]any{
			"id":     id,
			"method": MethodTurnStart,
			"params": TurnStartParams{ThreadID: threadID, Prompt: prompt},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal turn request: %v", err)
		}
		if err := srv.handleLine(context.Background(), raw); err != nil {
			t.Fatalf("turn/start: %v", err)
		}
		msgs := waitForNotificationCount(t, out, NotificationTurnCompleted, completedCount)
		completed := notificationsByMethod(msgs, NotificationTurnCompleted)
		return remarshal[TurnCompletedNotification](t, completed[len(completed)-1]["params"]).Turn
	}

	firstTurn := startTurn("2", "first prompt", 1)
	var firstAgentItem ThreadItem
	for _, item := range firstTurn.Items {
		if item.Type == ThreadItemAgentMessage {
			firstAgentItem = item
			break
		}
	}
	if firstAgentItem.ID == "" {
		t.Fatalf("expected first turn to contain assistant item: %+v", firstTurn)
	}
	_ = startTurn("3", "second prompt", 2)

	payload := map[string]any{
		"id":     "4",
		"method": MethodThreadFork,
		"params": ThreadForkParams{
			ThreadID: threadID,
			TurnID:   firstTurn.ID,
			ItemID:   firstAgentItem.ID,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fork request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/fork: %v", err)
	}

	msgs := parseOutput(t, out.String())
	forkResponse := responseByID(t, msgs, "4")
	if forkResponse["error"] != nil {
		t.Fatalf("thread/fork returned error: %+v", forkResponse["error"])
	}
	result := remarshal[ThreadForkResult](t, forkResponse["result"])
	fork := result.Thread
	if fork.ID == "" || fork.ID == threadID {
		t.Fatalf("expected new fork thread id, got %+v", fork)
	}
	if fork.ForkedFromID != threadID || fork.ForkedFromTurnID != firstTurn.ID || fork.ForkedFromItemID != firstAgentItem.ID {
		t.Fatalf("fork metadata not returned: %+v", fork)
	}
	if len(fork.Turns) != 1 || len(fork.Turns[0].Items) != 2 {
		t.Fatalf("expected fork to stop at first assistant item, got %+v", fork.Turns)
	}
	if fork.Turns[0].Items[0].Text != "first prompt" || fork.Turns[0].Items[1].Text != "first answer" {
		t.Fatalf("unexpected fork turn items: %+v", fork.Turns[0].Items)
	}

	forkHistory, err := loadChatMessages(session.FilePath(rt.SessionDir, fork.ID))
	if err != nil {
		t.Fatalf("load fork history: %v", err)
	}
	if len(forkHistory) != 2 || forkHistory[0].Content != "first prompt" || forkHistory[1].Content != "first answer" {
		t.Fatalf("unexpected persisted fork history: %+v", forkHistory)
	}
	sourceHistory, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load source history: %v", err)
	}
	if len(sourceHistory) != 4 {
		t.Fatalf("source history should remain intact, got %+v", sourceHistory)
	}

	metadata, ok, err := session.Find(rt.SessionDir, fork.ID)
	if err != nil {
		t.Fatalf("find fork metadata: %v", err)
	}
	if !ok || metadata.ForkedFromID != threadID || metadata.ForkedFromTurnID != firstTurn.ID || metadata.ForkedFromItemID != firstAgentItem.ID {
		t.Fatalf("fork metadata not persisted: ok=%v metadata=%+v", ok, metadata)
	}
	started := notificationsByMethod(msgs, NotificationThreadStarted)
	if len(started) < 2 {
		t.Fatalf("expected fork to emit thread/started notification, got %+v", msgs)
	}
	forkStarted := remarshal[ThreadStartedNotification](t, started[len(started)-1]["params"])
	if forkStarted.Thread.ID != fork.ID {
		t.Fatalf("unexpected fork started notification: %+v", forkStarted)
	}
}

func TestServerTurnStartAcceptsImageOnlyPrompt(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{Content: "saw it"},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{
			ThreadID: threadID,
			Images: []TurnStartImage{{
				MediaType: "image/png",
				Data:      "ZmFrZS1pbWFnZQ==",
			}},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	started := remarshal[TurnStartResult](t, responseByID(t, msgs, "2")["result"])
	if len(started.Turn.Items) != 1 || len(started.Turn.Items[0].Images) != 1 {
		t.Fatalf("start response missing user image: %+v", started.Turn)
	}
	completed := remarshal[TurnCompletedNotification](t, notificationByMethod(t, msgs, NotificationTurnCompleted)["params"])
	if len(completed.Turn.Items) < 1 || len(completed.Turn.Items[0].Images) != 1 {
		t.Fatalf("completed turn missing user image: %+v", completed.Turn)
	}

	client.mu.Lock()
	requestCount := len(client.requests)
	var messages []providers.ChatMessage
	if requestCount > 0 {
		messages = append([]providers.ChatMessage(nil), client.requests[0].Messages...)
	}
	client.mu.Unlock()
	if requestCount != 1 {
		t.Fatalf("expected one provider request, got %d", requestCount)
	}
	if len(messages) < 2 || messages[1].Role != "user" || messages[1].Content != "" || len(messages[1].Images) != 1 {
		t.Fatalf("unexpected provider messages: %+v", messages)
	}
	if messages[1].Images[0].MediaType != "image/png" || messages[1].Images[0].Data != "ZmFrZS1pbWFnZQ==" {
		t.Fatalf("unexpected provider image: %+v", messages[1].Images[0])
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(persisted) != 2 || len(persisted[0].Images) != 1 {
		t.Fatalf("unexpected persisted history: %+v", persisted)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Summary != "[Image #1]" {
		t.Fatalf("unexpected session index: %+v", sessions)
	}
}

func TestServerTurnStartAcceptsPDFOnlyPrompt(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{Content: "read it"},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	payload := map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{
			ThreadID: threadID,
			Files: []TurnStartFile{{
				MediaType: "application/pdf",
				Data:      "JVBERi0xLjQ=",
				Filename:  "brief.pdf",
			}},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	started := remarshal[TurnStartResult](t, responseByID(t, msgs, "2")["result"])
	if len(started.Turn.Items) != 1 || len(started.Turn.Items[0].Files) != 1 {
		t.Fatalf("start response missing user file: %+v", started.Turn)
	}
	if started.Turn.Items[0].Files[0].Filename != "brief.pdf" {
		t.Fatalf("unexpected thread item file: %+v", started.Turn.Items[0].Files[0])
	}

	client.mu.Lock()
	requestCount := len(client.requests)
	var messages []providers.ChatMessage
	if requestCount > 0 {
		messages = append([]providers.ChatMessage(nil), client.requests[0].Messages...)
	}
	client.mu.Unlock()
	if requestCount != 1 {
		t.Fatalf("expected one provider request, got %d", requestCount)
	}
	if len(messages) < 2 || messages[1].Role != "user" || messages[1].Content != "" || len(messages[1].Files) != 1 {
		t.Fatalf("unexpected provider messages: %+v", messages)
	}
	if messages[1].Files[0].MediaType != "application/pdf" || messages[1].Files[0].Data != "JVBERi0xLjQ=" || messages[1].Files[0].Filename != "brief.pdf" {
		t.Fatalf("unexpected provider file: %+v", messages[1].Files[0])
	}

	persisted, err := loadChatMessages(session.FilePath(rt.SessionDir, threadID))
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(persisted) != 2 || len(persisted[0].Files) != 1 {
		t.Fatalf("unexpected persisted history: %+v", persisted)
	}
	sessions, err := session.List(rt.SessionDir, 1)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Summary != "[brief.pdf]" {
		t.Fatalf("unexpected session index: %+v", sessions)
	}
}

func TestServerThreadListUsesSessionIndexMetadata(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if _, err := session.CreateWithMetadata(rt.SessionDir, "old-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "old-thread", 2, "old summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "new-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "new-thread", 2, "new summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "archived-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if _, err := session.UpdateArchived(rt.SessionDir, "archived-thread", true); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "other-thread", filepath.Join(rt.RootDir, "other")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.UpdatePinned(rt.SessionDir, "old-thread", true); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 2 {
		t.Fatalf("expected two visible workspace threads, got %+v", result.Threads)
	}
	if result.Threads[0].ID != "old-thread" || !result.Threads[0].Pinned || result.Threads[0].Preview != "old summary" {
		t.Fatalf("expected pinned old thread first, got %+v", result.Threads)
	}
	if result.Threads[1].ID != "new-thread" || result.Threads[1].Archived {
		t.Fatalf("unexpected second thread: %+v", result.Threads[1])
	}
}

func TestServerThreadListOrdersSessionsByUpdatedAt(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if _, err := session.CreateWithMetadata(rt.SessionDir, "first-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "first-thread", 2, "first summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CreateWithMetadata(rt.SessionDir, "second-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "second-thread", 2, "second summary"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := session.UpdateIndex(rt.SessionDir, "first-thread", 4, "ignored later summary"); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 2 {
		t.Fatalf("expected two visible workspace threads, got %+v", result.Threads)
	}
	if result.Threads[0].ID != "first-thread" || result.Threads[1].ID != "second-thread" {
		t.Fatalf("expected recently updated thread first, got %+v", result.Threads)
	}
	if !result.Threads[0].UpdatedAt.After(result.Threads[1].UpdatedAt) {
		t.Fatalf("expected first thread updated_at to be newer, got %+v", result.Threads)
	}
}

func TestServerThreadSearchMatchesHistoryContent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	userThread, err := session.CreateWithMetadata(rt.SessionDir, "user-thread", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rewriteChatHistory(session.FilePath(rt.SessionDir, userThread.ID), []providers.ChatMessage{
		{Role: "user", Content: "Investigate the delta-vector login failure"},
		{Role: "assistant", Content: "The login failure comes from stale config."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, userThread.ID, 2, "login summary"); err != nil {
		t.Fatal(err)
	}
	assistantThread, err := session.CreateWithMetadata(rt.SessionDir, "assistant-thread", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rewriteChatHistory(session.FilePath(rt.SessionDir, assistantThread.ID), []providers.ChatMessage{
		{Role: "user", Content: "summarize the deploy"},
		{Role: "assistant", Content: "The deploy note mentions orion-cache warming."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, assistantThread.ID, 2, "deploy summary"); err != nil {
		t.Fatal(err)
	}
	archivedThread, err := session.CreateWithMetadata(rt.SessionDir, "archived-thread", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rewriteChatHistory(session.FilePath(rt.SessionDir, archivedThread.ID), []providers.ChatMessage{
		{Role: "user", Content: "delta-vector archived"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.UpdateArchived(rt.SessionDir, archivedThread.ID, true); err != nil {
		t.Fatal(err)
	}
	otherThread, err := session.CreateWithMetadata(rt.SessionDir, "other-thread", filepath.Join(rt.RootDir, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rewriteChatHistory(session.FilePath(rt.SessionDir, otherThread.ID), []providers.ChatMessage{
		{Role: "user", Content: "delta-vector other workspace"},
	}); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	userPayload := map[string]any{
		"id":     "1",
		"method": MethodThreadSearch,
		"params": ThreadSearchParams{Query: "delta-vector"},
	}
	rawUserPayload, err := json.Marshal(userPayload)
	if err != nil {
		t.Fatalf("marshal user search request: %v", err)
	}
	if err := srv.handleLine(context.Background(), rawUserPayload); err != nil {
		t.Fatalf("thread/search user query: %v", err)
	}
	assistantPayload := map[string]any{
		"id":     "2",
		"method": MethodThreadSearch,
		"params": ThreadSearchParams{Query: "orion-cache"},
	}
	rawAssistantPayload, err := json.Marshal(assistantPayload)
	if err != nil {
		t.Fatalf("marshal assistant search request: %v", err)
	}
	if err := srv.handleLine(context.Background(), rawAssistantPayload); err != nil {
		t.Fatalf("thread/search assistant content: %v", err)
	}

	msgs := parseOutput(t, out.String())
	userResult := remarshal[ThreadSearchResult](t, responseByID(t, msgs, "1")["result"])
	if len(userResult.Results) != 1 || userResult.Results[0].Thread.ID != userThread.ID {
		t.Fatalf("expected user-thread only, got %+v", userResult.Results)
	}
	if !strings.Contains(userResult.Results[0].Snippet, "delta-vector") {
		t.Fatalf("expected user query snippet, got %q", userResult.Results[0].Snippet)
	}
	assistantResult := remarshal[ThreadSearchResult](t, responseByID(t, msgs, "2")["result"])
	if len(assistantResult.Results) != 1 || assistantResult.Results[0].Thread.ID != assistantThread.ID {
		t.Fatalf("expected assistant-thread only, got %+v", assistantResult.Results)
	}
	if !strings.Contains(assistantResult.Results[0].Snippet, "orion-cache") {
		t.Fatalf("expected assistant content snippet, got %q", assistantResult.Results[0].Snippet)
	}
}

func TestServerThreadListIncludesDirectChildAgents(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu", "state")
	if _, err := session.CreateWithMetadata(rt.SessionDir, "root-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "root-thread", 2, "root summary"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	store := agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "threads"))
	metas := []agentthread.Metadata{
		{
			ID:        "root-thread",
			Path:      agentthread.RootPath,
			Status:    agentthread.StatusRunning,
			CreatedAt: now,
			UpdatedAt: now,
			Source:    agentthread.Source{Kind: agentthread.SourceRoot, Depth: 1},
		},
		{
			ID:        "worker-1",
			SessionID: "root-thread",
			ParentID:  "root-thread",
			Path:      "/root/inspect",
			TaskName:  "inspect",
			Role:      "worker",
			Status:    agentthread.StatusRunning,
			CreatedAt: now.Add(time.Second),
			UpdatedAt: now.Add(time.Second),
			Source: agentthread.Source{
				Kind:           agentthread.SourceThreadSpawn,
				ParentThreadID: "root-thread",
				ParentPath:     agentthread.RootPath,
				Depth:          2,
			},
		},
		{
			ID:        "worker-2",
			SessionID: "root-thread",
			ParentID:  "worker-1",
			Path:      "/root/inspect/deeper",
			TaskName:  "deeper",
			Role:      "worker",
			Status:    agentthread.StatusPending,
			CreatedAt: now.Add(2 * time.Second),
			UpdatedAt: now.Add(2 * time.Second),
			Source: agentthread.Source{
				Kind:           agentthread.SourceThreadSpawn,
				ParentThreadID: "worker-1",
				ParentPath:     "/root/inspect",
				Depth:          3,
			},
		},
	}
	for _, meta := range metas {
		if err := store.UpsertThread(meta); err != nil {
			t.Fatalf("upsert thread %s: %v", meta.ID, err)
		}
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadListResult](t, responseByID(t, msgs, "1")["result"])
	if len(result.Threads) != 1 {
		t.Fatalf("expected one root thread, got %+v", result.Threads)
	}
	agents := result.Threads[0].ChildAgents
	if len(agents) != 1 {
		t.Fatalf("expected only the direct child agent, got %+v", agents)
	}
	if agents[0].ID != "worker-1" || agents[0].TaskName != "inspect" || agents[0].NestedCount != 1 || agents[0].NestedRunningCount != 1 {
		t.Fatalf("unexpected child agent summary: %+v", agents[0])
	}
}

func TestServerThreadResumeLoadsChildAgentSession(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu", "state")
	if _, err := session.CreateWithMetadata(rt.SessionDir, "root-thread", rt.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateIndex(rt.SessionDir, "root-thread", 2, "root summary"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	meta := agentthread.Metadata{
		ID:              "worker-1",
		SessionID:       "root-thread",
		ParentID:        "root-thread",
		Path:            "/root/inspect",
		TaskName:        "inspect",
		Role:            "worker",
		LastTaskMessage: "inspect the UI",
		CWD:             rt.RootDir,
		Model:           "worker-model",
		Status:          agentthread.StatusCompleted,
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
		Source: agentthread.Source{
			Kind:           agentthread.SourceThreadSpawn,
			ParentThreadID: "root-thread",
			ParentPath:     agentthread.RootPath,
			Depth:          2,
		},
	}
	store := agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "threads"))
	if err := store.UpsertThread(meta); err != nil {
		t.Fatalf("upsert worker thread: %v", err)
	}

	workerDir := filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "root-thread"), "workers")
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		t.Fatalf("mkdir worker history: %v", err)
	}
	rec := persistedAgentHistory{
		ID:          "worker-1",
		Type:        "worker",
		TaskName:    "inspect",
		AgentPath:   "/root/inspect",
		ParentID:    "root-thread",
		Description: "inspect",
		Status:      "completed",
		StartedAt:   now,
		CompletedAt: now.Add(time.Minute),
		Model:       "worker-model",
		Prompt:      "inspect the UI",
		Result:      "child session result",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "worker system"},
			{Role: "user", Content: "inspect the UI"},
			{Role: "assistant", Content: "child session result"},
		},
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal worker history: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workerDir, "worker-1.json"), data, 0o644); err != nil {
		t.Fatalf("write worker history: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload, err := json.Marshal(map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: "worker-1"},
	})
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), payload); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, msgs, "1")["result"])
	thread := result.Thread
	if thread.ID != "worker-1" || !thread.ReadOnly || thread.ParentID != "root-thread" || thread.AgentPath != "/root/inspect" {
		t.Fatalf("unexpected child thread identity: %+v", thread)
	}
	if thread.Model != "worker-model" || thread.Preview != "inspect" {
		t.Fatalf("unexpected child thread metadata: %+v", thread)
	}
	if len(thread.Turns) != 1 || len(thread.Turns[0].Items) != 2 {
		t.Fatalf("unexpected child thread turns: %+v", thread.Turns)
	}
	if got := thread.Turns[0].Items[1].Text; got != "child session result" {
		t.Fatalf("unexpected child agent message: %q", got)
	}
	resumed := remarshal[ThreadResumedNotification](t, notificationByMethod(t, msgs, NotificationThreadResumed)["params"])
	if resumed.Thread.ID != "worker-1" || !resumed.Thread.ReadOnly {
		t.Fatalf("unexpected resumed notification: %+v", resumed)
	}
}

func TestServerChildAgentSessionIsLiveWhileRunning(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu", "state")
	rootID := "root-live"
	artifactDir := statepath.SessionArtifactDir(rt.StateDir, rootID)
	workerClient := newBlockingStreamClient("child live result")
	coord, err := agentcontrol.New(agentcontrol.Config{
		Client:       workerClient,
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    rootID,
		HistoryDir:   filepath.Join(artifactDir, "workers"),
		ThreadDir:    filepath.Join(artifactDir, "threads"),
		HarnessDir:   filepath.Join(artifactDir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	rootThread := newThreadState(rootID, nil, rt.ProviderName, rt.Model, rt.RootDir, "", time.Now().UTC())
	rootThread.execRuntime = &runtime.ThreadRuntime{AgentControl: coord}
	srv.mu.Lock()
	srv.threads[rootID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(rootID, rootThread.execRuntime)

	spawned, err := coord.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "live_child",
		Description: "live child",
		Prompt:      "do it live",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-workerClient.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start streaming")
	}

	waitForMethod(t, out, NotificationTurnStarted)
	payload, err := json.Marshal(map[string]any{
		"id":     "resume-child",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: spawned.AgentID},
	})
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), payload); err != nil {
		t.Fatalf("thread/resume child: %v", err)
	}
	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, msgs, "resume-child")["result"])
	if result.Thread.ID != spawned.AgentID || !result.Thread.ReadOnly || result.Thread.Status != ThreadStatusInProgress {
		t.Fatalf("unexpected live child thread: %+v", result.Thread)
	}
	if len(result.Thread.Turns) != 1 || result.Thread.Turns[0].Status != TurnStatusInProgress {
		t.Fatalf("expected running child turn, got %+v", result.Thread.Turns)
	}

	close(workerClient.release)
	msgs = waitForMethod(t, out, NotificationTurnCompleted)
	var childCompleted bool
	var childDelta bool
	for _, msg := range msgs {
		if msg["method"] == NotificationAgentMessageDelta {
			params := msg["params"].(map[string]any)
			if params["thread_id"] == spawned.AgentID && params["delta"] == "child live result" {
				childDelta = true
			}
		}
		if msg["method"] == NotificationTurnCompleted {
			params := msg["params"].(map[string]any)
			if params["thread_id"] == spawned.AgentID {
				childCompleted = true
			}
		}
	}
	if !childDelta || !childCompleted {
		t.Fatalf("expected child delta and completion notifications, delta=%v completed=%v output:\n%s", childDelta, childCompleted, out.String())
	}
}

func TestServerThreadPinAndArchive(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	pinPayload, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadPin,
		"params": ThreadPinParams{ThreadID: threadID, Pinned: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), pinPayload); err != nil {
		t.Fatalf("thread/pin: %v", err)
	}
	pinResult := remarshal[ThreadPinResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if pinResult.Thread.ID != threadID || !pinResult.Thread.Pinned {
		t.Fatalf("unexpected pin result: %+v", pinResult)
	}
	pinned, ok, err := session.Find(rt.SessionDir, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pinned.PinnedAt == nil {
		t.Fatalf("pin not persisted: ok=%v session=%+v", ok, pinned)
	}

	archivePayload, err := json.Marshal(map[string]any{
		"id":     "3",
		"method": MethodThreadArchive,
		"params": ThreadArchiveParams{ThreadID: threadID, Archived: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleLine(context.Background(), archivePayload); err != nil {
		t.Fatalf("thread/archive: %v", err)
	}
	archiveResult := remarshal[ThreadArchiveResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"])
	if archiveResult.Thread.ID != threadID || !archiveResult.Thread.Archived || archiveResult.Thread.Pinned {
		t.Fatalf("unexpected archive result: %+v", archiveResult)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"4","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	listResult := remarshal[ThreadListResult](t, responseByID(t, parseOutput(t, out.String()), "4")["result"])
	if len(listResult.Threads) != 0 {
		t.Fatalf("archived thread should be hidden, got %+v", listResult.Threads)
	}
}

func TestServerRejectsUnknownTurnParams(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"turn/start","params":{"thread_id":"x","prompt":"p","extra":true}}`)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "1")
	if resp["error"] == nil {
		t.Fatalf("expected response error, got %+v", resp)
	}
}

func TestServerTurnItemsIncludeReasoningAndAgentMessage(t *testing.T) {
	client := &fakeClient{
		response: providers.ChatResponse{
			ReasoningContent: "inspect first",
			Content:          "done",
		},
	}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	raw, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: threadID, Prompt: "hello"},
	})
	if err != nil {
		t.Fatalf("marshal turn request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := remarshal[TurnCompletedNotification](t, notificationByMethod(t, msgs, NotificationTurnCompleted)["params"])
	var reasoning, agent int
	for _, item := range completed.Turn.Items {
		switch item.Type {
		case ThreadItemReasoning:
			reasoning++
			if item.Text != "inspect first" {
				t.Fatalf("unexpected reasoning item: %+v", item)
			}
		case ThreadItemAgentMessage:
			agent++
			if item.Text != "done" {
				t.Fatalf("unexpected agent item: %+v", item)
			}
		}
	}
	if reasoning != 1 || agent != 1 {
		t.Fatalf("expected one reasoning and one agent item, got reasoning=%d agent=%d turn=%+v", reasoning, agent, completed.Turn)
	}
}

func TestServerThreadResumeLoadsSessionHistory(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.MkdirAll(rt.SessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionID := "20260523-000000-test"
	sessionPath := filepath.Join(rt.SessionDir, sessionID+".jsonl")
	history := strings.Join([]string{
		`{"role":"system","content":"system prompt"}`,
		`{"role":"user","content":"hello"}`,
		`{"role":"assistant","content":"done"}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(history), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload := map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sessionID},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	msgs := parseOutput(t, out.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, msgs, "1")["result"])
	if result.Thread.ID != sessionID || len(result.Thread.Turns) != 1 {
		t.Fatalf("unexpected resume result: %+v", result)
	}
	if turn := result.Thread.Turns[0]; turn.StartedAt != nil || turn.CompletedAt != nil || turn.DurationMS != nil {
		t.Fatalf("historical turn should leave unknown timing unset: %+v", turn)
	}
	resumed := remarshal[ThreadResumedNotification](t, notificationByMethod(t, msgs, NotificationThreadResumed)["params"])
	if resumed.Thread.ID != sessionID || len(resumed.Thread.Turns) != 1 {
		t.Fatalf("unexpected resume notification: %+v", resumed)
	}

	th := srv.thread(sessionID)
	if th == nil {
		t.Fatal("expected resumed thread")
	}
	if len(th.History) != 3 || th.History[1].Role != "user" || th.History[1].Content != "hello" {
		t.Fatalf("unexpected resumed history: %+v", th.History)
	}
}

func TestServerCompactedTurnPersistsAndResumes(t *testing.T) {
	client := &fakeClient{
		responses: []providers.ChatResponse{
			{Content: "summary of older single-turn tool run"},
			{Content: "after compact"},
		},
	}
	rt := newTestRuntime(t, client)
	rt.StreamRunner.Model = "gpt-4-turbo"
	rt.StreamRunner.ContextWindowOverride = 5000

	if err := os.MkdirAll(rt.SessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionID := "20260527-000000-compact"
	sess, err := session.CreateWithMetadata(rt.SessionDir, sessionID, rt.RootDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionPath := session.FilePath(rt.SessionDir, sess.ID)
	largeToolOutput := strings.Repeat("large output ", 1200)
	initialHistory := []providers.ChatMessage{
		{Role: "user", Content: "debug the failing workbench request"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "run_shell", Arguments: `{"command":"rg ContextOverflow"}`},
		}},
		{Role: "tool", Name: "run_shell", ToolCallID: "call_1", Content: largeToolOutput},
		{Role: "assistant", Content: "I found the first clue."},
	}
	if err := rewriteChatHistory(sessionPath, initialHistory); err != nil {
		t.Fatalf("write initial history: %v", err)
	}
	if err := session.UpdateIndex(rt.SessionDir, sess.ID, persistableMessageCount(initialHistory), threadPreview(initialHistory)); err != nil {
		t.Fatalf("update index: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	rawResume, err := json.Marshal(map[string]any{
		"id":     "resume-1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sess.ID},
	})
	if err != nil {
		t.Fatalf("marshal resume: %v", err)
	}
	if err := srv.handleLine(context.Background(), rawResume); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	rawTurn, err := json.Marshal(map[string]any{
		"id":     "turn-1",
		"method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: sess.ID, Prompt: "continue"},
	})
	if err != nil {
		t.Fatalf("marshal turn: %v", err)
	}
	if err := srv.handleLine(context.Background(), rawTurn); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	msgs := waitForMethod(t, out, NotificationTurnCompleted)
	completed := remarshal[TurnCompletedNotification](t, notificationByMethod(t, msgs, NotificationTurnCompleted)["params"])
	if completed.Content != "after compact" || completed.Turn.Status != TurnStatusCompleted {
		t.Fatalf("unexpected turn completion: %+v", completed)
	}
	if turnEventByType(t, msgs, providers.EventCompact) == nil {
		t.Fatal("expected compact event during resumed turn")
	}

	persisted, err := loadChatMessages(sessionPath)
	if err != nil {
		t.Fatalf("load compacted history: %v", err)
	}
	if len(persisted) != 4 {
		t.Fatalf("expected compacted persisted history of 4 messages, got %+v", persisted)
	}
	if persisted[0].Role != "system" || !strings.Contains(persisted[0].Content, "summary of older single-turn tool run") {
		t.Fatalf("expected persisted compact summary first, got %+v", persisted[0])
	}
	if persisted[1].Role != "assistant" || persisted[1].Content != "I found the first clue." {
		t.Fatalf("expected recent assistant tail after summary, got %+v", persisted[1])
	}
	if persisted[2].Role != "user" || persisted[2].Content != "continue" {
		t.Fatalf("expected resumed user message after recent tail, got %+v", persisted[2])
	}
	if persisted[3].Role != "assistant" || persisted[3].Content != "after compact" {
		t.Fatalf("expected final assistant message after compact, got %+v", persisted[3])
	}

	out2 := &lockedBuffer{}
	resumedSrv := New(rt, out2)
	rawResume2, err := json.Marshal(map[string]any{
		"id":     "resume-2",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sess.ID},
	})
	if err != nil {
		t.Fatalf("marshal second resume: %v", err)
	}
	if err := resumedSrv.handleLine(context.Background(), rawResume2); err != nil {
		t.Fatalf("second thread/resume: %v", err)
	}
	resumeMsgs := parseOutput(t, out2.String())
	result := remarshal[ThreadResumeResult](t, responseByID(t, resumeMsgs, "resume-2")["result"])
	if result.Thread.ID != sess.ID || len(result.Thread.Turns) != 1 {
		t.Fatalf("unexpected resumed compacted thread: %+v", result.Thread)
	}
	if len(result.Thread.Turns[0].Items) != 3 || result.Thread.Turns[0].Items[1].Type != ThreadItemContextCompaction {
		t.Fatalf("expected resumed turn to include context compaction item: %+v", result.Thread.Turns[0])
	}
	th := resumedSrv.thread(sess.ID)
	if th == nil {
		t.Fatal("expected resumed compacted thread state")
	}
	if len(th.History) != 5 {
		t.Fatalf("expected base system prompt plus compacted persisted history, got %+v", th.History)
	}
	if th.History[1].Role != "system" || !strings.Contains(th.History[1].Content, "summary of older single-turn tool run") {
		t.Fatalf("expected compact summary after base system prompt, got %+v", th.History)
	}
}

func TestServerThreadResumeReturnsLoadedRunningThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID
	th := srv.thread(threadID)
	if th == nil {
		t.Fatal("expected loaded thread")
	}
	now := time.Now().UTC()
	th.mu.Lock()
	th.startTurnLocked("turn-loaded-running", providers.ChatMessage{Role: "user", Content: "keep running"}, now)
	th.mu.Unlock()

	raw, err := json.Marshal(map[string]any{
		"id":     "2",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: threadID},
	})
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	if srv.thread(threadID) != th {
		t.Fatal("resume should not replace an already loaded thread")
	}
	result := remarshal[ThreadResumeResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if result.Thread.ID != threadID || result.Thread.Status != ThreadStatusInProgress || len(result.Thread.Turns) != 1 {
		t.Fatalf("unexpected loaded resume result: %+v", result.Thread)
	}
}

func TestServerThreadResumeNormalizesToolResultOrder(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	if err := os.MkdirAll(rt.SessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionID := "20260523-000001-tools"
	sessionPath := filepath.Join(rt.SessionDir, sessionID+".jsonl")
	history := strings.Join([]string{
		`{"role":"system","content":"system prompt"}`,
		`{"role":"user","content":"inspect"}`,
		`{"role":"assistant","tool_calls":[{"id":"call_1","name":"read_file","arguments":"{}"}]}`,
		`{"role":"user","content":"mid-turn context"}`,
		`{"role":"tool","name":"read_file","tool_call_id":"call_1","content":"ok"}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(history), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	payload := map[string]any{
		"id":     "1",
		"method": MethodThreadResume,
		"params": ThreadResumeParams{SessionID: sessionID},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal resume request: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}

	th := srv.thread(sessionID)
	if th == nil {
		t.Fatal("expected resumed thread")
	}
	if err := providers.ValidateMessageSequence(th.History); err != nil {
		t.Fatalf("expected valid resumed history, got %v: %+v", err, th.History)
	}
	roles := make([]string, 0, len(th.History))
	for _, msg := range th.History {
		roles = append(roles, msg.Role)
	}
	if got, want := strings.Join(roles, ","), "system,user,assistant,tool,user"; got != want {
		t.Fatalf("unexpected resumed order: got %s want %s", got, want)
	}

	persisted, err := loadChatMessages(sessionPath)
	if err != nil {
		t.Fatalf("load rewritten session: %v", err)
	}
	roles = roles[:0]
	for _, msg := range persisted {
		roles = append(roles, msg.Role)
	}
	if got, want := strings.Join(roles, ","), "user,assistant,tool,user"; got != want {
		t.Fatalf("unexpected persisted order: got %s want %s", got, want)
	}
}

func TestTurnsFromHistoryRestoresToolCallItems(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "user", Content: "inspect"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: `{"path":"internal/appserver/model.go"}`,
				Display:   &providers.ToolCallDisplay{Kind: "read", Text: "读取 model.go"},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"path":"internal/appserver/model.go","num_lines":20}`,
		},
		{Role: "assistant", Content: "done"},
	}

	turns := turnsFromHistory("thread", history, time.Unix(0, 0).UTC())
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 3 {
		t.Fatalf("expected user, tool, and assistant items, got %+v", items)
	}
	toolItem := items[1]
	if toolItem.Type != ThreadItemToolCall || toolItem.Name != "read_file" || toolItem.Arguments == "" || toolItem.Result == "" {
		t.Fatalf("unexpected restored tool item: %+v", toolItem)
	}
	if toolItem.Display == nil || toolItem.Display.Text != "读取 model.go" {
		t.Fatalf("expected restored tool display metadata, got %+v", toolItem.Display)
	}
	if items[2].Type != ThreadItemAgentMessage || items[2].Text != "done" {
		t.Fatalf("unexpected assistant item: %+v", items[2])
	}
}

func TestTurnsFromHistoryRestoresCollabAgentToolItems(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "user", Content: "delegate"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "spawn_agent",
				Arguments: `{"task_name":"inspect","message":"inspect"}`,
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"agent_id":"worker-1","agent_path":"/root/inspect","status":"running"}`,
		},
	}

	turns := turnsFromHistory("thread", history, time.Unix(0, 0).UTC())
	if len(turns) != 1 || len(turns[0].Items) != 2 {
		t.Fatalf("unexpected turns: %+v", turns)
	}
	item := turns[0].Items[1]
	if item.Type != ThreadItemCollabAgentTool || item.Name != "spawn_agent" || item.Result == "" {
		t.Fatalf("unexpected collab agent item: %+v", item)
	}
}

func TestServerForwardsAgentNotifications(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	workerClient := &fakeClient{response: providers.ChatResponse{Content: "agent done"}}
	coord, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(workerClient),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "sess-agents",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	rt.AgentControl = coord

	out := &lockedBuffer{}
	srv := New(rt, out)
	threadID := "sess-agents"
	srv.subscribeThreadRuntime(threadID, &runtime.ThreadRuntime{AgentControl: coord})
	res, err := coord.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "check_bridge",
		Description: "check bridge",
		Prompt:      "do it",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msgs := waitForMethod(t, out, NotificationAgentMailbox)
	updated := remarshal[AgentUpdatedNotification](t, notificationByMethod(t, msgs, NotificationAgentUpdated)["params"])
	if updated.ThreadID != threadID || updated.Agent.ID != res.AgentID || updated.Agent.TaskName != "check_bridge" {
		t.Fatalf("unexpected agent update: %+v", updated)
	}
	mailbox := remarshal[AgentMailboxNotification](t, notificationByMethod(t, msgs, NotificationAgentMailbox)["params"])
	if mailbox.ThreadID != threadID || mailbox.Message.AgentID != res.AgentID || mailbox.Message.Result != "agent done" || mailbox.Message.Type != "agent_result" {
		t.Fatalf("unexpected mailbox notification: %+v", mailbox)
	}
}

func TestServerAutoResumesRootAgentOnAgentCompletion(t *testing.T) {
	mainClient := &fakeClient{response: providers.ChatResponse{Content: "integrated result"}}
	rt := newTestRuntime(t, mainClient)
	workerClient := &fakeClient{response: providers.ChatResponse{Content: "agent done"}}
	coord, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(workerClient),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "sess-agents",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	threadID := "sess-agents"
	threadRuntime := &runtime.ThreadRuntime{
		StreamRunner: rt.StreamRunner,
		AgentControl: coord,
	}
	rootThread := newThreadState(threadID, []providers.ChatMessage{
		{Role: "user", Content: "please inspect"},
	}, rt.ProviderName, rt.Model, rt.RootDir, "", time.Now().UTC())
	rootThread.execRuntime = threadRuntime
	srv.mu.Lock()
	srv.threads[threadID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(threadID, threadRuntime)

	res, err := coord.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "check_bridge",
		Description: "check bridge",
		Prompt:      "do it",
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msgs := waitForTurnCompletedForThread(t, out, threadID)
	completed := remarshal[TurnCompletedNotification](t, notificationByMethodForThread(t, msgs, NotificationTurnCompleted, threadID)["params"])
	if completed.Content != "integrated result" {
		t.Fatalf("unexpected root turn completion: %+v", completed)
	}

	mainClient.mu.Lock()
	requests := append([]providers.ChatRequest(nil), mainClient.requests...)
	mainClient.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("expected one main agent request, got %d", len(requests))
	}
	var handoff providers.ChatMessage
	for _, msg := range requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, res.AgentID) && strings.Contains(msg.Content, "agent done") {
			handoff = msg
			break
		}
	}
	if handoff.Content == "" {
		t.Fatalf("main agent request missing worker completion handoff: %+v", requests[0].Messages)
	}
	var communication agentthread.InterAgentCommunication
	if err := json.Unmarshal([]byte(handoff.Content), &communication); err != nil {
		t.Fatalf("handoff is not inter-agent JSON: %v\n%s", err, handoff.Content)
	}
	if !communication.TriggerTurn || communication.Recipient != agentthread.RootAgentPath() {
		t.Fatalf("unexpected handoff envelope: %+v", communication)
	}
}

func TestServerQueuesAgentCompletionWhileRootTurnIsRunning(t *testing.T) {
	mainClient := newBlockingStreamClient("root turn done")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = mainClient
	workerClient := &fakeClient{response: providers.ChatResponse{Content: "agent done"}}
	coord, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(workerClient),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "sess-agents",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	threadID := "sess-agents"
	threadRuntime := &runtime.ThreadRuntime{
		StreamRunner: rt.StreamRunner,
		AgentControl: coord,
	}
	rootThread := newThreadState(threadID, []providers.ChatMessage{
		{Role: "user", Content: "please inspect"},
	}, rt.ProviderName, rt.Model, rt.RootDir, "", time.Now().UTC())
	rootThread.execRuntime = threadRuntime
	srv.mu.Lock()
	srv.threads[threadID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(threadID, threadRuntime)

	req := fmt.Sprintf(`{"id":"1","method":"turn/start","params":{"thread_id":%q,"prompt":"keep working"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(req)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	<-mainClient.started

	if _, err := coord.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "check_bridge",
		Description: "check bridge",
		Prompt:      "do it",
		Synchronous: true,
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForMethod(t, out, NotificationAgentMailbox)

	close(mainClient.release)
	waitForTurnCompletedCountForThread(t, out, threadID, 2)

	rootThread.mu.Lock()
	history := append([]providers.ChatMessage(nil), rootThread.History...)
	rootThread.mu.Unlock()
	var foundHandoff bool
	for _, msg := range history {
		if msg.Role == "user" && strings.Contains(msg.Content, "agent done") && strings.Contains(msg.Content, `"trigger_turn":true`) {
			foundHandoff = true
			break
		}
	}
	if !foundHandoff {
		t.Fatalf("root history missing queued worker completion handoff: %+v", history)
	}
}

func newTestRuntime(t *testing.T, client *fakeClient) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	return &runtime.Session{
		ProviderName: "fake-provider",
		Model:        "fake-model",
		RootDir:      root,
		ConfigPath:   root + "/.wuu.json",
		SessionDir:   root + "/.wuu/sessions",
		StreamRunner: &agent.StreamRunner{
			Client:       providers.AdaptStreamClient(client),
			Model:        "fake-model",
			SystemPrompt: "system prompt",
		},
	}
}

func toolDefinitionNames(defs []providers.ToolDefinition) map[string]bool {
	names := make(map[string]bool, len(defs))
	for _, def := range defs {
		names[def.Name] = true
	}
	return names
}

func parseOutput(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var msgs []map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("parse output line %q: %v", line, err)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func waitForMethod(t *testing.T, out *lockedBuffer, method string) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		for _, msg := range msgs {
			if msg["method"] == method {
				return msgs
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; output:\n%s", method, out.String())
	return nil
}

func waitForTurnCompletedForThread(t *testing.T, out *lockedBuffer, threadID string) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		for _, msg := range msgs {
			if msg["method"] != NotificationTurnCompleted || msg["id"] != nil {
				continue
			}
			params := remarshal[TurnCompletedNotification](t, msg["params"])
			if params.ThreadID == threadID {
				return msgs
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for completed turn on %s; output:\n%s", threadID, out.String())
	return nil
}

func waitForTurnCompletedCountForThread(t *testing.T, out *lockedBuffer, threadID string, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		seen := 0
		for _, msg := range msgs {
			if msg["method"] != NotificationTurnCompleted || msg["id"] != nil {
				continue
			}
			params := remarshal[TurnCompletedNotification](t, msg["params"])
			if params.ThreadID == threadID {
				seen++
			}
		}
		if seen >= count {
			return msgs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d completed turns on %s; output:\n%s", count, threadID, out.String())
	return nil
}

func waitForNotificationCount(t *testing.T, out *lockedBuffer, method string, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := parseOutput(t, out.String())
		if len(notificationsByMethod(msgs, method)) >= count {
			return msgs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %s notifications; output:\n%s", count, method, out.String())
	return nil
}

func responseByID(t *testing.T, msgs []map[string]any, id string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["id"] == id && msg["method"] == nil {
			return msg
		}
	}
	t.Fatalf("response id %s not found in %+v", id, msgs)
	return nil
}

func notificationByMethod(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] == nil {
			return msg
		}
	}
	t.Fatalf("notification %s not found in %+v", method, msgs)
	return nil
}

func notificationByMethodForThread(t *testing.T, msgs []map[string]any, method, threadID string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] != method || msg["id"] != nil {
			continue
		}
		switch method {
		case NotificationTurnCompleted:
			params := remarshal[TurnCompletedNotification](t, msg["params"])
			if params.ThreadID == threadID {
				return msg
			}
		case NotificationTurnStarted:
			params := remarshal[TurnStartedNotification](t, msg["params"])
			if params.ThreadID == threadID {
				return msg
			}
		}
	}
	t.Fatalf("notification %s for thread %s not found in %+v", method, threadID, msgs)
	return nil
}

func notificationsByMethod(msgs []map[string]any, method string) []map[string]any {
	var out []map[string]any
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] == nil {
			out = append(out, msg)
		}
	}
	return out
}

func testStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func requestByMethod(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] == method && msg["id"] != nil {
			return msg
		}
	}
	t.Fatalf("request %s not found in %+v", method, msgs)
	return nil
}

func turnEventByType(t *testing.T, msgs []map[string]any, typ providers.StreamEventType) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["id"] != nil || msg["method"] != NotificationTurnEvent {
			continue
		}
		params := remarshal[TurnEventNotification](t, msg["params"])
		if params.Event.Type == typ {
			return msg
		}
	}
	t.Fatalf("turn event %s not found in %+v", typ, msgs)
	return nil
}

func remarshal[T any](t *testing.T, value any) T {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	return out
}
