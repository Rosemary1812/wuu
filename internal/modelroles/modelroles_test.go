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
