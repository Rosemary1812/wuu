package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
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
	// 0 = unlimited; no hard cap.
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

func TestConfig_ModelRoles(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key_env": "OPENAI_API_KEY",
      "model": "gpt-5-codex"
    },
    "anthropic": {
      "type": "anthropic",
      "base_url": "https://api.anthropic.com",
      "api_key_env": "ANTHROPIC_API_KEY",
      "model": "claude-sonnet-4-5"
    }
  },
  "agent": {
    "model_roles": {
      "review": {"provider": "anthropic", "model": "claude-sonnet-4-5"},
      "worker": {"model": "gpt-5-codex", "variant": "high"}
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
	if cfg.Agent.ModelRoles.Review.Provider != "anthropic" || cfg.Agent.ModelRoles.Review.Model != "claude-sonnet-4-5" {
		t.Fatalf("review role not parsed: %+v", cfg.Agent.ModelRoles.Review)
	}
	if cfg.Agent.ModelRoles.Worker.Variant != "high" {
		t.Fatalf("worker variant not parsed: %+v", cfg.Agent.ModelRoles.Worker)
	}
}

func TestConfig_ModelRolesRejectUnknownProvider(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {
				Type:    "openai-compatible",
				BaseURL: "https://example.com/v1",
				Model:   "gpt-5-codex",
			},
		},
		Agent: AgentConfig{
			ModelRoles: ModelRolesConfig{
				Review: ModelRoleConfig{Provider: "missing", Model: "review-model"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `agent.model_roles.review.provider "missing" not found`) {
		t.Fatalf("expected unknown provider validation error, got %v", err)
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

func TestMemoryConfig_DreamIntervalDays(t *testing.T) {
	var cfg MemoryConfig
	if got := cfg.DreamIntervalDaysValue(); got != DefaultDreamIntervalDays {
		t.Fatalf("default dream interval = %d, want %d", got, DefaultDreamIntervalDays)
	}

	disabled := 0
	cfg.DreamIntervalDays = &disabled
	if got := cfg.DreamIntervalDaysValue(); got != 0 {
		t.Fatalf("disabled dream interval = %d, want 0", got)
	}

	custom := 14
	cfg.DreamIntervalDays = &custom
	if got := cfg.DreamIntervalDaysValue(); got != 14 {
		t.Fatalf("custom dream interval = %d, want 14", got)
	}
}

func TestValidateRejectsNegativeDreamInterval(t *testing.T) {
	cfg := Default()
	negative := -1
	cfg.Memory.DreamIntervalDays = &negative
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "dream_interval_days") {
		t.Fatalf("expected negative dream interval error, got %v", err)
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

func TestAgentConfig_ProfileMemoryEnabled(t *testing.T) {
	// Memory is now a single per-user global store. ProfileMemoryEnabled
	// returns true unconditionally so every session — default or named —
	// gets the durable memory attached. The flag is preserved for
	// future config gating.
	if !(AgentConfig{}).ProfileMemoryEnabled() {
		t.Fatal("zero-value AgentConfig should enable global memory")
	}
	if !(AgentConfig{Name: DefaultAgentName}).ProfileMemoryEnabled() {
		t.Fatal("explicit default profile should still enable global memory")
	}
	if !(AgentConfig{Name: "Mia"}).ProfileMemoryEnabled() {
		t.Fatal("named agent profile should enable global memory")
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

func TestDefaultConfigUsesPermissionModeAsPolicySource(t *testing.T) {
	cfg := Default()
	permissions := ResolveAgentPermissions(cfg.Agent)
	if permissions.Mode != PermissionModeAgent ||
		permissions.PermissionProfile != PermissionProfileWorkspaceWrite ||
		permissions.ApprovalPolicy != ApprovalPolicyOnRequest ||
		permissions.ApprovalsReviewer != ApprovalsReviewerUser {
		t.Fatalf("default permissions = %+v", permissions)
	}
}

func TestToolPolicyProfileForPermissionModeDefaultsEmptyMode(t *testing.T) {
	if got := ToolPolicyProfileForPermissionMode(""); got != PermissionModeAgent {
		t.Fatalf("empty permission mode tool policy profile = %q, want agent", got)
	}
}

func TestPermissionPresetsUseCodexStyleAxes(t *testing.T) {
	tests := []struct {
		mode     string
		profile  string
		approval string
		reviewer string
	}{
		{
			mode:     PermissionModeReadOnly,
			profile:  PermissionProfileReadOnly,
			approval: ApprovalPolicyOnRequest,
			reviewer: ApprovalsReviewerUser,
		},
		{
			mode:     PermissionModeAgent,
			profile:  PermissionProfileWorkspaceWrite,
			approval: ApprovalPolicyOnRequest,
			reviewer: ApprovalsReviewerUser,
		},
		{
			mode:     PermissionModeAutoReview,
			profile:  PermissionProfileWorkspaceWrite,
			approval: ApprovalPolicyOnRequest,
			reviewer: ApprovalsReviewerAutoReview,
		},
		{
			mode:     PermissionModeFullAccess,
			profile:  PermissionProfileDangerFullAccess,
			approval: ApprovalPolicyNever,
			reviewer: ApprovalsReviewerUser,
		},
	}

	for _, tt := range tests {
		got, ok := PermissionPresetForMode(tt.mode)
		if !ok {
			t.Fatalf("%s preset missing", tt.mode)
		}
		if got.Mode != tt.mode ||
			got.PermissionProfile != tt.profile ||
			got.ApprovalPolicy != tt.approval ||
			got.ApprovalsReviewer != tt.reviewer {
			t.Fatalf("%s preset = %+v", tt.mode, got)
		}
	}
}

func TestApplyPermissionModePresetRewritesAxesAndClearsToolPolicy(t *testing.T) {
	agent := AgentConfig{
		PermissionMode:    PermissionModeFullAccess,
		PermissionProfile: PermissionProfileDangerFullAccess,
		ApprovalPolicy:    ApprovalPolicyNever,
		ApprovalsReviewer: ApprovalsReviewerUser,
		ToolPolicy: ToolPolicyConfig{
			Tools: map[string]string{"bash": "allow"},
			Risks: map[string]string{"high": "deny"},
		},
	}

	permissions, err := ApplyPermissionModePreset(&agent, PermissionModeReadOnly)
	if err != nil {
		t.Fatalf("ApplyPermissionModePreset: %v", err)
	}
	if permissions.Mode != PermissionModeReadOnly ||
		agent.PermissionMode != PermissionModeReadOnly ||
		agent.PermissionProfile != PermissionProfileReadOnly ||
		agent.ApprovalPolicy != ApprovalPolicyOnRequest ||
		agent.ApprovalsReviewer != ApprovalsReviewerUser {
		t.Fatalf("read_only preset not applied: permissions=%+v agent=%+v", permissions, agent)
	}
	if agent.ToolPolicy.DefaultAction != "" || len(agent.ToolPolicy.Tools) != 0 || len(agent.ToolPolicy.Risks) != 0 {
		t.Fatalf("permission preset should clear stale tool policy: %+v", agent.ToolPolicy)
	}
}

func TestResolvePermissionModePresetRejectsInvalidMode(t *testing.T) {
	if _, err := ResolvePermissionModePreset("not-a-mode"); err == nil {
		t.Fatal("expected invalid permission mode error")
	}
}

func TestAutoReviewKeepsAgentBoundaryAndOnlyChangesReviewer(t *testing.T) {
	agent, ok := PermissionPresetForMode(PermissionModeAgent)
	if !ok {
		t.Fatal("agent preset missing")
	}
	autoReview, ok := PermissionPresetForMode(PermissionModeAutoReview)
	if !ok {
		t.Fatal("auto_review preset missing")
	}
	if autoReview.PermissionProfile != agent.PermissionProfile ||
		autoReview.ApprovalPolicy != agent.ApprovalPolicy {
		t.Fatalf("auto_review should share agent boundary and approval policy: agent=%+v auto_review=%+v", agent, autoReview)
	}
	if autoReview.ApprovalsReviewer != ApprovalsReviewerAutoReview {
		t.Fatalf("auto_review reviewer = %q, want auto_review", autoReview.ApprovalsReviewer)
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
          "read_only": true,
          "capability": "search.semantic"
        },
        "write": {
          "read_only": false,
          "concurrency_safe": false,
          "capability": "file.edit"
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
	if search.Capability != capability.CapabilitySearchSemantic {
		t.Fatalf("search.capability = %q, want %q", search.Capability, capability.CapabilitySearchSemantic)
	}
	write := cfg.MCPServers["docs"].ToolOverrides["write"]
	if write.ReadOnly == nil || *write.ReadOnly != false {
		t.Fatalf("write.read_only = %v, want false", write.ReadOnly)
	}
	if write.ConcurrencySafe == nil || *write.ConcurrencySafe != false {
		t.Fatalf("write.concurrency_safe = %v, want false", write.ConcurrencySafe)
	}
	if write.Capability != capability.CapabilityFileEdit {
		t.Fatalf("write.capability = %q, want %q", write.Capability, capability.CapabilityFileEdit)
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
        "medium": "auto_classify",
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
	if cfg.Agent.ToolPolicy.Risks["medium"] != "auto_classify" {
		t.Fatalf("medium risk action = %q, want auto_classify", cfg.Agent.ToolPolicy.Risks["medium"])
	}
	if cfg.Agent.ToolPolicy.Risks["high"] != "deny" {
		t.Fatalf("high risk action = %q, want deny", cfg.Agent.ToolPolicy.Risks["high"])
	}
}

func TestConfig_ToolPolicyRejectsProfileField(t *testing.T) {
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
      "profile": "reckless"
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := LoadFrom(workdir, "")
	if err == nil || !strings.Contains(err.Error(), `unknown field "profile"`) {
		t.Fatalf("expected tool_policy.profile to be rejected, got %v", err)
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
	if !strings.Contains(prompt, "local-first") || !strings.Contains(prompt, "coding agent") {
		t.Fatalf("default system prompt must reflect local-first coding agent positioning: %q", prompt)
	}
	for _, want := range []string{
		"Default tone",
		"tool rules > instruction files > your general defaults",
		"memory as a snapshot",
		"root cause",
		"focused on the task",
		"fully addresses the task",
		"Ambition vs. precision",
		"surgical",
		"harness",
		"approval flow is the harness's responsibility",
		"Use dedicated tools",
		"active tool surface exposes",
		"Do not push",
		"Do not commit",
		"Do not invent",
		"Do not add copyright",
		`"what" comments`,
		`"why" comments`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt missing calibrated main-agent guidance %q: %q", want, prompt)
		}
	}
	if strings.Contains(prompt, "read-oriented") {
		t.Fatalf("default system prompt still describes main agent as read-oriented: %q", prompt)
	}
	if strings.Contains(prompt, "CLI coding assistant") {
		t.Fatalf("default system prompt still describes main agent as CLI-first: %q", prompt)
	}
}

func TestDefaultSystemPrompt_ComposeDecisionPaths(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"Choose the lightest path",
		"Direct:",
		"Skill:",
		"update_plan",
		"create_goal",
		"start_workflow",
		"spawn_agent",
		"write_memory",
		"read_memory",
		"explicitly requested",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must define orchestration path guidance %q: %q", want, prompt)
		}
	}
	for _, bad := range []string{
		"Compose",
		"one single always-on Compose mode",
		"There are no separate build or plan modes",
		"Do not make the user choose between build, plan, and compose",
	} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("default system prompt should avoid obsolete mode wording %q: %q", bad, prompt)
		}
	}
}

func TestDefaultSystemPrompt_GoalWorkflowAgentClosure(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"Before claiming durable work is complete",
		"durable state such as a goal, workflow, or delegated worker result",
		"A completed child task is evidence for the broader objective",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt missing goal/workflow closure guidance %q: %q", want, prompt)
		}
	}
	for _, bad := range []string{
		"record_workflow_team",
		"spawn each worker with the workflow goal_id and goal_dir",
		"phase(...)",
		"spawnBatch([...])",
	} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("default system prompt should keep workflow execution details out of the core prompt %q: %q", bad, prompt)
		}
	}
}

func TestDefaultSystemPrompt_ToolDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "in parallel") {
		t.Fatalf("default system prompt must encourage parallel tool calls: %q", prompt)
	}
	if !strings.Contains(prompt, "apply_patch") {
		t.Fatalf("default system prompt must teach model-aware edit tool use: %q", prompt)
	}
	for _, want := range []string{
		"Use command execution only when the active tool surface exposes that capability",
		"command execution and command-based verification are unavailable",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must stay capability-neutral for command use %q: %q", want, prompt)
		}
	}
	for _, want := range []string{
		"Verification is your responsibility",
		"approval flow is the harness's responsibility",
		"profile without command execution",
		"Report failed or skipped validation plainly",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include validation guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_UpdatePlanDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"multi-step work",
		"visible checklist",
		"update_plan",
		"exactly one item in_progress",
		"constraints that need a visible checklist",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include update_plan guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_CommunicationStyle(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "Keep progress updates short") {
		t.Fatalf("default system prompt must teach concise progress updates: %q", prompt)
	}
	if !strings.Contains(prompt, "Default to concise") || !strings.Contains(prompt, "user-facing impact first") {
		t.Fatalf("default system prompt must teach concise final answers: %q", prompt)
	}
	if !strings.Contains(prompt, "10 lines or fewer") || !strings.Contains(prompt, "at most 6 bullets") || !strings.Contains(prompt, "1-2 bullets per file") {
		t.Fatalf("default system prompt must include verbosity caps: %q", prompt)
	}
}

func TestDefaultSystemPrompt_FinalAnswerReferences(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"path:line",
		"do not use file URI formats",
		"line ranges",
		"If validation was not run or was incomplete",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include final-answer reference guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_AgentDelegation(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "spawn_agent") {
		t.Fatalf("default system prompt must mention spawn_agent: %q", prompt)
	}
	if strings.Contains(prompt, "fork_agent") {
		t.Fatalf("default system prompt must not mention removed fork_agent tool: %q", prompt)
	}
	if strings.Contains(prompt, "fork_turns") || strings.Contains(prompt, "isolation='inplace'") {
		t.Fatalf("default system prompt must not mention old sub-agent fields: %q", prompt)
	}
	for _, want := range []string{
		"independent investigation",
		"parallel implementation slices",
		"risky verification",
		"separate context",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include sub-agent decision guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_CommentDiscipline(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		`"what" comments`,
		`"why" comments`,
		`"do this later"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include comment guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_NoBannedWords(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, banned := range defaultSystemPromptBannedWords {
		if strings.Contains(prompt, banned) {
			t.Fatalf("default system prompt must not teach unavailable command path %q:\n%s", banned, prompt)
		}
	}
}

var defaultSystemPromptBannedWords = []string{
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

func TestConfig_CompactThresholdPct(t *testing.T) {
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
    "compact_threshold_pct": 0.5
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.CompactThresholdPct != 0.5 {
		t.Fatalf("CompactThresholdPct = %v, want 0.5", cfg.Agent.CompactThresholdPct)
	}
}

func TestUpdateAdvancedRuntimePersistsAgentAndProviderSettings(t *testing.T) {
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
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	maxSteps := 12
	maxContext := 256000
	temperature := 0.4
	compactPct := 0.5
	disableAutoCompact := true
	providerContext := 512000
	if err := UpdateAdvancedRuntime(configPath, "main", AdvancedRuntimeUpdate{
		MaxSteps:              &maxSteps,
		MaxContextTokens:      &maxContext,
		Temperature:           &temperature,
		CompactThresholdPct:   &compactPct,
		DisableAutoCompact:    &disableAutoCompact,
		ProviderContextWindow: &providerContext,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFrom(workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.MaxSteps != maxSteps ||
		cfg.Agent.MaxContextTokens != maxContext ||
		cfg.Agent.Temperature != temperature ||
		cfg.Agent.CompactThresholdPct != compactPct ||
		!cfg.Agent.DisableAutoCompact {
		t.Fatalf("advanced agent settings not persisted: %+v", cfg.Agent)
	}
	if cfg.Providers["main"].ContextWindow != providerContext {
		t.Fatalf("provider context_window = %d, want %d", cfg.Providers["main"].ContextWindow, providerContext)
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
	if err := UpdateProviderRuntime(path, "next", "custom-model", &baseURL, &apiKey, nil, nil, nil); err != nil {
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

func TestUpdateProviderRuntimePersistsPermissionModePreset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "agent": {
    "tool_policy": {
      "tools": {
        "run_shell": "allow"
      }
    }
  },
  "default_provider": "old",
  "providers": {
    "old": {
      "type": "openai-compatible",
      "base_url": "https://old.example.com",
      "model": "old-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	mode := PermissionModeAutoReview
	if err := UpdateProviderRuntime(path, "old", "old-model", nil, nil, nil, nil, &mode); err != nil {
		t.Fatalf("UpdateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadFrom(dir, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Agent.ToolPolicy.DefaultAction != "" || len(cfg.Agent.ToolPolicy.Tools) != 0 || len(cfg.Agent.ToolPolicy.Risks) != 0 {
		t.Fatalf("permission mode should clear old tool policy config: %+v", cfg.Agent.ToolPolicy)
	}
	permissions := ResolveAgentPermissions(cfg.Agent)
	if permissions.Mode != PermissionModeAutoReview ||
		permissions.PermissionProfile != PermissionProfileWorkspaceWrite ||
		permissions.ApprovalPolicy != ApprovalPolicyOnRequest ||
		permissions.ApprovalsReviewer != ApprovalsReviewerAutoReview {
		t.Fatalf("permission mode not persisted: %+v", permissions)
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
	if err := CreateProviderRuntime(path, "custom-1", "custom-model", &baseURL, &apiKey, nil, nil, nil); err != nil {
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
