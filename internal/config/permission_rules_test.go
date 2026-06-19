package config

import (
	"encoding/json"
	"testing"
)

func TestPermissionRulesConfigUnmarshalShorthandAndPatterns(t *testing.T) {
	var cfg AgentConfig
	raw := []byte(`{
		"permission_rules": {
			"bash": {
				"git status *": "allow",
				"npm run *": {"action": "ask"}
			},
			"mcp_docs_*": "deny"
		}
	}`)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal permission rules: %v", err)
	}
	if cfg.PermissionRules["bash"]["git status *"] != PermissionRuleActionAllow {
		t.Fatalf("bash git status rule not parsed: %+v", cfg.PermissionRules)
	}
	if cfg.PermissionRules["bash"]["npm run *"] != PermissionRuleActionAsk {
		t.Fatalf("bash npm run rule not parsed: %+v", cfg.PermissionRules)
	}
	if cfg.PermissionRules["mcp_docs_*"]["*"] != PermissionRuleActionDeny {
		t.Fatalf("mcp shorthand rule not parsed: %+v", cfg.PermissionRules)
	}
}

func TestValidatePermissionRulesConfigRejectsInvalidAction(t *testing.T) {
	cfg := Config{
		DefaultProvider: "local",
		Providers: map[string]ProviderConfig{
			"local": {Model: "test"},
		},
		Agent: AgentConfig{
			PermissionRules: PermissionRulesConfig{
				"bash": {"git status *": "maybe"},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should reject invalid permission rule action")
	}
}
