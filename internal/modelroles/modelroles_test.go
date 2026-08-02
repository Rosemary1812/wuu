package modelroles

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestResolveDefaultsRolesToMainSelection(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-5-codex",
			},
		},
	}

	roles, err := Resolve(cfg, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if roles.Main.Provider != "openai" || roles.Main.Model != "gpt-5-codex" {
		t.Fatalf("unexpected main role: %+v", roles.Main)
	}
	if roles.Main.Behavior.Family != "codex" || !roles.Main.Capabilities.FreeformTool {
		t.Fatalf("unexpected main model facts: capabilities=%+v behavior=%+v", roles.Main.Capabilities, roles.Main.Behavior)
	}
	if !roles.Review.Inherited || roles.Review.Model != roles.Main.Model || roles.Review.Provider != roles.Main.Provider {
		t.Fatalf("review should inherit main selection: main=%+v review=%+v", roles.Main, roles.Review)
	}
	if !roles.Title.Inherited || roles.Title.APIModel != roles.Main.APIModel {
		t.Fatalf("title should inherit main API model: main=%+v title=%+v", roles.Main, roles.Title)
	}
}

func TestResolveHonorsExplicitReviewProvider(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-4.1",
			},
			"anthropic": {
				Type:    "anthropic",
				BaseURL: "https://api.anthropic.com",
				Model:   "claude-sonnet-4-5",
			},
		},
		Agent: config.AgentConfig{
			ModelRoles: config.ModelRolesConfig{
				Review: config.ModelRoleConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-5",
				},
			},
		},
	}

	roles, err := Resolve(cfg, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if roles.Review.Inherited || roles.Review.Provider != "anthropic" || roles.Review.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected review role: %+v", roles.Review)
	}
	if roles.Review.Behavior.Family != "claude" || roles.Review.Behavior.ExactEditReliability <= roles.Review.Behavior.PatchReliability {
		t.Fatalf("review should carry Claude behavior facts: %+v", roles.Review.Behavior)
	}
	if !roles.Worker.Inherited || roles.Worker.Provider != "openai" {
		t.Fatalf("worker should still inherit main selection: %+v", roles.Worker)
	}
}

func TestResolveRoleUsesAPIModelVariantAndLimits(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "custom",
		Providers: map[string]config.ProviderConfig{
			"custom": {
				Type:    "openai-compatible",
				BaseURL: "https://example.test/v1",
				Model:   "main-model",
				Models: map[string]config.ProviderModelConfig{
					"worker-alias": {
						ID: "real-worker-model",
						Limit: &config.ProviderModelLimitConfig{
							Context: 123000,
							Input:   120000,
							Output:  3000,
						},
						Variants: map[string]map[string]any{
							"deep": {"reasoningEffort": "high"},
						},
					},
				},
			},
		},
		Agent: config.AgentConfig{
			ModelRoles: config.ModelRolesConfig{
				Worker: config.ModelRoleConfig{
					Model:   "worker-alias",
					Variant: "deep",
				},
			},
		},
	}

	roles, err := Resolve(cfg, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if roles.Worker.Inherited {
		t.Fatalf("worker should be explicit: %+v", roles.Worker)
	}
	if roles.Worker.APIModel != "real-worker-model" || roles.Worker.Variant != "deep" {
		t.Fatalf("unexpected worker selection: %+v", roles.Worker)
	}
	if got := roles.Worker.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("worker provider options = %#v", roles.Worker.ProviderOptions)
	}
	if roles.Worker.Capabilities.ContextWindow != 123000 ||
		roles.Worker.Capabilities.InputLimit != 120000 ||
		roles.Worker.Capabilities.OutputLimit != 3000 {
		t.Fatalf("worker capabilities did not use configured limits: %+v", roles.Worker.Capabilities)
	}
}

func TestResolveUsesCatalogToolAndMediaCapabilities(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "text-embedding-3-large",
			},
		},
	}

	roles, err := Resolve(cfg, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if roles.Main.Capabilities.Tools || roles.Main.Capabilities.ToolCalling != "none" {
		t.Fatalf("embedding model should not expose tools: %+v", roles.Main.Capabilities)
	}

	cfg.Providers["openai"] = config.ProviderConfig{
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Model:   "o3",
	}
	roles, err = Resolve(cfg, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve o3: %v", err)
	}
	if !roles.Main.Capabilities.Tools || !roles.Main.Capabilities.ImageInput || !roles.Main.Capabilities.FileInput {
		t.Fatalf("o3 should expose tool and media capabilities: %+v", roles.Main.Capabilities)
	}
}

func TestResolveMediaInputCapabilities(t *testing.T) {
	resolveCaps := func(t *testing.T, model string) (bool, bool) {
		t.Helper()
		cfg := config.Config{
			DefaultProvider: "openai",
			Providers: map[string]config.ProviderConfig{
				"openai": {
					Type:    "openai",
					BaseURL: "https://api.openai.com/v1",
					Model:   model,
				},
			},
		}
		roles, err := Resolve(cfg, ResolveOptions{})
		if err != nil {
			t.Fatalf("Resolve %s: %v", model, err)
		}
		return roles.Main.Capabilities.ImageInput, roles.Main.Capabilities.FileInput
	}

	// Catalog model whose modality data excludes image/pdf input: rejected.
	imageInput, _ := resolveCaps(t, "text-embedding-3-large")
	if imageInput {
		t.Fatal("embedding model should not admit image input")
	}

	// Unknown model without catalog modality data: conservatively text-only,
	// matching other mature agent runtimes; user configuration can override.
	imageInput, fileInput := resolveCaps(t, "wuu-test-unknown-model")
	if imageInput || fileInput {
		t.Fatalf("unknown model should be text-only, got image=%v file=%v", imageInput, fileInput)
	}
}

func TestResolveAliasProducesCompleteSelection(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-4.1",
			},
			"anthropic": {
				Type:    "anthropic",
				BaseURL: "https://api.anthropic.com",
				Model:   "claude-sonnet-4-5",
			},
		},
		Agent: config.AgentConfig{
			ModelAliases: map[string]config.ModelRoleConfig{
				"frontend": {Provider: "anthropic", Model: "claude-sonnet-4-5", Effort: "high"},
			},
		},
	}

	selection, err := ResolveAlias(cfg, "frontend")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if selection.Role != RoleWorker {
		t.Fatalf("role = %q, want worker", selection.Role)
	}
	if selection.Inherited {
		t.Fatalf("alias selection should not be inherited: %+v", selection)
	}
	if selection.Provider != "anthropic" || selection.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected provider/model: %+v", selection)
	}
	if selection.Effort != "high" {
		t.Fatalf("effort = %q, want high", selection.Effort)
	}
	if selection.Behavior.Family != "claude" {
		t.Fatalf("expected claude behavior facts: %+v", selection.Behavior)
	}
	if selection.Capabilities.ContextWindow == 0 {
		t.Fatalf("expected non-zero context window: %+v", selection.Capabilities)
	}
}

func TestResolveAliasDoesNotInheritFromMain(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Type:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-4.1",
			},
		},
		Agent: config.AgentConfig{
			ModelAliases: map[string]config.ModelRoleConfig{
				" cheap ": {Provider: "openai", Model: "gpt-5-mini"},
			},
		},
	}

	selection, err := ResolveAlias(cfg, "cheap")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if selection.Provider != "openai" || selection.Model != "gpt-5-mini" {
		t.Fatalf("alias should not inherit main model: %+v", selection)
	}
}

func TestResolveAliasUnknownAlias(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		Providers: map[string]config.ProviderConfig{
			"openai": {Type: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4.1"},
		},
	}
	if _, err := ResolveAlias(cfg, "missing"); err == nil {
		t.Fatalf("expected error for unknown alias")
	}
}

func TestResolveAliasInvalidVariant(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "custom",
		Providers: map[string]config.ProviderConfig{
			"custom": {
				Type:    "openai-compatible",
				BaseURL: "https://example.test/v1",
				Model:   "main-model",
				Models: map[string]config.ProviderModelConfig{
					"worker-model": {
						Variants: map[string]map[string]any{
							"low":  {"reasoningEffort": "low"},
							"high": {"reasoningEffort": "high"},
						},
					},
				},
			},
		},
		Agent: config.AgentConfig{
			ModelAliases: map[string]config.ModelRoleConfig{
				"bad": {Provider: "custom", Model: "worker-model", Variant: "medium"},
			},
		},
	}
	if _, err := ResolveAlias(cfg, "bad"); err == nil {
		t.Fatalf("expected error for invalid variant")
	}
}

func TestResolveAliasColonContainingModelID(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "local",
		Providers: map[string]config.ProviderConfig{
			"local": {
				Type:    "openai-compatible",
				BaseURL: "http://127.0.0.1:11434/v1",
				Model:   "llama3.2",
			},
		},
		Agent: config.AgentConfig{
			ModelAliases: map[string]config.ModelRoleConfig{
				"ollama": {Provider: "local", Model: "llama3.2:latest"},
			},
		},
	}
	selection, err := ResolveAlias(cfg, "ollama")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if selection.Model != "llama3.2:latest" {
		t.Fatalf("colon-containing model ID lost: %q", selection.Model)
	}
	if selection.Role != RoleWorker {
		t.Fatalf("role = %q, want worker", selection.Role)
	}
}
