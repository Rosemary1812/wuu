package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
)

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
	for _, blocked := range []string{"ask_user"} {
		if defs[blocked] {
			t.Fatalf("worker toolkit should hide %s", blocked)
		}
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

	first, err := rt.NewThreadRuntime("thread-a", nil)
	if err != nil {
		t.Fatalf("NewThreadRuntime first: %v", err)
	}
	second, err := rt.NewThreadRuntime("thread-b", nil)
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
