package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/cron"
	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/hooks"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/skills"
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

func TestEnvContextInjectorIncludesExtraTypedBlocks(t *testing.T) {
	root := t.TempDir()
	writeSessionTestFile(t, filepath.Join(root, "go.mod"), "module example.com/runtime\n")
	writeSessionTestFile(t, filepath.Join(root, "main.go"), "package main\n")
	inject := EnvContextInjector(root, nil, "", func() []wuucontext.Block {
		return []wuucontext.Block{{
			Kind:    wuucontext.BlockTaskState,
			Title:   "Current visible task plan",
			Source:  "update_plan",
			Content: "plan:\n- [in_progress] edit",
		}}
	})

	msgs := inject()
	if len(msgs) != 1 {
		t.Fatalf("expected one context message, got %+v", msgs)
	}
	content := msgs[0].Content
	for _, want := range []string{"<system-reminder>", "[ENVIRONMENT]", "[REPO_MAP]", "source: runtime.repo_map", "main.go", "[TASK_STATE]", "source: update_plan", "[in_progress] edit"} {
		if !strings.Contains(content, want) {
			t.Fatalf("injected context missing %q:\n%s", want, content)
		}
	}
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

func waitForScheduledGoalState(t *testing.T, stateDir, taskID string) goalrunner.State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(filepath.Join(stateDir, "goals"))
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cron-goal-"+taskID+"-") {
				continue
			}
			state, err := goalrunner.NewStore(filepath.Join(stateDir, "goals", entry.Name())).LoadState()
			if err != nil {
				lastErr = err
				continue
			}
			if state.Status == goalrunner.StatusCompleted {
				return state
			}
			lastErr = nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("scheduled goal state not found: %v", lastErr)
	}
	t.Fatalf("scheduled goal state for task %q not completed", taskID)
	return goalrunner.State{}
}

func TestCronSchedulerRunsScheduledPrompt(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("WUU_HOME", filepath.Join(t.TempDir(), "wuu-home"))

	taskStore := cron.NewTaskStore(statepath.ScheduledTasksPath(stateDir))
	prompt := "Run workflow weekly-qa with arguments: settings search"
	if err := taskStore.Add(cron.Task{
		ID:        "prompt-1",
		Cron:      "* * * * *",
		Prompt:    prompt,
		Metadata:  map[string]string{"kind": "workflow", "workflow_name": "weekly-qa"},
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("taskStore.Add: %v", err)
	}

	client := &sessionRecordingClient{}
	rt := &Session{
		RootDir:  root,
		StateDir: stateDir,
		StreamRunner: &agent.StreamRunner{
			Client: client,
			Model:  "test-model",
		},
	}
	if err := rt.StartCronScheduler(); err != nil {
		t.Fatalf("StartCronScheduler: %v", err)
	}
	t.Cleanup(func() { _, _ = rt.Cleanup() })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req := client.LastRequest()
		if len(req.Messages) > 0 {
			foundPrompt := false
			for _, msg := range req.Messages {
				if msg.Role == "user" && msg.Content == prompt {
					foundPrompt = true
					break
				}
			}
			if !foundPrompt {
				t.Fatalf("unexpected scheduled prompt request: %+v", req.Messages)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(client.LastRequest().Messages) == 0 {
		t.Fatal("expected scheduled prompt to run")
	}

	tasks, err := taskStore.List()
	if err != nil {
		t.Fatalf("taskStore.List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("one-shot workflow task should be removed after firing, got %+v", tasks)
	}

	goalState := waitForScheduledGoalState(t, stateDir, "prompt-1")
	if goalState.Status != goalrunner.StatusCompleted {
		t.Fatalf("scheduled goal status = %s, want completed: %+v", goalState.Status, goalState)
	}
	if goalState.Trigger.Type != "scheduled" || goalState.Trigger.Source != "cron" {
		t.Fatalf("scheduled goal trigger not recorded: %+v", goalState.Trigger)
	}
	if goalState.Trigger.Payload["workflow_name"] != "weekly-qa" || goalState.Trigger.Payload["kind"] != "workflow" {
		t.Fatalf("scheduled goal missing workflow metadata: %+v", goalState.Trigger.Payload)
	}
	if goalState.AssignedAgent != "cron-scheduler" {
		t.Fatalf("scheduled goal assigned agent = %q", goalState.AssignedAgent)
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

func TestNewSessionMemoryDisableDisablesDreamScheduler(t *testing.T) {
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
			Memory: config.MemoryConfig{Disable: true},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if rt.DreamIntervalDays != 0 {
		t.Fatalf("DreamIntervalDays = %d, want disabled", rt.DreamIntervalDays)
	}
	if rt.StreamRunner.AfterTurn != nil {
		t.Fatal("memory.disable should disable automatic dream AfterTurn hook")
	}
}

func TestNewSessionDiscoversWorkflowDefinitions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	writeSessionTestFile(t, filepath.Join(home, ".claude", "workflows", "feature-delivery", "WORKFLOW.md"), `---
name: feature-delivery
description: User legacy feature workflow.
---

## Phases

1. User phase
`)
	writeSessionTestFile(t, filepath.Join(root, ".claude", "workflows", "feature-delivery", "WORKFLOW.md"), `---
name: feature-delivery
description: Project legacy feature workflow.
---

## Phases

1. Project legacy phase
`)
	writeSessionTestFile(t, filepath.Join(root, ".wuu", "workflows", "feature-delivery", "WORKFLOW.md"), `---
name: feature-delivery
description: Project native feature workflow.
---

## Phases

1. Project phase
`)
	writeSessionTestFile(t, filepath.Join(home, "state", "workflows", "weekly-qa", "WORKFLOW.md"), `---
name: weekly-qa
description: Native user weekly QA sweep.
---

## Phases

1. Inspect
`)
	writeSessionTestFile(t, filepath.Join(home, ".claude", "workflows", "legacy-audit", "WORKFLOW.md"), `---
name: legacy-audit
description: Legacy user audit workflow.
---

## Phases

1. Audit
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
	if len(rt.Workflows) != 4 {
		t.Fatalf("Workflows = %+v", rt.Workflows)
	}
	feature, ok := workflow.Find(rt.Workflows, "feature-delivery")
	if !ok {
		t.Fatal("feature-delivery workflow not found")
	}
	if feature.Source != "project" || feature.Description != "Project native feature workflow." || !strings.Contains(feature.Path, filepath.Join(".wuu", "workflows")) {
		t.Fatalf("native project workflow should override legacy/user workflow: %+v", feature)
	}
	if _, ok := workflow.Find(rt.Workflows, "weekly-qa"); !ok {
		t.Fatalf("native user workflow not discovered: %+v", rt.Workflows)
	}
	if _, ok := workflow.Find(rt.Workflows, "legacy-audit"); !ok {
		t.Fatalf("legacy user workflow not discovered: %+v", rt.Workflows)
	}
	compose, ok := workflow.Find(rt.Workflows, "compose")
	if !ok || compose.Source != "bundled" || !strings.Contains(compose.Content, "session_memory") {
		t.Fatalf("bundled compose workflow not discovered: %+v", compose)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "Project native feature workflow.") || !strings.Contains(rt.BaseSystemPrompt, "`start_workflow`") {
		t.Fatalf("workflow catalog not injected into system prompt:\n%s", rt.BaseSystemPrompt)
	}
	if rt.Toolkit == nil || len(rt.Toolkit.Workflows()) != 4 {
		t.Fatalf("toolkit workflows not wired: %+v", rt.Toolkit)
	}
	defs := map[string]bool{}
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"list_workflows", "load_workflow", "save_workflow", "start_workflow", "workflow_control", "workflow_status"} {
		if !defs[name] {
			t.Fatalf("workflow tool %q missing from Definitions()", name)
		}
	}
	for _, name := range []string{"create_workflow", "run_workflow"} {
		if defs[name] {
			t.Fatalf("lower-level workflow driver %q should be deferred from Definitions()", name)
		}
		info, ok := rt.Toolkit.ToolInfo(name)
		if !ok {
			t.Fatalf("workflow driver %q missing from ToolInfo()", name)
		}
		if info.Exposure != tools.ToolExposureDeferred {
			t.Fatalf("workflow driver %q exposure = %s, want %s", name, info.Exposure, tools.ToolExposureDeferred)
		}
	}
}

func TestNewSessionDiscoversPluginSkillsAndWorkflows(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	pluginRoot := filepath.Join(root, ".wuu", "plugins", "compose-kit")
	writeSessionTestFile(t, filepath.Join(pluginRoot, "plugin.json"), `{
  "id": "compose-kit",
  "description": "Compose plugin assets",
  "skills": ["skills"],
  "workflows": ["workflows"]
}`)
	writeSessionTestFile(t, filepath.Join(pluginRoot, "skills", "brainstorm.md"), `---
name: brainstorm
description: Explore product options.
---
Brainstorm options.
`)
	writeSessionTestFile(t, filepath.Join(pluginRoot, "workflows", "release", "WORKFLOW.md"), `---
name: release-compose
description: Compose release workflow.
---
Release body.
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
	if len(rt.Plugins) != 1 || rt.Plugins[0].ID != "compose-kit" {
		t.Fatalf("plugins not discovered: %+v", rt.Plugins)
	}
	skill, ok := skills.Find(rt.Skills, "brainstorm")
	if !ok || skill.Source != "plugin:compose-kit" {
		t.Fatalf("plugin skill not discovered with source: %+v", skill)
	}
	wf, ok := workflow.Find(rt.Workflows, "release-compose")
	if !ok || wf.Source != "plugin:compose-kit" {
		t.Fatalf("plugin workflow not discovered with source: %+v", wf)
	}
}

func TestDiscoverSkillsUsesOpencodeAndAgentsPaths(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	workspace := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	writeSessionTestFile(t, filepath.Join(root, ".opencode", "skill", "root-skill", "SKILL.md"), `---
name: root-skill
description: Root opencode skill.
---
Root body.
`)
	writeSessionTestFile(t, filepath.Join(workspace, ".opencode", "skills", "app-skill", "SKILL.md"), `---
name: app-skill
description: App opencode skill.
---
App body.
`)
	writeSessionTestFile(t, filepath.Join(workspace, ".agents", "skills", "agent-skill", "SKILL.md"), `---
name: agent-skill
description: Agent skill.
---
Agent body.
`)
	writeSessionTestFile(t, filepath.Join(home, ".config", "opencode", "skills", "global-skill", "SKILL.md"), `---
name: global-skill
description: Global opencode skill.
---
Global body.
`)

	got := discoverSkills(workspace, home, filepath.Join(home, ".wuu"), nil)
	for _, name := range []string{"root-skill", "app-skill", "agent-skill", "global-skill"} {
		if _, ok := skills.Find(got, name); !ok {
			t.Fatalf("skill %q not discovered in %+v", name, got)
		}
	}
}

func TestNewSessionWiresPluginHooks(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	pluginRoot := filepath.Join(root, ".wuu", "plugins", "hook-kit")
	writeSessionTestFile(t, filepath.Join(pluginRoot, "plugin.json"), `{
  "id": "hook-kit",
  "hooks": {
    "PreToolUse": [
      {"matcher": "read_file", "command": "printf '{\"additional_context\":\"plugin hook ran\"}'"}
    ]
  }
}`)

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
	out, err := rt.HookDispatcher.Dispatch(context.Background(), hooks.PreToolUse, &hooks.Input{ToolName: "read_file"})
	if err != nil {
		t.Fatalf("Dispatch plugin hook: %v", err)
	}
	if out.Context != "plugin hook ran" {
		t.Fatalf("plugin hook context = %q", out.Context)
	}
}

func TestMCPServersFromConfigAndPluginsPrefixesPluginServers(t *testing.T) {
	plugins := []pluginpkg.Plugin{{
		Manifest: pluginpkg.Manifest{
			ID: "docs",
			MCPServers: map[string]config.MCPServerConfig{
				"search": {Command: "plugin-docs"},
			},
		},
	}}
	cfg := config.Config{
		MCPServers: map[string]config.MCPServerConfig{
			"search": {Command: "user-docs"},
		},
	}
	servers := mcpServersFromConfigAndPlugins(cfg, plugins)
	if servers["search"].Command != "user-docs" {
		t.Fatalf("user MCP server changed: %+v", servers)
	}
	if servers["plugin.docs.search"].Command != "plugin-docs" {
		t.Fatalf("plugin MCP server missing or unprefixed: %+v", servers)
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
		Type:        agentcontrol.DefaultSubagentType,
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
		Type:         agentcontrol.DefaultSubagentType,
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
	for _, want := range []string{
		"# Harness Adapter",
		"Provider/model: openai/gpt-5.5",
		"same product regardless of provider, model family, or BYOK backend",
		"Do not choose direct work, subagents, or workflows based on provider/model family or brand.",
	} {
		if !strings.Contains(rt.BaseSystemPrompt, want) {
			t.Fatalf("BaseSystemPrompt missing harness adapter text %q:\n%s", want, rt.BaseSystemPrompt)
		}
	}
	if got := rt.StreamRunner.ProviderOptions["serviceTier"]; got != "priority" {
		t.Fatalf("ProviderOptions serviceTier = %#v", got)
	}
	if rt.StreamRunner.MaxInputTokens != 922000 {
		t.Fatalf("MaxInputTokens = %d", rt.StreamRunner.MaxInputTokens)
	}
}

func TestSessionRefreshSystemPromptUpdatesRunnerPrompt(t *testing.T) {
	rt := &Session{
		RootDir:          t.TempDir(),
		UserSystemPrompt: "Prefer concise answers.",
		StreamRunner:     &agent.StreamRunner{SystemPrompt: "old prompt"},
	}

	prompt := rt.RefreshSystemPrompt("openai", "gpt-5-codex")

	for _, want := range []string{
		"# Harness Adapter",
		"Provider/model: openai/gpt-5-codex",
		"same product regardless of provider, model family, or BYOK backend",
		"Prefer concise answers.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("refreshed prompt missing %q:\n%s", want, prompt)
		}
	}
	if rt.BaseSystemPrompt != prompt || rt.StreamRunner.SystemPrompt != prompt {
		t.Fatalf("refresh should update session and runner prompts")
	}
	if strings.Contains(prompt, "old prompt") {
		t.Fatalf("refreshed prompt should not keep stale runner prompt:\n%s", prompt)
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

func TestApplyWorkerToolFilter_HidesRecursiveAgentControls(t *testing.T) {
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	wt, err := agentcontrol.LookupWorkerType(agentcontrol.DefaultSubagentType)
	if err != nil {
		t.Fatalf("agent type: %v", err)
	}

	applyWorkerToolFilter(kit, wt)

	defs := map[string]bool{}
	for _, def := range kit.Definitions() {
		defs[def.Name] = true
	}
	for _, allowed := range []string{"read_file", "write_file", "run_shell", "run_test", "update_plan", "agent_report"} {
		if !defs[allowed] {
			t.Fatalf("subagent toolkit should keep %s", allowed)
		}
	}
	for _, blocked := range []string{"spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents"} {
		if defs[blocked] {
			t.Fatalf("subagent toolkit should hide recursive control tool %s", blocked)
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
		Profile:       "balanced",
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
	if policy.Profile != tools.ToolPolicyProfileBalanced {
		t.Fatalf("Profile = %s, want balanced", policy.Profile)
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

func TestToolPolicyFromConfigAppliesProfileDefaults(t *testing.T) {
	policy := ToolPolicyFromConfig(config.ToolPolicyConfig{
		Profile: "enterprise_restricted",
		Risks: map[string]string{
			"medium": "allow",
		},
	})

	if policy.DefaultAction != tools.ToolPolicyDeny {
		t.Fatalf("DefaultAction = %s, want deny", policy.DefaultAction)
	}
	if policy.RiskActions[tools.ToolRiskLow] != tools.ToolPolicyAllow {
		t.Fatalf("low risk action = %s, want allow", policy.RiskActions[tools.ToolRiskLow])
	}
	if policy.RiskActions[tools.ToolRiskHigh] != tools.ToolPolicyDeny {
		t.Fatalf("high risk action = %s, want deny", policy.RiskActions[tools.ToolRiskHigh])
	}
	if policy.RiskActions[tools.ToolRiskMedium] != tools.ToolPolicyAllow {
		t.Fatalf("explicit medium risk override = %s, want allow", policy.RiskActions[tools.ToolRiskMedium])
	}
}

func TestToolPolicyFromConfigAppliesAutoProfileDefaults(t *testing.T) {
	policy := ToolPolicyFromConfig(config.ToolPolicyConfig{
		Profile: "auto",
	})

	if policy.Profile != tools.ToolPolicyProfileAuto {
		t.Fatalf("Profile = %s, want auto", policy.Profile)
	}
	if policy.DefaultAction != tools.ToolPolicyAutoClassify {
		t.Fatalf("DefaultAction = %s, want auto_classify", policy.DefaultAction)
	}
	if policy.RiskActions[tools.ToolRiskLow] != tools.ToolPolicyAllow {
		t.Fatalf("low risk action = %s, want allow", policy.RiskActions[tools.ToolRiskLow])
	}
	if policy.RiskActions[tools.ToolRiskMedium] != tools.ToolPolicyAutoClassify {
		t.Fatalf("medium risk action = %s, want auto_classify", policy.RiskActions[tools.ToolRiskMedium])
	}
	if policy.RiskActions[tools.ToolRiskHigh] != tools.ToolPolicyAutoClassify {
		t.Fatalf("high risk action = %s, want auto_classify", policy.RiskActions[tools.ToolRiskHigh])
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
