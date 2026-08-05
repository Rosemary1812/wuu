package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/extensions"
)

func writeSelfReferentialSymlink(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	if err := os.Symlink(filepath.Base(path), path); err != nil {
		t.Skipf("self-referential symlinks are unavailable: %v", err)
	}
}

func TestLoadFrom_UsesUserProviderAndProjectAgentSettings(t *testing.T) {
	t.Setenv("WUU_HOME", "")
	workdir := t.TempDir()
	home := t.TempDir()

	homeConfig := filepath.Join(home, ".wuu", "config.json")
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
	if path != homeConfig {
		t.Fatalf("expected user path %q, got %q", homeConfig, path)
	}
	if cfg.DefaultProvider != "home" {
		t.Fatalf("expected user default provider, got %q", cfg.DefaultProvider)
	}
	if _, ok := cfg.Providers["local"]; ok {
		t.Fatalf("project provider must not be introduced: %+v", cfg.Providers)
	}
	if cfg.Agent.MaxSteps != 3 || cfg.Agent.SystemPrompt != "local" {
		t.Fatalf("project agent settings were not applied: %+v", cfg.Agent)
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

	cfg, _, err := LoadProjectConfig(workdir)
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
	if cfg.Agent.MaxParallel != DefaultAgentMaxParallel {
		t.Fatalf("expected default max_parallel %d, got %d", DefaultAgentMaxParallel, cfg.Agent.MaxParallel)
	}
	if cfg.Agent.UltraMode {
		t.Fatal("expected ultra_mode to be disabled by default")
	}
	if cfg.Agent.SystemPrompt != "" {
		t.Fatalf("expected config system_prompt to remain user-owned, got %q", cfg.Agent.SystemPrompt)
	}
	if cfg.Agent.ToolLoadingPreference() != ToolLoadingAuto {
		t.Fatalf("expected default tool_loading auto, got %q", cfg.Agent.ToolLoadingPreference())
	}
	if cfg.Agent.ExperimentalDeferredToolBundles {
		t.Fatal("expected native deferred bundle activation to be experimental and off by default")
	}
	if cfg.Agent.ExperimentalHelpMe {
		t.Fatal("expected HelpMe to be experimental and off by default")
	}
	if cfg.Agent.ProfileName() != DefaultAgentName {
		t.Fatalf("expected default agent name %q, got %q", DefaultAgentName, cfg.Agent.ProfileName())
	}
	if DefaultSystemPrompt() == "" {
		t.Fatal("expected built-in default system prompt")
	}
}

func TestLoadProjectConfigParsesUltraModeAndMaxParallel(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "model": "gpt-4.1"
    }
  },
  "agent": {
    "ultra_mode": true,
    "max_parallel": 3
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.Agent.UltraMode || cfg.Agent.MaxParallel != 3 || cfg.Agent.MaxParallelValue() != 3 {
		t.Fatalf("unexpected agent config: %+v", cfg.Agent)
	}
}

func TestConfigRejectsNegativeMaxParallel(t *testing.T) {
	cfg := Default()
	cfg.Agent.MaxParallel = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.max_parallel") {
		t.Fatalf("Validate error = %v, want agent.max_parallel error", err)
	}
}

func TestLoadFrom_DefaultsTemperatureToAuto(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key_env": "OPENAI_API_KEY",
      "model": "gpt-5.5",
      "wire_api": "responses"
    }
  },
  "agent": {}
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Agent.Temperature != 0 {
		t.Fatalf("expected missing temperature to use Auto/0, got %v", cfg.Agent.Temperature)
	}
}

func TestLoadFrom_ToolLoadingConfig(t *testing.T) {
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
    }
  },
  "agent": {
    "tool_loading": "flat",
    "experimental_deferred_tool_bundles": true
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Agent.ToolLoadingPreference() != ToolLoadingFlat {
		t.Fatalf("expected explicit flat tool loading, got %q", cfg.Agent.ToolLoadingPreference())
	}
	if !cfg.Agent.ExperimentalDeferredToolBundles {
		t.Fatal("expected experimental deferred bundle flag to parse")
	}
}

func TestLoadFrom_ProviderCacheCreationOmittedConfig(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "anthropic",
      "base_url": "https://compatible.example.com/anthropic",
      "api_key_env": "ANTHROPIC_API_KEY",
      "model": "generic-coder",
      "cache_creation_input_tokens_omitted": true
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if !cfg.Providers["main"].CacheCreationInputTokensOmitted {
		t.Fatal("expected cache creation omission flag to parse")
	}
}

func TestLoadFrom_ProviderInputTokensIncludeCacheReadConfig(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "anthropic",
      "base_url": "https://compatible.example.com/anthropic",
      "api_key_env": "ANTHROPIC_API_KEY",
      "model": "generic-coder",
      "input_tokens_include_cache_read": true
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if !cfg.Providers["main"].InputTokensIncludeCacheRead {
		t.Fatal("expected input_tokens_include_cache_read flag to parse")
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

	cfg, _, err := LoadProjectConfig(workdir)
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

func TestMemoryConfigLegacyIndexedMemoryFieldsRemainLoadable(t *testing.T) {
	cfg, err := decodeConfig([]byte(`{
  "memory": {
    "nudge_interval": 3,
    "memory_char_limit": 2200,
    "user_char_limit": 1375
  }
}`), "legacy-memory.json")
	if err != nil {
		t.Fatalf("decodeConfig: %v", err)
	}
	if cfg.Memory.NudgeInterval == nil || *cfg.Memory.NudgeInterval != 3 ||
		cfg.Memory.MemoryCharLimit != 2200 || cfg.Memory.UserCharLimit != 1375 {
		t.Fatalf("legacy memory fields were not preserved: %+v", cfg.Memory)
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

func TestMemoryConfig_DreamEnabledDefault(t *testing.T) {
	var cfg MemoryConfig
	if cfg.DreamEnabled() {
		t.Fatal("Dream should be disabled by default")
	}
	cfg.Dream = &DreamConfig{Enabled: true}
	if !cfg.DreamEnabled() {
		t.Fatal("Dream should be enabled when Dream.Enabled is true")
	}
	cfg.Dream = &DreamConfig{Enabled: false}
	if cfg.DreamEnabled() {
		t.Fatal("Dream should be disabled when Dream.Enabled is false")
	}
}

func TestMemoryConfig_DreamEnabledLegacyCompatibility(t *testing.T) {
	var cfg MemoryConfig
	days := 7
	cfg.DreamIntervalDays = &days
	if !cfg.DreamEnabled() {
		t.Fatal("legacy positive dream_interval_days should enable Dream")
	}
	zero := 0
	cfg.DreamIntervalDays = &zero
	if cfg.DreamEnabled() {
		t.Fatal("legacy zero dream_interval_days should disable Dream")
	}
}

func TestMemoryConfig_DreamConfigOverridesLegacyInterval(t *testing.T) {
	cfg := MemoryConfig{
		DreamIntervalDays: intPtr(14),
		Dream:             &DreamConfig{Enabled: true, IntervalDays: 3},
	}
	if got := cfg.DreamIntervalDaysValue(); got != 3 {
		t.Fatalf("Dream.IntervalDays should win over legacy DreamIntervalDays, got %d", got)
	}
}

func TestMemoryConfig_DreamProviderModel(t *testing.T) {
	cfg := MemoryConfig{Dream: &DreamConfig{Provider: "openai", Model: "gpt-4.1"}}
	if cfg.DreamProvider() != "openai" {
		t.Fatalf("DreamProvider = %q, want openai", cfg.DreamProvider())
	}
	if cfg.DreamModel() != "gpt-4.1" {
		t.Fatalf("DreamModel = %q, want gpt-4.1", cfg.DreamModel())
	}
}

func TestValidateRejectsUnknownDreamProvider(t *testing.T) {
	cfg := Default()
	cfg.Memory.Dream = &DreamConfig{Enabled: true, Provider: "missing"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "memory.dream.provider") {
		t.Fatalf("expected unknown dream provider error, got %v", err)
	}
}

func TestUpdateGeneralSettings_DreamFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	if err := writeConfigJSON(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	enabled := true
	interval := 5
	provider := "openai"
	model := "gpt-4.1"
	if err := UpdateGeneralSettings(path, GeneralSettingsUpdate{
		DreamEnabled:      &enabled,
		DreamIntervalDays: &interval,
		DreamProvider:     &provider,
		DreamModel:        &model,
	}); err != nil {
		t.Fatalf("update dream settings: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	memory, _ := raw["memory"].(map[string]any)
	if memory == nil {
		t.Fatal("memory section missing")
	}
	dream, _ := memory["dream"].(map[string]any)
	if dream == nil {
		t.Fatal("dream section missing")
	}
	if dream["enabled"] != true {
		t.Fatalf("dream.enabled = %v, want true", dream["enabled"])
	}
	if dream["interval_days"] != float64(5) {
		t.Fatalf("dream.interval_days = %v, want 5", dream["interval_days"])
	}
	if dream["provider"] != "openai" {
		t.Fatalf("dream.provider = %v, want openai", dream["provider"])
	}
	if dream["model"] != "gpt-4.1" {
		t.Fatalf("dream.model = %v, want gpt-4.1", dream["model"])
	}
	if _, ok := memory["dream_interval_days"]; ok {
		t.Fatal("legacy dream_interval_days should be removed after dream update")
	}
}

func TestUpdateExtensionSettingsPreservesConcurrentDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfigJSON(path, Default()); err != nil {
		t.Fatalf("write config: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, subjectID := range []string{"plugin:project:first", "plugin:project:second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := UpdateExtensionSettings(path, func(settings *extensions.Settings) error {
				settings.SetDisabled(subjectID, true)
				return nil
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("update extension settings: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.Extensions == nil || !cfg.Extensions.IsDisabled("plugin:project:first") || !cfg.Extensions.IsDisabled("plugin:project:second") {
		t.Fatalf("concurrent extension decisions were not preserved: %+v", cfg.Extensions)
	}
}

func TestUpdateGeneralSettings_DreamDisabledCleansSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Memory.Dream = &DreamConfig{Enabled: true, IntervalDays: 7}
	if err := writeConfigJSON(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	enabled := false
	if err := UpdateGeneralSettings(path, GeneralSettingsUpdate{DreamEnabled: &enabled}); err != nil {
		t.Fatalf("update dream settings: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	memory, _ := raw["memory"].(map[string]any)
	if memory == nil {
		t.Fatal("memory section missing")
	}
	if dream, ok := memory["dream"]; !ok || dream.(map[string]any)["enabled"] != false {
		t.Fatalf("dream.enabled should be false, got %v", dream)
	}
}

func writeConfigJSON(path string, cfg Config) error {
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func intPtr(v int) *int {
	return &v
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

	cfg, _, err := LoadProjectConfig(workdir)
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
	if strings.Contains(tpl, `"temperature"`) {
		t.Fatalf("template should omit Auto temperature override:\n%s", tpl)
	}
}

func TestDefaultConfigUsesPermissionModeAsPolicySource(t *testing.T) {
	cfg := Default()
	permissions := ResolveAgentPermissions(cfg.Agent)
	if permissions.Mode != PermissionModeStandard {
		t.Fatalf("default permissions = %+v", permissions)
	}
}

func TestNormalizePermissionModeUsesThreeStateAuthority(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: PermissionModeStandard},
		{in: PermissionModeStandard, want: PermissionModeStandard},
		{in: PermissionModeReadOnly, want: PermissionModeReadOnly},
		{in: PermissionModeUnconfined, want: PermissionModeUnconfined},
		{in: "not-a-mode", want: PermissionModeStandard},
	}
	for _, tt := range tests {
		if got := NormalizePermissionMode(tt.in); got != tt.want {
			t.Fatalf("NormalizePermissionMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveAgentPermissionsKeepsOnlyMode(t *testing.T) {
	permissions := ResolveAgentPermissions(AgentConfig{PermissionMode: PermissionModeReadOnly})
	if permissions.Mode != PermissionModeReadOnly {
		t.Fatalf("permissions = %+v, want read_only", permissions)
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

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Providers["main"].WireAPI != "responses" {
		t.Fatalf("expected wire_api responses, got %q", cfg.Providers["main"].WireAPI)
	}
}

func TestConfig_ProviderStreamTransport(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "wire_api": "responses",
      "stream_transport": "websocket-cached",
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

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Providers["main"].StreamTransport != "websocket-cached" {
		t.Fatalf("expected stream_transport websocket-cached, got %q", cfg.Providers["main"].StreamTransport)
	}
}

func TestConfig_RejectsUnknownStreamTransport(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com",
      "wire_api": "responses",
      "stream_transport": "socket-party",
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

	_, _, err := LoadProjectConfig(workdir)
	if err == nil || !strings.Contains(err.Error(), "stream_transport") {
		t.Fatalf("expected stream_transport validation error, got %v", err)
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

	cfg, _, err := LoadProjectConfig(workdir)
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

func TestConfig_IgnoresLegacyPermissionKeys(t *testing.T) {
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
    "permission_mode": "full_access",
    "permission_profile": "danger_full_access",
    "approval_policy": "never",
    "approvals_reviewer": "auto_review",
    "permission_rules": {
      "bash": "ask"
    },
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

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Agent.PermissionMode != PermissionModeStandard {
		t.Fatalf("legacy full_access should normalize to standard, got %q", cfg.Agent.PermissionMode)
	}
}

func TestConfig_CodexSubscriptionAllowsDefaultBaseURL(t *testing.T) {
	workdir := t.TempDir()
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

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Providers["main"].Type != "openai-codex" {
		t.Fatalf("provider type = %q", cfg.Providers["main"].Type)
	}
}

func TestConfig_CodexSubscriptionParsesReuseCodexCredentials(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-codex",
      "wire_api": "responses",
      "model": "gpt-5-codex",
      "reuse_codex_credentials": true
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.Providers["main"].ReuseCodexCredentials {
		t.Fatal("expected reuse_codex_credentials to parse as true")
	}
}

func TestConfig_CodexSubscriptionDefaultsLegacyCredentialReuse(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-codex",
      "base_url": "https://chatgpt.com/backend-api/codex",
      "wire_api": "responses",
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

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.Providers["main"].ReuseCodexCredentials {
		t.Fatal("expected legacy openai-codex config to default reuse_codex_credentials")
	}
}

func TestConfig_CodexSubscriptionPreservesExplicitCredentialReuseFalse(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, ".wuu.json")
	jsonData := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-codex",
      "base_url": "https://chatgpt.com/backend-api/codex",
      "wire_api": "responses",
      "model": "gpt-5-codex",
      "reuse_codex_credentials": false
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Providers["main"].ReuseCodexCredentials {
		t.Fatal("expected explicit reuse_codex_credentials=false to be preserved")
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

	_, _, err := LoadProjectConfig(workdir)
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

	_, _, err := LoadProjectConfig(workdir)
	if err == nil {
		t.Fatal("expected unknown wire_api validation error")
	}
	if !strings.Contains(err.Error(), "wire_api") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultSystemPrompt_ToolUsingMainAgent(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if !strings.Contains(prompt, "wuu") || !strings.Contains(prompt, "coding agent") {
		t.Fatalf("default system prompt must identify the agent: %q", prompt)
	}
	for _, want := range []string{
		"visible text outside tool calls is shown to the user",
		"Before the first tool call for a non-trivial task",
		"send concise updates at reasonable intervals",
		"Skip an update only for a single trivial action",
		"runtime guidance, not user-authored text",
		"prompt injection",
		"Commit only when",
		"Write to remotes only when the user explicitly requests it",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt missing invariant guidance %q: %q", want, prompt)
		}
	}
	if len(prompt) > 2*1024 {
		t.Fatalf("default system prompt should remain a small Wuu-specific contract, got %d bytes", len(prompt))
	}
}

func TestDefaultSystemPromptDoesNotReteachGenericCodingBehavior(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, generic := range []string{
		"# Communication",
		"# Doing tasks",
		"# Using tools",
		"# Final answers",
		"in parallel",
		"root cause",
		"Verify changed behavior",
		"A progress update is not a final answer",
		"Lead with the user-visible outcome",
		"Do not add copyright",
		"non-obvious rationale",
	} {
		if strings.Contains(prompt, generic) {
			t.Fatalf("default system prompt should rely on trained coding behavior instead of teaching %q: %q", generic, prompt)
		}
	}
}

func TestDefaultSystemPromptLeavesToolManualsToActiveSurface(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, toolName := range []string{
		"tool_search",
		"update_plan",
		"spawn_agent",
		"helpme",
		"inception",
		"apply_patch",
		"web_search",
	} {
		if strings.Contains(prompt, toolName) {
			t.Fatalf("default system prompt should not name optional tool %q: %q", toolName, prompt)
		}
	}
}

func TestDefaultSystemPrompt_GoalWorkflowAgentClosure(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"completed subagent task does not mean the overall task is complete",
		"integrate the result and verify the overall work",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt missing subagent completion boundary %q: %q", want, prompt)
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

func TestDefaultSystemPrompt_FinalAnswerReferences(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"[label](relative/path#L12)",
		"[label](/absolute/path#L12)",
		"Do not use `file://`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include final-answer reference guidance %q: %q", want, prompt)
		}
	}
}

func TestDefaultSystemPrompt_MainCoordination(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"completed subagent task does not mean the overall task is complete",
		"integrate the result and verify the overall work",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default system prompt must include main-agent coordination guidance %q: %q", want, prompt)
		}
	}
	for _, duplicatedTerm := range []string{"hidden context", "result card", "conversation participant"} {
		if strings.Contains(prompt, duplicatedTerm) {
			t.Fatalf("default system prompt should leave per-message result-card guidance out %q: %q", duplicatedTerm, prompt)
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
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.DisableAutoCompact {
		t.Fatal("expected DisableAutoCompact=true")
	}
}

func TestAgentConfigGitAttributionDefaultsEnabledAndSupportsOptOut(t *testing.T) {
	var agent AgentConfig
	if !agent.GitAttributionEnabledValue() {
		t.Fatal("git attribution should default to enabled")
	}

	disabled := false
	agent.GitAttributionEnabled = &disabled
	if agent.GitAttributionEnabledValue() {
		t.Fatal("explicit false should disable git attribution")
	}

	enabled := true
	agent.GitAttributionEnabled = &enabled
	if !agent.GitAttributionEnabledValue() {
		t.Fatal("explicit true should enable git attribution")
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
	cfg, _, err := LoadProjectConfig(workdir)
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
	cfg, _, err := LoadProjectConfig(workdir)
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
	compactKeepRecent := 20000
	disableAutoCompact := true
	providerContext := 512000
	if err := UpdateAdvancedRuntime(configPath, "main", AdvancedRuntimeUpdate{
		MaxSteps:                &maxSteps,
		MaxContextTokens:        &maxContext,
		Temperature:             &temperature,
		CompactThresholdPct:     &compactPct,
		CompactKeepRecentTokens: &compactKeepRecent,
		DisableAutoCompact:      &disableAutoCompact,
		ProviderContextWindow:   &providerContext,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.MaxSteps != maxSteps ||
		cfg.Agent.MaxContextTokens != maxContext ||
		cfg.Agent.Temperature != temperature ||
		cfg.Agent.CompactThresholdPct != compactPct ||
		cfg.Agent.CompactKeepRecentTokens != compactKeepRecent ||
		!cfg.Agent.DisableAutoCompact {
		t.Fatalf("advanced agent settings not persisted: %+v", cfg.Agent)
	}
	if cfg.Providers["main"].ContextWindow != providerContext {
		t.Fatalf("provider context_window = %d, want %d", cfg.Providers["main"].ContextWindow, providerContext)
	}
}

func TestUpdateAdvancedRuntimeDeletesTemperatureForAuto(t *testing.T) {
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
    "temperature": 0.4
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	auto := 0.0
	if err := UpdateAdvancedRuntime(configPath, "main", AdvancedRuntimeUpdate{Temperature: &auto}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Temperature != 0 {
		t.Fatalf("temperature = %v, want Auto/0", cfg.Agent.Temperature)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"temperature"`) {
		t.Fatalf("Auto temperature should remove config override:\n%s", string(data))
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
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.ExperimentalCoordinatorMode {
		t.Fatal("expected ExperimentalCoordinatorMode=true")
	}
}

func TestConfig_ExperimentalHelpMe(t *testing.T) {
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
    "experimental_helpme": true
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.ExperimentalHelpMe {
		t.Fatal("expected ExperimentalHelpMe=true")
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
	cfg, _, err := LoadProjectConfig(workdir)
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

	cfg, _, err := LoadProjectConfig(workdir)
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
	cfg, _, err := LoadProjectConfig(workdir)
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

func TestLoadFrom_EmptyHomeDoesNotImplicitlyTrustProjectConfig(t *testing.T) {
	t.Setenv("WUU_HOME", filepath.Join(t.TempDir(), "missing-user-home"))
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, localPrimaryConfig), []byte(`{
  "default_provider": "project",
  "providers": {
    "project": {
      "type": "openai-compatible",
      "base_url": "https://project.example/v1",
      "api_key_env": "PROJECT_KEY",
      "model": "project-model"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	if _, _, err := LoadFrom(workdir, ""); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("LoadFrom with empty home = %v, want ErrConfigNotFound", err)
	}
	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("explicit LoadProjectConfig: %v", err)
	}
	if cfg.DefaultProvider != "project" {
		t.Fatalf("explicit project config was not loaded: %+v", cfg)
	}
}

func TestLoadFrom_EmptyHomeArgumentUsesWUUHome(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	if err := os.MkdirAll(wuuHome, 0o700); err != nil {
		t.Fatalf("mkdir WUU_HOME: %v", err)
	}
	configPath := filepath.Join(wuuHome, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "default_provider": "user",
  "providers": {
    "user": {
      "type": "openai-compatible",
      "base_url": "https://user.example/v1",
      "api_key_env": "USER_KEY",
      "model": "user-model"
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write WUU_HOME config: %v", err)
	}

	cfg, loadedPath, err := LoadFrom(t.TempDir(), "")
	if err != nil {
		t.Fatalf("LoadFrom with WUU_HOME and empty home: %v", err)
	}
	if loadedPath != configPath || cfg.DefaultProvider != "user" {
		t.Fatalf("loaded path=%q config=%+v, want WUU_HOME config %q", loadedPath, cfg, configPath)
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
	_, _, err := LoadProjectConfig(workdir)
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
	_, _, err := LoadProjectConfig(workdir)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("invalid config wrongly classified as not-found: %v", err)
	}
}

func TestLoadProjectConfig_OnlyFallsBackWhenPrimaryIsMissing(t *testing.T) {
	t.Run("missing primary", func(t *testing.T) {
		workdir := t.TempDir()
		fallbackPath := filepath.Join(workdir, localFallbackConfig)
		if err := os.WriteFile(fallbackPath, []byte(migrateTestConfigJSON), 0o644); err != nil {
			t.Fatalf("write fallback config: %v", err)
		}

		_, loadedPath, err := LoadProjectConfig(workdir)
		if err != nil {
			t.Fatalf("LoadProjectConfig: %v", err)
		}
		if loadedPath != fallbackPath {
			t.Fatalf("loaded path = %q, want fallback %q", loadedPath, fallbackPath)
		}
	})

	t.Run("primary read error", func(t *testing.T) {
		workdir := t.TempDir()
		primaryPath := filepath.Join(workdir, localPrimaryConfig)
		writeSelfReferentialSymlink(t, primaryPath)
		if err := os.WriteFile(filepath.Join(workdir, localFallbackConfig), []byte(migrateTestConfigJSON), 0o644); err != nil {
			t.Fatalf("write fallback config: %v", err)
		}

		_, _, err := LoadProjectConfig(workdir)
		if err == nil {
			t.Fatal("expected the primary config read error")
		}
		if errors.Is(err, ErrConfigNotFound) {
			t.Fatalf("primary read error classified as not found: %v", err)
		}
		if !strings.Contains(err.Error(), primaryPath) {
			t.Fatalf("error %q does not identify primary config %q", err, primaryPath)
		}
	})
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

	cfg, _, err := LoadProjectConfig(dir)
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

	cfg, _, err := LoadProjectConfig(dir)
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

	cfg, _, err := LoadProjectConfig(dir)
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
	if err := UpdateProviderRuntime(path, "next", "custom-model", &baseURL, &apiKey, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadProjectConfig(dir)
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

func TestRuntimeUpdatesPersistUltraAtomicallyAndPreserveNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "model": "old-model"
    }
  },
  "agent": {
    "max_parallel": 2
  },
  "memory": {
    "disable": true
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	enabled := true
	if err := UpdateProviderRuntime(path, "main", "new-model", nil, nil, nil, nil, nil, nil, &enabled); err != nil {
		t.Fatalf("combined runtime update: %v", err)
	}
	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload combined update: %v", err)
	}
	if !cfg.Agent.UltraMode || cfg.Agent.MaxParallel != 2 || cfg.Providers["main"].Model != "new-model" || !cfg.Memory.Disable {
		t.Fatalf("combined update was not persisted: %+v", cfg)
	}

	if err := UpdateProviderRuntime(path, "main", "next-model", nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("nil Ultra update: %v", err)
	}
	cfg, _, err = LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload nil update: %v", err)
	}
	if !cfg.Agent.UltraMode || cfg.Providers["main"].Model != "next-model" {
		t.Fatalf("nil Ultra update did not preserve mode: %+v", cfg)
	}

	disabled := false
	if err := UpdateAgentUltraMode(path, &disabled); err != nil {
		t.Fatalf("Ultra-only update: %v", err)
	}
	cfg, _, err = LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload Ultra-only update: %v", err)
	}
	if cfg.Agent.UltraMode || cfg.Agent.MaxParallel != 2 || cfg.Providers["main"].Model != "next-model" || !cfg.Memory.Disable {
		t.Fatalf("Ultra-only update changed unrelated config: %+v", cfg)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if strings.Contains(string(raw), "ultra_mode") {
		t.Fatalf("disabled Ultra should use the default omission: %s", raw)
	}
	if !strings.Contains(string(raw), `"memory": {`) {
		t.Fatalf("unrelated config was not preserved: %s", raw)
	}
}

func TestUpdateProviderRuntimePersistsPermissionMode(t *testing.T) {
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

	mode := PermissionModeUnconfined
	if err := UpdateProviderRuntime(path, "old", "old-model", nil, nil, nil, nil, nil, &mode, nil); err != nil {
		t.Fatalf("UpdateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	permissions := ResolveAgentPermissions(cfg.Agent)
	if permissions.Mode != PermissionModeUnconfined {
		t.Fatalf("permission mode not persisted: %+v", permissions)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, legacyKey := range []string{"tool_policy", "permission_profile", "approval_policy", "approvals_reviewer", "permission_rules"} {
		if strings.Contains(string(raw), legacyKey) {
			t.Fatalf("legacy permission key %q should be removed from config:\n%s", legacyKey, raw)
		}
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
	if err := CreateProviderRuntime(path, "custom-1", nil, "custom-model", &baseURL, &apiKey, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("CreateProviderRuntime: %v", err)
	}

	cfg, _, err := LoadProjectConfig(dir)
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

func TestRemoveProviderInactiveKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "keep",
  "providers": {
    "keep": {
      "type": "openai-compatible",
      "base_url": "https://keep.example.com/v1",
      "model": "keep-model"
    },
    "drop": {
      "type": "openai-compatible",
      "base_url": "https://drop.example.com/v1",
      "model": "drop-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	newDefault, err := RemoveProvider(path, "drop", "", "")
	if err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}
	if newDefault != "" {
		t.Fatalf("expected empty newDefault when inactive provider is removed, got %q", newDefault)
	}

	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "keep" {
		t.Fatalf("default provider changed: %q", cfg.DefaultProvider)
	}
	if _, _, err := cfg.ResolveProvider("drop"); err == nil {
		t.Fatal("expected drop provider to be removed")
	}
	if _, _, err := cfg.ResolveProvider("keep"); err != nil {
		t.Fatalf("keep provider unexpectedly removed: %v", err)
	}
}

func TestRemoveProviderActiveSwapsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "drop",
  "providers": {
    "drop": {
      "type": "openai-compatible",
      "base_url": "https://drop.example.com/v1",
      "model": "drop-model"
    },
    "keep": {
      "type": "openai-compatible",
      "base_url": "https://keep.example.com/v1",
      "model": "keep-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	newDefault, err := RemoveProvider(path, "drop", "keep", "")
	if err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}
	if newDefault != "keep" {
		t.Fatalf("expected newDefault=keep, got %q", newDefault)
	}

	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "keep" {
		t.Fatalf("default provider not swapped: %q", cfg.DefaultProvider)
	}
	keep, _, _ := cfg.ResolveProvider("keep")
	if keep.Model != "keep-model" {
		t.Fatalf("keep provider model unexpectedly changed: %q", keep.Model)
	}
}

func TestRemoveProviderActiveAppliesFallbackModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "drop",
  "providers": {
    "drop": {
      "type": "openai-compatible",
      "base_url": "https://drop.example.com/v1",
      "model": "drop-model"
    },
    "keep": {
      "type": "openai-compatible",
      "base_url": "https://keep.example.com/v1",
      "model": "keep-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProvider(path, "drop", "keep", "fallback-model"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}

	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	keep, _, _ := cfg.ResolveProvider("keep")
	if keep.Model != "fallback-model" {
		t.Fatalf("expected fallback model applied, got %q", keep.Model)
	}
}

func TestRemoveProviderRejectsLastProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "only",
  "providers": {
    "only": {
      "type": "openai-compatible",
      "base_url": "https://only.example.com/v1",
      "model": "only-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProvider(path, "only", "", ""); err == nil {
		t.Fatal("expected error when removing the only provider without fallback")
	}

	cfg, _, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultProvider != "only" {
		t.Fatalf("default provider changed on failed removal: %q", cfg.DefaultProvider)
	}
	if _, _, err := cfg.ResolveProvider("only"); err != nil {
		t.Fatalf("only provider should still exist: %v", err)
	}
}

func TestRemoveProviderRejectsUnknownProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "real",
  "providers": {
    "real": {
      "type": "openai-compatible",
      "base_url": "https://real.example.com/v1",
      "model": "real-model"
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProvider(path, "ghost", "", ""); err == nil {
		t.Fatal("expected error when removing a non-existent provider")
	}
}

func TestRemoveProviderClearsModelRoleReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "keep",
  "providers": {
    "keep": {
      "type": "openai-compatible",
      "base_url": "https://keep.example.com/v1",
      "model": "keep-model"
    },
    "drop": {
      "type": "openai-compatible",
      "base_url": "https://drop.example.com/v1",
      "model": "drop-model"
    }
  },
  "agent": {
    "model_roles": {
      "review": { "provider": "drop", "model": "drop-model" },
      "compact": { "provider": "keep", "model": "keep-model" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProvider(path, "drop", "", ""); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"provider": "drop"`) {
		t.Fatalf("review role provider was not cleared: %s", data)
	}
	if !strings.Contains(string(data), `"provider": "keep"`) {
		t.Fatalf("compact role provider was unexpectedly removed: %s", data)
	}
}

func TestConfig_ModelAliases(t *testing.T) {
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
    "model_aliases": {
      "cheap": {"provider": "main", "model": "gpt-5-mini"},
      "frontend": {"provider": "anthropic", "model": "claude-sonnet-4-5", "effort": "high"}
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadProjectConfig(workdir)
	if err != nil {
		t.Fatalf("LoadProjectConfig returned error: %v", err)
	}
	if cfg.Agent.ModelAliases["cheap"].Provider != "main" || cfg.Agent.ModelAliases["cheap"].Model != "gpt-5-mini" {
		t.Fatalf("cheap alias not parsed: %+v", cfg.Agent.ModelAliases["cheap"])
	}
	if cfg.Agent.ModelAliases["frontend"].Provider != "anthropic" || cfg.Agent.ModelAliases["frontend"].Effort != "high" {
		t.Fatalf("frontend alias not parsed: %+v", cfg.Agent.ModelAliases["frontend"])
	}
}

func TestConfig_ModelAliasesColonContainingModelID(t *testing.T) {
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
			ModelAliases: map[string]ModelRoleConfig{
				"local": {Provider: "main", Model: "llama3.2:latest"},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("colon-containing API model ID should be valid: %v", err)
	}
}

func TestConfig_ModelAliasesRejectInvalidName(t *testing.T) {
	cases := []string{"", "Frontend", "1cheap", "cheap!", "cheap alias"}
	for _, name := range cases {
		cfg := Config{
			DefaultProvider: "main",
			Providers: map[string]ProviderConfig{
				"main": {Type: "openai-compatible", BaseURL: "https://example.com/v1", Model: "gpt-5-codex"},
			},
			Agent: AgentConfig{
				ModelAliases: map[string]ModelRoleConfig{
					name: {Provider: "main", Model: "gpt-5-mini"},
				},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid alias name %q to be rejected", name)
		}
	}
}

func TestConfig_ModelAliasesRejectDuplicateNormalizedName(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {Type: "openai-compatible", BaseURL: "https://example.com/v1", Model: "gpt-5-codex"},
		},
		Agent: AgentConfig{
			ModelAliases: map[string]ModelRoleConfig{
				"cheap":  {Provider: "main", Model: "gpt-5-mini"},
				" cheap": {Provider: "main", Model: "gpt-5-nano"},
			},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "normalize") {
		t.Fatalf("expected duplicate normalized alias error, got %v", err)
	}
}

func TestConfig_ModelAliasesRejectEmptyProvider(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {Type: "openai-compatible", BaseURL: "https://example.com/v1", Model: "gpt-5-codex"},
		},
		Agent: AgentConfig{
			ModelAliases: map[string]ModelRoleConfig{
				"cheap": {Provider: "", Model: "gpt-5-mini"},
			},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "provider is required") {
		t.Fatalf("expected empty provider error, got %v", err)
	}
}

func TestConfig_ModelAliasesRejectEmptyModel(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {Type: "openai-compatible", BaseURL: "https://example.com/v1", Model: "gpt-5-codex"},
		},
		Agent: AgentConfig{
			ModelAliases: map[string]ModelRoleConfig{
				"cheap": {Provider: "main", Model: ""},
			},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("expected empty model error, got %v", err)
	}
}

func TestConfig_ModelAliasesRejectUnknownProvider(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {Type: "openai-compatible", BaseURL: "https://example.com/v1", Model: "gpt-5-codex"},
		},
		Agent: AgentConfig{
			ModelAliases: map[string]ModelRoleConfig{
				"cheap": {Provider: "missing", Model: "gpt-5-mini"},
			},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `provider "missing" not found`) {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestConfig_ModelAliasesRejectInvalidConfiguredVariant(t *testing.T) {
	cfg := Config{
		DefaultProvider: "main",
		Providers: map[string]ProviderConfig{
			"main": {
				Type:    "openai-compatible",
				BaseURL: "https://example.com/v1",
				Model:   "gpt-5-codex",
				Models: map[string]ProviderModelConfig{
					"custom-model": {
						Variants: map[string]map[string]any{
							"low":  {"reasoningEffort": "low"},
							"high": {"reasoningEffort": "high"},
						},
					},
				},
			},
		},
		Agent: AgentConfig{
			ModelAliases: map[string]ModelRoleConfig{
				"bad": {Provider: "main", Model: "custom-model", Effort: "medium"},
			},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected invalid effort error, got %v", err)
	}
}

func TestRemoveProviderDeletesModelAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "keep",
  "providers": {
    "keep": {
      "type": "openai-compatible",
      "base_url": "https://keep.example.com/v1",
      "model": "keep-model"
    },
    "drop": {
      "type": "openai-compatible",
      "base_url": "https://drop.example.com/v1",
      "model": "drop-model"
    }
  },
  "agent": {
    "model_aliases": {
      "drop-alias": { "provider": "drop", "model": "drop-model" },
      "keep-alias": { "provider": "keep", "model": "keep-model" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProvider(path, "drop", "", ""); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"drop-alias"`) {
		t.Fatalf("drop alias was not deleted: %s", data)
	}
	if !strings.Contains(string(data), `"keep-alias"`) {
		t.Fatalf("keep alias was unexpectedly deleted: %s", data)
	}
}

func TestUpdateAdvancedRuntimeModelAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "model": "gpt-5-codex"
    }
  },
  "agent": {
    "model_aliases": {
      "old": { "provider": "main", "model": "old-model" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	cheap := ModelRoleConfig{Provider: "main", Model: "gpt-5-mini"}
	frontend := ModelRoleConfig{Provider: "main", Model: "gpt-5-frontend", Effort: "high"}
	if err := UpdateAdvancedRuntime(path, "main", AdvancedRuntimeUpdate{
		ModelAliases: map[string]*ModelRoleConfig{
			"cheap":    &cheap,
			"frontend": &frontend,
			"old":      nil,
		},
	}); err != nil {
		t.Fatalf("UpdateAdvancedRuntime: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"old"`) {
		t.Fatalf("old alias was not removed: %s", data)
	}
	if !strings.Contains(string(data), `"cheap"`) || !strings.Contains(string(data), `"frontend"`) {
		t.Fatalf("new aliases were not written: %s", data)
	}
	if !strings.Contains(string(data), `"effort": "high"`) {
		t.Fatalf("frontend effort was not written: %s", data)
	}
}

func TestUpdateAdvancedRuntimeModelAliasesEmptyMapClearsAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wuu.json")
	orig := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "model": "gpt-5-codex"
    }
  },
  "agent": {
    "model_aliases": {
      "old": { "provider": "main", "model": "old-model" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateAdvancedRuntime(path, "main", AdvancedRuntimeUpdate{ModelAliases: map[string]*ModelRoleConfig{}}); err != nil {
		t.Fatalf("UpdateAdvancedRuntime: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "model_aliases") {
		t.Fatalf("model_aliases was not cleared: %s", data)
	}
}
