package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFrom_Priority(t *testing.T) {
	workdir := t.TempDir()
	home := t.TempDir()

	homeConfig := filepath.Join(home, ".config", "wuu", "config.json")
	if err := os.MkdirAll(filepath.Dir(homeConfig), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	homeJSON := `{
  "default_provider": "home",
  "providers": {
    "home": {
      "type": "openai-compatible",
      "base_url": "https://home.example/v1",
      "api_key_env": "HOME_KEY",
      "model": "home-model"
    }
  },
  "agent": {
    "max_steps": 4,
    "temperature": 0.1,
    "system_prompt": "home"
  }
}`
	if err := os.WriteFile(homeConfig, []byte(homeJSON), 0o644); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	localPath := filepath.Join(workdir, ".wuu.json")
	localJSON := `{
  "default_provider": "local",
  "providers": {
    "local": {
      "type": "openai-compatible",
      "base_url": "https://local.example/v1",
      "api_key_env": "LOCAL_KEY",
      "model": "local-model"
    }
  },
  "agent": {
    "max_steps": 3,
    "temperature": 0.3,
    "system_prompt": "local"
  }
}`
	if err := os.WriteFile(localPath, []byte(localJSON), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	cfg, path, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if path != localPath {
		t.Fatalf("expected local path %q, got %q", localPath, path)
	}
	if cfg.DefaultProvider != "local" {
		t.Fatalf("expected local default provider, got %q", cfg.DefaultProvider)
	}
}

func TestLoadFrom_Defaults(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key_env": "OPENAI_API_KEY",
      "model": "gpt-4.1"
    }
  },
  "agent": {}
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	// 0 = unlimited; aligned with Claude Code's default (no hard cap).
	if cfg.Agent.MaxSteps != 0 {
		t.Fatalf("expected default max_steps 0 (unlimited), got %d", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.MaxContextTokens != 0 {
		t.Fatalf("expected default max_context_tokens 0 (auto), got %d", cfg.Agent.MaxContextTokens)
	}
	if cfg.Agent.SystemPrompt != "" {
		t.Fatalf("expected config system_prompt to remain user-owned, got %q", cfg.Agent.SystemPrompt)
	}
	if cfg.Agent.ProfileName() != DefaultAgentName {
		t.Fatalf("expected default agent name %q, got %q", DefaultAgentName, cfg.Agent.ProfileName())
	}
	if DefaultSystemPrompt() == "" {
		t.Fatal("expected built-in default system prompt")
	}
}

func TestMemoryConfig_ProfileMemoryNudgeInterval(t *testing.T) {
	var cfg MemoryConfig
	if got := cfg.ProfileMemoryNudgeInterval(); got != 10 {
		t.Fatalf("default nudge interval = %d, want 10", got)
	}

	disabled := 0
	cfg.NudgeInterval = &disabled
	if got := cfg.ProfileMemoryNudgeInterval(); got != 0 {
		t.Fatalf("disabled nudge interval = %d, want 0", got)
	}

	custom := 3
	cfg.NudgeInterval = &custom
	if got := cfg.ProfileMemoryNudgeInterval(); got != 3 {
		t.Fatalf("custom nudge interval = %d, want 3", got)
	}
}

func TestMemoryConfig_ProfileMemoryCharLimits(t *testing.T) {
	var cfg MemoryConfig
	if got := cfg.ProfileMemoryCharLimit(); got != DefaultMemoryCharLimit {
		t.Fatalf("default memory char limit = %d, want %d", got, DefaultMemoryCharLimit)
	}
	if got := cfg.ProfileUserCharLimit(); got != DefaultUserMemoryCharLimit {
		t.Fatalf("default user char limit = %d, want %d", got, DefaultUserMemoryCharLimit)
	}

	cfg.MemoryCharLimit = 12
	cfg.UserCharLimit = 8
	if got := cfg.ProfileMemoryCharLimit(); got != 12 {
		t.Fatalf("custom memory char limit = %d, want 12", got)
	}
	if got := cfg.ProfileUserCharLimit(); got != 8 {
		t.Fatalf("custom user char limit = %d, want 8", got)
	}
}

func TestLoadFrom_AgentName(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key_env": "OPENAI_API_KEY",
      "model": "gpt-4.1"
    }
  },
  "agent": {
    "name": "Mia"
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Agent.ProfileName() != "Mia" {
		t.Fatalf("ProfileName() = %q, want Mia", cfg.Agent.ProfileName())
	}
}

func TestTemplateJSONDoesNotSerializeBuiltInSystemPrompt(t *testing.T) {
	cfg := Default()
	if cfg.Agent.SystemPrompt != "" {
		t.Fatalf("default config should not carry built-in prompt, got %q", cfg.Agent.SystemPrompt)
	}
	tpl, err := TemplateJSON()
	if err != nil {
		t.Fatalf("TemplateJSON: %v", err)
	}
	if strings.Contains(tpl, "You are wuu") {
		t.Fatalf("template should not serialize built-in system prompt:\n%s", tpl)
	}
}

func TestAgentConfig_UserSystemPromptAppendsLegacyAndPreferredFields(t *testing.T) {
	cfg := AgentConfig{
		SystemPrompt:       "legacy instructions",
		AppendSystemPrompt: "preferred instructions",
	}
	got := cfg.UserSystemPrompt()
	if !strings.Contains(got, "legacy instructions") || !strings.Contains(got, "preferred instructions") {
		t.Fatalf("expected both user prompt fields, got %q", got)
	}
	if strings.Index(got, "legacy instructions") > strings.Index(got, "preferred instructions") {
		t.Fatalf("legacy field should keep stable order before append field, got %q", got)
	}
}

func TestConfig_ProviderWireAPI(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "wire_api": "responses",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Providers["main"].WireAPI != "responses" {
		t.Fatalf("expected wire_api responses, got %q", cfg.Providers["main"].WireAPI)
	}
}

func TestConfig_MCPToolOverrides(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test"
  },
  "mcp_servers": {
    "docs": {
      "command": "docs-mcp",
      "tool_overrides": {
        "search": {
          "read_only": true
        },
        "write": {
          "read_only": false,
          "concurrency_safe": false
        }
      }
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	search := cfg.MCPServers["docs"].ToolOverrides["search"]
	if search.ReadOnly == nil || *search.ReadOnly != true {
		t.Fatalf("search.read_only = %v, want true", search.ReadOnly)
	}
	if search.ConcurrencySafe != nil {
		t.Fatalf("search.concurrency_safe = %v, want nil", search.ConcurrencySafe)
	}
	write := cfg.MCPServers["docs"].ToolOverrides["write"]
	if write.ReadOnly == nil || *write.ReadOnly != false {
		t.Fatalf("write.read_only = %v, want false", write.ReadOnly)
	}
	if write.ConcurrencySafe == nil || *write.ConcurrencySafe != false {
		t.Fatalf("write.concurrency_safe = %v, want false", write.ConcurrencySafe)
	}
}

func TestConfig_ToolPolicy(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test",
    "tool_policy": {
      "default_action": "allow",
      "tools": {
        "run_shell": "require_approval"
      },
      "kinds": {
        "web": "allow"
      },
      "risks": {
        "high": "deny"
      }
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Agent.ToolPolicy.DefaultAction != "allow" {
		t.Fatalf("default_action = %q, want allow", cfg.Agent.ToolPolicy.DefaultAction)
	}
	if cfg.Agent.ToolPolicy.Tools["run_shell"] != "require_approval" {
		t.Fatalf("run_shell action = %q, want require_approval", cfg.Agent.ToolPolicy.Tools["run_shell"])
	}
	if cfg.Agent.ToolPolicy.Kinds["web"] != "allow" {
		t.Fatalf("web action = %q, want allow", cfg.Agent.ToolPolicy.Kinds["web"])
	}
	if cfg.Agent.ToolPolicy.Risks["high"] != "deny" {
		t.Fatalf("high risk action = %q, want deny", cfg.Agent.ToolPolicy.Risks["high"])
	}
}

func TestConfig_ToolPolicyRejectsInvalidAction(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test",
    "tool_policy": {
      "risks": {
        "high": "maybe"
      }
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := LoadFrom(workdir, "")
	if err == nil || !strings.Contains(err.Error(), "agent.tool_policy.risks.high") {
		t.Fatalf("expected invalid policy action error, got %v", err)
	}
}

func TestConfig_CodexSubscriptionAllowsDefaultBaseURL(t *testing.T) {
	workdir := t.TempDir()
	home := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-codex",
      "wire_api": "responses",
      "model": "gpt-5-codex"
    }
  },
  "agent": {
    "max_steps": 0,
    "temperature": 0.2,
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Providers["main"].Type != "openai-codex" {
		t.Fatalf("provider type = %q", cfg.Providers["main"].Type)
	}
}

func TestConfig_CodexSubscriptionRejectsChatWireAPI(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-codex",
      "wire_api": "chat",
      "model": "gpt-5-codex"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := LoadFrom(workdir, "")
	if err == nil {
		t.Fatal("expected codex wire_api validation error")
	}
	if !strings.Contains(err.Error(), "wire_api") || !strings.Contains(err.Error(), "responses") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfig_RejectsUnknownWireAPI(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "wire_api": "legacy",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := LoadFrom(workdir, "")
	if err == nil {
		t.Fatal("expected unknown wire_api validation error")
	}
	if !strings.Contains(err.Error(), "wire_api") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultSystemPrompt_ToolUsingMainAgent(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "wuu") {
		t.Fatalf("default system prompt must identify the agent: %q", prompt)
	}
	if !strings.Contains(prompt, "GUI-first") || !strings.Contains(prompt, "local coding agent") {
		t.Fatalf("default system prompt must reflect GUI-first local agent positioning: %q", prompt)
	}
	if !strings.Contains(prompt, "make real changes") {
		t.Fatalf("default system prompt must encourage tool use: %q", prompt)
	}
	if !strings.Contains(prompt, "minimal changes") {
		t.Fatalf("default system prompt must teach minimal changes: %q", prompt)
	}
	if strings.Contains(prompt, "read-oriented") {
		t.Fatalf("default system prompt still describes main agent as read-oriented: %q", prompt)
	}
	if strings.Contains(prompt, "CLI coding assistant") {
		t.Fatalf("default system prompt still describes main agent as CLI-first: %q", prompt)
	}
}

func TestDefaultSystemPrompt_ToolDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "in parallel") {
		t.Fatalf("default system prompt must encourage parallel tool calls: %q", prompt)
	}
	if !strings.Contains(prompt, "apply_patch") || !strings.Contains(prompt, "edit_file") || !strings.Contains(prompt, "write_file") {
		t.Fatalf("default system prompt must teach model-aware edit tool use: %q", prompt)
	}
	if !strings.Contains(prompt, "non-interactive") {
		t.Fatalf("default system prompt must teach non-interactive shell: %q", prompt)
	}
	if !strings.Contains(prompt, "git commit -e") {
		t.Fatalf("default system prompt must forbid interactive git: %q", prompt)
	}
}

func TestDefaultSystemPrompt_UpdatePlanDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"multi-step work",
		"visible checklist",
		"update_plan",
		"exactly one item in_progress",
		"mark every item completed",
		"trivial one-step tasks",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include update_plan guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_CommunicationStyle(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "Before your first tool call") || !strings.Contains(prompt, "short sentence") {
		t.Fatalf("default system prompt must teach proactive communication: %q", prompt)
	}
	if !strings.Contains(prompt, "While working") || !strings.Contains(prompt, "meaningful moments") {
		t.Fatalf("default system prompt must teach progress updates: %q", prompt)
	}
	if !strings.Contains(prompt, "No fluff") {
		t.Fatalf("default system prompt must forbid fluff: %q", prompt)
	}
}

func TestDefaultSystemPrompt_AgentDelegation(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "spawn sub-agents") {
		t.Fatalf("default system prompt must mention sub-agent spawning: %q", prompt)
	}
	if !strings.Contains(prompt, "spawn_agent") {
		t.Fatalf("default system prompt must mention spawn_agent: %q", prompt)
	}
	if !strings.Contains(prompt, "fork_turns") {
		t.Fatalf("default system prompt must mention fork_turns: %q", prompt)
	}
	if strings.Contains(prompt, "fork_agent") {
		t.Fatalf("default system prompt must not mention removed fork_agent tool: %q", prompt)
	}
	for _, want := range []string{
		"delegation materially improves",
		"Keep work local",
		"Context:",
		"Workspace:",
		"Waiting:",
		"fork_turns='all'",
		"fork_turns='none'",
		"isolation='inplace'",
		"isolation='worktree'",
		"acceptance criteria",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include sub-agent decision guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_CommentDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"three comment buckets",
		"Do not write 'what' comments",
		"Write 'why' comments only",
		"future agents will read",
		"'I will do it later'",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include comment guidance %q: %q", want, prompt)
		}
	}
}

func TestConfig_DisableAutoCompact(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://x",
      "api_key": "k",
      "model": "test"
    }
  },
  "agent": {
    "disable_auto_compact": true
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.DisableAutoCompact {
		t.Fatal("expected DisableAutoCompact=true")
	}
}

func TestConfig_DisableAutoCompactDefaultsFalse(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://x",
      "api_key": "k",
      "model": "test"
    }
  },
  "agent": {}
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.DisableAutoCompact {
		t.Fatal("expected DisableAutoCompact to default false")
	}
}

func TestConfig_ExperimentalCoordinatorMode(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://x",
      "api_key": "k",
      "model": "test"
    }
  },
  "agent": {
    "experimental_coordinator_mode": true
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.ExperimentalCoordinatorMode {
		t.Fatal("expected ExperimentalCoordinatorMode=true")
	}
}

func TestConfig_CatwalkAutoupdate(t *testing.T) {
	workdir := t.TempDir()
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {"type": "openai-compatible", "base_url": "https://x", "api_key": "k", "model": "test"}
  },
  "agent": {"catwalk_autoupdate": true}
}`
	if err := os.WriteFile(filepath.Join(workdir, ".wuu.json"), []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.CatwalkAutoupdate {
		t.Fatal("expected CatwalkAutoupdate=true")
	}
}

func TestConfig_HooksConfigParsing(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key": "sk-test",
      "model": "gpt-4"
    }
  },
  "agent": {
    "system_prompt": "test"
  },
  "hooks": {
    "PreToolUse": [
      {"matcher": "run_shell", "command": "check.sh", "timeout": 10}
    ],
    "SessionStart": [
      {"command": "setup.sh"}
    ]
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("expected 2 hook events, got %d", len(cfg.Hooks))
	}
	pre, ok := cfg.Hooks["PreToolUse"]
	if !ok || len(pre) != 1 {
		t.Fatal("expected 1 PreToolUse hook")
	}
	if pre[0].Matcher != "run_shell" {
		t.Fatalf("expected matcher run_shell, got %s", pre[0].Matcher)
	}
	if pre[0].Timeout != 10 {
		t.Fatalf("expected timeout 10, got %d", pre[0].Timeout)
	}
	start, ok := cfg.Hooks["SessionStart"]
	if !ok || len(start) != 1 {
		t.Fatal("expected 1 SessionStart hook")
	}
	if start[0].Command != "setup.sh" {
		t.Fatalf("expected command setup.sh, got %s", start[0].Command)
	}
}

func TestConfig_HooksOmittedWhenEmpty(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key": "sk-test",
      "model": "gpt-4"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hooks != nil && len(cfg.Hooks) != 0 {
		t.Fatalf("expected nil or empty hooks, got %v", cfg.Hooks)
	}
}

func TestLoadFrom_NotFound(t *testing.T) {
	_, _, err := LoadFrom(t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected error when config is missing")
	}
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "wuu init") {
		t.Fatalf("expected init hint, got %v", err)
	}
}

// A present-but-broken config must NOT look like ErrConfigNotFound,
// otherwise callers that recover missing config could silently overwrite
// the user's existing .wuu.json.
func TestLoadFrom_BrokenConfigIsNotNotFound(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, ".wuu.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := LoadFrom(workdir, "")
	if err == nil {
		t.Fatal("expected error for malformed config")
	}
	if errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("malformed config wrongly classified as not-found: %v", err)
	}
}

func TestLoadFrom_InvalidConfigIsNotNotFound(t *testing.T) {
	workdir := t.TempDir()
	// Valid JSON, fails Validate (no providers).
	if err := os.WriteFile(filepath.Join(workdir, ".wuu.json"), []byte(`{"default_provider":"x"}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := LoadFrom(workdir, "")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("invalid config wrongly classified as not-found: %v", err)
	}
}

func TestUpdateProviderModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "myp",
  "providers": {
    "myp": {
      "type": "anthropic",
      "base_url": "https://example.com",
      "model": "old-model"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateProviderModel(path, "myp", "new-model"); err != nil {
		t.Fatalf("UpdateProviderModel: %v", err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	p, _, _ := cfg.ResolveProvider("myp")
	if p.Model != "new-model" {
		t.Fatalf("expected new-model, got %s", p.Model)
	}
}

func TestLoadFromAcceptsOpenCodeModelMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	data := `{
  "default_provider": "google",
  "providers": {
    "google": {
      "type": "openai-compatible",
      "base_url": "https://generativelanguage.googleapis.com/v1beta",
      "npm": "@ai-sdk/google",
      "model": "gemini-3-flash",
      "models": {
        "gemini-3-flash": {
          "id": "gemini-3-flash",
          "name": "Gemini 3 Flash",
          "release_date": "2026-01-01",
          "reasoning": true,
          "provider": {
            "npm": "@ai-sdk/google"
          },
          "limit": {
            "context": 1048576,
            "output": 65536
          }
        }
      }
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	model := cfg.Providers["google"].Models["gemini-3-flash"]
	if model.Provider == nil || model.Provider.NPM != "@ai-sdk/google" {
		t.Fatalf("provider metadata = %+v", model.Provider)
	}
	if model.Limit == nil || model.Limit.Output != 65536 {
		t.Fatalf("limit metadata = %+v", model.Limit)
	}
	if model.Reasoning == nil || !*model.Reasoning {
		t.Fatalf("reasoning metadata = %+v", model.Reasoning)
	}
}

func TestUpdateProviderModel_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	os.WriteFile(path, []byte(`{
  "default_provider": "a",
  "providers": {"a": {"type": "x", "base_url": "http://x", "model": "m"}},
  "agent": {"system_prompt": "t"}
}`), 0o644)

	if err := UpdateProviderModel(path, "nonexistent", "m"); err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestUpdateProviderSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "old",
  "providers": {
    "old": {
      "type": "openai-compatible",
      "base_url": "https://old.example.com",
      "model": "old-model"
    },
    "next": {
      "type": "openai-compatible",
      "base_url": "https://next.example.com",
      "model": "next-model"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateProviderSelection(path, "next", "chosen-model"); err != nil {
		t.Fatalf("UpdateProviderSelection: %v", err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "next" {
		t.Fatalf("expected default provider next, got %q", cfg.DefaultProvider)
	}
	p, _, _ := cfg.ResolveProvider("next")
	if p.Model != "chosen-model" {
		t.Fatalf("expected chosen-model, got %s", p.Model)
	}
	old, _, _ := cfg.ResolveProvider("old")
	if old.Model != "old-model" {
		t.Fatalf("old provider model changed: %s", old.Model)
	}
}

func TestUpdateProviderRuntimePersistsConnectionFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "old",
  "providers": {
    "old": {
      "type": "openai-compatible",
      "base_url": "https://old.example.com",
      "api_key_env": "OLD_KEY",
      "model": "old-model"
    },
    "next": {
      "type": "openai-compatible",
      "base_url": "https://next.example.com",
      "model": "next-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	baseURL := "https://custom.example.com/v1"
	apiKey := "sk-custom"
	if err := UpdateProviderRuntime(path, "next", "custom-model", &baseURL, &apiKey, nil, nil); err != nil {
		t.Fatalf("UpdateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "next" {
		t.Fatalf("expected default provider next, got %q", cfg.DefaultProvider)
	}
	next, _, _ := cfg.ResolveProvider("next")
	if next.Model != "custom-model" || next.BaseURL != baseURL || next.APIKey != apiKey {
		t.Fatalf("provider runtime fields not persisted: %+v", next)
	}
	if next.APIKeyEnv != "" {
		t.Fatalf("expected explicit api_key to clear api_key_env, got %q", next.APIKeyEnv)
	}
	old, _, _ := cfg.ResolveProvider("old")
	if old.Model != "old-model" || old.BaseURL != "https://old.example.com" {
		t.Fatalf("old provider changed: %+v", old)
	}
}

func TestCreateProviderRuntimePersistsNewProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "old",
  "providers": {
    "old": {
      "type": "openai-compatible",
      "base_url": "https://old.example.com",
      "api_key": "old-key",
      "model": "old-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	baseURL := "https://custom.example.com/v1"
	apiKey := "sk-custom"
	if err := CreateProviderRuntime(path, "custom-1", "custom-model", &baseURL, &apiKey, nil, nil); err != nil {
		t.Fatalf("CreateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "custom-1" {
		t.Fatalf("expected default provider custom-1, got %q", cfg.DefaultProvider)
	}
	custom, _, _ := cfg.ResolveProvider("custom-1")
	if custom.Type != "openai-compatible" || custom.Model != "custom-model" || custom.BaseURL != baseURL || custom.APIKey != apiKey {
		t.Fatalf("new provider not persisted: %+v", custom)
	}
	old, _, _ := cfg.ResolveProvider("old")
	if old.Model != "old-model" || old.BaseURL != "https://old.example.com" {
		t.Fatalf("old provider changed: %+v", old)
	}
}
