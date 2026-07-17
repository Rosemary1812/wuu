package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestLocalAppServerControllerInitializeAndResumeThread(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
		"default_provider": "test",
		"providers": {
			"test": {
				"type": "openai-compatible",
				"base_url": "https://example.test/v1",
				"api_key": "sk-test",
				"model": "gpt-test"
			}
		},
		"agent": {
			"permission_mode": "standard"
		}
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controller, err := NewLocalAppServerController(ctx, Options{
		Workdir:    root,
		ConfigPath: configPath,
		Ultra:      true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer controller.Shutdown(context.Background())

	init, err := controller.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.WorkspaceRoot != root || init.Provider != "test" || init.Model != "gpt-test" {
		t.Fatalf("unexpected initialize result: %+v", init)
	}
	if !init.Ultra || init.MaxParallel != config.DefaultAgentMaxParallel {
		t.Fatalf("ultra override was not reflected by initialize: %+v", init)
	}
	thread, err := controller.StartThread(ctx, false)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if thread.ID == "" || thread.Ephemeral {
		t.Fatalf("unexpected started thread: %+v", thread)
	}
	resumed, err := controller.ResumeThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("ResumeThread: %v", err)
	}
	if resumed.ID != thread.ID {
		t.Fatalf("resumed thread = %q, want %q", resumed.ID, thread.ID)
	}
}

func TestLocalAppServerControllerEffortOverrideClearsConfiguredVariant(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
		"default_provider": "test",
		"providers": {
			"test": {
				"type": "openai-compatible",
				"base_url": "https://example.test/v1",
				"api_key": "sk-test",
				"model": "gpt-5.5"
			}
		},
		"agent": {
			"effort": "xhigh",
			"variant": "xhigh"
		}
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controller, err := NewLocalAppServerController(ctx, Options{
		Workdir:    root,
		ConfigPath: configPath,
		Effort:     "low",
		NoTools:    true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer controller.Shutdown(context.Background())

	init, err := controller.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.Effort != "low" || init.Variant != "low" {
		t.Fatalf("effort override should select low variant, got effort=%q variant=%q", init.Effort, init.Variant)
	}
}

func TestLocalAppServerControllerIgnoreUserConfigReloadsProjectLayers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(t.TempDir(), "unused-user-home"))
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
  "default_provider": "test",
  "providers": {
    "test": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {"max_steps": 4, "append_system_prompt": "base prompt"}
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	settingsDir := filepath.Join(root, ".wuu")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir project settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(`{
  "agent": {"max_steps": 17, "append_system_prompt": "layer prompt"}
}`), 0o644); err != nil {
		t.Fatalf("write project settings: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controller, err := NewLocalAppServerController(ctx, Options{
		Workdir:          root,
		IgnoreUserConfig: true,
		NoTools:          true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer controller.Shutdown(context.Background())

	init, err := controller.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.AdvancedSettings.MaxSteps != 17 || init.GeneralSettings.AppendSystemPrompt != "layer prompt" {
		t.Fatalf("initialize lost explicitly trusted project layer: advanced=%+v general=%+v", init.AdvancedSettings, init.GeneralSettings)
	}
}

func TestNewLocalAppServerControllerMarksExplicitPermissionOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(t.TempDir(), "wuu-home"))
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
		"default_provider": "test",
		"providers": {
			"test": {
				"type": "openai-compatible",
				"base_url": "https://example.test/v1",
				"api_key": "sk-test",
				"model": "gpt-test"
			}
		},
		"agent": {
			"permission_mode": "unconfined"
		}
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flagged, err := NewLocalAppServerController(ctx, Options{
		Workdir:        root,
		ConfigPath:     configPath,
		PermissionMode: "read_only",
		NoTools:        true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer flagged.Shutdown(context.Background())
	flaggedRT := flagged.(*localAppServerController).rt
	if !flaggedRT.PermissionModeExplicit || flaggedRT.Permissions.Mode != config.PermissionModeReadOnly {
		t.Fatalf("--permission-mode should become the explicit override: explicit=%t mode=%q", flaggedRT.PermissionModeExplicit, flaggedRT.Permissions.Mode)
	}

	unflagged, err := NewLocalAppServerController(ctx, Options{
		Workdir:    root,
		ConfigPath: configPath,
		NoTools:    true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController without flag: %v", err)
	}
	defer unflagged.Shutdown(context.Background())
	unflaggedRT := unflagged.(*localAppServerController).rt
	if unflaggedRT.PermissionModeExplicit || unflaggedRT.Permissions.Mode != config.PermissionModeUnconfined {
		t.Fatalf("config-sourced mode must not count as explicit: explicit=%t mode=%q", unflaggedRT.PermissionModeExplicit, unflaggedRT.Permissions.Mode)
	}
}

func TestApplyConfigOverridesEffortClearsConfiguredVariant(t *testing.T) {
	cfg := config.Config{
		Agent: config.AgentConfig{
			Effort:  "xhigh",
			Variant: "xhigh",
		},
	}

	if err := applyConfigOverrides(&cfg, Options{Effort: "low"}); err != nil {
		t.Fatalf("applyConfigOverrides: %v", err)
	}

	if cfg.Agent.Effort != "low" {
		t.Fatalf("Effort = %q, want low", cfg.Agent.Effort)
	}
	if cfg.Agent.Variant != "" {
		t.Fatalf("Variant should be cleared by explicit effort override, got %q", cfg.Agent.Variant)
	}
}

func TestApplyConfigOverridesExplicitVariantWinsOverEffort(t *testing.T) {
	cfg := config.Config{
		Agent: config.AgentConfig{
			Effort:  "medium",
			Variant: "medium",
		},
	}

	if err := applyConfigOverrides(&cfg, Options{Effort: "low", Variant: "high"}); err != nil {
		t.Fatalf("applyConfigOverrides: %v", err)
	}

	if cfg.Agent.Effort != "low" {
		t.Fatalf("Effort = %q, want low", cfg.Agent.Effort)
	}
	if cfg.Agent.Variant != "high" {
		t.Fatalf("Variant = %q, want high", cfg.Agent.Variant)
	}
}

func TestApplyConfigOverridesNormalizesPermissionMode(t *testing.T) {
	for name, tc := range map[string]struct {
		opts Options
		want string
	}{
		"standard":   {opts: Options{PermissionMode: config.PermissionModeStandard}, want: config.PermissionModeStandard},
		"read only":  {opts: Options{PermissionMode: config.PermissionModeReadOnly}, want: config.PermissionModeReadOnly},
		"unconfined": {opts: Options{PermissionMode: config.PermissionModeUnconfined}, want: config.PermissionModeUnconfined},
		"unknown":    {opts: Options{PermissionMode: "not-a-mode"}, want: config.PermissionModeStandard},
	} {
		cfg := config.Config{}
		if err := applyConfigOverrides(&cfg, tc.opts); err != nil {
			t.Fatalf("%s: applyConfigOverrides: %v", name, err)
		}
		if got := cfg.Agent.PermissionMode; got != tc.want {
			t.Fatalf("%s: PermissionMode = %q, want %q", name, got, tc.want)
		}
	}
}

func TestApplyConfigOverridesUltraOnlyEnables(t *testing.T) {
	for name, tc := range map[string]struct {
		configured bool
		override   bool
		want       bool
	}{
		"explicit enable": {configured: false, override: true, want: true},
		"flag omitted":    {configured: true, override: false, want: true},
	} {
		cfg := config.Config{Agent: config.AgentConfig{UltraMode: tc.configured}}
		if err := applyConfigOverrides(&cfg, Options{Ultra: tc.override}); err != nil {
			t.Fatalf("%s: applyConfigOverrides: %v", name, err)
		}
		if cfg.Agent.UltraMode != tc.want {
			t.Fatalf("%s: UltraMode = %t, want %t", name, cfg.Agent.UltraMode, tc.want)
		}
	}
}
