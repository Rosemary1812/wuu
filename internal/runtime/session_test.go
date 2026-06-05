package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

type sessionRecordingClient struct {
	mu   sync.Mutex
	last providers.ChatRequest
}

func (c *sessionRecordingClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	c.mu.Lock()
	c.last = req
	c.mu.Unlock()
	return providers.ChatResponse{Content: "done"}, nil
}

func (c *sessionRecordingClient) StreamChat(_ context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	c.mu.Lock()
	c.last = req
	c.mu.Unlock()
	ch := make(chan providers.StreamEvent, 2)
	ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: "done"}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

func (c *sessionRecordingClient) LastRequest() providers.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func writeSessionTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNewSessionUsesUserStateNotWorkspaceDotWuu(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if !strings.HasPrefix(rt.StateDir, filepath.Join(wuuHome, "workspaces")+string(os.PathSeparator)) {
		t.Fatalf("StateDir = %q, want under %q", rt.StateDir, filepath.Join(wuuHome, "workspaces"))
	}
	if rt.SessionDir != statepath.SessionsDir(wuuHome) {
		t.Fatalf("SessionDir = %q, want %q", rt.SessionDir, statepath.SessionsDir(wuuHome))
	}
	if _, err := os.Stat(filepath.Join(root, ".wuu")); !os.IsNotExist(err) {
		t.Fatalf("workspace .wuu should not be created, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(rt.StateDir, "runtime", "processes")); err != nil {
		t.Fatalf("process registry should be under user state: %v", err)
	}
}

func TestNewSessionDefaultProfileIsMemoryless(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if rt.Toolkit == nil {
		t.Fatal("expected toolkit")
	}
	if rt.Toolkit.Memory() != nil {
		t.Fatalf("default profile should be memoryless, got %T", rt.Toolkit.Memory())
	}
	if strings.Contains(rt.BaseSystemPrompt, "# Persistent Memory") {
		t.Fatalf("default profile should not inject persistent memory:\n%s", rt.BaseSystemPrompt)
	}
	defs := make(map[string]bool)
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"read_memory", "write_memory"} {
		if defs[name] {
			t.Fatalf("default profile should not expose %s", name)
		}
	}
}

func TestNewSessionDiscoversWorkflowDefinitions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	writeSessionTestFile(t, filepath.Join(home, ".claude", "workflows", "feature-delivery", "WORKFLOW.md"), `---
name: feature-delivery
description: User feature workflow.
---

## Phases

1. User phase
`)
	writeSessionTestFile(t, filepath.Join(root, ".claude", "workflows", "feature-delivery", "WORKFLOW.md"), `---
name: feature-delivery
description: Project feature workflow.
---

## Phases

1. Project phase
`)
	writeSessionTestFile(t, filepath.Join(home, ".claude", "workflows", "weekly-qa", "WORKFLOW.md"), `---
name: weekly-qa
description: Weekly QA sweep.
---

## Phases

1. Inspect
`)

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(rt.Workflows) != 2 {
		t.Fatalf("Workflows = %+v", rt.Workflows)
	}
	feature, ok := workflow.Find(rt.Workflows, "feature-delivery")
	if !ok {
		t.Fatal("feature-delivery workflow not found")
	}
	if feature.Source != "project" || feature.Description != "Project feature workflow." {
		t.Fatalf("project workflow should override user workflow: %+v", feature)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "Project feature workflow.") || !strings.Contains(rt.BaseSystemPrompt, "`create_workflow`") {
		t.Fatalf("workflow catalog not injected into system prompt:\n%s", rt.BaseSystemPrompt)
	}
	if rt.Toolkit == nil || len(rt.Toolkit.Workflows()) != 2 {
		t.Fatalf("toolkit workflows not wired: %+v", rt.Toolkit)
	}
	defs := map[string]bool{}
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"list_workflows", "load_workflow", "create_workflow", "workflow_control", "workflow_status"} {
		if !defs[name] {
			t.Fatalf("workflow tool %q missing from Definitions()", name)
		}
	}
}

func TestNewThreadRuntimeOrdinarySpawnIsMemoryless(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	client := &sessionRecordingClient{}
	rt.WorkerClient = client
	threadRT, err := rt.NewThreadRuntime("thread-memoryless")
	if err != nil {
		t.Fatalf("NewThreadRuntime: %v", err)
	}
	defer func() {
		threadRT.AgentControl.StopAll()
		time.Sleep(100 * time.Millisecond)
	}()
	if _, err := threadRT.AgentControl.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:        "worker",
		TaskName:    "inspect_repo",
		Prompt:      "inspect the repo",
		Synchronous: true,
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	req := client.LastRequest()
	for _, def := range req.Tools {
		if def.Name == "read_memory" || def.Name == "write_memory" {
			t.Fatalf("ordinary worker should not receive memory tool %q", def.Name)
		}
	}
	if len(req.Messages) == 0 || strings.Contains(req.Messages[0].Content, "# Persistent Memory") {
		t.Fatalf("ordinary worker should not receive persistent memory prompt: %+v", req.Messages)
	}
}

func TestNewThreadRuntimeAgentProfileSpawnReceivesMemory(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	agentProfile := "qa workflow"
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	profileDir, err := statepath.ProfileDir(wuuHome, agentProfile)
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	provider, err := memstore.NewFileProvider(statepath.ProfileMemoryDir(profileDir))
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	if _, err := provider.Store(context.Background(), memstore.Entry{
		Content: "QA workflow checks visual regressions before release",
		Tags:    []string{"target:memory", "qa"},
		Source:  memstore.SourceAssistant,
	}); err != nil {
		t.Fatalf("Store memory: %v", err)
	}

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if rt.Toolkit.Memory() != nil {
		t.Fatalf("ordinary root session should be memoryless, got %T", rt.Toolkit.Memory())
	}
	client := &sessionRecordingClient{}
	rt.WorkerClient = client
	threadRT, err := rt.NewThreadRuntime("thread-profile")
	if err != nil {
		t.Fatalf("NewThreadRuntime: %v", err)
	}
	defer func() {
		threadRT.AgentControl.StopAll()
		time.Sleep(100 * time.Millisecond)
	}()
	res, err := threadRT.AgentControl.Spawn(context.Background(), agentcontrol.SpawnRequest{
		Type:         "worker",
		TaskName:     "qa_check",
		AgentProfile: agentProfile,
		Prompt:       "run the QA workflow",
		Synchronous:  true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.AgentProfile != agentProfile {
		t.Fatalf("AgentProfile = %q, want %q", res.AgentProfile, agentProfile)
	}

	req := client.LastRequest()
	toolNames := map[string]bool{}
	for _, def := range req.Tools {
		toolNames[def.Name] = true
	}
	for _, name := range []string{"read_memory", "write_memory"} {
		if !toolNames[name] {
			t.Fatalf("profile worker missing %s in tools: %+v", name, req.Tools)
		}
	}
	if len(req.Messages) == 0 {
		t.Fatal("profile worker sent no messages")
	}
	systemPrompt := req.Messages[0].Content
	for _, want := range []string{"# Persistent Memory", "QA workflow checks visual regressions"} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("profile worker system prompt missing %q:\n%s", want, systemPrompt)
		}
	}
}

func TestNewSessionUsesProfileMemoryStore(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
			Agent: config.AgentConfig{Name: "Mia Agent"},
			Memory: config.MemoryConfig{
				MemoryCharLimit: 42,
				UserCharLimit:   24,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if rt.Toolkit == nil {
		t.Fatal("expected toolkit")
	}
	provider, ok := rt.Toolkit.Memory().(*memstore.FileProvider)
	if !ok {
		t.Fatalf("memory provider = %T, want *FileProvider", rt.Toolkit.Memory())
	}
	profileDir, err := statepath.ProfileDir(wuuHome, "Mia Agent")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	want := statepath.ProfileMemoryDir(profileDir)
	if provider.Dir() != want {
		t.Fatalf("memory dir = %q, want %q", provider.Dir(), want)
	}
	if strings.HasPrefix(provider.Dir(), rt.StateDir+string(os.PathSeparator)) {
		t.Fatalf("memory dir should be profile-scoped, got workspace path %q", provider.Dir())
	}
	memoryLimit, userLimit := rt.Toolkit.MemoryLimits()
	if memoryLimit != 42 || userLimit != 24 {
		t.Fatalf("memory limits = (%d, %d), want (42, 24)", memoryLimit, userLimit)
	}
	defs := make(map[string]bool)
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"read_memory", "write_memory"} {
		if !defs[name] {
			t.Fatalf("named profile should expose %s", name)
		}
	}
}

func TestNewSessionInjectsProfileMemorySnapshot(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	agentName := "Mia Agent"
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	profileDir, err := statepath.ProfileDir(wuuHome, agentName)
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	provider, err := memstore.NewFileProvider(statepath.ProfileMemoryDir(profileDir))
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	if _, err := provider.Store(context.Background(), memstore.Entry{
		Content: "User prefers concise Chinese replies",
		Tags:    []string{"target:user", "tone"},
		Source:  memstore.SourceUser,
	}); err != nil {
		t.Fatalf("Store user memory: %v", err)
	}
	if _, err := provider.Store(context.Background(), memstore.Entry{
		Content: "Project uses make install for local CLI refresh",
		Tags:    []string{"target:memory", "wuu"},
		Source:  memstore.SourceAssistant,
	}); err != nil {
		t.Fatalf("Store agent memory: %v", err)
	}

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
			Agent: config.AgentConfig{Name: agentName},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for _, want := range []string{
		"# Persistent Memory",
		"User prefers concise Chinese replies",
		"Project uses make install",
	} {
		if !strings.Contains(rt.BaseSystemPrompt, want) {
			t.Fatalf("BaseSystemPrompt missing %q:\n%s", want, rt.BaseSystemPrompt)
		}
	}
	if strings.Contains(rt.BaseSystemPrompt, "target:user") || strings.Contains(rt.BaseSystemPrompt, "target:memory") {
		t.Fatalf("internal target tags leaked into prompt:\n%s", rt.BaseSystemPrompt)
	}
}

func TestNewSessionAppendsUserPromptAfterBuiltInBase(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
			Agent: config.AgentConfig{
				SystemPrompt:       "legacy custom behavior",
				AppendSystemPrompt: "preferred custom behavior",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	baseIdx := strings.Index(rt.BaseSystemPrompt, config.DefaultSystemPrompt())
	legacyIdx := strings.Index(rt.BaseSystemPrompt, "legacy custom behavior")
	preferredIdx := strings.Index(rt.BaseSystemPrompt, "preferred custom behavior")
	if baseIdx == -1 || legacyIdx == -1 || preferredIdx == -1 {
		t.Fatalf("assembled prompt missing base or user additions:\n%s", rt.BaseSystemPrompt)
	}
	if baseIdx > legacyIdx || legacyIdx > preferredIdx {
		t.Fatalf("prompt order should be built-in base, legacy append, preferred append:\n%s", rt.BaseSystemPrompt)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "Follow these user-defined instructions unless they conflict") {
		t.Fatalf("user prompt section must preserve built-in behavior boundary:\n%s", rt.BaseSystemPrompt)
	}
}

func TestNewSessionResolvesConfiguredVariantOptions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "xiaomi",
			Providers: map[string]config.ProviderConfig{
				"xiaomi": {
					Type:      "openai-compatible",
					BaseURL:   "https://token-plan-cn.xiaomimimo.com/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "mimo-v2.5-pro",
				},
			},
			Agent: config.AgentConfig{
				Variant: "high",
				Effort:  "low",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if rt.StreamRunner.Variant != "high" {
		t.Fatalf("Variant = %q, want high", rt.StreamRunner.Variant)
	}
	if rt.StreamRunner.Effort != "" {
		t.Fatalf("legacy Effort should be empty when variant options are active, got %q", rt.StreamRunner.Effort)
	}
	if got := rt.StreamRunner.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("ProviderOptions reasoningEffort = %#v", got)
	}
	if rt.StreamRunner.ContextWindowOverride != 1048576 {
		t.Fatalf("ContextWindowOverride = %d", rt.StreamRunner.ContextWindowOverride)
	}
}

func TestNewSessionUsesCatalogModelAPIIDAndOptions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("OPENAI_API_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "openai",
			Providers: map[string]config.ProviderConfig{
				"openai": {
					Type:    "openai",
					BaseURL: "https://api.openai.com/v1",
					Model:   "gpt-5.5-fast",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if rt.StreamRunner.Model != "gpt-5.5-fast" {
		t.Fatalf("Model = %q", rt.StreamRunner.Model)
	}
	if rt.StreamRunner.APIModel != "gpt-5.5" {
		t.Fatalf("APIModel = %q", rt.StreamRunner.APIModel)
	}
	if got := rt.StreamRunner.ProviderOptions["serviceTier"]; got != "priority" {
		t.Fatalf("ProviderOptions serviceTier = %#v", got)
	}
	if rt.StreamRunner.MaxInputTokens != 922000 {
		t.Fatalf("MaxInputTokens = %d", rt.StreamRunner.MaxInputTokens)
	}
}

func TestResolveInputWindow_CapsCodexSubscriptionGPT5(t *testing.T) {
	got := ResolveInputWindow("gpt-5.5", config.ProviderConfig{
		Type:  "openai-codex",
		Model: "gpt-5.5",
	}, 1_048_576)
	if got != codexSubscriptionGPT5InputCap {
		t.Fatalf("ResolveInputWindow = %d, want %d", got, codexSubscriptionGPT5InputCap)
	}
}

func TestApplyWorkerToolFilter_HidesOrchestrationTools(t *testing.T) {
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	wt, err := agentcontrol.LookupWorkerType("worker")
	if err != nil {
		t.Fatalf("worker type: %v", err)
	}

	applyWorkerToolFilter(kit, wt)

	defs := map[string]bool{}
	for _, def := range kit.Definitions() {
		defs[def.Name] = true
	}
	for _, allowed := range []string{"read_file", "write_file", "run_shell", "update_plan", "spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents"} {
		if !defs[allowed] {
			t.Fatalf("worker toolkit should keep %s", allowed)
		}
	}
}

func TestMCPToolOverridesFromConfig(t *testing.T) {
	readOnly := true
	concurrencySafe := false

	out := mcpToolOverrides(map[string]config.MCPToolOverride{
		"search": {
			ReadOnly:        &readOnly,
			ConcurrencySafe: &concurrencySafe,
		},
	})

	override, ok := out["search"]
	if !ok {
		t.Fatal("missing converted override")
	}
	if override.ReadOnly == nil || *override.ReadOnly != true {
		t.Fatalf("ReadOnly = %v, want true", override.ReadOnly)
	}
	if override.ConcurrencySafe == nil || *override.ConcurrencySafe != false {
		t.Fatalf("ConcurrencySafe = %v, want false", override.ConcurrencySafe)
	}
}

func TestToolPolicyFromConfig(t *testing.T) {
	policy := ToolPolicyFromConfig(config.ToolPolicyConfig{
		DefaultAction: "allow",
		Tools: map[string]string{
			"run_shell": "require_approval",
		},
		Kinds: map[string]string{
			"web": "allow",
		},
		Risks: map[string]string{
			"high": "deny",
		},
	})

	if policy.DefaultAction != tools.ToolPolicyAllow {
		t.Fatalf("DefaultAction = %s, want allow", policy.DefaultAction)
	}
	if policy.ToolActions["run_shell"] != tools.ToolPolicyRequireApproval {
		t.Fatalf("run_shell action = %s, want require_approval", policy.ToolActions["run_shell"])
	}
	if policy.KindActions[tools.ToolKindWeb] != tools.ToolPolicyAllow {
		t.Fatalf("web action = %s, want allow", policy.KindActions[tools.ToolKindWeb])
	}
	if policy.RiskActions[tools.ToolRiskHigh] != tools.ToolPolicyDeny {
		t.Fatalf("high risk action = %s, want deny", policy.RiskActions[tools.ToolRiskHigh])
	}
}

func TestNewThreadRuntimeCreatesIsolatedMutableRuntime(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "test",
			Providers: map[string]config.ProviderConfig{
				"test": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-test",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	first, err := rt.NewThreadRuntime("thread-a")
	if err != nil {
		t.Fatalf("NewThreadRuntime first: %v", err)
	}
	second, err := rt.NewThreadRuntime("thread-b")
	if err != nil {
		t.Fatalf("NewThreadRuntime second: %v", err)
	}

	if rt.Toolkit.SessionID() != "" {
		t.Fatalf("base toolkit session should not be mutated, got %q", rt.Toolkit.SessionID())
	}
	if first.Toolkit == rt.Toolkit || second.Toolkit == rt.Toolkit || first.Toolkit == second.Toolkit {
		t.Fatal("thread runtimes must not share toolkit instances")
	}
	if first.Toolkit.SessionID() != "thread-a" || second.Toolkit.SessionID() != "thread-b" {
		t.Fatalf("unexpected thread toolkit sessions: first=%q second=%q", first.Toolkit.SessionID(), second.Toolkit.SessionID())
	}
	if first.StreamRunner == rt.StreamRunner || second.StreamRunner == rt.StreamRunner || first.StreamRunner == second.StreamRunner {
		t.Fatal("thread runtimes must not share stream runner instances")
	}
	if first.AgentControl == nil || second.AgentControl == nil || first.AgentControl == second.AgentControl {
		t.Fatal("thread runtimes must have distinct agent control instances")
	}
	if first.AgentControl.SessionID() != "thread-a" || second.AgentControl.SessionID() != "thread-b" {
		t.Fatalf("unexpected agent control sessions: first=%q second=%q", first.AgentControl.SessionID(), second.AgentControl.SessionID())
	}
}
