package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/capability"
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

func TestRuntimeContextInjectorIncludesOnlyDynamicTypedBlocks(t *testing.T) {
	inject := RuntimeContextInjector(nil, "", func() []wuucontext.Block {
		return []wuucontext.Block{{
			Kind:    wuucontext.BlockTaskState,
			Title:   "Current visible task plan",
			Source:  "update_plan",
			Content: "plan:\n- [in_progress] edit",
		}}
	})

	msgs := inject()
	messages := flattenContextSegmentsForTest(msgs)
	if len(messages) != 1 {
		t.Fatalf("expected split context messages, got %+v", msgs)
	}
	combined := strings.Builder{}
	names := make(map[string]bool, len(messages))
	for _, msg := range messages {
		if msg.Role != "user" || !msg.Hidden || !wuucontext.IsSystemReminder(msg.Name, msg.Content) {
			t.Fatalf("expected hidden context reminder message, got %+v", msg)
		}
		if names[msg.Name] {
			t.Fatalf("context message names should be unique: %+v", msgs)
		}
		names[msg.Name] = true
		combined.WriteString(msg.Content)
		combined.WriteString("\n")
	}
	content := combined.String()
	for _, want := range []string{"<system-reminder>", "[TASK_STATE]", "source: update_plan", "[in_progress] edit"} {
		if !strings.Contains(content, want) {
			t.Fatalf("injected context missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "[ENVIRONMENT]") {
		t.Fatalf("stable environment should stay out of request-only context:\n%s", content)
	}
}

func (c *sessionRecordingClient) LastRequest() providers.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func flattenContextSegmentsForTest(segments []agent.ContextSegment) []providers.ChatMessage {
	var out []providers.ChatMessage
	for _, segment := range segments {
		out = append(out, segment.Messages...)
	}
	return out
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

func TestNewSessionDefaultProfileEnablesGlobalMemory(t *testing.T) {
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
	if rt.Toolkit.Memory() == nil {
		t.Fatal("default profile should attach the global long-term memory store")
	}
	if rt.Toolkit.ActiveSurface().ProfileName == "" {
		t.Fatal("expected runtime toolkit to install a model surface")
	}
	if !strings.Contains(rt.BaseSystemPrompt, "[Tool surface:") || !strings.Contains(rt.BaseSystemPrompt, "Terminal work") {
		t.Fatalf("base system prompt should include compiled tool-surface fragment:\n%s", rt.BaseSystemPrompt)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "# Persistent Memory") {
		t.Fatalf("default profile should inject the global memory snapshot:\n%s", rt.BaseSystemPrompt)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "# Runtime Tool Policy") ||
		!strings.Contains(rt.BaseSystemPrompt, "approval_policy: on_request") {
		t.Fatalf("default profile should inject stable tool policy into the system prompt:\n%s", rt.BaseSystemPrompt)
	}
	defs := make(map[string]bool)
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"read_memory", "write_memory"} {
		if defs[name] {
			t.Fatalf("default profile should keep %s deferred", name)
		}
		info, ok := rt.Toolkit.ToolInfo(name)
		if !ok {
			t.Fatalf("ToolInfo(%q) not found", name)
		}
		if info.Exposure != tools.ToolExposureDeferred {
			t.Fatalf("%s exposure = %s, want %s", name, info.Exposure, tools.ToolExposureDeferred)
		}
	}
}

func TestNewSessionKeepsGitContextOutOfBaseSystemPrompt(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	writeSessionTestFile(t, filepath.Join(root, "changed.txt"), "dirty\n")

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

	for _, disallowed := range []string{"# Git Context", "Recent commits:", "Status:\n", "Branch:"} {
		if strings.Contains(rt.BaseSystemPrompt, disallowed) {
			t.Fatalf("base system prompt should not include volatile git context %q:\n%s", disallowed, rt.BaseSystemPrompt)
		}
	}

	if !strings.Contains(rt.BaseSystemPrompt, "# Environment") ||
		!strings.Contains(rt.BaseSystemPrompt, "- Current working directory: "+root) ||
		!strings.Contains(rt.BaseSystemPrompt, "- Current date:") {
		t.Fatalf("base system prompt should include stable environment context:\n%s", rt.BaseSystemPrompt)
	}
	foundEnvironmentSection := false
	for _, section := range rt.BaseSystemPromptSections {
		if section.Key == "environment" && section.Static {
			foundEnvironmentSection = true
			break
		}
	}
	if !foundEnvironmentSection {
		t.Fatalf("base system prompt should report a static environment section: %+v", rt.BaseSystemPromptSections)
	}
	if strings.Contains(rt.BaseSystemPrompt, "Git status:") || strings.Contains(rt.BaseSystemPrompt, "Git branch:") {
		t.Fatalf("base system prompt should not include volatile git state:\n%s", rt.BaseSystemPrompt)
	}

	segments := RuntimeContextInjector(nil, "")()
	msgs := flattenContextSegmentsForTest(segments)
	if len(msgs) != 0 {
		t.Fatalf("default runtime context should not inject stable environment messages, got %+v", segments)
	}
	content := rt.BaseSystemPrompt
	if strings.Contains(content, "[REPO_MAP]") || strings.Contains(content, "source: runtime.repo_map") {
		t.Fatalf("base system prompt should not inject repo map by default:\n%s", content)
	}
	if strings.Contains(content, "[RECENT_DIFF]") {
		t.Fatalf("base system prompt should not inject recent diff by default:\n%s", content)
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
	if strings.Contains(rt.BaseSystemPrompt, "Project native feature workflow.") ||
		strings.Contains(rt.BaseSystemPrompt, "Workflow guidance") {
		t.Fatalf("workflow catalog should stay behind tool_search:\n%s", rt.BaseSystemPrompt)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "start_workflow") || !strings.Contains(rt.BaseSystemPrompt, "# Tool Discovery") {
		t.Fatalf("workflow prompt should keep lightweight orchestration and discovery guidance:\n%s", rt.BaseSystemPrompt)
	}
	if rt.Toolkit == nil || len(rt.Toolkit.Workflows()) != 4 {
		t.Fatalf("toolkit workflows not wired: %+v", rt.Toolkit)
	}
	defs := map[string]bool{}
	for _, def := range rt.Toolkit.Definitions() {
		defs[def.Name] = true
	}
	for _, name := range []string{"list_workflows", "load_workflow", "save_workflow", "start_workflow", "workflow_status", "create_workflow", "run_workflow", "workflow_control"} {
		if defs[name] {
			t.Fatalf("workflow tool %q should be deferred from Definitions()", name)
		}
		info, ok := rt.Toolkit.ToolInfo(name)
		if !ok {
			t.Fatalf("workflow tool %q missing from ToolInfo()", name)
		}
		if info.Exposure != tools.ToolExposureDeferred {
			t.Fatalf("workflow tool %q exposure = %s, want %s", name, info.Exposure, tools.ToolExposureDeferred)
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

func TestNewThreadRuntimeWorkerUsesWorkerProfileToolSurface(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")
	t.Setenv("TEST_ANTHROPIC_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "openai",
			Providers: map[string]config.ProviderConfig{
				"openai": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "gpt-5-codex",
				},
				"anthropic": {
					Type:      "anthropic",
					BaseURL:   "https://api.anthropic.com",
					APIKeyEnv: "TEST_ANTHROPIC_KEY",
					Model:     "claude-sonnet-4-5",
				},
			},
			Agent: config.AgentConfig{
				ModelRoles: config.ModelRolesConfig{
					Worker: config.ModelRoleConfig{Provider: "anthropic", Model: "claude-sonnet-4-5"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if !strings.Contains(rt.BaseSystemPrompt, "[Tool surface: openai_codex]") {
		t.Fatalf("main prompt should use main Codex surface:\n%s", rt.BaseSystemPrompt)
	}

	client := &sessionRecordingClient{}
	rt.WorkerClient = client
	threadRT, err := rt.NewThreadRuntime("thread-worker-surface")
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
	toolNames := map[string]bool{}
	for _, def := range req.Tools {
		toolNames[def.Name] = true
	}
	for _, want := range []string{"bash", "edit_file", "write_file"} {
		if !toolNames[want] {
			t.Fatalf("worker should receive %s from worker profile surface; tools=%v", want, toolNames)
		}
	}
	if toolNames["apply_patch"] {
		t.Fatalf("worker should not inherit Codex apply_patch surface; tools=%v", toolNames)
	}
	if len(req.Messages) == 0 {
		t.Fatal("worker sent no messages")
	}
	systemPrompt := req.Messages[0].Content
	for _, want := range []string{"[Tool surface: anthropic_claude]", "Your file editing primitives are read_file, edit_file, and write_file"} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("worker system prompt should use worker profile fragment %q:\n%s", want, systemPrompt)
		}
	}
}

func TestNewThreadRuntimeLocalWorkerDoesNotTeachTerminalPaths(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "ollama",
			Providers: map[string]config.ProviderConfig{
				"ollama": {
					Type:    "openai-compatible",
					BaseURL: "http://127.0.0.1:11434/v1",
					APIKey:  "dummy",
					Model:   "llama-coder",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	client := &sessionRecordingClient{}
	rt.WorkerClient = client
	threadRT, err := rt.NewThreadRuntime("thread-local-worker-surface")
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
	toolNames := map[string]bool{}
	for _, def := range req.Tools {
		toolNames[def.Name] = true
	}
	for _, want := range []string{"read_file", "edit_file", "write_file"} {
		if !toolNames[want] {
			t.Fatalf("local worker should keep %s from generic edit surface; tools=%v", want, toolNames)
		}
	}
	for _, hidden := range []string{"bash", "run_shell", "run_test", "start_process", "git", "apply_patch"} {
		if toolNames[hidden] {
			t.Fatalf("local worker must not expose %s; tools=%v", hidden, toolNames)
		}
	}
	if len(req.Messages) == 0 {
		t.Fatal("worker sent no messages")
	}
	systemPrompt := req.Messages[0].Content
	if !strings.Contains(systemPrompt, "[Tool surface: generic") {
		t.Fatalf("local worker prompt missing generic surface fragment:\n%s", systemPrompt)
	}
	for _, banned := range []string{
		"bash",
		"run_shell",
		"run_test",
		"start_process",
		"command.bash",
		"terminal",
		"shell",
		"git",
		"git status",
		"git diff",
		"git commit",
		"npx vitest",
		"npm test",
		"npm run dev",
	} {
		if strings.Contains(systemPrompt, banned) {
			t.Fatalf("local worker prompt must not teach terminal path %q:\n%s", banned, systemPrompt)
		}
	}
}

func TestNewThreadRuntimeAgentProfileSpawnReceivesMemory(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	agentProfile := "qa workflow"
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	provider, err := memstore.NewFileProvider(statepath.GlobalMemoryDir(wuuHome))
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
	if rt.Toolkit.Memory() == nil {
		t.Fatal("ordinary root session should attach the global long-term memory store")
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
		if toolNames[name] {
			t.Fatalf("profile worker should keep %s deferred from top-level tools: %+v", name, req.Tools)
		}
	}
	if !toolNames["tool_search"] {
		t.Fatalf("profile worker should expose tool_search for deferred memory tools: %+v", req.Tools)
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
	if !strings.Contains(systemPrompt, "[Tool surface:") || !strings.Contains(systemPrompt, "Terminal work") {
		t.Fatalf("profile worker system prompt missing tool-surface fragment:\n%s", systemPrompt)
	}
}

func TestNewSessionUsesGlobalMemoryStore(t *testing.T) {
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
	want := statepath.GlobalMemoryDir(wuuHome)
	if provider.Dir() != want {
		t.Fatalf("memory dir = %q, want %q", provider.Dir(), want)
	}
	if strings.HasPrefix(provider.Dir(), rt.StateDir+string(os.PathSeparator)) {
		t.Fatalf("memory dir should be the global user store, got workspace path %q", provider.Dir())
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
		if defs[name] {
			t.Fatalf("default session should keep %s deferred", name)
		}
		info, ok := rt.Toolkit.ToolInfo(name)
		if !ok {
			t.Fatalf("ToolInfo(%q) not found", name)
		}
		if info.Exposure != tools.ToolExposureDeferred {
			t.Fatalf("%s exposure = %s, want %s", name, info.Exposure, tools.ToolExposureDeferred)
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

	provider, err := memstore.NewFileProvider(statepath.GlobalMemoryDir(wuuHome))
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

func TestNewSessionUsesConfiguredModelLimitForContextWindow(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "custom",
			Providers: map[string]config.ProviderConfig{
				"custom": {
					Type:      "anthropic",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "private-1m-model",
					Models: map[string]config.ProviderModelConfig{
						"private-1m-model": {
							Limit: &config.ProviderModelLimitConfig{
								Context: 1_000_000,
								Output:  128_000,
							},
						},
					},
				},
			},
			Agent: config.AgentConfig{Name: "Mia Agent"},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if rt.StreamRunner.ContextWindowOverride != 1_000_000 {
		t.Fatalf("ContextWindowOverride = %d", rt.StreamRunner.ContextWindowOverride)
	}
	if rt.StreamRunner.MaxInputTokens != 0 {
		t.Fatalf("MaxInputTokens = %d, want 0 without an explicit input limit", rt.StreamRunner.MaxInputTokens)
	}
	if rt.StreamRunner.OutputReserveTokens != 128_000 {
		t.Fatalf("OutputReserveTokens = %d", rt.StreamRunner.OutputReserveTokens)
	}
}

func TestNewSessionUnknownModelDisablesProactiveContextWindow(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "custom",
			Providers: map[string]config.ProviderConfig{
				"custom": {
					Type:      "anthropic",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "private-unknown-byo-model",
				},
			},
			Agent: config.AgentConfig{Name: "Mia Agent"},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if rt.StreamRunner.ContextWindowOverride != 0 {
		t.Fatalf("ContextWindowOverride = %d, want 0 for unknown BYOK model", rt.StreamRunner.ContextWindowOverride)
	}
	if rt.StreamRunner.MaxInputTokens != 0 {
		t.Fatalf("MaxInputTokens = %d, want 0 for unknown BYOK model", rt.StreamRunner.MaxInputTokens)
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
		"[Tool surface: openai_gpt]",
		"Your editing primitive is apply_patch",
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
	if rt.StreamRunner.OutputReserveTokens != 128000 {
		t.Fatalf("OutputReserveTokens = %d", rt.StreamRunner.OutputReserveTokens)
	}
}

func TestNewSessionResolvesRoleModelSelections(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "custom",
			Providers: map[string]config.ProviderConfig{
				"custom": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "main-model",
					Models: map[string]config.ProviderModelConfig{
						"title-alias": {
							ID: "title-api-model",
						},
						"worker-alias": {
							ID: "worker-api-model",
							Variants: map[string]map[string]any{
								"deep": {"reasoningEffort": "high"},
							},
						},
					},
				},
			},
			Agent: config.AgentConfig{
				ModelRoles: config.ModelRolesConfig{
					Title:  config.ModelRoleConfig{Model: "title-alias", Effort: "low"},
					Worker: config.ModelRoleConfig{Model: "worker-alias", Variant: "deep"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if rt.StreamRunner.Model != "main-model" {
		t.Fatalf("main runner model = %q", rt.StreamRunner.Model)
	}
	if rt.ModelRoles.Title.Inherited || rt.ModelRoles.Title.APIModel != "title-api-model" || rt.ModelRoles.Title.LegacyEffort != "low" {
		t.Fatalf("unexpected title role: %+v", rt.ModelRoles.Title)
	}
	if rt.ModelRoles.Worker.Inherited || rt.ModelRoles.Worker.APIModel != "worker-api-model" || rt.ModelRoles.Worker.Variant != "deep" {
		t.Fatalf("unexpected worker role: %+v", rt.ModelRoles.Worker)
	}
	if got := rt.ModelRoles.Worker.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("worker role provider options = %#v", rt.ModelRoles.Worker.ProviderOptions)
	}
	if rt.TitleClient == nil || rt.WorkerClient == nil {
		t.Fatalf("expected title and worker clients to be configured: title=%T worker=%T", rt.TitleClient, rt.WorkerClient)
	}
}

func TestNewSessionAppliesPermissionBoundary(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(home, "state"))
	t.Setenv("TEST_WUU_KEY", "abc")

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config: config.Config{
			DefaultProvider: "custom",
			Providers: map[string]config.ProviderConfig{
				"custom": {
					Type:      "openai-compatible",
					BaseURL:   "https://example.test/v1",
					APIKeyEnv: "TEST_WUU_KEY",
					Model:     "main-model",
				},
			},
			Agent: config.AgentConfig{
				PermissionMode: config.PermissionModeReadOnly,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	_, err = rt.Toolkit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"blocked.txt","content":"nope"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_boundary_denied") {
		t.Fatalf("expected read-only runtime boundary, got %v", err)
	}
}

func TestNewThreadRuntimeWorkerReceivesCurrentPermissionContext(t *testing.T) {
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

	permissions, err := config.ResolvePermissionModePreset(config.PermissionModeReadOnly)
	if err != nil {
		t.Fatalf("ResolvePermissionModePreset: %v", err)
	}
	rt.Permissions = permissions
	rt.ToolPolicy = config.ToolPolicyConfig{}
	ConfigureToolkitPermissions(rt.Toolkit, rt.ToolPolicy, rt.Permissions)

	client := &sessionRecordingClient{}
	rt.WorkerClient = client
	threadRT, err := rt.NewThreadRuntime("thread-read-only-worker")
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
	var joined strings.Builder
	for _, msg := range req.Messages {
		joined.WriteString(msg.Content)
		joined.WriteByte('\n')
	}
	content := joined.String()
	if !strings.Contains(content, "permission_profile: read_only") ||
		!strings.Contains(content, "boundary: read_only") {
		t.Fatalf("worker request missing current permission context:\n%s", content)
	}
}

func TestSessionRefreshSystemPromptUpdatesRunnerPrompt(t *testing.T) {
	root := t.TempDir()
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex", false)
	rt := &Session{
		RootDir:          root,
		UserSystemPrompt: "Prefer concise answers.",
		StreamRunner:     &agent.StreamRunner{SystemPrompt: "old prompt"},
		Toolkit:          kit,
	}

	prompt := rt.RefreshSystemPrompt("openai", "gpt-5-codex")

	for _, want := range []string{
		"# Harness Adapter",
		"Provider/model: openai/gpt-5-codex",
		"[Tool surface: openai_codex]",
		"Your editing primitive is apply_patch",
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

func TestDiscoverMemoryHonorsLegacyOptIn(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "CLAUDE.md"), []byte("legacy project rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	if files := discoverMemory(root, home, config.MemoryConfig{}); len(files) != 0 {
		t.Fatalf("default runtime memory discovery should skip legacy files, got %+v", files)
	}

	includeLegacy := true
	files := discoverMemory(root, home, config.MemoryConfig{IncludeLegacyMemory: &includeLegacy})
	if len(files) != 1 || files[0].Content != "legacy project rule" {
		t.Fatalf("legacy opt-in did not load legacy file: %+v", files)
	}
}

func TestBuildBaseSystemPromptNoToolsSkipsToolLoadedGuidance(t *testing.T) {
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		"base prompt",
		"",
		"openai-codex",
		"gpt-5.5",
		capability.Surface{},
		nil,
		nil,
		false,
		0,
		0,
		[]skills.Skill{{Name: "commit", Description: "Create a commit."}},
		[]workflow.Definition{{Name: "release", Description: "Release workflow."}},
	)

	for _, bad := range []string{
		"Skills provide specialized instructions",
		"<available_skills>",
		"Create a commit",
		"Workflow guidance",
		"Release workflow",
		"`start_workflow`",
		"Tool Discovery",
	} {
		if strings.Contains(promptText, bad) {
			t.Fatalf("no-tools prompt should not advertise tool-loaded guidance %q:\n%s", bad, promptText)
		}
	}
}

func TestBuildBaseSystemPromptAddsToolDiscoveryForToolSearchSurface(t *testing.T) {
	surface := compiledSurfaceForProviderModel("openai", "gpt-5-codex")
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		"base prompt",
		"",
		"openai",
		"gpt-5-codex",
		surface,
		nil,
		nil,
		false,
		0,
		0,
		nil,
		nil,
	)

	for _, want := range []string{
		"# Tool Discovery",
		"`tool_search`",
		"select:<tool_name>",
		"Do not use `tool_search` for visible core tools",
	} {
		if !strings.Contains(promptText, want) {
			t.Fatalf("tool-search surface prompt missing %q:\n%s", want, promptText)
		}
	}
}

func TestBuildBaseSystemPromptDefersWorkflowCatalogForToolSearchSurface(t *testing.T) {
	surface := compiledSurfaceForProviderModel("openai", "gpt-5-codex")
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		"base prompt",
		"",
		"openai",
		"gpt-5-codex",
		surface,
		nil,
		nil,
		false,
		0,
		0,
		nil,
		[]workflow.Definition{{Name: "release", Description: "Release workflow."}},
	)

	for _, want := range []string{
		"# Tool Discovery",
		"workflows",
		"`tool_search`",
	} {
		if !strings.Contains(promptText, want) {
			t.Fatalf("tool-search workflow prompt missing %q:\n%s", want, promptText)
		}
	}
	for _, bad := range []string{
		"Workflow guidance",
		"Release workflow",
	} {
		if strings.Contains(promptText, bad) {
			t.Fatalf("tool-search workflow prompt should defer catalog %q:\n%s", bad, promptText)
		}
	}
}

func TestBuildBaseSystemPromptFiltersSkillsBySurface(t *testing.T) {
	surface := compiledSurfaceForProviderModel("ollama", "llama-coder")
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		"base prompt",
		"",
		"ollama",
		"llama-coder",
		surface,
		nil,
		nil,
		false,
		0,
		0,
		[]skills.Skill{
			{
				Name:         "commit",
				Description:  "Create a commit.",
				WhenToUse:    "Use when asked to commit.",
				Content:      "Use bash to run git status.",
				AllowedTools: []string{"bash"},
			},
			{
				Name:         "misdeclared-shell",
				Description:  "Misdeclared shell workflow.",
				WhenToUse:    "Use when asked to inspect a repo.",
				Content:      "Git: run git-status before continuing.",
				AllowedTools: []string{"read_file"},
			},
			{
				Name:         "claude-style-shell",
				Description:  "Claude style tool declaration.",
				WhenToUse:    "Use when asked to inspect terminal output.",
				Content:      "Run the command.",
				AllowedTools: []string{"Bash(git status:*)"},
			},
			{
				Name:         "implementation-plan",
				Description:  "Plan the implementation.",
				WhenToUse:    "Use before broad edits.",
				Content:      "Create a scoped plan.",
				AllowedTools: []string{"read_file", "grep", "glob"},
			},
		},
		nil,
	)

	if strings.Contains(promptText, "Create a commit") ||
		strings.Contains(promptText, "misdeclared-shell") ||
		strings.Contains(promptText, "claude-style-shell") ||
		strings.Contains(promptText, "Git:") ||
		strings.Contains(promptText, "git-status") ||
		strings.Contains(promptText, "Use bash to run git status") {
		t.Fatalf("local/no-shell prompt must not advertise terminal-only skills:\n%s", promptText)
	}
	if !strings.Contains(promptText, "implementation-plan") {
		t.Fatalf("local/no-shell prompt should keep compatible skills:\n%s", promptText)
	}
}

func TestBuildBaseSystemPromptFiltersWorkflowsBySurface(t *testing.T) {
	surface := capability.Surface{
		ProfileName:    "portable_no_shell",
		Tools:          map[string]capability.Capability{"read_file": capability.CapabilityFileRead},
		Capabilities:   []capability.Capability{capability.CapabilityFileRead},
		SystemFragment: "Portable no-shell profile.",
	}
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		"base prompt",
		"",
		"ollama",
		"llama-coder",
		surface,
		nil,
		nil,
		false,
		0,
		0,
		nil,
		[]workflow.Definition{
			{
				Name:        "terminal-release",
				Description: "Git: release workflow.",
				WhenToUse:   "Use when a release needs command checks.",
				Content:     "Run bash, git-status, git_status, and package checks before release.",
			},
			{
				Name:        "portable-plan",
				Description: "Plan a portable change.",
				WhenToUse:   "Use for planning.",
				Content:     "Create a scoped implementation plan.",
			},
		},
	)

	if strings.Contains(promptText, "terminal-release") ||
		strings.Contains(promptText, "Git:") ||
		strings.Contains(promptText, "git-status") ||
		strings.Contains(promptText, "git_status") ||
		strings.Contains(promptText, "Run bash") {
		t.Fatalf("local/no-shell prompt must not advertise terminal workflows:\n%s", promptText)
	}
	if !strings.Contains(promptText, "portable-plan") {
		t.Fatalf("local/no-shell prompt should keep compatible workflows:\n%s", promptText)
	}
}

func TestBuildBaseSystemPromptLocalNoShellDoesNotTeachTerminalPaths(t *testing.T) {
	surface := compiledSurfaceForProviderModel("ollama", "llama-coder")
	promptText := buildBaseSystemPrompt(
		t.TempDir(),
		config.DefaultSystemPrompt(),
		"",
		"ollama",
		"llama-coder",
		surface,
		nil,
		nil,
		false,
		0,
		0,
		nil,
		nil,
	)

	for _, banned := range []string{
		"bash",
		"run_shell",
		"run_test",
		"start_process",
		"command.bash",
		"terminal",
		"shell",
		"git",
		"git status",
		"git diff",
		"git commit",
		"npx vitest",
		"npm test",
		"npm run dev",
	} {
		if strings.Contains(promptText, banned) {
			t.Fatalf("local/no-shell prompt must not teach terminal path %q:\n%s", banned, promptText)
		}
	}
}

// TestBuildBaseSystemPrompt_WorkerExcludesMainOnlyOrchestration locks in the
// split between prompts.System() (base sections shared with workers) and
// prompts.SystemMain() (the Orchestration path-selection map that lives only
// in the main agent's prompt). The Orchestration section lists main-agent
// planning and orchestration paths (update_plan, create_goal,
// start_workflow, spawn_agent, helpme, write_memory, read_memory); if it
// leaked into a worker's system prompt the worker would receive the wrong
// path-selection map. Inception is available to workers through the tool
// surface, but its worker guidance lives in the tool description.
func TestBuildBaseSystemPrompt_WorkerExcludesMainOnlyOrchestration(t *testing.T) {
	surface := compiledSurfaceForProviderModel("openai", "gpt-5")

	mainPrompt := buildBaseSystemPrompt(
		t.TempDir(),
		config.DefaultSystemPrompt(),
		"",
		"openai",
		"gpt-5",
		surface,
		nil, nil, false, 0, 0, nil, nil,
	)
	workerPrompt := buildBaseSystemPrompt(
		t.TempDir(),
		config.WorkerSystemPrompt(),
		"",
		"openai",
		"gpt-5",
		surface,
		nil, nil, false, 0, 0, nil, nil,
	)

	for _, want := range []string{
		"# Orchestration",
		"- helpme:",
		"- inception:",
		"- update_plan:",
		"- create_goal:",
		"- start_workflow:",
		"- spawn_agent:",
	} {
		if !strings.Contains(mainPrompt, want) {
			t.Fatalf("main agent prompt must contain %q; got prompt:\n%s", want, mainPrompt)
		}
	}
	for _, banned := range []string{
		"# Orchestration",
		"- helpme:",
		"- inception:",
		"- update_plan:",
		"- create_goal:",
		"- start_workflow:",
		"- spawn_agent:",
		"- write_memory",
		"- read_memory",
	} {
		if strings.Contains(workerPrompt, banned) {
			t.Fatalf("worker prompt must not contain main-only guidance %q; got prompt:\n%s", banned, workerPrompt)
		}
	}
}

func TestResolveInputWindow_CapsCodexSubscriptionGPT5(t *testing.T) {
	got := ResolveInputWindow("gpt-5.5", config.ProviderConfig{
		Type:  "openai-codex",
		Model: "gpt-5.5",
	})
	if got != codexSubscriptionGPT5InputCap {
		t.Fatalf("ResolveInputWindow = %d, want %d", got, codexSubscriptionGPT5InputCap)
	}
}

func TestResolveWindowsFallBackToAPIModelLimits(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		Model: "fast-alias",
		Models: map[string]config.ProviderModelConfig{
			"fast-alias": {
				ID: "base-model",
			},
			"base-model": {
				Limit: &config.ProviderModelLimitConfig{
					Context: 1_000_000,
					Input:   900_000,
				},
			},
		},
	}

	if got := ResolveContextWindow("fast-alias", provider, 0); got != 1_000_000 {
		t.Fatalf("ResolveContextWindow = %d, want 1000000", got)
	}
	if got := ResolveInputWindow("fast-alias", provider); got != 900_000 {
		t.Fatalf("ResolveInputWindow = %d, want 900000", got)
	}
}

func TestResolveInputWindowDoesNotSynthesizeFromContextWindow(t *testing.T) {
	got := ResolveInputWindow("private-1m-model", config.ProviderConfig{
		Type:          "anthropic",
		Model:         "private-1m-model",
		ContextWindow: 1_000_000,
	})
	if got != 0 {
		t.Fatalf("ResolveInputWindow = %d, want 0 without explicit input limit", got)
	}
}

func TestApplyWorkerToolFilter_HidesRecursiveAgentControls(t *testing.T) {
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex", false)
	kit.SetAgentIdentity("worker-1", string(agentthread.RootPath)+"/worker-1")
	wt, err := agentcontrol.LookupWorkerType(agentcontrol.DefaultSubagentType)
	if err != nil {
		t.Fatalf("agent type: %v", err)
	}

	applyWorkerToolFilter(kit, wt)

	defs := map[string]bool{}
	for _, def := range kit.Definitions() {
		defs[def.Name] = true
	}
	for _, allowed := range []string{"read_file", "apply_patch", "bash", "update_plan", "agent_report"} {
		if !defs[allowed] {
			t.Fatalf("subagent toolkit should keep %s", allowed)
		}
	}
	for _, blocked := range []string{"spawn_agent", "send_message", "followup_task", "await_agents", "close_agent", "list_agents"} {
		if defs[blocked] {
			t.Fatalf("subagent toolkit should hide recursive control tool %s", blocked)
		}
	}
}

func TestApplyWorkerToolFilter_RestrictedWorkerKeepsBashFirstSurface(t *testing.T) {
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex", false)
	kit.SetAgentIdentity("worker-1", string(agentthread.RootPath)+"/worker-1")
	wt, err := agentcontrol.LookupWorkerType("verification")
	if err != nil {
		t.Fatalf("agent type: %v", err)
	}

	applyWorkerToolFilter(kit, wt)

	defs := map[string]bool{}
	for _, def := range kit.Definitions() {
		defs[def.Name] = true
	}
	for _, allowed := range []string{"read_file", "grep", "glob", "bash", "agent_report"} {
		if !defs[allowed] {
			t.Fatalf("verification worker toolkit should keep %s; defs=%v", allowed, defs)
		}
	}
	for _, hidden := range []string{"run_shell", "run_test", "start_process", "git", "apply_patch", "edit_file", "write_file"} {
		if defs[hidden] {
			t.Fatalf("verification worker toolkit should not expose %s; defs=%v", hidden, defs)
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
			Capability:      capability.CapabilitySearchSemantic,
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
	if override.Capability != capability.CapabilitySearchSemantic {
		t.Fatalf("Capability = %q, want %q", override.Capability, capability.CapabilitySearchSemantic)
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
	if policy.Profile != "" {
		t.Fatalf("Profile = %s, want no profile from explicit override config", policy.Profile)
	}
	if policy.ToolActions["run_shell"] != tools.ToolPolicyRequireApproval {
		t.Fatalf("run_shell action = %s, want require_approval", policy.ToolActions["run_shell"])
	}
	if policy.ToolActions["bash"] != tools.ToolPolicyRequireApproval {
		t.Fatalf("bash alias action = %s, want require_approval", policy.ToolActions["bash"])
	}
	if policy.KindActions[tools.ToolKindWeb] != tools.ToolPolicyAllow {
		t.Fatalf("web action = %s, want allow", policy.KindActions[tools.ToolKindWeb])
	}
	if policy.RiskActions[tools.ToolRiskHigh] != tools.ToolPolicyDeny {
		t.Fatalf("high risk action = %s, want deny", policy.RiskActions[tools.ToolRiskHigh])
	}
}

func TestToolPolicyFromConfigAliasesUnifiedCommandTools(t *testing.T) {
	policy := ToolPolicyFromConfig(config.ToolPolicyConfig{
		Tools: map[string]string{
			"bash": "deny",
		},
	})
	for _, name := range []string{"bash", "run_shell", "run_test", "git", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process"} {
		if policy.ToolActions[name] != tools.ToolPolicyDeny {
			t.Fatalf("%s action = %s, want deny", name, policy.ToolActions[name])
		}
	}

	legacy := ToolPolicyFromConfig(config.ToolPolicyConfig{
		Tools: map[string]string{
			"run_shell": "require_approval",
		},
	})
	if legacy.ToolActions["bash"] != tools.ToolPolicyRequireApproval {
		t.Fatalf("legacy run_shell action did not alias to bash: %+v", legacy.ToolActions)
	}
}

func TestToolPolicyFromConfigAndPermissionsDerivesAgentProfile(t *testing.T) {
	policy := ToolPolicyFromConfigAndPermissions(config.ToolPolicyConfig{
		Risks: map[string]string{
			"high": "deny",
		},
	}, config.ResolvedPermissions{Mode: config.PermissionModeAgent})

	if policy.Profile != tools.ToolPolicyProfileAgent {
		t.Fatalf("Profile = %s, want agent", policy.Profile)
	}
	if policy.DefaultAction != tools.ToolPolicyAllow {
		t.Fatalf("DefaultAction = %s, want allow", policy.DefaultAction)
	}
	if policy.RiskActions[tools.ToolRiskHigh] != tools.ToolPolicyDeny {
		t.Fatalf("high risk action = %s, want deny", policy.RiskActions[tools.ToolRiskHigh])
	}
}

func TestToolPolicyFromConfigAndPermissionsKeepsAutoReviewAsReviewerOnlyProfile(t *testing.T) {
	agent := ToolPolicyFromConfigAndPermissions(config.ToolPolicyConfig{}, config.ResolvedPermissions{Mode: config.PermissionModeAgent})
	autoReview := ToolPolicyFromConfigAndPermissions(config.ToolPolicyConfig{}, config.ResolvedPermissions{Mode: config.PermissionModeAutoReview})

	if agent.Profile != tools.ToolPolicyProfileAgent {
		t.Fatalf("agent Profile = %s, want agent", agent.Profile)
	}
	if autoReview.Profile != tools.ToolPolicyProfileAutoReview {
		t.Fatalf("auto review Profile = %s, want auto_review", autoReview.Profile)
	}
	if agent.DefaultAction != autoReview.DefaultAction {
		t.Fatalf("auto_review should not change policy action defaults: agent=%s auto_review=%s", agent.DefaultAction, autoReview.DefaultAction)
	}
}

func TestToolPolicyFromConfigAndPermissionsUsesApprovalPolicyAxis(t *testing.T) {
	fullAccessOnRequest := ToolPolicyFromConfigAndPermissions(config.ToolPolicyConfig{}, config.ResolvedPermissions{
		Mode:           config.PermissionModeFullAccess,
		ApprovalPolicy: config.ApprovalPolicyOnRequest,
	})
	if fullAccessOnRequest.Profile != tools.ToolPolicyProfileFullAccess ||
		fullAccessOnRequest.ApprovalPolicy != tools.ToolApprovalPolicyOnRequest {
		t.Fatalf("full_access with on_request approval policy mapped incorrectly: %+v", fullAccessOnRequest)
	}

	agentNever := ToolPolicyFromConfigAndPermissions(config.ToolPolicyConfig{}, config.ResolvedPermissions{
		Mode:           config.PermissionModeAgent,
		ApprovalPolicy: config.ApprovalPolicyNever,
	})
	if agentNever.Profile != tools.ToolPolicyProfileAgent ||
		agentNever.ApprovalPolicy != tools.ToolApprovalPolicyNever {
		t.Fatalf("agent with never approval policy mapped incorrectly: %+v", agentNever)
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
	if first.Toolkit.ActiveSurface().ProfileName == "" || first.Toolkit.ActiveSurface().ProfileName != rt.Toolkit.ActiveSurface().ProfileName {
		t.Fatalf("thread toolkit should inherit active surface, got %q want %q", first.Toolkit.ActiveSurface().ProfileName, rt.Toolkit.ActiveSurface().ProfileName)
	}
	if first.StreamRunner == rt.StreamRunner || second.StreamRunner == rt.StreamRunner || first.StreamRunner == second.StreamRunner {
		t.Fatal("thread runtimes must not share stream runner instances")
	}
	if len(rt.BaseSystemPromptSections) == 0 {
		t.Fatal("base system prompt should expose section metadata")
	}
	if len(first.StreamRunner.SystemPromptSections) != len(rt.BaseSystemPromptSections) {
		t.Fatalf("thread stream runner lost system prompt sections: got %d want %d", len(first.StreamRunner.SystemPromptSections), len(rt.BaseSystemPromptSections))
	}
	if first.StreamRunner.SystemPromptSections[0].Key != rt.BaseSystemPromptSections[0].Key ||
		first.StreamRunner.SystemPromptSections[0].Hash != rt.BaseSystemPromptSections[0].Hash {
		t.Fatalf("thread stream runner copied wrong system prompt section: got %+v want %+v", first.StreamRunner.SystemPromptSections[0], rt.BaseSystemPromptSections[0])
	}
	if first.StreamRunner.PromptCacheKey != "thread-a" || second.StreamRunner.PromptCacheKey != "thread-b" {
		t.Fatalf("unexpected thread prompt cache keys: first=%q second=%q", first.StreamRunner.PromptCacheKey, second.StreamRunner.PromptCacheKey)
	}
	if first.AgentControl == nil || second.AgentControl == nil || first.AgentControl == second.AgentControl {
		t.Fatal("thread runtimes must have distinct agent control instances")
	}
	if first.AgentControl.SessionID() != "thread-a" || second.AgentControl.SessionID() != "thread-b" {
		t.Fatalf("unexpected agent control sessions: first=%q second=%q", first.AgentControl.SessionID(), second.AgentControl.SessionID())
	}
	if first.GoalRuntime == nil || second.GoalRuntime == nil {
		t.Fatal("thread runtimes must include goal runtime")
	}
	if first.Toolkit.GoalRuntime() != first.GoalRuntime || second.Toolkit.GoalRuntime() != second.GoalRuntime {
		t.Fatal("thread toolkits must be attached to their thread goal runtime")
	}
	if first.GoalRuntime == second.GoalRuntime {
		t.Fatal("thread runtimes must not share goal runtime instances")
	}
	firstGoalPath := statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-a")
	secondGoalPath := statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-b")
	if first.GoalRuntime.Store().Path() != firstGoalPath || second.GoalRuntime.Store().Path() != secondGoalPath {
		t.Fatalf("unexpected goal runtime paths: first=%q second=%q", first.GoalRuntime.Store().Path(), second.GoalRuntime.Store().Path())
	}
}
